import { useEffect, useRef, useState } from "react";
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import "@xterm/xterm/css/xterm.css";
import type { Project } from "./api";
import styles from "./App.module.css";

type TerminalStatus = "connecting" | "connected" | "exited" | "error";

export function TerminalPanel({ project, preferredContainerID, csrfToken, onSelectContainer }: {
  project: Project;
  preferredContainerID?: string;
  csrfToken: string;
  onSelectContainer: (id: string) => void;
}) {
  const [selectedID, setSelectedID] = useState(
    project.containers.find((item) => item.id === preferredContainerID && item.state === "running" && item.terminalAvailable !== false)?.id
    || project.containers.find((item) => item.state === "running" && item.terminalAvailable !== false)?.id
    || preferredContainerID || project.containers[0]?.id || "",
  );
  const [retryKey, setRetryKey] = useState(0);
  const [status, setStatus] = useState<TerminalStatus>("connecting");
  const [message, setMessage] = useState("");
  const surfaceRef = useRef<HTMLDivElement>(null);
  const container = project.containers.find((item) => item.id === selectedID);

  useEffect(() => {
    if (!container || container.state !== "running" || container.terminalAvailable === false || !surfaceRef.current) return;
    const surface = surfaceRef.current;
    const palette = getComputedStyle(surface);
    const terminal = new Terminal({
      cursorBlink: true,
      cursorStyle: "bar",
      cursorWidth: 3,
      cursorInactiveStyle: "bar",
      fontFamily: palette.getPropertyValue("--font-code").trim(),
      fontSize: 13,
      theme: {
        background: palette.getPropertyValue("--color-terminal").trim(),
        foreground: palette.getPropertyValue("--color-text-primary").trim(),
        cursor: palette.getPropertyValue("--color-text-primary").trim(),
        selectionBackground: palette.getPropertyValue("--color-border-strong").trim(),
      },
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(surface);
    fit.fit();

    let active = true;
    let ready = false;
    let focusFrame = 0;
    const focusTerminal = () => {
      cancelAnimationFrame(focusFrame);
      focusFrame = requestAnimationFrame(() => {
        if (active && surface.isConnected) {
          terminal.focus();
          terminal.refresh(0, terminal.rows - 1);
        }
      });
    };
    focusTerminal();
    let ended = false;
    setStatus("connecting");
    setMessage("");
    const protocol = location.protocol === "https:" ? "wss:" : "ws:";
    const socket = new WebSocket(`${protocol}//${location.host}/api/containers/${container.id}/terminal`);
    socket.binaryType = "arraybuffer";
    const encoder = new TextEncoder();
    const resize = () => {
      fit.fit();
      if (ready && socket.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({
          type: "resize",
          cols: Math.max(20, Math.min(500, terminal.cols)),
          rows: Math.max(5, Math.min(200, terminal.rows)),
        }));
      }
    };
    const observer = new ResizeObserver(resize);
    observer.observe(surface);
    const input = terminal.onData((data) => {
      if (ready && socket.readyState === WebSocket.OPEN) socket.send(encoder.encode(data));
    });

    socket.onopen = () => {
      fit.fit();
      socket.send(JSON.stringify({
        type: "open", csrfToken,
        cols: Math.max(20, Math.min(500, terminal.cols)),
        rows: Math.max(5, Math.min(200, terminal.rows)),
      }));
    };
    socket.onmessage = (event) => {
      if (!active) return;
      if (event.data instanceof ArrayBuffer) {
        terminal.write(new Uint8Array(event.data));
        return;
      }
      try {
        const response = JSON.parse(event.data as string) as { type: string; code?: number; error?: string };
        if (response.type === "ready") {
          ready = true;
          setStatus("connected");
          resize();
          focusTerminal();
        } else if (response.type === "exit") {
          ready = false;
          ended = true;
          setStatus("exited");
          setMessage(`Shell exited with code ${response.code ?? 0}.`);
        } else if (response.type === "error") {
          ready = false;
          ended = true;
          setStatus("error");
          setMessage(response.error || "Terminal connection failed.");
        }
      } catch {
        ready = false;
        ended = true;
        setStatus("error");
        setMessage("Invalid terminal response.");
        socket.close();
      }
    };
    socket.onclose = () => {
      ready = false;
      if (active && !ended) {
        setStatus("error");
        setMessage("Terminal disconnected. Reconnect to try again.");
      }
    };
    socket.onerror = () => {
      ready = false;
      if (active && !ended) {
        setStatus("error");
        setMessage("Cannot connect to the terminal.");
      }
    };

    return () => {
      active = false;
      cancelAnimationFrame(focusFrame);
      observer.disconnect();
      input.dispose();
      socket.close();
      terminal.dispose();
      surface.replaceChildren();
    };
  }, [selectedID, retryKey, csrfToken, container?.state, container?.terminalAvailable]);

  if (!container) return <p className={styles.detailSummary}>No containers are available for a terminal.</p>;

  return <section className={styles.terminalPanel} aria-label="Container terminal">
    <div className={styles.logsToolbar}>
      {project.containers.length > 1
        ? <label className={styles.logsSelector}>Container
            <select value={container.id} onChange={(event) => {
              setStatus("connecting");
              setMessage("");
              setSelectedID(event.target.value);
              onSelectContainer(event.target.value);
            }}>
              {project.containers.map((item) => <option key={item.id} value={item.id} disabled={item.terminalAvailable === false}>{item.service ? `${item.service} · ${item.name}` : item.name}{item.terminalAvailable === false ? " (no /bin/sh)" : item.state === "running" ? "" : " (stopped)"}</option>)}
            </select>
          </label>
        : <strong className={styles.logsContainerName}>{container.service || container.name}</strong>}
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label="Reconnect terminal" title="Reconnect terminal" disabled={container.state !== "running" || container.terminalAvailable === false} onClick={() => setRetryKey((key) => key + 1)}><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" /></button>
    </div>
    {container.state !== "running" && <p className={styles.detailSummary}>Start this container before opening a terminal.</p>}
    {container.state === "running" && container.terminalAvailable === false && <p className={styles.detailSummary}>Terminal unavailable: this container has no /bin/sh shell.</p>}
    {container.state === "running" && container.terminalAvailable !== false && <>
      <p className={styles.logStatus} role="status">{status === "connecting" ? "Connecting…" : status === "connected" ? "Connected" : status === "exited" ? "Session ended" : "Disconnected"}</p>
      {message && <p className={status === "error" ? styles.inventoryError : styles.detailSummary} role={status === "error" ? "alert" : "status"}>{message}</p>}
      <div ref={surfaceRef} className={styles.terminalSurface} data-drawer-swipe-ignore aria-label={`${container.service || container.name} interactive terminal`} />
    </>}
    <p className={styles.terminalHint}>Commands run inside the selected container as its configured user. This session is not saved.</p>
  </section>;
}
