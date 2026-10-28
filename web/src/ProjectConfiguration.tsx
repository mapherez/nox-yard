import { useCallback, useEffect, useId, useRef, useState } from "react";
import { readProjectConfiguration, saveProjectConfiguration, revealContainerEnvironment, type ProjectConfiguration, type ConfigurationEdit } from "./api";
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
  const [editing, setEditingState] = useState(false);
  const deferred = useRef(false);
  const readVersion = useRef(0);
  const [retry, setRetry] = useState(0);
  const setEditing = useCallback((editor: string, active: boolean) => {
    if (active) editors.current.add(editor); else editors.current.delete(editor);
    setEditingState(editors.current.size > 0);
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
      setEditingState(false);
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
  return { configuration, valuesLoaded, busy, error, reload, save, setEditing, editing };
}
export type ConfigurationControl = ReturnType<typeof useProjectConfiguration>;

export function ComposeConfiguration({ control }: { control: ConfigurationControl }) {
  const editorID = useId();
  const [open, setOpen] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [message, setMessage] = useState("");
  const { configuration } = control;
  const hasDrafts = Object.keys(drafts).length > 0;
  useEffect(() => {
    control.setEditing("compose-files", hasDrafts);
    return () => control.setEditing("compose-files", false);
  }, [control.setEditing, hasDrafts]);
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
    {control.valuesLoaded ? configuration.files.map(file => <form key={file.path} onSubmit={event => { event.preventDefault(); void save(file.path); }}>
      <label htmlFor={`${editorID}-${file.path}`}>{file.path}</label>
      <textarea id={`${editorID}-${file.path}`} name="content" value={drafts[file.path] ?? file.content} rows={file.compose ? 16 : 7} spellCheck={false} autoCapitalize="off" autoCorrect="off" onChange={event => { setDrafts(current => ({ ...current, [file.path]: event.target.value })); setMessage(""); }} />
      <button type="submit" disabled={control.busy || drafts[file.path] === undefined || drafts[file.path] === file.content}>Save</button>
    </form>) : <p>Loading file contents…</p>}
    <p role="status">{message}</p>{control.error && <p role="alert">{control.error} <button type="button" disabled={control.busy} onClick={() => { void control.reload(true).catch(() => {}); }}>Reload files</button></p>}
  </details>;
}

export function EnvironmentValue({ name, value, revealed, service, containerID, csrfToken, control }: { name: string; value?: string; revealed: boolean; service: string; containerID: string; csrfToken: string; control?: ConfigurationControl }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const trackEditing = control?.setEditing;
  useEffect(() => {
    const editor = `env:${service}:${name}`;
    trackEditing?.(editor, editing);
    return () => trackEditing?.(editor, false);
  }, [trackEditing, service, name, editing]);
  const saved = control?.configuration?.variables.find(variable => variable.service === service && variable.name === name);
  async function edit() {
    if (!control) return;
    setLoading(true); setError("");
    try {
      const configuration = control.valuesLoaded ? control.configuration : await control.reload(true);
      const stored = configuration?.variables.find(variable => variable.service === service && variable.name === name);
      let current = stored?.value ?? value;
      if (current === undefined) current = (await revealContainerEnvironment(containerID, csrfToken)).environment.find(variable => variable.name === name)?.value;
      setDraft(current ?? ""); setEditing(true);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to edit variable."); }
    finally { setLoading(false); }
  }
  async function save() { if (!control) return; setError(""); try { await control.save({ service, variable: name, content: draft }); setEditing(false); setDraft(""); } catch(cause) { setError(cause instanceof Error ? cause.message : "Unable to save variable."); } }
  return <><div className={styles.value}>
    {editing ? <form onSubmit={event => { event.preventDefault(); void save(); }}><input aria-label={`Value for ${name}`} type={revealed ? "text" : "password"} value={draft} autoFocus autoComplete="off" onChange={event => setDraft(event.target.value)} /><button type="submit" aria-label={`Save ${name}`} title="Save" disabled={control?.busy}><i className="ph-bold ph-check" aria-hidden="true" /></button><button type="button" aria-label={`Cancel editing ${name}`} title="Cancel" onClick={() => { setEditing(false); setDraft(""); }}><i className="ph-bold ph-x" aria-hidden="true" /></button></form>
      : <><code>{revealed ? (control?.valuesLoaded && saved ? saved.value : value) || "(empty)" : "••••••"}</code>{control?.configuration?.available && <button type="button" aria-label={`Edit ${name}`} title="Edit" disabled={loading || control.busy} onClick={() => { void edit(); }}><i className="ph-bold ph-pencil-simple" aria-hidden="true" /></button>}</>}
  </div>{saved?.pending && <small className={styles.pending}>Saved · Recreate required</small>}{error && <span role="alert">{error}</span>}</>;
}
