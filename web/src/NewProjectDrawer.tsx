import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { deployManaged, getManagedJob, loadManagedSource, previewManaged, type ManagedJob, type ManagedPreview, type ManagedRequest, type ManagedSource, type ManagedSourceInput } from "./api";
import { useDrawerSwipe } from "./useDrawerSwipe";
import appStyles from "./App.module.css";
import styles from "./NewProjectDrawer.module.css";

type EnvRow = { id: string; key: string; value: string };
let nextEnvRowId = 0;

export function NewProjectDrawer({ open, csrfToken, onClose, onChanged }: { open: boolean; csrfToken: string; onClose: () => void; onChanged: () => void }) {
  const drawer = useRef<HTMLDialogElement>(null);
  const modal = useRef<HTMLDialogElement>(null);
  const dismiss = useCallback(() => drawer.current?.close(), []);
  useDrawerSwipe(drawer, "right", open, dismiss);
  const [kind, setKind] = useState<ManagedSourceInput["kind"]>("url");
  const [url, setURL] = useState("");
  const [yaml, setYAML] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [input, setInput] = useState<ManagedSourceInput | null>(null);
  const [source, setSource] = useState<ManagedSource | null>(null);
  const [name, setName] = useState("");
  const [variables, setVariables] = useState<Record<string, string>>({});
  const [envRows, setEnvRows] = useState<Record<string, EnvRow[]>>({});
  const [envUploads, setEnvUploads] = useState<Record<string, { filename: string; content: string }>>({});
  const [showValues, setShowValues] = useState(false);
  const [mode, setMode] = useState<ManagedRequest["mode"]>("new");
  const [preview, setPreview] = useState<ManagedPreview | null>(null);
  const [job, setJob] = useState<ManagedJob | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => { if (open && !drawer.current?.open) drawer.current?.showModal(); if (!open && drawer.current?.open) drawer.current.close(); }, [open]);
  useEffect(() => { if (preview && !modal.current?.open) modal.current?.showModal(); if (!preview && modal.current?.open) modal.current.close(); }, [preview]);
  useEffect(() => {
    if (job?.status !== "running") return;
    let live = true;
    const timer = window.setInterval(() => { void getManagedJob(job.id).then((next) => {
      if (live) { setJob(next); if (next.status !== "running") onChanged(); }
    }).catch((cause: unknown) => { if (live) setError(cause instanceof Error ? cause.message : "Unable to read operation status."); }); }, 1500);
    return () => { live = false; window.clearInterval(timer); };
  }, [job, onChanged]);

  async function readSource(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError(""); setSource(null); setInput(null); setJob(null);
    try {
      let next: ManagedSourceInput;
      if (kind === "url") next = { kind, url: url.trim() };
      else if (kind === "paste") next = { kind, yaml };
      else {
        if (!file) throw new Error("Choose a Compose file.");
        if (file.size > 1024 * 1024) throw new Error("Compose file exceeds 1 MiB.");
        next = { kind, filename: file.name, yaml: new TextDecoder("utf-8", { fatal: true }).decode(await file.arrayBuffer()) };
      }
      const loaded = await loadManagedSource(next, csrfToken);
      setSource(loaded); setInput(next); setName(loaded.suggestedName || ""); setVariables({}); setEnvRows({}); setEnvUploads({}); setShowValues(false); setMode("new");
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to read source."); }
    finally { setBusy(false); }
  }

  function providedEnvFiles(): Record<string, string> {
    const result: Record<string, string> = {};
    for (const file of source?.envFiles || []) {
      if (envUploads[file.path]) { result[file.path] = envUploads[file.path].content; continue; }
      const rows = envRows[file.path] || [];
      if (!rows.length) continue;
      const used = new Set<string>();
      result[file.path] = rows.map((row) => {
        const key = row.key.trim();
        if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) throw new Error(`Enter a valid variable name for ${file.path}.`);
        if (used.has(key)) throw new Error(`Duplicate variable ${key} in ${file.path}.`);
        used.add(key);
        if (/[\r\n\0]/.test(row.value)) throw new Error(`Variable ${key} must be on one line.`);
        return `${key}='${row.value.replaceAll("'", "\\'")}'`;
      }).join("\n") + "\n";
    }
    return result;
  }

  function providedVariables(): Record<string, string> {
    return Object.fromEntries(Object.entries(variables).filter(([, value]) => value !== ""));
  }

  function updateRow(path: string, id: string, field: "key" | "value", value: string) {
    setEnvRows((current) => ({ ...current, [path]: (current[path] || []).map((row) => row.id === id ? { ...row, [field]: value } : row) }));
  }

  function rootEnvDefines(key: string): boolean {
    if (envRows[".env"]?.some((row) => row.key.trim() === key)) return true;
    return envUploads[".env"]?.content.split(/\r?\n/).some((line) => {
      const match = line.trim().match(/^([A-Za-z_][A-Za-z0-9_]*)\s*[=:]/);
      return match?.[1] === key;
    }) || false;
  }

  async function uploadEnv(path: string, file: File | null) {
    if (!file) return;
    setError("");
    try {
      if (file.size > 1024 * 1024) throw new Error("Environment file exceeds 1 MiB.");
      const content = new TextDecoder("utf-8", { fatal: true }).decode(await file.arrayBuffer());
      setEnvUploads((current) => ({ ...current, [path]: { filename: file.name, content } }));
      setEnvRows((current) => ({ ...current, [path]: [] }));
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to read environment file."); }
  }

  async function validate(nextMode: ManagedRequest["mode"] = mode) {
    if (!input) return;
    setBusy(true); setError("");
    try { setPreview(await previewManaged({ name: name.trim(), source: input, variables: providedVariables(), envFiles: providedEnvFiles(), mode: nextMode }, csrfToken)); setMode(nextMode); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to validate project."); }
    finally { setBusy(false); }
  }

  async function confirm() {
    if (!input || !preview) return;
    setBusy(true); setError("");
    try { setJob(await deployManaged({ name: name.trim(), source: input, variables: providedVariables(), envFiles: providedEnvFiles(), mode, fingerprint: preview.fingerprint }, csrfToken)); setPreview(null); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to deploy project."); setPreview(null); }
    finally { setBusy(false); }
  }

  const conflict = !!preview && (mode === "new" && (preview.duplicates.length > 0 || preview.externalMatch) || mode === "copy" && (preview.duplicates.length === 0 || preview.externalMatch || preview.duplicates.includes(name.trim())) || mode === "sync" && !preview.duplicates.includes(name.trim()) || mode === "adopt" && !preview.externalMatch);

  return <><dialog ref={drawer} id="new-project-drawer" className={`${appStyles.settingsDrawer} ${appStyles.projectDrawer}`} aria-labelledby="new-project-title" onClose={onClose} onClick={(event) => { if (event.target === drawer.current) drawer.current.close(); }}>
    <div className={appStyles.settingsBody}>
      <header className={appStyles.settingsHeader}><div><span className={appStyles.sectionLabel}>MANAGED COMPOSE</span><h2 id="new-project-title">New Project</h2></div><button type="button" className={appStyles.closeButton} autoFocus onClick={() => drawer.current?.close()} aria-label="Close New Project">×</button></header>
      <div className={`${appStyles.settingsContent} ${styles.content}`}>
        <p className={styles.intro}>Choose a Compose source, enter its variables, then review the project before deployment.</p>
        <form className={styles.form} onSubmit={(event) => { void readSource(event); }}>
          <fieldset className={styles.sourceChoices} disabled={busy}><legend>Source</legend>
            <label><input type="radio" name="kind" checked={kind === "url"} onChange={() => { setKind("url"); setSource(null); }} /> HTTPS URL</label>
            <label><input type="radio" name="kind" checked={kind === "paste"} onChange={() => { setKind("paste"); setSource(null); }} /> Paste YAML</label>
            <label><input type="radio" name="kind" checked={kind === "upload"} onChange={() => { setKind("upload"); setSource(null); }} /> Upload file</label>
          </fieldset>
          {kind === "url" && <div className={styles.field}><label htmlFor="managed-url">Public HTTPS URL</label><input id="managed-url" type="url" value={url} required onChange={(event) => { setURL(event.target.value); setSource(null); }} /></div>}
          {kind === "paste" && <div className={styles.field}><label htmlFor="managed-yaml">Compose YAML</label><textarea id="managed-yaml" rows={12} value={yaml} required spellCheck={false} onChange={(event) => { setYAML(event.target.value); setSource(null); }} /></div>}
          {kind === "upload" && <div className={styles.field}><label htmlFor="managed-file">Compose file</label><input id="managed-file" type="file" accept=".yaml,.yml,text/yaml" required onChange={(event) => { setFile(event.target.files?.[0] || null); setSource(null); }} /></div>}
          <button className={appStyles.primaryButton} type="submit" disabled={busy}>Read source</button>
        </form>
        {source && !job && <form className={styles.form} onSubmit={(event) => { event.preventDefault(); void validate(); }}>
          <div className={styles.loaded}><strong>Source loaded</strong><span>{source.filename || source.url || "Pasted YAML"}</span></div>
          <div className={styles.field}><label htmlFor="managed-name">Project name</label><input id="managed-name" value={name} required pattern="[a-z0-9][a-z0-9_-]{0,62}" onChange={(event) => setName(event.target.value)} /></div>
          {(source.variables.length > 0 || source.envFiles.length > 0) && <label className={styles.visibilityToggle}><input type="checkbox" checked={showValues} onChange={(event) => setShowValues(event.target.checked)} /> Show environment values</label>}
          {source.variables.length > 0 && <fieldset className={styles.envSection}><legend>Compose interpolation variables</legend>
            {source.variables.map((variable) => <div className={styles.field} key={variable.name}><label htmlFor={`managed-var-${variable.name}`}>{variable.name}{variable.required && !rootEnvDefines(variable.name) ? " *" : ""}</label><input id={`managed-var-${variable.name}`} type={showValues ? "text" : "password"} value={variables[variable.name] ?? ""} required={variable.required && !rootEnvDefines(variable.name)} placeholder={variable.default} autoComplete="off" onChange={(event) => setVariables((current) => ({ ...current, [variable.name]: event.target.value }))} /></div>)}
          </fieldset>}
          {source.envFiles.map((envFile, index) => <fieldset className={styles.envSection} key={envFile.path}>
            <legend>Environment file: <code>{envFile.path}</code>{envFile.required ? " *" : " (optional)"}</legend>
            <p className={styles.hint}>Load this file from your computer or enter its variables below. Values are hidden by default.</p>
            <div className={styles.field}><label htmlFor={`managed-env-file-${index}`}>Load variables from .env file</label><input id={`managed-env-file-${index}`} type="file" accept=".env,.txt,text/plain" onChange={(event) => { void uploadEnv(envFile.path, event.target.files?.[0] || null); }} /></div>
            {envUploads[envFile.path] && <div className={styles.uploaded}><span>{envUploads[envFile.path].filename} loaded · values hidden</span><button type="button" onClick={() => setEnvUploads((current) => { const next = { ...current }; delete next[envFile.path]; return next; })}>Remove file</button></div>}
            {!envUploads[envFile.path] && <>
              {(envRows[envFile.path] || []).map((row) => <div className={styles.envRow} key={row.id}>
                <div className={styles.field}><label htmlFor={`env-key-${row.id}`}>Variable name</label><input id={`env-key-${row.id}`} value={row.key} required pattern="[A-Za-z_][A-Za-z0-9_]*" autoComplete="off" onChange={(event) => updateRow(envFile.path, row.id, "key", event.target.value)} /></div>
                <div className={styles.field}><label htmlFor={`env-value-${row.id}`}>Value</label><input id={`env-value-${row.id}`} type={showValues ? "text" : "password"} value={row.value} autoComplete="off" onChange={(event) => updateRow(envFile.path, row.id, "value", event.target.value)} /></div>
                <button type="button" className={styles.rowRemove} onClick={() => setEnvRows((current) => ({ ...current, [envFile.path]: (current[envFile.path] || []).filter((item) => item.id !== row.id) }))} aria-label={`Remove ${row.key || "variable"} from ${envFile.path}`}>Remove</button>
              </div>)}
              <button type="button" className={styles.addVariable} onClick={() => setEnvRows((current) => ({ ...current, [envFile.path]: [...(current[envFile.path] || []), { id: String(++nextEnvRowId), key: "", value: "" }] }))}>Add variable</button>
            </>}
          </fieldset>)}
          <button className={appStyles.primaryButton} type="submit" disabled={busy}>Validate and preview</button>
        </form>}
        {job && <div className={styles.loaded} role="status"><strong>{job.status === "running" ? "Operation in progress" : job.status === "succeeded" ? "Project ready" : "Operation failed"}</strong><span>{job.operation} · {job.projectName}</span>{job.error && <p>{job.error}</p>}</div>}
        {error && <p className={appStyles.formError} role="alert">{error}</p>}
      </div>
    </div>
  </dialog>
  <dialog ref={modal} className={styles.previewDialog} aria-labelledby="managed-preview-title" onClose={() => setPreview(null)} onCancel={(event) => { if (busy) event.preventDefault(); }}>
    {preview && <div className={styles.previewBody}>
      <h2 id="managed-preview-title">Review {preview.name}</h2>
      <p>Docker Compose validated this project. Confirm to pull its images and deploy it.</p>
      {(preview.duplicates.length > 0 || preview.externalMatch) && <fieldset className={styles.sourceChoices} disabled={busy}><legend>Existing project</legend>
        {preview.duplicates.length > 0 && <p>Source URL already used by: {preview.duplicates.join(", ")}.</p>}
        {preview.externalMatch && <p>An external Compose project already uses this name.</p>}
        {preview.duplicates.length > 0 && !preview.duplicates.includes(name.trim()) && <label><input type="radio" name="mode" checked={mode === "copy"} onChange={() => { void validate("copy"); }} /> Create a separate copy</label>}
        {preview.duplicates.includes(name.trim()) && <label><input type="radio" name="mode" checked={mode === "sync"} onChange={() => { void validate("sync"); }} /> Sync existing managed project</label>}
        {preview.externalMatch && <label><input type="radio" name="mode" checked={mode === "adopt"} onChange={() => { void validate("adopt"); }} /> Adopt existing external project</label>}
      </fieldset>}
      {preview.changes.length > 0 && <section><h3>Changes</h3><ul>{preview.changes.map((change) => <li key={change}>{change}</li>)}</ul></section>}
      <section><h3>Services and images</h3>{preview.services.map((service) => <div className={styles.service} key={service.name}><strong>{service.name}</strong><code>{service.image}</code>{service.ports.length > 0 && <p>Ports: {service.ports.join(", ")}</p>}{service.volumes.length > 0 && <p>Mounts: {service.volumes.join(", ")}</p>}{service.networks.length > 0 && <p>Networks: {service.networks.join(", ")}</p>}</div>)}</section>
      <p>Volumes: {preview.volumes.join(", ") || "none"}</p><p>Networks: {preview.networks.join(", ") || "default"}</p>
      {preview.envFiles.length > 0 && <p>Environment files: {preview.envFiles.join(", ")} (values hidden)</p>}
      {error && <p className={appStyles.formError} role="alert">{error}</p>}
      <div className={styles.previewActions}><button type="button" onClick={() => setPreview(null)} disabled={busy}>Cancel</button><button type="button" className={appStyles.primaryButton} disabled={busy || conflict} onClick={() => { void confirm(); }}>{mode === "adopt" ? "Confirm adoption" : "Confirm deployment"}</button></div>
    </div>}
  </dialog></>;
}
