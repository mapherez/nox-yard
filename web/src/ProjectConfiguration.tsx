import { useCallback, useEffect, useId, useRef, useState } from "react";
import { readProjectConfiguration, saveProjectConfiguration, revealContainerEnvironment, type ProjectConfiguration, type ConfigurationEdit } from "./api";
import { OverrideDrawer } from "./OverrideDrawer";
import styles from "./ProjectConfiguration.module.css";

export function useProjectConfiguration(target: string | undefined, csrfToken: string, revision?: string) {
  const currentTarget = useRef(target); currentTarget.current = target;
  const [configuration, setConfiguration] = useState<ProjectConfiguration | null>(null);
  const [valuesLoaded, setValuesLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const loadedTarget = useRef<{ target: string | undefined; csrfToken: string } | null>(null);
  const revealed = useRef(false);
  const editors = useRef(new Set<string>());
  const dirtyEditors = useRef(new Set<string>());
  const [hasUnsavedChanges, setHasUnsavedChanges] = useState(false);
  const deferred = useRef(false);
  const readVersion = useRef(0);
  const [retry, setRetry] = useState(0);
  const setEditing = useCallback((editor: string, active: boolean, dirty = false) => {
    if (active) editors.current.add(editor); else editors.current.delete(editor);
    if (active && dirty) dirtyEditors.current.add(editor); else dirtyEditors.current.delete(editor);
    setHasUnsavedChanges(dirtyEditors.current.size > 0);
    if (!editors.current.size && deferred.current) {
      deferred.current = false;
      setRetry(value => value + 1);
    }
  }, []);
  const reload = useCallback(async (reveal = false) => {
    if (!target?.startsWith("compose:")) return null;
    const version = ++readVersion.current;
    try { const next = await readProjectConfiguration(target, csrfToken, reveal);
      if(currentTarget.current !== target || version !== readVersion.current) return null;
      revealed.current = reveal;
      setConfiguration(next); setValuesLoaded(reveal); setError(""); return next;
    } catch(cause) { if(currentTarget.current === target && version === readVersion.current) setError(cause instanceof Error ? cause.message : "Unable to read configuration."); throw cause; }
  }, [target, csrfToken]);
  useEffect(() => {
    let active = true;
    const changed = loadedTarget.current?.target !== target || loadedTarget.current?.csrfToken !== csrfToken;
    loadedTarget.current = { target, csrfToken };
    if (changed) {
      editors.current.clear(); deferred.current = false; revealed.current = false;
      dirtyEditors.current.clear(); setHasUnsavedChanges(false);
      setConfiguration(null); setValuesLoaded(false); setError("");
    }
    // Keep the editor's snapshot (including its conflict fingerprint) until
    // every draft has been saved or cancelled. Status updates still flow.
    if (editors.current.size) { deferred.current = true; return; }
    const version = ++readVersion.current;
    const reveal = revealed.current;
    if (target?.startsWith("compose:")) void readProjectConfiguration(target, csrfToken, reveal).then(value => {
      if (!active || version !== readVersion.current) return;
      if (editors.current.size) { deferred.current = true; return; }
      setConfiguration(value); setValuesLoaded(reveal); setError("");
    }).catch(cause => { if (active && version === readVersion.current) setError(cause instanceof Error ? cause.message : "Unable to read project configuration."); });
    return () => { active = false; };
  }, [target, csrfToken, revision, retry]);
  async function save(edit: Omit<ConfigurationEdit, "fingerprint">) {
    if (!target || !configuration) throw Error("Reload the project configuration.");
    setBusy(true); setError("");
    ++readVersion.current;
    try { const next = await saveProjectConfiguration(target, { ...edit, fingerprint: configuration.fingerprint }, csrfToken); if(currentTarget.current === target) {revealed.current = true; setConfiguration(next); setValuesLoaded(true);} return next; }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to save configuration."); throw cause; }
    finally { setBusy(false); }
  }
  return { target,csrfToken,configuration, valuesLoaded, busy, error, reload, save, setEditing, hasUnsavedChanges };
}
export type ConfigurationControl = ReturnType<typeof useProjectConfiguration>;

export function ComposeConfiguration({ control }: { control: ConfigurationControl }) {
  const editorID = useId();
  const [open, setOpen] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [message, setMessage] = useState("");
 const [interpolationDraft,setInterpolationDraft]=useState<string|null>(null);
 const [override,setOverride]=useState<{removePath?:string}|null>(null);
  const { configuration } = control;
  const hasDrafts = Object.entries(drafts).some(([path, content]) => content !== configuration?.files.find(file => file.path === path)?.content);
  useEffect(() => {
    control.setEditing("compose-files", hasDrafts, hasDrafts);
    return () => control.setEditing("compose-files", false);
  }, [control.setEditing, hasDrafts]);
  useEffect(()=>()=>control.setEditing("interpolation-files",false),[control.setEditing]);
  useEffect(()=>{control.setEditing("override",override!==null,override!==null);return()=>control.setEditing("override",false);},[control.setEditing,override]);
  async function reveal() { try { await control.reload(true); } catch { /* error is rendered by the caller */ } }
  async function save(path: string) {
    try { await control.save({ path, content: drafts[path] ?? "" }); setDrafts(current => { const next = { ...current }; delete next[path]; return next; }); setMessage("Saved. Use Recreate to apply the changes."); } catch { /* keep the draft */ }
  }
  if (control.error && !configuration) return <p role="alert">{control.error} <button type="button" onClick={() => { void control.reload(); }}>Retry configuration</button></p>;
  if (!configuration) return null;
  if (!configuration.available) return null;
  return <details className={styles.panel} open={open} onToggle={event => { const next = event.currentTarget.open; setOpen(next); if (next && !control.valuesLoaded) void reveal(); }}>
    <summary><i className="ph-bold ph-caret-right" aria-hidden="true" />Compose and environment files</summary>
    <p><code>{configuration.directory}</code></p>
    {configuration.error && <p role="alert">{configuration.error}</p>}
    {control.valuesLoaded && (configuration.availableProfiles?.length ?? 0)>0 && <fieldset><legend>Active profiles</legend>{configuration.availableProfiles?.map(profile=><label key={profile}><input type="checkbox" checked={configuration.profiles?.includes(profile)??false} disabled={control.busy||hasDrafts} onChange={event=>{const next=new Set(configuration.profiles);if(event.target.checked)next.add(profile);else next.delete(profile);void control.save({profiles:[...next],content:""}).then(()=>setMessage("Saved · Recreate required")).catch(()=>{});}} />{profile}</label>)}</fieldset>}
    {control.valuesLoaded && <form onSubmit={event=>{event.preventDefault();if(interpolationDraft!==null)void control.save({interpolationFiles:interpolationDraft.split(",").map(value=>value.trim()).filter(Boolean),content:""}).then(()=>{setInterpolationDraft(null);control.setEditing("interpolation-files",false);setMessage("Saved · Recreate required");}).catch(()=>{});}}><label htmlFor={`${editorID}-interpolation`}>Interpolation files (in order, separated by commas)</label><input id={`${editorID}-interpolation`} value={interpolationDraft??configuration.interpolationFiles?.join(", ")??".env"} onChange={event=>{setInterpolationDraft(event.target.value);control.setEditing("interpolation-files",true,true);}} /><small>Use .env for the default project environment. An empty list disables implicit .env loading.</small><button type="submit" disabled={control.busy||hasDrafts||interpolationDraft===null}>Save environment selection</button>{interpolationDraft!==null&&<button type="button" disabled={control.busy} onClick={()=>{setInterpolationDraft(null);control.setEditing("interpolation-files",false);}}>Cancel environment selection</button>}</form>}
    {configuration.composeFiles && <p>Compose order: {configuration.composeFiles.join(" → ")}</p>}
    {control.valuesLoaded ? configuration.files.map(file => <form key={file.path} onSubmit={event => { event.preventDefault(); void save(file.path); }}>
      <label htmlFor={`${editorID}-${file.path}`}>{file.path}</label>{file.missing&&<p role="alert">This selected file is missing. Paste its contents to restore it, or remove it from the selection.</p>}
      <textarea id={`${editorID}-${file.path}`} name="content" value={drafts[file.path] ?? file.content} rows={file.compose ? 16 : 7} spellCheck={false} autoCapitalize="off" autoCorrect="off" onChange={event => {
        const next = { ...drafts };
        if (event.target.value === file.content) delete next[file.path]; else next[file.path] = event.target.value;
        const dirty = Object.entries(next).some(([path, content]) => content !== configuration.files.find(item => item.path === path)?.content);
        control.setEditing("compose-files", dirty, dirty);
        setDrafts(next); setMessage("");
      }} />
      <button type="submit" disabled={control.busy || drafts[file.path] === undefined || drafts[file.path] === file.content}>Save</button>
 {configuration.composeFiles?.slice(1).includes(file.path) && <button type="button" disabled={control.busy||hasDrafts} onClick={()=>setOverride({removePath:file.path})}>Remove from selection</button>}
    </form>) : <p>Loading file contents…</p>}
    <button type="button" disabled={control.busy||hasDrafts||interpolationDraft!==null||!control.valuesLoaded} onClick={()=>setOverride({})}><i className="ph-bold ph-plus" aria-hidden="true" /> Add override</button>
    {hasDrafts && <p>Save or cancel file edits before adding an override.</p>}
    {override&&control.target&&<OverrideDrawer key={control.target} projectID={control.target} csrfToken={control.csrfToken} configuration={configuration} removePath={override.removePath} onClose={()=>setOverride(null)} onSaved={async()=>{await control.reload(true);setMessage("Saved · Recreate required");}} />}
    <p role="status">{message}</p>{control.error && <p role="alert">{control.error} <button type="button" disabled={control.busy} onClick={() => { void control.reload(true).catch(() => {}); }}>Reload files</button></p>}
  </details>;
}

export function EnvironmentValue({ name, value, revealed, service, containerID, csrfToken, control }: { name: string; value?: string; revealed: boolean; service: string; containerID: string; csrfToken: string; control?: ConfigurationControl }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [initialDraft, setInitialDraft] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const trackEditing = control?.setEditing;
  useEffect(() => {
    const editor = `env:${service}:${name}`;
    trackEditing?.(editor, editing, editing && draft !== initialDraft);
  }, [trackEditing, service, name, editing, draft, initialDraft]);
  useEffect(() => () => { trackEditing?.(`env:${service}:${name}`, false); }, [trackEditing, service, name]);
  const saved = control?.configuration?.variables.find(variable => variable.service === service && variable.name === name);
  async function edit() {
    if (!control) return;
    setLoading(true); setError("");
    try {
      const configuration = control.valuesLoaded ? control.configuration : await control.reload(true);
      const stored = configuration?.variables.find(variable => variable.service === service && variable.name === name);
      let current = stored?.value ?? value;
      if (current === undefined) current = (await revealContainerEnvironment(containerID, csrfToken)).environment.find(variable => variable.name === name)?.value;
      setDraft(current ?? ""); setInitialDraft(current ?? ""); setEditing(true);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to edit variable."); }
    finally { setLoading(false); }
  }
  async function save() { if (!control) return; setError(""); try { await control.save({ service, variable: name, content: draft }); setEditing(false); setDraft(""); } catch(cause) { setError(cause instanceof Error ? cause.message : "Unable to save variable."); } }
  return <><div className={styles.value}>
    {editing ? <form onSubmit={event => { event.preventDefault(); void save(); }}><input aria-label={`Value for ${name}`} type={revealed ? "text" : "password"} value={draft} autoFocus autoComplete="off" onChange={event => { trackEditing?.(`env:${service}:${name}`, true, event.target.value !== initialDraft); setDraft(event.target.value); }} /><button type="submit" aria-label={`Save ${name}`} title="Save" disabled={control?.busy}><i className="ph-bold ph-check" aria-hidden="true" /></button><button type="button" aria-label={`Cancel editing ${name}`} title="Cancel" onClick={() => { setEditing(false); setDraft(""); }}><i className="ph-bold ph-x" aria-hidden="true" /></button></form>
      : <><code>{revealed ? (control?.valuesLoaded && saved ? saved.value : value) || "(empty)" : "••••••"}</code>{control?.configuration?.available && <button type="button" aria-label={`Edit ${name}`} title="Edit" disabled={loading || control.busy} onClick={() => { void edit(); }}><i className="ph-bold ph-pencil-simple" aria-hidden="true" /></button>}</>}
  </div>{saved?.pending && <small className={styles.pending}>Saved · Recreate required</small>}{error && <span role="alert">{error}</span>}</>;
}
