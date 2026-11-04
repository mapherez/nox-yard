import { useCallback, useEffect, useState } from "react";
import { getSuiteStatus, retrySuiteDeployment, type SuiteStatus } from "./api";
import styles from "./SettingsDrawer.module.css";
import app from "./App.module.css";

export function SuiteSettings({ csrfToken }: { csrfToken: string }) {
  const [status, setStatus] = useState<SuiteStatus | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const refresh = useCallback(async () => {
    try { setStatus(await getSuiteStatus()); setError(""); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to load Suite status."); }
  }, []);
  useEffect(() => {
    let active = true;
    const load = async () => {
      try { const value = await getSuiteStatus(); if (active) { setStatus(value); setError(""); } }
      catch (cause) { if (active) setError(cause instanceof Error ? cause.message : "Unable to load Suite status."); }
    };
    void load();
    const timer = window.setInterval(() => { if (document.visibilityState === "visible") void load(); }, 15000);
    return () => { active = false; window.clearInterval(timer); };
  }, []);
  async function retry(id: string, revision: number) {
    setBusy(id);
    try { await retrySuiteDeployment(id, revision, csrfToken); await refresh(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to retry Suite integration."); }
    finally { setBusy(""); }
  }
  const deployments = status?.deployments.filter((item) => item.state !== "removed") ?? [];
  return <section className={styles.section} aria-labelledby="suite-settings-title">
    <div className={styles.heading}>
      <h3 id="suite-settings-title"><i className="ph-bold ph-plugs-connected" aria-hidden="true" />NoX Suite</h3>
      <button type="button" className={`${app.inspectButton} ${styles.action}`} onClick={() => { void refresh(); }}>Refresh</button>
    </div>
    <p className={styles.description}>{status?.ready ? "Connected to Backend. Participating products installed by Yard register automatically." : status?.status === "disabled" ? "Suite integration is disabled for this installation." : "Pair Yard from the Backend dashboard to enable automatic product registration."}</p>
    {status && <p className={styles.hint}>Connection: {status.ready ? "Ready" : status.status.replaceAll("_", " ")}</p>}
    {deployments.map((item) => <div key={item.id} className={styles.section}>
      <div className={styles.heading}>
        <strong>{item.productId}</strong>
        <span className={styles.description}>{item.state.replaceAll("_", " ")}</span>
      </div>
      <p className={styles.hint}>{item.projectId.replace(/^compose:/, "")} · {item.service}</p>
      {item.message && <>
        <p className={styles.description}>{item.message}</p>
        <button type="button" className={`${app.inspectButton} ${styles.action}`} disabled={busy !== ""} onClick={() => { void retry(item.id, item.revision); }}>{busy === item.id ? "Retrying…" : "Retry integration"}</button>
      </>}
    </div>)}
    {error && <p className={styles.error} role="alert">{error}</p>}
  </section>;
}
