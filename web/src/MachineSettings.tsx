import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { applyMachineNetwork, getMachineTokenHistory, rotateMachineToken, saveMachineSettings, type MachineSettings, type MachineStatus, type TokenEvent } from "./api";
import styles from "./App.module.css";
import settingsStyles from "./SettingsDrawer.module.css";
import machineStyles from "./MachineSettings.module.css";

export function MCPIndicator({ status, compact, onClick }: { status: MachineStatus | null; compact: boolean; onClick: () => void }) {
  const operational = status?.operational === true;
  const label = !status?.settings ? "MCP status unavailable" : !status.settings.mcpEnabled ? "MCP disabled" : operational ? "MCP operational" : "MCP unavailable";
  return <button type="button" className={`${styles.navLink} ${machineStyles.indicator}`} data-compact={compact} aria-label={`MCP Connection: ${label}`} title={label} aria-haspopup="dialog" onClick={onClick}>
    <span>{compact ? "MCP" : "MCP Connection"}</span><span className={machineStyles.dot} data-ready={operational} aria-hidden="true" />
  </button>;
}

function date(value: number) { return value ? new Date(value * 1000).toLocaleString() : "Never"; }
type Modal = "rotate" | "revoke" | "history" | "apply" | "save" | "token" | null;

export function MachineAccessSettings({ status, error: loadError, csrfToken, onRefresh }: { status: MachineStatus | null; error: string; csrfToken: string; onRefresh: () => Promise<void> }) {
  const [draft, setDraft] = useState<MachineSettings | null>(status?.settings ?? null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [modal, setModal] = useState<Modal>(null);
  const [token, setToken] = useState("");
  const [copyStatus, setCopyStatus] = useState("");
  const [events, setEvents] = useState<TokenEvent[]>([]);
  const dialog = useRef<HTMLDialogElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const tokenInput = useRef<HTMLInputElement>(null);
  const prefix = useId();
  const editable = (value: MachineSettings) => { const { hasToken: _hasToken, tokenCreatedAt: _created, ...rest } = value; return rest; };
  const dirty = draft && status?.settings && JSON.stringify(editable(draft)) !== JSON.stringify(editable(status.settings));
  useEffect(() => { setDraft(status?.settings ?? null); }, [status?.settings?.revision]);
  useEffect(() => { if (modal && !dialog.current?.open) { opener.current = document.activeElement as HTMLElement; dialog.current?.showModal(); } if (!modal && dialog.current?.open) dialog.current.close(); }, [modal]);
  function close() { if (busy) return; dialog.current?.close(); setModal(null); setToken(""); setError(""); opener.current?.focus(); }
  function open(value: Modal) { setError(""); setCopyStatus(""); setModal(value); }
  async function copyToken() {
    try { if (!navigator.clipboard) throw new Error("Clipboard unavailable"); await navigator.clipboard.writeText(token); setCopyStatus("Token copied."); }
    catch { tokenInput.current?.focus(); tokenInput.current?.select(); setCopyStatus("Token selected. Use your device's copy command."); }
  }
  async function save() {
    if (!draft) return;
    const saved = await saveMachineSettings(draft, csrfToken);
    setDraft(saved.settings); setNotice(saved.applyError ?? "Settings saved. Access permissions apply immediately; network changes require recreation.");
    await onRefresh();
  }
  async function perform(action: Exclude<Modal, null | "token">) {
    setBusy(true); setError("");
    try {
      if (action === "history") { setEvents(await getMachineTokenHistory()); return; }
      if (action === "rotate" || action === "revoke") {
        const response = await rotateMachineToken(action === "revoke", csrfToken);
        await onRefresh();
        if (action === "rotate") { setToken(response.token); setModal("token"); return; }
        setNotice("Yard token revoked. Authenticated machine access requires a new token.");
      } else if (action === "apply") {
        if (!status?.settings) return;
        await applyMachineNetwork(status.settings.revision, csrfToken);
        setNotice("Network recreation started. Yard may disconnect briefly. Reconnect using the requested Yard address if its port or bind changes.");
        await onRefresh();
      } else await save();
      dialog.current?.close(); setModal(null); opener.current?.focus();
    } catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to complete this action."); }
    finally { setBusy(false); }
  }
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (draft?.autoApply && JSON.stringify(draft.network) !== JSON.stringify(status?.settings.network)) { open("save"); return; }
    await perform("save");
  }
  const network = status?.networkStatus;
  const applying = network?.job?.status === "running";
  const modalTitle = modal === "rotate" ? "Generate a new Yard token?" : modal === "revoke" ? "Revoke the Yard token?" : modal === "history" ? "Token history" : modal === "token" ? "Your new Yard token" : modal === "save" ? "Save and apply network changes?" : "Apply network changes?";

  return <details id="machine-access-settings" aria-labelledby={`${prefix}-title`} className={`${settingsStyles.section} ${machineStyles.accordion}`}>
    <summary className={`${settingsStyles.heading} ${machineStyles.summary}`}>
      <h3 id={`${prefix}-title`}><i className="ph-bold ph-plugs-connected" aria-hidden="true" />MCP &amp; access</h3>
      <span className={machineStyles.summaryState}><span className={machineStyles.state}><span className={machineStyles.dot} data-ready={status?.settings.mcpEnabled === true} aria-hidden="true" />{!status?.settings ? "Unavailable" : status.settings.mcpEnabled ? "Enabled" : "Disabled"}</span><i className={`ph-bold ph-caret-right ${machineStyles.caret}`} aria-hidden="true" /></span>
    </summary>
    <div className={machineStyles.content}>
    <p className={settingsStyles.description}>Connect MCP clients directly to Yard. MCP is off by default and uses its own port.</p>
    {loadError && <p className={settingsStyles.error} role="alert">{loadError}</p>}
    {!draft ? <button type="button" className={styles.inspectButton} onClick={() => { void onRefresh(); }}>Retry machine settings</button> : <>
      <form onSubmit={event => { void submit(event); }}>
        <fieldset className={machineStyles.fields} disabled={busy || applying}>
          <legend className={machineStyles.legend}>Direct access</legend>
          <label className={machineStyles.check}><input type="checkbox" checked={draft.mcpEnabled} onChange={event => setDraft({ ...draft, mcpEnabled: event.target.checked })} />Enable MCP</label>
          <label className={machineStyles.check}><input type="checkbox" checked={draft.apiEnabled} onChange={event => setDraft({ ...draft, apiEnabled: event.target.checked })} />Enable Control API (Yard token required)</label>
          <div className={settingsStyles.field}><label htmlFor={`${prefix}-auth`}>MCP authentication</label><select id={`${prefix}-auth`} className={settingsStyles.input} value={draft.authMode} onChange={event => setDraft({ ...draft, authMode: event.target.value as MachineSettings["authMode"] })}><option value="token">Yard token</option><option value="anonymous">No authentication</option></select></div>
          {draft.authMode === "anonymous" && <p className={machineStyles.warning}>Anyone who can reach the MCP endpoint can use the permissions selected below. NoX authentication does not protect this direct access.</p>}
          <p className={settingsStyles.hint}>Inspection is always allowed for authorized clients. The permissions below apply to the Yard token and anonymous MCP access.</p>
          <label className={machineStyles.check}><input type="checkbox" checked={draft.write} onChange={event => setDraft({ ...draft, write: event.target.checked })} />Allow writes: actions, pulls, deployment, removal and settings</label>
          <label className={machineStyles.check}><input type="checkbox" checked={draft.secrets} onChange={event => setDraft({ ...draft, secrets: event.target.checked })} />Allow sensitive reads: environment values, file contents and logs</label>
          <div className={machineStyles.subheading}>Network on Docker host</div>
          <p className={settingsStyles.hint}>127.0.0.1 allows connections from the Docker host. 0.0.0.0 publishes to the network. Changing the Yard address may disconnect this browser.</p>
          <div className={machineStyles.network}>
            {(["yard", "mcp"] as const).map(service => <fieldset key={service} className={machineStyles.endpoint}><legend>{service === "yard" ? "Yard UI & Control API" : "MCP"}</legend><div className={settingsStyles.field}><label htmlFor={`${prefix}-${service}-bind`}>Bind address</label><select id={`${prefix}-${service}-bind`} className={settingsStyles.input} value={draft.network[service].bind} onChange={event => setDraft({ ...draft, network: { ...draft.network, [service]: { ...draft.network[service], bind: event.target.value } } })}><option value="127.0.0.1">127.0.0.1 — Local host</option><option value="0.0.0.0">0.0.0.0 — All interfaces</option></select></div><div className={settingsStyles.field}><label htmlFor={`${prefix}-${service}-port`}>Port</label><input id={`${prefix}-${service}-port`} className={settingsStyles.input} type="number" min="1" max="65535" required value={draft.network[service].port || ""} onChange={event => setDraft({ ...draft, network: { ...draft.network, [service]: { ...draft.network[service], port: Number(event.target.value) } } })} /></div></fieldset>)}
          </div>
          <label className={machineStyles.check}><input type="checkbox" checked={draft.autoApply} onChange={event => setDraft({ ...draft, autoApply: event.target.checked })} />Recreate Yard automatically when saving network changes</label>
          <button type="submit" className={styles.inspectButton} disabled={!dirty}><i className="ph-bold ph-check" aria-hidden="true" />Save access settings</button>
        </fieldset>
      </form>
      <div className={machineStyles.tokenSection}><strong>Yard token</strong><p className={settingsStyles.hint}>One active token, shared by MCP and Control API. Generating a new token immediately invalidates the previous one.</p><p className={settingsStyles.hint}>Last generated: {date(status?.settings.tokenCreatedAt ?? 0)} · {status?.settings.hasToken ? "Active" : "No active token"}</p><div className={machineStyles.actions}><button type="button" className={styles.inspectButton} disabled={busy} onClick={() => open("rotate")}><i className="ph-bold ph-key" aria-hidden="true" />Generate new token</button><button type="button" className={styles.inspectButton} disabled={busy} onClick={() => { open("history"); setEvents([]); void perform("history"); }}><i className="ph-bold ph-clock-counter-clockwise" aria-hidden="true" />History</button><button type="button" className={styles.inspectButton} disabled={busy || !status?.settings.hasToken} onClick={() => open("revoke")}>Revoke</button></div></div>
      {network?.available ? <div className={machineStyles.pending}><strong>{applying ? "Applying network changes" : network.pending ? "Pending network changes" : "Network settings applied"}</strong>{network.applied && <p className={settingsStyles.hint}>Current Yard: {network.applied.yard.bind || "Unpublished"}:{network.applied.yard.port || "—"} · MCP: {network.applied.mcp.bind || "Unpublished"}:{network.applied.mcp.port || "—"}</p>}{network.pending && <><p className={settingsStyles.description}>Saved network changes require a brief Yard recreation. The current image is kept.</p><button type="button" className={styles.inspectButton} disabled={busy || applying || !!dirty} onClick={() => open("apply")}><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" />Apply now</button></>}{network.job?.error && <p className={settingsStyles.error}>{network.job.error} {network.job.rollback && `Recovery: ${network.job.rollback}.`}</p>}</div> : <p className={settingsStyles.hint}>{network?.reason ?? "Network recreation requires Yard running in Docker Compose with accessible project files."}</p>}
    </>}
    {notice && <p role="status" className={settingsStyles.hint}>{notice}</p>}
    {error && !modal && <p role="alert" className={settingsStyles.error}>{error}</p>}
    <dialog ref={dialog} className={`${styles.confirmDialog} ${machineStyles.modal}`} aria-labelledby={`${prefix}-modal-title`} onCancel={event => { event.stopPropagation(); if (busy) event.preventDefault(); }} onClose={event => { event.stopPropagation(); setModal(null); setToken(""); opener.current?.focus(); }}>
      <div className={styles.confirmBody}><h2 id={`${prefix}-modal-title`}>{modalTitle}</h2>
        {(modal === "rotate" || modal === "revoke") && <p>{modal === "rotate" ? "The previous token will stop working immediately. Update all connected clients with the new token." : "All clients using the current Yard token will lose authenticated machine access."}</p>}
        {(modal === "apply" || modal === "save") && <><p>Recreate Yard with the saved host bindings and ports? The browser may disconnect briefly.</p><p>Requested Yard: {(modal === "save" ? draft : status?.settings)?.network.yard.bind}:{(modal === "save" ? draft : status?.settings)?.network.yard.port}</p><p>127.0.0.1 will only be reachable from the Docker host. Failed verification triggers recovery to the previous configuration.</p></>}
        {modal === "history" && <>{busy ? <p>Loading history…</p> : events.length ? <ol className={machineStyles.history}>{events.map(event => <li key={event.id}><strong>{event.action}</strong><time dateTime={new Date(event.createdAt * 1000).toISOString()}>{date(event.createdAt)}</time><span>{event.actor}</span></li>)}</ol> : <p>No token events yet.</p>}<p>Latest 10 events. Token values are never included.</p></>}
        {modal === "token" && <><p>Copy this token now. It will not be shown again.</p><input ref={tokenInput} className={settingsStyles.input} aria-label="New Yard token" readOnly value={token} autoComplete="off" spellCheck={false} onFocus={event => event.currentTarget.select()} /><button type="button" className={styles.inspectButton} onClick={() => { void copyToken(); }}><i className="ph-bold ph-copy" aria-hidden="true" />Copy token</button>{copyStatus && <p role="status">{copyStatus}</p>}</>}
        {error && <p className={settingsStyles.error} role="alert">{error}</p>}
        <div className={styles.confirmActions}><button type="button" autoFocus className={styles.inspectButton} disabled={busy} onClick={close}>{modal === "history" || modal === "token" ? "Close" : "Cancel"}</button>{modal && modal !== "history" && modal !== "token" && <button type="button" className={styles.inspectButton} disabled={busy} onClick={() => { void perform(modal); }}>{busy ? "Working…" : modal === "rotate" ? "Generate token" : modal === "revoke" ? "Revoke token" : "Apply changes"}</button>}</div>
      </div>
    </dialog>
    </div>
  </details>;
}
