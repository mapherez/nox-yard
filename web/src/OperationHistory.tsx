import { useId, useRef, useState } from "react";
import { acknowledgeJobRecovery, type ManagedJob } from "./api";
import styles from "./App.module.css";

export function operationStage(job: ManagedJob) {
  if (job.outcome === "recovery_required") return "Recovery required";
  if (job.outcome === "recovery_acknowledged") return "Recovery acknowledged";
  if (job.outcome === "skipped") return "Automatic check skipped";
  if (job.outcome === "unchanged") return "No image changes · containers preserved";
  if (job.outcome === "cached") return "Images pulled · containers preserved";
  if (job.outcome === "rolled_back") return "Failed · previous version restored";
  if (job.status === "succeeded") return job.outcome === "reconciled" ? "Completed · verified after interruption" : "Completed";
  if (job.status === "failed") return "Failed";
  return ({ queued: "Queued", launching: "Starting worker", preparing: "Checking configuration", writing_files: "Writing project files", pulling: "Pulling images", replacing: "Applying containers", executing: "Applying operation", restarting: "Restarting Yard", verifying: "Verifying services", committing_files: "Saving verified project files", committing: "Saving result", rolling_back: "Restoring previous version", legacy_reconciliation: "Reviewing interrupted operation" } as Record<string, string>)[job.stage] || "Operation in progress";
}

export function OperationHistory({ history, error, loading, csrfToken, onRefresh }: { history: ManagedJob[]; error: string; loading: boolean; csrfToken: string; onRefresh: () => void }) {
  const [reviewed, setReviewed] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [actionError, setActionError] = useState("");
  const heading = useId();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const latest = history[0];
  const recovery = history.find(job => job.outcome === "recovery_required");
  async function acknowledge(job: ManagedJob) {
    setBusy(true); setActionError("");
    try { await acknowledgeJobRecovery(job, csrfToken); setReviewed(null); headingRef.current?.focus(); onRefresh(); }
    catch (cause) { setActionError(cause instanceof Error ? cause.message : "Unable to acknowledge recovery."); }
    finally { setBusy(false); }
  }
  return <section className={styles.operationHistory} aria-labelledby={heading}>
    <h3 id={heading} ref={headingRef} tabIndex={-1}>Operations</h3>
    <p className={styles.operationStatus} data-status={latest?.status} role="status">{loading ? "Reading operation history…" : latest ? `${latest.scheduledFor ? "Automatic " : ""}${latest.operation}: ${operationStage(latest)}` : "No recorded operations."}</p>
    {error && <p className={styles.inventoryError} role="alert">{error} <button type="button" className={styles.inspectButton} onClick={onRefresh}>Retry</button></p>}
    {recovery && <div className={styles.actionConfirm}>
      <p>{recovery.error}</p>
      <p>Acknowledgement releases the reservation so you can retry. It does not restore or remove application containers/files. Temporary rollback image references may be cleaned up.</p>
      <label htmlFor={`${heading}-review`}><input id={`${heading}-review`} type="checkbox" checked={reviewed === recovery.id} onChange={event => setReviewed(event.target.checked ? recovery.id : null)} /> I inspected the target and retained resources on the host.</label>
      <button type="button" className={styles.inspectButton} disabled={busy || loading || Boolean(error) || reviewed !== recovery.id} onClick={() => { void acknowledge(recovery); }}>Acknowledge recovery</button>
    </div>}
    {actionError && <p className={styles.inventoryError} role="alert">{actionError}</p>}
    {history.length > 0 && <details><summary>Recent operations ({history.length})</summary><ol className={styles.operationList}>
      {history.map(job => <li key={job.id}>
        <strong>{job.scheduledFor ? "Automatic " : ""}{job.operation} · {operationStage(job)}</strong>
        <time dateTime={new Date(job.createdAt * 1000).toISOString()}>{new Date(job.createdAt * 1000).toLocaleString()}</time>
        {job.error && <p>{job.error}</p>}
        {job.rollback && <p>Rollback: {job.rollback.replaceAll("_", " ")}</p>}
        {job.cleanupError && <p>Cleanup: {job.cleanupError}</p>}
        {job.targetImages?.some(item => item.previousContainerID) && <ul aria-label="Container outcomes">{job.targetImages.map(item => <li key={item.previousContainerID || item.containerID}>
          <code>{item.service || item.previousContainerID?.slice(0, 12)} → {item.containerID?.slice(0, 12)}</code> · {item.outcome?.replaceAll("_", " ")} · {item.state}
        </li>)}</ul>}
      </li>)}
    </ol></details>}
  </section>;
}
