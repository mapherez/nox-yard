import { useCallback, useEffect, useRef, useState } from "react";
import { getJobHistory, type ManagedJob } from "./api";

export function useOperationHistory(target: string | undefined, onChanged: () => void) {
  const [history, setHistory] = useState<ManagedJob[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revision, setRevision] = useState(0);
  const changed = useRef(onChanged);
  changed.current = onChanged;
  const refresh = useCallback(() => setRevision(value => value + 1), []);
  useEffect(() => {
    setHistory([]); setError(""); setLoading(Boolean(target));
    if (!target) return;
    let live = true;
    let timer: number | undefined;
    let previous = "";
    async function read() {
      try {
        const next = await getJobHistory(target!);
        if (!live) return;
        const signature = next.map(job => `${job.id}:${job.status}:${job.stage}:${job.outcome}`).join(";");
        if (previous && signature !== previous) changed.current();
        previous = signature;
        setHistory(next); setError(""); setLoading(false);
      } catch (cause) {
        if (live) { setError(cause instanceof Error ? cause.message : "Unable to read operation history."); setLoading(false); }
      }
      if (live) timer = window.setTimeout(() => { void read(); }, 2000);
    }
    void read();
    return () => { live = false; window.clearTimeout(timer); };
  }, [target, revision]);
  return { history, error, loading, refresh, blocked: loading || Boolean(error) || history.some(job => job.status === "running" || job.outcome === "recovery_required") };
}
