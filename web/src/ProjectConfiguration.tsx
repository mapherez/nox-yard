import { useCallback, useEffect, useId, useRef, useState } from "react";
import { readProjectConfiguration, saveProjectConfiguration, retryConfigurationMigration, revealContainerEnvironment, getConfigurationMigration, type ConfigurationMigration, type ProjectConfiguration, type ConfigurationEdit } from "./api";
import styles from "./ProjectConfiguration.module.css";

export function useProjectConfiguration(target: string | undefined, csrfToken: string, revision?: string) {
  const currentTarget = useRef(target); currentTarget.current = target;
  const [configuration, setConfiguration] = useState<ProjectConfiguration | null>(null);
  const [valuesLoaded, setValuesLoaded] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const reload = useCallback(async (reveal = false) => {
    if (!target?.startsWith("compose:")) return null;
    try { const next = await readProjectConfiguration(target, csrfToken, reveal);
      if(currentTarget.current !== target) return null;
      setConfiguration(next); setValuesLoaded(reveal); setError(""); return next;
    } catch(cause) { if(currentTarget.current === target) setError(cause instanceof Error ? cause.message : "Unable to read configuration."); throw cause; }
  }, [target, csrfToken]);
  useEffect(() => {
    let active = true;
    setConfiguration(null); setValuesLoaded(false); setError("");
    if (target?.startsWith("compose:")) void readProjectConfiguration(target, csrfToken).then(value => { if (active) setConfiguration(value); }).catch(cause => { if (active) setError(cause instanceof Error ? cause.message : "Unable to read project configuration."); });
    return () => { active = false; };
  }, [target, csrfToken, revision]);
  useEffect(() => {
    if (!configuration || configuration.state === "ready") return;
    const timer = window.setInterval(() => { void reload().catch(cause => setError(cause instanceof Error ? cause.message : "Unable to read migration status.")); }, 3000);
    return () => window.clearInterval(timer);
  }, [configuration?.state, reload]);
  async function save(edit: Omit<ConfigurationEdit, "fingerprint">) {
    if (!target || !configuration) throw Error("Reload the project configuration.");
    setBusy(true); setError("");
    try { const next = await saveProjectConfiguration(target, { ...edit, fingerprint: configuration.fingerprint }, csrfToken); if(currentTarget.current === target) {setConfiguration(next); setValuesLoaded(true);} return next; }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to save configuration."); throw cause; }
    finally { setBusy(false); }
  }
  async function retry(useCurrentFiles: boolean) {
    if (!target) return;
    setBusy(true); setError("");
    try { const next = await retryConfigurationMigration(target, useCurrentFiles, csrfToken); setConfiguration(next); setValuesLoaded(true); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Migration could not finish."); }
    finally { setBusy(false); }
  }
  return { configuration, valuesLoaded, busy, error, reload, save, retry, blocked: !!configuration && configuration.state !== "ready" };
}
export type ConfigurationControl = ReturnType<typeof useProjectConfiguration>;

export function ConfigurationMigrationStatus() {
  const [states, setStates] = useState<ConfigurationMigration[]>([]);
  const [error, setError] = useState("");
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    let active = true;
    async function refresh() { try { const next = await getConfigurationMigration(); if (active) { setStates(Array.isArray(next) ? next : []); setError(""); } } catch { if (active) setError("Unable to read configuration migration status."); } }
    void refresh(); const timer = window.setInterval(() => { void refresh(); }, 3000);
    return () => { active = false; window.clearInterval(timer); };
  }, []);
  const ready = states.filter(state => state.state === "ready").length;
  const pending = states.length - ready;
  useEffect(() => {
    if (!states.length || pending) { setDismissed(false); return; }
    const timer = window.setTimeout(() => setDismissed(true), 30_000);
    return () => window.clearTimeout(timer);
  }, [pending, states.length]);
  if (error) return <p role="alert">{error} Open a project to verify its file status before editing.</p>;
  if (!states.length || (dismissed && !pending)) return null;
  return <section className={styles.panel} aria-label="Configuration migration status" aria-live="polite">
    <strong>{pending ? `Configuration migration: ${ready}/${states.length} Ready` : "Configuration migration complete · files are Ready"}</strong>
    {states.filter(state => state.state !== "ready").map(state => <p key={state.name}>{state.name}: {state.state === "migrating" ? "Migrating" : "Needs attention"}{state.error ? ` · ${state.error}` : ""}. Open the project for details.</p>)}
  </section>;
}

export function ComposeConfiguration({ control }: { control: ConfigurationControl }) {
  const editorID = useId();
  const [open, setOpen] = useState(false);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [message, setMessage] = useState("");
  const { configuration } = control;
  async function reveal() { try { await control.reload(true); } catch { /* error is rendered by the caller */ } }
  async function save(path: string) {
    try { await control.save({ path, content: drafts[path] ?? "" }); setDrafts(current => { const next = { ...current }; delete next[path]; return next; }); setMessage("Saved. Use Recreate to apply the changes."); } catch { /* keep the draft */ }
  }
  if (control.error && !configuration) return <p role="alert">{control.error} <button type="button" onClick={() => { void control.reload(); }}>Retry configuration</button></p>;
  if (!configuration) return null;
  if (configuration.state !== "ready") return <section className={styles.panel} aria-label="Configuration migration">
    <strong>{configuration.state === "migrating" ? "Migrating configuration…" : "Configuration needs attention"}</strong>
    <p>{configuration.error || "File editing and Recreate become available when the project is Ready."}</p>
    {configuration.state === "needs_attention" && <><button type="button" disabled={control.busy} onClick={() => { void control.retry(false); }}>Retry migration</button><button type="button" disabled={control.busy} onClick={() => { void control.retry(true); }}>Use current files</button><p>Use current files preserves the existing host files and validates them before completing migration.</p></>}
    {control.error && <p role="alert">{control.error}</p>}
  </section>;
  if (!configuration.available) return null;
  return <details className={styles.panel} open={open} onToggle={event => { const next = event.currentTarget.open; setOpen(next); if (next && !control.valuesLoaded) void reveal(); }}>
    <summary>Compose and environment files</summary>
    <p><strong>Ready</strong> · <code>{configuration.directory}</code></p>
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
      : <><code>{revealed ? (control?.valuesLoaded && saved ? saved.value : value) || "(empty)" : "••••••"}</code>{control?.configuration?.available && <button type="button" aria-label={`Edit ${name}`} title="Edit" disabled={loading || control.busy || control.blocked} onClick={() => { void edit(); }}><i className="ph-bold ph-pencil-simple" aria-hidden="true" /></button>}</>}
  </div>{saved?.pending && <small className={styles.pending}>Saved · Recreate required</small>}{error && <span role="alert">{error}</span>}</>;
}
