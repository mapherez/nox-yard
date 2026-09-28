import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { loadManagedSource, type ManagedSource, type ManagedSourceInput } from "./api";
import { useDrawerSwipe } from "./useDrawerSwipe";
import appStyles from "./App.module.css";
import styles from "./NewProjectDrawer.module.css";

const maxSourceBytes = 1024 * 1024;
type SourceKind = ManagedSourceInput["kind"];

export function NewProjectDrawer({ open, csrfToken, onClose }: {
  open: boolean;
  csrfToken: string;
  onClose: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const dismiss = useCallback(() => dialogRef.current?.close(), []);
  useDrawerSwipe(dialogRef, "right", open, dismiss);
  const [kind, setKind] = useState<SourceKind>("url");
  const [url, setURL] = useState("");
  const [yaml, setYAML] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [loaded, setLoaded] = useState<ManagedSource | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");
    setLoaded(null);
    try {
      let input: ManagedSourceInput;
      if (kind === "url") {
        input = { kind, url: url.trim() };
      } else if (kind === "paste") {
        input = { kind, yaml };
      } else {
        if (!file) throw new Error("Choose a Compose file.");
        if (file.size > maxSourceBytes) throw new Error("Compose file exceeds 1 MiB.");
        const contents = new TextDecoder("utf-8", { fatal: true }).decode(await file.arrayBuffer());
        input = { kind, filename: file.name, yaml: contents };
      }
      setLoaded(await loadManagedSource(input, csrfToken));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to read Compose source.");
    } finally {
      setBusy(false);
    }
  }

  function changeKind(next: SourceKind) {
    setKind(next);
    setLoaded(null);
    setError("");
  }

  return <dialog
    id="new-project-drawer"
    ref={dialogRef}
    className={`${appStyles.settingsDrawer} ${appStyles.projectDrawer}`}
    aria-labelledby="new-project-title"
    onClose={onClose}
    onClick={(event) => { if (event.target === dialogRef.current) dismiss(); }}
    {...{ closedby: "any" }}
  >
    <div className={appStyles.settingsBody}>
      <header className={appStyles.settingsHeader}>
        <div>
          <span className={appStyles.sectionLabel}>MANAGED COMPOSE</span>
          <h2 id="new-project-title">New Project</h2>
        </div>
        <button type="button" className={appStyles.closeButton} autoFocus onClick={dismiss} aria-label="Close New Project">×</button>
      </header>
      <div className={`${appStyles.settingsContent} ${styles.content}`}>
        <p className={styles.intro}>Choose a Compose source. This first step reads the file; it does not deploy containers.</p>
        <form action="/api/managed/source" method="post" className={styles.form} onSubmit={(event) => { void submit(event); }}>
          <fieldset className={styles.sourceChoices} disabled={busy}>
            <legend>Source</legend>
            <label><input type="radio" name="kind" value="url" checked={kind === "url"} onChange={() => changeKind("url")} /> HTTPS URL</label>
            <label><input type="radio" name="kind" value="paste" checked={kind === "paste"} onChange={() => changeKind("paste")} /> Paste YAML</label>
            <label><input type="radio" name="kind" value="upload" checked={kind === "upload"} onChange={() => changeKind("upload")} /> Upload file</label>
          </fieldset>
          {kind === "url" && <div className={styles.field}>
            <label htmlFor="new-project-url">Public HTTPS URL</label>
            <input id="new-project-url" name="url" type="url" inputMode="url" placeholder="https://example.com/compose.yaml" value={url} required disabled={busy} onChange={(event) => { setURL(event.target.value); setLoaded(null); }} />
            <span className={styles.hint}>Direct public URL on port 443. Redirects are not followed.</span>
          </div>}
          {kind === "paste" && <div className={styles.field}>
            <label htmlFor="new-project-yaml">Compose YAML</label>
            <textarea id="new-project-yaml" name="yaml" value={yaml} required disabled={busy} rows={12} spellCheck={false} onChange={(event) => { setYAML(event.target.value); setLoaded(null); }} />
          </div>}
          {kind === "upload" && <div className={styles.field}>
            <label htmlFor="new-project-file">Compose file</label>
            <input id="new-project-file" name="file" type="file" accept=".yaml,.yml,text/yaml,application/x-yaml" required disabled={busy} onChange={(event) => { setFile(event.target.files?.[0] || null); setLoaded(null); }} />
            <span className={styles.hint}>YAML file up to 1 MiB.</span>
          </div>}
          <button type="submit" className={appStyles.primaryButton} disabled={busy}>{busy ? "Reading source…" : "Read source"}</button>
        </form>
        {error && <p className={appStyles.formError} role="alert">{error}</p>}
        {loaded && <div className={styles.loaded} role="status">
          <strong>Source loaded</strong>
          <span>{loaded.filename || loaded.url || "Pasted YAML"} · {new TextEncoder().encode(loaded.yaml).length.toLocaleString()} bytes</span>
          <p>Compose validation, preview, and deployment are the next steps in this flow.</p>
        </div>}
      </div>
    </div>
  </dialog>;
}
