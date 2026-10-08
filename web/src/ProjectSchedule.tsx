import { useEffect, useId, useRef, useState } from "react";
import { getProjectSchedule, setProjectSchedule, type ProjectScheduleStatus } from "./api";
import styles from "./ProjectSchedule.module.css";

export function ProjectSchedule({ target, revision, csrfToken, onChanged }: { target: string; revision: string; csrfToken: string; onChanged: () => void }) {
  const id = useId();
  const [status, setStatus] = useState<ProjectScheduleStatus | null>(null);
  const [draft, setDraft] = useState<boolean | null>(null);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const [retry, setRetry] = useState(0);
  const request = useRef(0);
  const savingRef = useRef(false);
  useEffect(() => {
    const timer = window.setInterval(() => setRetry(value => value + 1), 60_000);
    return () => window.clearInterval(timer);
  }, [target]);
  useEffect(() => {
    if (savingRef.current) return;
    const current = ++request.current;
    void getProjectSchedule(target).then(value => {
      if (current === request.current) { setStatus(value); setError(""); }
    }).catch(cause => {
      if (current === request.current) setError(cause instanceof Error ? cause.message : "Unable to read automatic updates.");
    });
    return () => { request.current++; };
  }, [target, revision, retry]);
  async function save() {
    if (!status || draft === null) return;
    savingRef.current = true;
    request.current++;
    setSaving(true); setError(""); setMessage("");
    try {
      const value = await setProjectSchedule(target, draft, csrfToken);
      setStatus(value); setDraft(null); setMessage("Automatic update settings saved."); onChanged();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to save automatic updates."); }
    finally { savingRef.current = false; setSaving(false); }
  }
  const enabled = draft ?? status?.enabled ?? false;
  function timestamp(value: number) {
    return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short", timeZone: status?.timezone }).format(new Date(value * 1000));
  }
  return <section className={styles.panel}>
    <h3>Automatic image updates</h3>
    {status && <>
      <p id={`${id}-hint`}>Daily at {status.time} ({status.timezone}, server timezone). Only supported running projects are updated. Back up application data before enabling.</p>
      <label className={styles.choice} htmlFor={`${id}-enabled`}><input id={`${id}-enabled`} type="checkbox" aria-describedby={`${id}-hint`} checked={enabled} disabled={saving || (!status.eligible && !status.enabled)} onChange={event => { setDraft(event.target.checked); setMessage(""); }} /> Enable automatic updates for this project</label>
      {status.reason && <p>{status.reason}</p>}
      <button type="button" disabled={saving || draft === null || draft === status.enabled} onClick={() => { void save(); }}>Save automatic updates</button>
      <dl>
        <dt>Next check</dt><dd>{status.enabled && status.nextAt ? `${timestamp(status.nextAt)} (${status.timezone})` : "Disabled"}</dd>
        <dt>Last result</dt><dd>{status.lastOutcome ? `${status.lastOutcome.replaceAll("_", " ")}${status.lastAt ? ` · ${timestamp(status.lastAt)}` : ""}` : "No scheduled checks yet"}{status.lastReason && <p>{status.lastReason}</p>}</dd>
      </dl>
    </>}
    {!status && !error && <p>Reading automatic update settings…</p>}
    <p role="status">{message}</p>
    {error && <p role="alert">{error} <button type="button" disabled={saving} onClick={() => setRetry(value => value + 1)}>Retry automatic update settings</button></p>}
  </section>;
}
