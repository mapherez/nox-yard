import { useEffect, useId, useRef, useState } from "react";
import { getProjectSchedule, setProjectSchedule, type ProjectScheduleStatus } from "./api";
import styles from "./ProjectSchedule.module.css";

export function ProjectSchedule({ target, revision, csrfToken, onChanged }: { target: string; revision: string; csrfToken: string; onChanged: () => void }) {
  const id = useId();
  const [status, setStatus] = useState<ProjectScheduleStatus | null>(null);
  const [draft, setDraft] = useState<boolean | null>(null);
  const [draftTime, setDraftTime] = useState<string | null>(null);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [saving, setSaving] = useState(false);
  const [retry, setRetry] = useState(0);
  const request = useRef(0);
  const savingRef = useRef(false);
  useEffect(() => { setDraft(null); setDraftTime(null); }, [target]);
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
    if (!status) return;
    savingRef.current = true;
    request.current++;
    setSaving(true); setError(""); setMessage("");
    try {
      const value = await setProjectSchedule(target, draft ?? status.enabled, draftTime ?? status.time, csrfToken);
      setStatus(value); setDraft(null); setDraftTime(null); setMessage("Automatic update settings saved."); onChanged();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to save automatic updates."); }
    finally { savingRef.current = false; setSaving(false); }
  }
  const enabled = draft ?? status?.enabled ?? false;
  const checkTime = draftTime ?? status?.time ?? "03:00";
  const changed = status && (enabled !== status.enabled || checkTime !== status.time);
  function timestamp(value: number) {
    return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short", timeZone: status?.timezone }).format(new Date(value * 1000));
  }
  return <section className={styles.panel}>
    <h3>Automatic image updates</h3>
    {status && <>
      <p id={`${id}-hint`}>Daily at {status.time} ({status.timezone}, server timezone). Only supported running projects are updated. Back up application data before enabling.</p>
      <form onSubmit={event => { event.preventDefault(); void save(); }}>
      <label className={styles.choice} htmlFor={`${id}-enabled`}><input id={`${id}-enabled`} name="enabled" type="checkbox" aria-describedby={`${id}-hint`} checked={enabled} disabled={saving || (!status.eligible && !status.enabled)} onChange={event => { setDraft(event.target.checked); setMessage(""); }} /> Enable automatic updates for this project</label>
      <div className={styles.timeField}>
        <label htmlFor={`${id}-time`}>Daily check time ({status.timezone})</label>
        <input id={`${id}-time`} name="time" type="time" step={60} required value={checkTime} disabled={saving} aria-describedby={`${id}-hint`} onChange={event => { setDraftTime(event.target.value); setMessage(""); }} />
      </div>
      {status.reason && <p>{status.reason}</p>}
      <button type="submit" disabled={saving || !changed}>Save automatic updates</button>
      </form>
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
