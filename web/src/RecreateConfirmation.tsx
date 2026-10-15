import { useEffect, useId, useRef, useState } from "react";
import { previewRecreate, submitRecreate, type RecreatePreview } from "./api";
import styles from "./App.module.css";

export type RecreateTarget = { id: string; name: string; operation: "update" | "recreate" };

export function RecreateConfirmation({ target, csrfToken, onClose, onAccepted }: { target: RecreateTarget | null; csrfToken: string; onClose: () => void; onAccepted: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const invoker = useRef<HTMLElement | null>(null);
  const titleID = useId();
  const [preview, setPreview] = useState<RecreatePreview | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (target && !dialog.open) {
      const active = document.activeElement;
      invoker.current = active instanceof HTMLElement ? active.closest("details")?.querySelector("summary") ?? active : null;
      dialog.showModal();
    }
    if (!target) { if(dialog.open) dialog.close(); invoker.current?.focus(); }
  }, [target]);
  useEffect(() => {
    if (!target) return;
    let current = true;
    setPreview(null); setError(""); setLoading(true);
    void previewRecreate(target.id, target.operation, csrfToken)
      .then((result) => { if (current) setPreview(result); })
      .catch((cause: unknown) => { if (current) setError(cause instanceof Error ? cause.message : "Unable to assess this target."); })
      .finally(() => { if (current) setLoading(false); });
    return () => { current = false; };
  }, [target, csrfToken, revision]);
  async function submit() {
    if (!target || !preview || submitting) return;
    setSubmitting(true); setError("");
    try {
      await submitRecreate(target.id, target.operation, preview.fingerprint, csrfToken);
      onAccepted(); onClose();
    } catch (cause) {
      setPreview(null);
      setError(cause instanceof Error ? cause.message : "Unable to submit replacement.");
    } finally { setSubmitting(false); }
  }
  return <dialog ref={dialogRef} className={styles.confirmDialog} aria-labelledby={titleID} onClose={onClose} onCancel={(event) => { if (submitting) event.preventDefault(); }}>
    {target && <div className={styles.confirmBody}>
      <h2 id={titleID}>{target.operation === "update" ? "Update images?" : "Recreate containers?"}</h2>
      <p><strong>{target.name}</strong></p>
      <p>{target.operation === "update" ? "Pulls images and applies changed configuration." : "Reads the current Compose and environment files and recreates using local images. No images are pulled or built. When project files are unavailable, the existing container configuration is used."} Running/stopped states and persistent data are preserved.</p>
      <p>Failed replacements attempt to restore the previous container configuration and images. Named volumes and binds remain. Writable container layers and temporary files are not copied; rollback cannot undo application data writes or migrations. Automatically assigned network addresses may change.</p>
      {loading && <p role="status">Assessing configuration and dependencies…</p>}
      {error && <p className={styles.inventoryError} role="alert">{error}</p>}
      {preview && <>
        <p>Replacement order: {preview.order.join(" → ")}</p>
        <ul className={styles.removalList} aria-label="Containers to preserve">
          {preview.items.map((item) => <li key={item.id} className={styles.removalItem}>
            <strong>{item.name}</strong><code>{item.image}</code>
            <p>{item.running ? "Running" : "Stopped"} · preserves {item.preserved.join(", ")}. Environment values are hidden.</p>
          </li>)}
        </ul>
      </>}
      <div className={styles.confirmActions}>
        <button type="button" className={styles.inspectButton} autoFocus disabled={submitting} onClick={onClose}>Cancel</button>
        {error && <button type="button" className={styles.inspectButton} disabled={loading || submitting} onClick={() => setRevision((value) => value + 1)}>Review again</button>}
        {preview && <button type="button" className={styles.inspectButton} disabled={loading || submitting} onClick={() => { void submit(); }}>{submitting ? "Submitting…" : target.operation === "update" ? "Confirm update" : "Confirm recreate"}</button>}
      </div>
    </div>}
  </dialog>;
}
