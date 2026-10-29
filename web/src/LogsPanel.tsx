import { useEffect, useId, useMemo, useRef, useState } from "react";
import { loadDockerLogHistory,type DockerLogPage,type DockerLogEvent,containerLogsURL, notifySessionExpired, getLogExportStatus, exportCurrentLogs, type LogExportStatus, type Project } from "./api";
import styles from "./App.module.css";
import logStyles from "./LogsPanel.module.css";
type LogLine = DockerLogEvent;
const MAX_VISIBLE_LOG_BYTES=8*1024*1024;
const logSizes=new WeakMap<LogLine,number>();
function logSize(line:LogLine){let bytes=logSizes.get(line);if(bytes===undefined){bytes=Math.max(line.text.length*2,new TextEncoder().encode(line.text).byteLength)+128;logSizes.set(line,bytes);}return bytes;}
function boundedLogWindow(lines:LogLine[]):LogLine[]{let bytes=0,start=lines.length;while(start>0){const size=logSize(lines[start-1]);if(bytes+size>MAX_VISIBLE_LOG_BYTES)break;bytes+=size;start--;}return lines.slice(start);}
function logText(lines:LogLine[]){return lines.map(line=>line.text+(line.lineEnd===false?"":"\n")).join("");}
type LogStatus = "connecting" | "live" | "reconnecting" | "ended" | "paused" | "error";

export function LogsPanel({ project, preferredContainerID, onSelectContainer, csrfToken }: {
  project: Project;
  csrfToken: string;
  preferredContainerID?: string;
  onSelectContainer: (id: string) => void;
}) {
  const [selectedID, setSelectedID] = useState(preferredContainerID || project.containers[0]?.id || "");
  const [lines, setLines] = useState<LogLine[]>([]);
  const [status, setStatus] = useState<LogStatus>("connecting");
  const [error, setError] = useState("");
  const [historyMode,setHistoryMode]=useState(false);
  const [historyPage,setHistoryPage]=useState<DockerLogPage|null>(null);
  const [historyBusy,setHistoryBusy]=useState(false);
  const [historyRange,setHistoryRange]=useState("all");
  const [historySince,setHistorySince]=useState("");
  const [historyUntil,setHistoryUntil]=useState("");
  const [historyCursors,setHistoryCursors]=useState<string[]>([]);
  const historyRequest=useRef<AbortController|null>(null);
  const historyVersion=useRef(0);
  const historyBounds=useRef<{since?:string;until?:string}>({});
  const [retryKey, setRetryKey] = useState(0);
  const [visible, setVisible] = useState(document.visibilityState === "visible");
  const [followOutput, setFollowOutput] = useState(true);
  const outputRef = useRef<HTMLPreElement>(null);
  const container = project.containers.find((item) => item.id === selectedID) || project.containers[0];
  const [query, setQuery] = useState("");
  const [activeMatch, setActiveMatch] = useState(0);
  const [exportStatus, setExportStatus] = useState<LogExportStatus | null>(null);
  const [exportError, setExportError] = useState("");
  const [statusRetry, setStatusRetry] = useState(0);
  const [saving, setSaving] = useState(false);
  const [snapshot, setSnapshot] = useState<{ id: string; name: string; content: string; count: number } | null>(null);
  const exportDialog = useRef<HTMLDialogElement>(null);
  const exportButton = useRef<HTMLButtonElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);
  const confirmTitle = useId();

  useEffect(() => {
    const controller = new AbortController();
    setExportStatus(null); setExportError("");
    void getLogExportStatus(project.id, controller.signal).then(setExportStatus).catch(cause => {
      if (!controller.signal.aborted) setExportError(cause instanceof Error ? cause.message : "Cannot read the last log export.");
    });
    return () => controller.abort();
  }, [project.id, statusRetry]);

  useEffect(() => {
    const dialog = exportDialog.current;
    if (snapshot && dialog && !dialog.open) dialog.showModal();
    if (!snapshot && dialog?.open) { dialog.close(); exportButton.current?.focus(); }
  }, [snapshot]);

  const matches = useMemo(() => {
    let count = 0; let limited = false;
    const expression = query ? new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"), "giu") : null;
    const rendered = lines.map(line => {
      const parts: { text: string; match?: number }[] = [];
      let offset = 0;
      if (expression) {
        for (const match of line.text.matchAll(expression)) {
          const start = match.index;
          if (count >= 2000) { limited = true; break; }
          parts.push({ text: line.text.slice(offset, start) }, { text: match[0], match: count++ });
          offset = start + match[0].length;
        }
      }
      parts.push({ text: line.text.slice(offset) });
      return { ...line, parts };
    });
    return { rendered, count, limited };
  }, [lines, query]);

  useEffect(() => {
    setActiveMatch(current => Math.max(0, Math.min(current, matches.count - 1)));
  }, [matches.count]);
  useEffect(() => {
    if (query) outputRef.current?.querySelector(`[data-log-match="${activeMatch}"]`)?.scrollIntoView({ block: "center", inline: "nearest" });
  }, [query, activeMatch]);

  function search(value: string) { setQuery(value); setActiveMatch(0); if (value) setFollowOutput(false); }
  function navigate(direction: number) {
    if (matches.count) { setFollowOutput(false); setActiveMatch(index => (index + direction + matches.count) % matches.count); }
  }
  async function saveExport() {
    if (!snapshot || saving) return;
    setSaving(true); setExportError("");
    try {
      setExportStatus(await exportCurrentLogs(project.id, snapshot.id, snapshot.content, csrfToken));
      setSnapshot(null);
    } catch (cause) { setExportError(cause instanceof Error ? cause.message : "Unable to export the current log."); }
    finally { setSaving(false); }
  }

  useEffect(() => {
    const sync = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", sync);
    return () => document.removeEventListener("visibilitychange", sync);
  }, []);

  useEffect(() => {
    if (!container || !visible || historyMode) {
      setStatus("paused");
      return;
    }
    setLines([]);
    setError("");
    setStatus("connecting");
    const source = new EventSource(containerLogsURL(container.id));
    source.onopen = () => {
      setLines([]);
      setStatus("live");
      setError("");
    };
    source.addEventListener("log", (event) => {
      try {
        const line: unknown = JSON.parse((event as MessageEvent).data);
        if (!line || typeof line !== "object" || !("text" in line) || typeof line.text !== "string" ||
            !("stream" in line) || (line.stream !== "stdout" && line.stream !== "stderr" && line.stream !== "combined")) return;
        setLines((current) => boundedLogWindow([...current, line as LogLine]));
      } catch { /* Ignore a malformed event and keep the stream open. */ }
    });
    source.addEventListener("end", () => {
      setStatus("ended");
      source.close();
    });
    source.addEventListener("stream-error", () => {
      setStatus("error");
      setError("Docker stopped the log stream. Reconnect to try again.");
      source.close();
    });
    source.addEventListener("session-expired", () => {
      source.close();
      notifySessionExpired();
    });
    source.onerror = () => {
      if (source.readyState !== EventSource.CLOSED) {
        setStatus("reconnecting");
        setError("Cannot connect to logs. Check Docker access or the container's logging driver.");
      }
    };
    return () => source.close();
  }, [container?.id, retryKey, visible,historyMode]);

  useEffect(() => {
    if (followOutput && outputRef.current) outputRef.current.scrollTop = outputRef.current.scrollHeight;
  }, [lines, followOutput]);

  const statusLabels: Record<LogStatus, string> = {
    connecting: "Connecting…", live: "Live", reconnecting: "Reconnecting…",
    ended: "Stream ended", paused: "Paused while this tab is hidden", error: "Disconnected",
  };

  useEffect(()=>{historyRequest.current?.abort();historyVersion.current++;setHistoryMode(false);setHistoryBusy(false);setHistoryPage(null);setHistoryCursors([]);historyBounds.current={};return()=>{historyRequest.current?.abort();historyVersion.current++;};},[container?.id]);
  async function readHistory(cursor="",stack:string[]=[],reset=false){
   if(!container)return;historyRequest.current?.abort();const abort=new AbortController();historyRequest.current=abort;const version=++historyVersion.current;
   setHistoryMode(true);setHistoryBusy(true);setError("");setFollowOutput(false);
   try{
    const now=new Date();let {since,until}=historyBounds.current;
    if(reset){since=undefined;until=now.toISOString();if(historyRange==="1h"||historyRange==="24h")since=new Date(now.getTime()-(historyRange==="1h"?1:24)*3600000).toISOString();else if(historyRange==="custom"){if(historySince)since=new Date(historySince).toISOString();if(historyUntil)until=new Date(historyUntil).toISOString();}}
    historyBounds.current={since,until};
    const page=await loadDockerLogHistory(container.id,{since,until,cursor},abort.signal);
    if(version!==historyVersion.current)return;setLines(boundedLogWindow(page.lines));setHistoryPage(page);setHistoryCursors(stack);
   }catch(cause){if(version===historyVersion.current&&!abort.signal.aborted)setError(cause instanceof Error?cause.message:"Cannot read Docker history.");}
   finally{if(version===historyVersion.current)setHistoryBusy(false);}
  }
  if (!container) return <p className={styles.detailSummary}>No containers are available for logs.</p>;

  return <section className={logStyles.panel} aria-label="Container logs">
    <div className={styles.logsToolbar}>
      {project.containers.length > 1
        ? <label className={styles.logsSelector}>Container
            <select value={container.id} onChange={(event) => { setSelectedID(event.target.value); onSelectContainer(event.target.value); }}>
              {project.containers.map((item) => <option key={item.id} value={item.id}>{item.service ? `${item.service} · ${item.name}` : item.name}</option>)}
            </select>
          </label>
        : <strong className={styles.logsContainerName}>{container.service || container.name}</strong>}
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label="Reconnect logs" title="Reconnect logs" onClick={() => setRetryKey((key) => key + 1)}><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" /></button>
    </div>
    <div className={logStyles.historyControls} aria-label="Docker log history"><label>History<select value={historyRange} disabled={historyBusy} onChange={event=>setHistoryRange(event.target.value)}><option value="all">All available</option><option value="1h">Last hour</option><option value="24h">Last 24 hours</option><option value="custom">Time interval</option></select></label>{historyRange==="custom"&&<><label>From<input type="datetime-local" value={historySince} onChange={event=>setHistorySince(event.target.value)} /></label><label>Until<input type="datetime-local" value={historyUntil} onChange={event=>setHistoryUntil(event.target.value)} /></label></>}<button type="button" className={styles.inspectButton} disabled={historyBusy} onClick={()=>{void readHistory("",[],true);}}>{historyBusy?"Loading…":"Load history"}</button>{historyMode&&<button type="button" className={styles.inspectButton} onClick={()=>{historyRequest.current?.abort();historyVersion.current++;setHistoryMode(false);setHistoryBusy(false);setHistoryPage(null);}}>Live logs</button>}</div>
    <p className={styles.logStatus} role="status">{historyMode?"Docker retained history · oldest records first":statusLabels[status]}</p>
    <p className={logStyles.exportHint}>Read directly from Docker. No history is stored by Yard. The live view uses a bounded memory window; rotation or container removal can discard older logs.</p>
    {historyMode&&<div className={logStyles.historyControls}><button type="button" className={styles.inspectButton} disabled={historyBusy||!historyCursors.length} onClick={()=>{const stack=historyCursors.slice(0,-1);void readHistory(stack.at(-1)||"",stack);}}>Previous page</button><button type="button" className={styles.inspectButton} disabled={historyBusy||!historyPage?.next} onClick={()=>{if(historyPage?.next)void readHistory(historyPage.next,[...historyCursors,historyPage.next]);}}>Next page</button>{historyPage&&!historyPage.next&&<span>End of available history</span>}</div>}
    {lines.some(line=>line.continuation||line.lineEnd===false)&&<p className={logStyles.exportHint}>Long lines use transport fragments. They are joined without added line breaks; this page may begin or end within a line.</p>}
    {error && <p className={styles.inventoryError} role="alert">{error}</p>}
    <div className={logStyles.search} role="search" aria-label="Log search">
      <i className="ph-bold ph-magnifying-glass" aria-hidden="true" />
      <input ref={searchRef} type="search" aria-label="Search logs" placeholder="Search logs…" value={query} onChange={event => search(event.target.value)} onKeyDown={event => {
        if (event.key === "Enter") { event.preventDefault(); navigate(event.shiftKey ? -1 : 1); }
        if (event.key === "Escape" && query) { event.preventDefault(); event.stopPropagation(); search(""); }
      }} />
      {query && <span className={logStyles.matchCount} role="status">{matches.count ? `${activeMatch + 1}/${matches.count}${matches.limited ? "+" : ""}` : "No matches"}</span>}
      <button type="button" aria-label="Previous log match" title="Previous match" disabled={!matches.count} onClick={() => navigate(-1)}><i className="ph-bold ph-arrow-up" aria-hidden="true" /></button>
      <button type="button" aria-label="Next log match" title="Next match" disabled={!matches.count} onClick={() => navigate(1)}><i className="ph-bold ph-arrow-down" aria-hidden="true" /></button>
      {query && <button type="button" aria-label="Clear log search" title="Clear search" onClick={() => { search(""); searchRef.current?.focus(); }}><i className="ph-bold ph-x" aria-hidden="true" /></button>}
    </div>
    <pre ref={outputRef} className={`${styles.logOutput} ${logStyles.output}`} aria-label={`${container.service || container.name} log output`} data-drawer-swipe-ignore onScroll={(event) => {
      const output = event.currentTarget;
      if (!query) setFollowOutput(output.scrollHeight - output.scrollTop - output.clientHeight < 32);
    }}>{lines.length ? matches.rendered.map((line, index) => <span key={index} className={line.stream === "stderr" ? styles.logErrorLine : undefined}>{line.parts.map((part, index) => part.match === undefined ? part.text : <mark key={index} data-log-match={part.match} data-active={part.match === activeMatch}>{part.text}</mark>)}{line.lineEnd===false?"":"\n"}</span>) : (status === "ended"||historyMode) ? "No recent logs.\n" : "Waiting for log output…\n"}</pre>
    <label className={styles.followOutput}><input type="checkbox" checked={followOutput} onChange={(event) => setFollowOutput(event.target.checked)} /> Follow output</label>
    <footer className={logStyles.exportFooter}>
      <span>Last export: {exportStatus?.exportedAt ? new Date(exportStatus.exportedAt).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" }) : exportStatus ? "Never" : "Unavailable"}</span>
      <button ref={exportButton} type="button" className={styles.inspectButton} disabled={!lines.length || !exportStatus?.available} onClick={() => { setExportError(""); setSnapshot({ id: container.id, name: container.service || container.name, content: logText(lines), count: lines.length }); }}><i className="ph-bold ph-export" aria-hidden="true" />Export log</button>
    </footer>
    {exportStatus?.reason && <p className={logStyles.exportHint}>{exportStatus.reason}</p>}
    {exportError && !snapshot && <p className={logStyles.exportError} role="alert">{exportError} <button type="button" className={styles.inspectButton} onClick={() => setStatusRetry(value => value + 1)}>Retry export status</button></p>}
    <dialog ref={exportDialog} className={`${styles.confirmDialog} ${logStyles.confirm}`} aria-labelledby={confirmTitle} onClose={event => { event.stopPropagation(); setSnapshot(null); exportButton.current?.focus(); }} onCancel={event => { event.stopPropagation(); if (saving) event.preventDefault(); }}>
      {snapshot && <div className={styles.confirmBody}>
        <h2 id={confirmTitle}>Save current log?</h2>
        <p>Save {snapshot.count} log {snapshot.count === 1 ? "line" : "lines"} from <strong>{snapshot.name}</strong> as a text file?</p>
        <p className={logStyles.destination}>{exportStatus?.directory}/YYYY-MM-DD-HH-mm.txt</p>
        {exportError && <p role="alert">{exportError}</p>}
        <div className={styles.confirmActions}>
          <button type="button" className={styles.inspectButton} autoFocus disabled={saving} onClick={() => setSnapshot(null)}>Cancel</button>
          <button type="button" className={styles.inspectButton} disabled={saving} onClick={() => { void saveExport(); }}><i className="ph-bold ph-floppy-disk" aria-hidden="true" />{saving ? "Saving…" : "Save log"}</button>
        </div>
      </div>}
    </dialog>
  </section>;
}

