import { useCallback, useEffect, useId, useRef, useState, type FormEvent, type MouseEvent } from "react";
import {
  checkSelfUpdateNow,
  containerLogsURL,
  createAdministrator,
  getBootstrap,
  getContainerInspection,
  getProjects,
  getSelfUpdateStatus,
  pullContainerImage,
  pullProjectImages,
  previewRemoveContainer,
  previewRemoveProject,
  removeContainer,
  removeProject,
  saveSelfUpdateSettings,
  revealContainerEnvironment,
  runContainerAction,
  runProjectAction,
  signIn,
  signOut,
  type Bootstrap,
  type Container,
  type ContainerInspection,
  type LifecycleAction,
  type MaintenanceResult,
  type Project,
  type RemovalPlan,
  type RemovalReport,
  type SelfUpdateStatus,
} from "./api";
import styles from "./App.module.css";
import { useDrawerSwipe } from "./useDrawerSwipe";
import { TerminalPanel } from "./TerminalPanel";

type View =
  | { kind: "loading" }
  | { kind: "setup" }
  | { kind: "login" }
  | { kind: "dashboard"; username: string; csrfToken: string }
  | { kind: "error"; message: string };

function viewFromBootstrap(result: Bootstrap): View {
  if (result.needsSetup) return { kind: "setup" };
  if (result.authenticated && result.username && result.csrfToken) {
    return { kind: "dashboard", username: result.username, csrfToken: result.csrfToken };
  }
  return { kind: "login" };
}

export default function App() {
  const [view, setView] = useState<View>({ kind: "loading" });

  useEffect(() => {
    const titles: Record<View["kind"], string> = {
      loading: "Loading | NoX Yard",
      setup: "Create administrator | NoX Yard",
      login: "Sign in | NoX Yard",
      dashboard: "Projects | NoX Yard",
      error: "Unavailable | NoX Yard",
    };
    document.title = titles[view.kind];
  }, [view.kind]);

  useEffect(() => {
    let active = true;
    getBootstrap()
      .then((result) => {
        if (active) setView(viewFromBootstrap(result));
      })
      .catch((error: unknown) => {
        if (active) {
          setView({
            kind: "error",
            message: error instanceof Error ? error.message : "Unable to load NoX Yard.",
          });
        }
      });
    return () => {
      active = false;
    };
  }, []);

  if (view.kind === "loading") {
    return (
      <main className={styles.centerScreen}>
        <Brand />
        <p className={styles.muted}>Loading NoX Yard…</p>
      </main>
    );
  }

  if (view.kind === "error") {
    return (
      <main className={styles.centerScreen}>
        <Brand />
        <h1>Unable to open NoX Yard</h1>
        <p className={styles.muted}>{view.message}</p>
        <button className={styles.primaryButton} onClick={() => window.location.reload()}>
          Retry
        </button>
      </main>
    );
  }

  if (view.kind === "dashboard") {
    return <Dashboard username={view.username} csrfToken={view.csrfToken} onSignOut={() => setView({ kind: "login" })} />;
  }

  return <AuthForm mode={view.kind} onAuthenticated={(result) => setView(viewFromBootstrap(result))} />;
}

function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <div className={compact ? styles.brandCompact : styles.brand}>
      <span className={styles.brandMark} aria-hidden="true">N</span>
      <span className={styles.brandName}>NoX Yard</span>
    </div>
  );
}

function AuthForm({
  mode,
  onAuthenticated,
}: {
  mode: "setup" | "login";
  onAuthenticated: (result: Bootstrap) => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const isSetup = mode === "setup";

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError("");
    if (isSetup && password !== confirmation) {
      setError("Passwords do not match.");
      return;
    }
    setPending(true);
    try {
      const result = await (isSetup ? createAdministrator : signIn)({ username, password });
      onAuthenticated(result);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to continue.");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className={styles.authLayout}>
      <aside className={styles.authAside}>
        <Brand />
        <div className={styles.authAsideContent}>
          <span className={styles.eyebrow}>YOUR HOMELAB, IN VIEW</span>
          <h2>Keep your containers close.</h2>
          <p>A focused home for your Docker projects, built for the host you already run.</p>
          <div className={styles.authLines} aria-hidden="true"><span /><span /><span /></div>
        </div>
        <p className={styles.authAsideFooter}>SELF-HOSTED · LOCAL CONTROL</p>
      </aside>

      <main className={styles.authMain}>
        <div className={styles.authCard}>
          <div className={styles.authMobileBrand}><Brand compact /></div>
          <span className={styles.sectionLabel}>{isSetup ? "FIRST-RUN SETUP" : "WELCOME BACK"}</span>
          <h1>{isSetup ? "Create your admin account" : "Sign in to NoX Yard"}</h1>
          <p className={styles.authDescription}>
            {isSetup
              ? "This account will manage Docker on this host. Choose a strong password to get started."
              : "Enter your administrator credentials to manage this host."}
          </p>

          <form action={isSetup ? "/api/setup" : "/api/login"} method="post" onSubmit={handleSubmit} className={styles.authForm}>
            <div className={styles.field}>
              <label htmlFor="username">Username</label>
              <input
                id="username"
                name="username"
                type="text"
                autoComplete="username"
                required
                minLength={isSetup ? 3 : undefined}
                maxLength={32}
                pattern={isSetup ? "[A-Za-z0-9._\\-]{3,32}" : undefined}
                value={username}
                onChange={(event) => { setUsername(event.target.value); setError(""); }}
                aria-describedby={isSetup ? "username-hint" : undefined}
              />
              {isSetup && <span id="username-hint" className={styles.fieldHint}>3–32 letters, numbers, dots, underscores, or hyphens.</span>}
            </div>

            <div className={styles.field}>
              <label htmlFor="password">Password</label>
              <div className={styles.passwordField}>
                <input
                  id="password"
                  name="password"
                  type={showPassword ? "text" : "password"}
                  autoComplete={isSetup ? "new-password" : "current-password"}
                  required
                  minLength={isSetup ? 12 : undefined}
                  maxLength={128}
                  value={password}
                  onChange={(event) => { setPassword(event.target.value); setError(""); }}
                  aria-describedby={isSetup ? "password-hint" : undefined}
                />
                <button type="button" className={styles.revealButton} onClick={() => setShowPassword(!showPassword)}>
                  {showPassword ? "Hide" : "Show"}
                </button>
              </div>
              {isSetup && <span id="password-hint" className={styles.fieldHint}>At least 12 characters.</span>}
            </div>

            {isSetup && (
              <div className={styles.field}>
                <label htmlFor="confirmation">Confirm password</label>
                <input
                  id="confirmation"
                  name="confirmation"
                  type={showPassword ? "text" : "password"}
                  autoComplete="new-password"
                  required
                  value={confirmation}
                  onChange={(event) => { setConfirmation(event.target.value); setError(""); }}
                />
              </div>
            )}

            {error && <p className={styles.formError} role="alert">{error}</p>}
            <button className={styles.primaryButton} type="submit" disabled={pending}>
              {pending ? "Please wait…" : isSetup ? "Create administrator" : "Sign in"}
            </button>
          </form>
          <p className={styles.authNote}>Access is intended for a trusted local network or VPN.</p>
        </div>
      </main>
    </div>
  );
}

function Dashboard({
  username,
  csrfToken,
  onSignOut,
}: {
  username: string;
  csrfToken: string;
  onSignOut: () => void;
}) {
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const [projects, setProjects] = useState<Project[] | null>(null);
  const [inventoryError, setInventoryError] = useState("");
  const [collectedAt, setCollectedAt] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [refreshKey, setRefreshKey] = useState(0);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const skipLinkRef = useRef<HTMLAnchorElement>(null);
  const dashboardBodyRef = useRef<HTMLDivElement>(null);
  const sidebarRef = useRef<HTMLElement>(null);
  const [isMobile, setIsMobile] = useState(() => window.matchMedia("(max-width: 47.99rem)").matches);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try { return window.localStorage.getItem("nox-yard-sidebar-collapsed") === "true"; }
    catch { return false; }
  });
  const compactSidebar = !isMobile && sidebarCollapsed;
  const closeMobileMenu = useCallback(() => setMobileOpen(false), []);
  useDrawerSwipe(sidebarRef, "left", isMobile && mobileOpen, closeMobileMenu);

  const changeSidebar = useCallback((collapsed: boolean) => {
    setSidebarCollapsed(collapsed);
    try { window.localStorage.setItem("nox-yard-sidebar-collapsed", String(collapsed)); }
    catch { /* The layout still works when browser storage is unavailable. */ }
  }, []);

  useEffect(() => {
    const media = window.matchMedia("(max-width: 47.99rem)");
    const sync = () => {
      setIsMobile(media.matches);
      if (!media.matches) setMobileOpen(false);
    };
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  }, []);

  useEffect(() => {
    const content = dashboardBodyRef.current;
    const skipLink = skipLinkRef.current;
    if (!content) return;
    content.inert = mobileOpen;
    if (skipLink) skipLink.inert = mobileOpen;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && mobileOpen && !document.querySelector("dialog[open]")) setMobileOpen(false);
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      content.inert = false;
      if (skipLink) skipLink.inert = false;
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [mobileOpen]);

  useEffect(() => {
    let active = true;
    let busy = false;
    const controller = new AbortController();
    async function refresh() {
      if (busy || document.visibilityState === "hidden") return;
      busy = true;
      setRefreshing(true);
      try {
        const snapshot = await getProjects(controller.signal);
        if (!active) return;
        setProjects(snapshot.projects);
        setCollectedAt(snapshot.collectedAt);
        setInventoryError("");
      } catch (cause) {
        if (active) setInventoryError(cause instanceof Error ? cause.message : "Unable to load Docker projects.");
      } finally {
        busy = false;
        if (active) setRefreshing(false);
      }
    }
    void refresh();
    const interval = window.setInterval(() => { void refresh(); }, 20_000);
    function onVisible() { if (document.visibilityState === "visible") void refresh(); }
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      active = false;
      controller.abort();
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [refreshKey]);

  const selected = projects?.find((project) => project.id === selectedID);
  const displayName = username.charAt(0).toUpperCase() + username.slice(1);

  async function handleSignOut() {
    setError("");
    setPending(true);
    try {
      await signOut(csrfToken);
      onSignOut();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to sign out.");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className={styles.dashboard} data-collapsed={compactSidebar} data-mobile-open={mobileOpen}>
      <a ref={skipLinkRef} href="#content" className={styles.skipLink}>Skip to content</a>
      {mobileOpen && <button type="button" className={styles.sidebarScrim} aria-label="Close menu" tabIndex={-1} onClick={() => setMobileOpen(false)} />}
      <aside ref={sidebarRef} className={styles.sidebar} id="primary-sidebar">
        <div className={styles.sidebarHeader}>
          <div className={styles.sidebarBrand} aria-label="NoX Yard">
            <span className={styles.brandMark} aria-hidden="true">N</span>
            {!compactSidebar && <span className={styles.brandName}>NoX Yard</span>}
          </div>
          <button type="button" className={`${styles.sidebarToggle} ${compactSidebar ? styles.sidebarExpand : ""}`} aria-label={isMobile ? "Close menu" : compactSidebar ? "Expand sidebar" : "Hide sidebar"} title={isMobile ? "Close menu" : compactSidebar ? "Expand sidebar" : "Hide sidebar"} aria-expanded={!compactSidebar} onClick={() => isMobile ? setMobileOpen(false) : changeSidebar(!sidebarCollapsed)}>
            <i className={`ph-bold ${compactSidebar ? "ph-sidebar-simple" : "ph-sidebar"}`} aria-hidden="true" />
          </button>
        </div>
        <nav aria-label="Primary" className={styles.sidebarNav}>
          <a href="#projects" aria-current="page" className={styles.navLink} aria-label="Projects" title={compactSidebar ? "Projects" : undefined} onClick={() => setMobileOpen(false)}>
            <i className="ph-fill ph-squares-four" aria-hidden="true" />{!compactSidebar && <span>Projects</span>}
          </a>
        </nav>
        <div className={styles.sidebarFooter}>
          <button type="button" className={styles.navLink} aria-label="Settings" title={compactSidebar ? "Settings" : undefined} aria-haspopup="dialog" aria-controls="settings-drawer" aria-expanded={settingsOpen} onClick={() => { setMobileOpen(false); setSettingsOpen(true); }}>
            <i className="ph-bold ph-sliders-horizontal" aria-hidden="true" />{!compactSidebar && <span>Settings</span>}
          </button>
          <div className={styles.accountRow}>
            {compactSidebar
              ? <div className={styles.accountCompact}>
                <span className={styles.accountAvatar} aria-hidden="true">{displayName.charAt(0)}</span>
                <button type="button" onClick={handleSignOut} disabled={pending} className={styles.compactSignOut} aria-label={`Sign out ${displayName}`} title="Sign out"><i className="ph-bold ph-sign-out" aria-hidden="true" /></button>
              </div>
              : <>
                <span className={styles.accountAvatar} aria-hidden="true">{displayName.charAt(0)}</span>
                <span className={styles.accountName} title={displayName}>{displayName}</span>
                <button type="button" onClick={handleSignOut} disabled={pending} className={styles.signOutButton} aria-label="Sign out" title="Sign out"><i className="ph-bold ph-sign-out" aria-hidden="true" /></button>
              </>}
          </div>
        </div>
      </aside>

      <div ref={dashboardBodyRef} className={styles.dashboardBody}>
        <main id="content" tabIndex={-1} className={styles.content}>
          <div className={styles.dashboardHeader}>
            <div className={styles.pageHeading} id="projects">
              <h1 className={styles.visuallyHidden}>Projects</h1>
              <button type="button" className={styles.mobileMenuButton} aria-label="Open menu" aria-controls="primary-sidebar" aria-expanded={mobileOpen} onClick={() => setMobileOpen(true)}><i className="ph-bold ph-list" aria-hidden="true" /></button>
              <button type="button" className={styles.refreshButton} aria-label={refreshing ? "Refreshing projects" : "Refresh projects"} title={refreshing ? "Refreshing projects" : "Refresh projects"} onClick={() => setRefreshKey((key) => key + 1)} disabled={refreshing}>
                <i className="ph-bold ph-arrows-clockwise" aria-hidden="true" />
              </button>
            </div>
            {projects && projects.length > 0 && <div className={styles.inventoryMeta}>
              <span>{projects.length} {projects.length === 1 ? "project" : "projects"} · {projects.reduce((sum, project) => sum + project.containers.length, 0)} containers</span>
              <span>{collectedAt && `Updated ${new Date(collectedAt).toLocaleTimeString()}`}</span>
            </div>}
          </div>

          {error && <p className={styles.formError} role="alert">{error}</p>}
          {inventoryError && <p className={styles.inventoryError} role="alert">{inventoryError}</p>}
          {projects === null && !inventoryError && <div className={styles.emptyPanel} role="status">Loading Docker projects…</div>}
          {projects?.length === 0 && <section className={styles.emptyPanel} aria-labelledby="empty-title">
            <div className={styles.emptyIcon}><i className="ph-fill ph-squares-four" aria-hidden="true" /></div>
            <h2 id="empty-title">No containers found</h2>
            <p>Compose projects and standalone containers on this host will appear here.</p>
          </section>}
          {projects && projects.length > 0 && <>
            <section className={styles.projectGrid} aria-label="Docker projects">
              {projects.map((project) => <button
                key={project.id}
                type="button"
                className={styles.projectCard}
                aria-haspopup="dialog"
                aria-controls="project-drawer"
                aria-expanded={selectedID === project.id}
                onClick={() => setSelectedID(project.id)}
              >
                <span className={styles.cardTopline}>
                  <span className={styles.cardKind}>{project.kind === "external-compose" ? "COMPOSE" : "CONTAINER"}</span>
                  <span className={`${styles.statusBadge} ${statusClass(project.state)}`}>{project.state}</span>
                </span>
                <span className={styles.cardName}>{project.name}</span>
                <span className={styles.cardSubline}>{project.containers.length} {project.containers.length === 1 ? "container" : "containers"} · Health: {healthLabel(project.health)}</span>
                <span className={styles.cardMetrics}>
                  <Metric label="CPU" value={formatCPU(project.cpuPercent)} />
                  <Metric label="Memory" value={formatBytes(project.memoryBytes)} />
                  <Metric label="Uptime" value={formatUptime(project.uptimeSeconds)} />
                </span>
              </button>)}
            </section>
          </>}
        </main>
      </div>
      <ProjectDrawer project={selected} csrfToken={csrfToken} onChanged={() => setRefreshKey((key) => key + 1)} onClose={() => setSelectedID(null)} />
      <SettingsDrawer open={settingsOpen} csrfToken={csrfToken} onClose={() => setSettingsOpen(false)} />
    </div>
  );
}

type RemovalTarget = { kind: "project" | "container"; id: string; name: string };

function ProjectDrawer({ project, csrfToken, onChanged, onClose }: { project?: Project; csrfToken: string; onChanged: () => void; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const detailsTabRef = useRef<HTMLButtonElement>(null);
  const logsTabRef = useRef<HTMLButtonElement>(null);
  const terminalTabRef = useRef<HTMLButtonElement>(null);
  const backButtonRef = useRef<HTMLButtonElement>(null);
  const containerButtonsRef = useRef<Record<string, HTMLButtonElement | null>>({});
  const lastSelectionRef = useRef<string | null>(null);
  const tabsID = useId();
  const dismiss = useCallback(() => dialogRef.current?.close(), []);
  const [selectedContainerID, setSelectedContainerID] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<"details" | "logs" | "terminal">("details");
  const [busyTarget, setBusyTarget] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState("");
  const [actionError, setActionError] = useState("");
  const [removalTarget, setRemovalTarget] = useState<RemovalTarget | null>(null);
  useDrawerSwipe(dialogRef, "right", Boolean(project), dismiss);

  useEffect(() => {
    setActionMessage("");
    setActionError("");
    setSelectedContainerID(null);
    setActiveTab("details");
    lastSelectionRef.current = null;
  }, [project?.id]);

  useEffect(() => {
    if (selectedContainerID) backButtonRef.current?.focus();
    else if (lastSelectionRef.current) containerButtonsRef.current[lastSelectionRef.current]?.focus();
  }, [selectedContainerID]);

  const activeContainer = project?.containers.length === 1
    ? project.containers[0]
    : project?.containers.find((container) => container.id === selectedContainerID);

  function selectTab(tab: "details" | "logs" | "terminal", focus = false) {
    setActiveTab(tab);
    contentRef.current?.scrollTo({ top: 0 });
    if (focus) (tab === "details" ? detailsTabRef : tab === "logs" ? logsTabRef : terminalTabRef).current?.focus();
  }

  function showContainer(id: string | null) {
    if (id) lastSelectionRef.current = id;
    setSelectedContainerID(id);
    setActionMessage("");
    setActionError("");
    contentRef.current?.scrollTo({ top: 0 });
  }

  async function runAction(target: "project" | "container", id: string, action: LifecycleAction) {
    setBusyTarget(id);
    setActionMessage(`${action === "restart" ? "Restarting" : action === "stop" ? "Stopping" : "Starting"}…`);
    setActionError("");
    try {
      const result = target === "project"
        ? await runProjectAction(id, action, csrfToken)
        : await runContainerAction(id, action, csrfToken);
      const label = target === "project" ? "project" : "container";
      const verb: Record<LifecycleAction, string> = { start: "started", stop: "stopped", restart: "restarted" };
      const summary = `${result.succeeded} ${result.succeeded === 1 ? "container" : "containers"} ${verb[action]}; ${result.skipped} skipped.`;
      setActionMessage(result.queued && !result.succeeded ? "NoX Yard restart queued. Check its health after reconnecting." : `${summary}${result.queued ? " NoX Yard restart queued." : ""}`);
      if (result.failed) setActionError(result.errors?.join(" ") || `${result.failed} ${label} actions failed.`);
      onChanged();
      if (result.queued) window.setTimeout(onChanged, 12_000);
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : "Unable to complete the action.");
      onChanged();
    } finally {
      setBusyTarget(null);
    }
  }

  async function runMaintenance(target: "project" | "container", id: string) {
    setBusyTarget(id);
    setActionMessage("Pulling images… Containers will keep running.");
    setActionError("");
    try {
      const result: MaintenanceResult = target === "project" ? await pullProjectImages(id, csrfToken) : await pullContainerImage(id, csrfToken);
      setActionMessage(result.succeeded ? `Pulled ${result.succeeded} ${result.succeeded === 1 ? "image" : "images"}. Running containers were not changed.` : "");
      if (result.failed) setActionError(result.errors?.join(" ") || `${result.failed} operations failed.`);
    } catch (cause) {
      setActionMessage("");
      setActionError(cause instanceof Error ? cause.message : "Unable to complete the operation.");
      onChanged();
    } finally {
      setBusyTarget(null);
    }
  }

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (project && !dialog.open) dialog.showModal();
    if (!project && dialog.open) dialog.close();
  }, [project]);

  return <><dialog
    id="project-drawer"
    ref={dialogRef}
    className={`${styles.settingsDrawer} ${styles.projectDrawer}`}
    aria-labelledby="project-drawer-title"
    onClose={onClose}
    onClick={dismissDrawerBackdrop}
    {...{ closedby: "any" }}
  >
    {project && <div className={styles.settingsBody}>
      <header className={styles.settingsHeader}>
        <h2 id="project-drawer-title">{project.name}</h2>
        <button type="button" className={styles.closeButton} autoFocus onClick={() => dialogRef.current?.close()} aria-label="Close project details">×</button>
      </header>
      <div className={styles.projectTabs} role="tablist" aria-label={`${project.name} sections`} onKeyDown={(event) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        const tabs = ["details", "logs", "terminal"] as const;
        const index = tabs.indexOf(activeTab);
        selectTab(event.key === "Home" ? "details" : event.key === "End" ? "terminal" : tabs[(index + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length], true);
      }}>
        <button ref={detailsTabRef} id={`${tabsID}-details-tab`} type="button" role="tab" aria-selected={activeTab === "details"} aria-controls={`${tabsID}-details-panel`} tabIndex={activeTab === "details" ? 0 : -1} className={styles.projectTab} onClick={() => selectTab("details")}>Details</button>
        <button ref={logsTabRef} id={`${tabsID}-logs-tab`} type="button" role="tab" aria-selected={activeTab === "logs"} aria-controls={`${tabsID}-logs-panel`} tabIndex={activeTab === "logs" ? 0 : -1} className={styles.projectTab} onClick={() => selectTab("logs")}>Logs</button>
        <button ref={terminalTabRef} id={`${tabsID}-terminal-tab`} type="button" role="tab" aria-selected={activeTab === "terminal"} aria-controls={`${tabsID}-terminal-panel`} tabIndex={activeTab === "terminal" ? 0 : -1} className={styles.projectTab} onClick={() => selectTab("terminal")}>Terminal</button>
      </div>
      <div ref={contentRef} id={`${tabsID}-${activeTab}-panel`} role="tabpanel" aria-labelledby={`${tabsID}-${activeTab}-tab`} className={`${styles.settingsContent} ${styles.projectDrawerContent}`}>
        {activeTab === "details" ? <>
        {activeContainer && project.kind === "external-compose" && project.containers.length > 1 && <button ref={backButtonRef} type="button" className={styles.backButton} onClick={() => showContainer(null)}>← Back to project</button>}
        {!activeContainer && <>
          {project.kind === "external-compose" && <LifecycleControls
            name={project.name}
            state={project.state}
            selfTarget={project.containers.some((container) => project.name === "nox-yard" && container.service === "nox-yard")}
            busy={busyTarget !== null}
            onRun={(action) => runAction("project", project.id, action)}
            pullLabel="Pull project images"
            removeLabel="Remove project"
            onPull={() => { void runMaintenance("project", project.id); }}
            onRemove={() => setRemovalTarget({ kind: "project", id: project.id, name: project.name })}
          />}
          <section className={styles.projectOverview} aria-label="Project overview">
            <p className={styles.detailSummary}>
              {project.containers.length} {project.containers.length === 1 ? "container" : "containers"} · {project.state} · Health: {healthLabel(project.health)}
            </p>
            <div className={styles.projectOverviewMetrics}>
              <Metric label="CPU" value={formatCPU(project.cpuPercent)} />
              <Metric label="Memory" value={formatBytes(project.memoryBytes)} />
              <Metric label="Uptime" value={formatUptime(project.uptimeSeconds)} />
            </div>
          </section>
        </>}
        {actionMessage && <p className={styles.actionMessage} role="status">{actionMessage}</p>}
        {actionError && <p className={styles.inventoryError} role="alert">{actionError}</p>}
        {activeContainer
          ? <ContainerDetails
              key={activeContainer.id}
              container={activeContainer}
              csrfToken={csrfToken}
              busy={busyTarget !== null}
              selfTarget={project.name === "nox-yard" && activeContainer.service === "nox-yard"}
              helperTarget={activeContainer.name.startsWith("nox-yard-update-")}
              onRun={(action) => runAction("container", activeContainer.id, action)}
              pullLabel={project.kind === "external-compose" && project.containers.length === 1 ? "Pull project image" : "Pull image"}
              removeLabel={project.kind === "external-compose" && project.containers.length === 1 ? "Remove project" : "Remove container"}
              onPull={() => { void runMaintenance(project.kind === "external-compose" && project.containers.length === 1 ? "project" : "container", project.kind === "external-compose" && project.containers.length === 1 ? project.id : activeContainer.id); }}
              onRemove={() => setRemovalTarget(project.kind === "external-compose" && project.containers.length === 1
                ? { kind: "project", id: project.id, name: project.name }
                : { kind: "container", id: activeContainer.id, name: activeContainer.name })}
            />
          : <section className={styles.containerPicker} aria-labelledby={`${tabsID}-containers-title`}>
              <h3 id={`${tabsID}-containers-title`}>Containers</h3>
              <div className={styles.containerList}>
                {project.containers.map((container) => <button key={container.id} ref={(element) => { containerButtonsRef.current[container.id] = element; }} type="button" className={styles.containerChoice} onClick={() => showContainer(container.id)}>
                  <span className={styles.containerChoiceName}><strong>{container.service || container.name}</strong>{container.service && <small>{container.name}</small>}</span>
                  <span className={`${styles.statusBadge} ${statusClass(container.state)}`}>{container.state}</span>
                  <span className={styles.containerChoiceArrow} aria-hidden="true">›</span>
                </button>)}
              </div>
            </section>}
        </> : activeTab === "logs"
          ? <LogsPanel key={project.id} project={project} preferredContainerID={activeContainer?.id} onSelectContainer={setSelectedContainerID} />
          : <TerminalPanel key={project.id} project={project} preferredContainerID={activeContainer?.id} csrfToken={csrfToken} onSelectContainer={setSelectedContainerID} />}
      </div>
    </div>}
  </dialog>
    <RemoveConfirmation target={removalTarget} csrfToken={csrfToken} onClose={() => setRemovalTarget(null)} onChanged={onChanged} />
  </>;
}

type LogLine = { stream: "stdout" | "stderr"; text: string };
type LogStatus = "connecting" | "live" | "reconnecting" | "ended" | "paused" | "error";

function LogsPanel({ project, preferredContainerID, onSelectContainer }: {
  project: Project;
  preferredContainerID?: string;
  onSelectContainer: (id: string) => void;
}) {
  const [selectedID, setSelectedID] = useState(preferredContainerID || project.containers[0]?.id || "");
  const [lines, setLines] = useState<LogLine[]>([]);
  const [status, setStatus] = useState<LogStatus>("connecting");
  const [error, setError] = useState("");
  const [retryKey, setRetryKey] = useState(0);
  const [visible, setVisible] = useState(document.visibilityState === "visible");
  const [followOutput, setFollowOutput] = useState(true);
  const outputRef = useRef<HTMLPreElement>(null);
  const container = project.containers.find((item) => item.id === selectedID) || project.containers[0];

  useEffect(() => {
    const sync = () => setVisible(document.visibilityState === "visible");
    document.addEventListener("visibilitychange", sync);
    return () => document.removeEventListener("visibilitychange", sync);
  }, []);

  useEffect(() => {
    if (!container || !visible) {
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
            !("stream" in line) || (line.stream !== "stdout" && line.stream !== "stderr")) return;
        setLines((current) => [...current, line as LogLine].slice(-500));
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
    source.onerror = () => {
      if (source.readyState !== EventSource.CLOSED) {
        setStatus("reconnecting");
        setError("Cannot connect to logs. Check Docker access or the container's logging driver.");
      }
    };
    return () => source.close();
  }, [container?.id, retryKey, visible]);

  useEffect(() => {
    if (followOutput && outputRef.current) outputRef.current.scrollTop = outputRef.current.scrollHeight;
  }, [lines, followOutput]);

  const statusLabels: Record<LogStatus, string> = {
    connecting: "Connecting…", live: "Live", reconnecting: "Reconnecting…",
    ended: "Stream ended", paused: "Paused while this tab is hidden", error: "Disconnected",
  };

  if (!container) return <p className={styles.detailSummary}>No containers are available for logs.</p>;

  return <section className={styles.logsPanel} aria-label="Container logs">
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
    <p className={styles.logStatus} role="status">{statusLabels[status]}</p>
    {error && <p className={styles.inventoryError} role="alert">{error}</p>}
    <pre ref={outputRef} className={styles.logOutput} aria-label={`${container.service || container.name} log output`} onScroll={(event) => {
      const output = event.currentTarget;
      setFollowOutput(output.scrollHeight - output.scrollTop - output.clientHeight < 32);
    }}>{lines.length ? lines.map((line, index) => <span key={index} className={line.stream === "stderr" ? styles.logErrorLine : undefined}>{line.text}{"\n"}</span>) : status === "ended" ? "No recent logs.\n" : "Waiting for log output…\n"}</pre>
    <label className={styles.followOutput}><input type="checkbox" checked={followOutput} onChange={(event) => setFollowOutput(event.target.checked)} /> Follow output</label>
  </section>;
}

function ContainerDetails({ container, csrfToken, busy, selfTarget, helperTarget, onRun, pullLabel, removeLabel, onPull, onRemove }: {
  container: Container;
  csrfToken: string;
  busy: boolean;
  selfTarget: boolean;
  helperTarget: boolean;
  onRun: (action: LifecycleAction) => Promise<void>;
  pullLabel: string;
  removeLabel: string;
  onPull: () => void;
  onRemove: () => void;
}) {
  const revealController = useRef<AbortController | null>(null);
  const [inspection, setInspection] = useState<ContainerInspection | null>(null);
  const [detailError, setDetailError] = useState("");
  const [loading, setLoading] = useState(true);
  const [revealing, setRevealing] = useState(false);
  const [revealed, setRevealed] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    setInspection(null);
    setDetailError("");
    setLoading(true);
    setRevealed(false);
    setRevealing(false);
    void getContainerInspection(container.id, controller.signal)
      .then((detail) => { if (!controller.signal.aborted) setInspection(detail); })
      .catch((cause) => {
        if (!controller.signal.aborted) setDetailError(cause instanceof Error ? cause.message : "Unable to inspect container.");
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => {
      controller.abort();
      revealController.current?.abort();
    };
  }, [container.id]);

  async function revealEnvironment() {
    const controller = new AbortController();
    revealController.current = controller;
    setRevealing(true);
    setDetailError("");
    try {
      const detail = await revealContainerEnvironment(container.id, csrfToken, controller.signal);
      if (!controller.signal.aborted) {
        setInspection(detail);
        setRevealed(true);
      }
    } catch (cause) {
      if (!controller.signal.aborted) setDetailError(cause instanceof Error ? cause.message : "Unable to reveal environment values.");
    } finally {
      if (!controller.signal.aborted) setRevealing(false);
    }
  }

  function hideEnvironment() {
    setInspection((detail) => detail && {
      ...detail,
      environment: detail.environment.map(({ name }) => ({ name })),
    });
    setRevealed(false);
  }

  return <section className={styles.containerDetails} aria-label={`${container.service || container.name} details`}>
    <LifecycleControls name={container.service || container.name} state={container.state} selfTarget={selfTarget} helperTarget={helperTarget} busy={busy} onRun={onRun} pullLabel={pullLabel} removeLabel={removeLabel} onPull={onPull} onRemove={onRemove} />
    <ContainerRow container={container} />
    {loading && <p className={styles.detailSummary} role="status">Loading container details…</p>}
    {detailError && <p className={styles.inventoryError} role="alert">{detailError}</p>}
    {inspection?.id === container.id && <ContainerInspectionView
      inspection={inspection}
      revealed={revealed}
      revealing={revealing}
      onReveal={() => { void revealEnvironment(); }}
      onHide={hideEnvironment}
    />}
  </section>;
}

function LifecycleControls({ name, state, selfTarget = false, helperTarget = false, busy, onRun, pullLabel, removeLabel, onPull, onRemove }: {
  name: string;
  state: string;
  selfTarget?: boolean;
  helperTarget?: boolean;
  busy: boolean;
  onRun: (action: LifecycleAction) => Promise<void>;
  pullLabel: string;
  removeLabel: string;
  onPull: () => void;
  onRemove: () => void;
}) {
  const [confirming, setConfirming] = useState<"stop" | "restart" | null>(null);
  const running = state === "running" || state === "partial";

  return <div className={styles.lifecycleControls}>
    <div className={styles.actionRow} aria-label={`${name} actions`}>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Start ${name}`} title={`Start ${name}`} disabled={busy || state === "running"} onClick={() => { void onRun("start"); }}><i className="ph-fill ph-play" aria-hidden="true" /></button>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Stop ${name}`} title={`Stop ${name}`} disabled={busy || !running || selfTarget || helperTarget} onClick={() => setConfirming("stop")}><i className="ph-fill ph-stop" aria-hidden="true" /></button>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Restart ${name}`} title={`Restart ${name}`} disabled={busy || !running || helperTarget} onClick={() => setConfirming("restart")}><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" /></button>
      <MaintenanceMenu busy={busy} pullLabel={pullLabel} removeLabel={removeLabel} canPull={!helperTarget && !selfTarget} canRemove={!helperTarget && !selfTarget} onPull={onPull} onRemove={onRemove} />
    </div>
    {selfTarget && <p className={styles.actionHint}>NoX Yard cannot stop or remove itself. Use Settings for self-update; restart uses a temporary helper.</p>}
    {helperTarget && <p className={styles.actionHint}>Maintenance helpers are managed automatically.</p>}
    {confirming && <div className={styles.actionConfirm} role="group" aria-label={`Confirm ${confirming}`}>
      <p>{confirming === "stop" ? "Stop" : "Restart"} <strong>{name}</strong>?</p>
      <div className={styles.actionRow}>
        <button type="button" className={styles.inspectButton} disabled={busy} onClick={() => setConfirming(null)}>Cancel</button>
        <button type="button" className={`${styles.inspectButton} ${confirming === "stop" ? styles.dangerAction : ""}`} disabled={busy} onClick={() => { const action = confirming; if (!action) return; setConfirming(null); void onRun(action); }}>Confirm {confirming}</button>
      </div>
    </div>}
  </div>;
}

function MaintenanceMenu({ busy, pullLabel, removeLabel, canPull, canRemove, onPull, onRemove }: {
  busy: boolean;
  pullLabel: string;
  removeLabel: string;
  canPull: boolean;
  canRemove: boolean;
  onPull: () => void;
  onRemove: () => void;
}) {
  const ref = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    function closeOutside(event: PointerEvent) {
      if (ref.current && event.target instanceof Node && !ref.current.contains(event.target)) ref.current.open = false;
    }
    document.addEventListener("pointerdown", closeOutside);
    return () => document.removeEventListener("pointerdown", closeOutside);
  }, []);
  useEffect(() => { if (busy && ref.current) ref.current.open = false; }, [busy]);

  return <details ref={ref} className={styles.maintenanceMenu} onKeyDown={(event) => {
    if (event.key === "Escape" && ref.current?.open) {
      event.preventDefault();
      ref.current.open = false;
      ref.current.querySelector("summary")?.focus();
    }
  }}>
    <summary className={styles.inspectButton} aria-label="More actions" aria-disabled={busy} onClick={(event) => { if (busy) event.preventDefault(); }} title="More actions">···</summary>
    <div className={styles.maintenanceOptions}>
      <button type="button" disabled={busy || !canPull} onClick={() => { if (ref.current) ref.current.open = false; onPull(); }}>{pullLabel}</button>
      <button type="button" className={styles.maintenanceRemove} disabled={busy || !canRemove} onClick={() => { if (ref.current) ref.current.open = false; onRemove(); }}>{removeLabel}</button>
    </div>
  </details>;
}

function RemoveConfirmation({ target, csrfToken, onClose, onChanged }: {
  target: RemovalTarget | null;
  csrfToken: string;
  onClose: () => void;
  onChanged: () => void;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleID = useId();
  const [plan, setPlan] = useState<RemovalPlan | null>(null);
  const [report, setReport] = useState<RemovalReport | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [removing, setRemoving] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (target && !dialog.open) dialog.showModal();
    if (!target && dialog.open) dialog.close();
  }, [target]);

  useEffect(() => {
    if (!target) return;
    let current = true;
    setPlan(null);
    setReport(null);
    setError("");
    setLoading(true);
    const load = target.kind === "project" ? previewRemoveProject(target.id) : previewRemoveContainer(target.id);
    void load.then((result) => { if (current) setPlan(result); })
      .catch((cause: unknown) => { if (current) setError(cause instanceof Error ? cause.message : "Unable to inspect Docker resources."); })
      .finally(() => { if (current) setLoading(false); });
    return () => { current = false; };
  }, [target]);

  async function confirm() {
    if (!target || !plan || removing) return;
    setRemoving(true);
    setError("");
    try {
      const result = target.kind === "project"
        ? await removeProject(target.id, plan.fingerprint, csrfToken)
        : await removeContainer(target.id, plan.fingerprint, csrfToken);
      setReport(result);
      onChanged();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to complete removal.");
      // A changed preview must be reviewed again before a new attempt.
      setPlan(null);
    } finally {
      setRemoving(false);
    }
  }

  const removed = report?.items.filter((item) => item.status === "removed").length ?? 0;
  const retained = report?.items.filter((item) => item.status === "retained").length ?? 0;
  const failed = report?.items.filter((item) => item.status === "failed").length ?? 0;

  return <dialog ref={dialogRef} className={styles.confirmDialog} aria-labelledby={titleID} onClose={onClose} onCancel={(event) => { if (removing) event.preventDefault(); }}>
    {target && <div className={styles.confirmBody}>
      <h2 id={titleID}>{report ? "Removal report" : `Remove ${target.kind}?`}</h2>
      <p><strong>{target.name}</strong></p>
      {loading && <p role="status">Inspecting containers, volumes, networks, and images…</p>}
      {error && <p className={styles.inventoryError} role="alert">{error}</p>}
      {plan && !report && <>
        <p>NoX Yard will stop and remove the selected containers, then attempt to delete the volumes, networks, and images marked "will remove" below. Items marked "will keep" remain on the host for the stated reason.</p>
        <RemovalItems items={plan.items.map((item) => ({ ...item, status: item.action === "remove" ? "will remove" : "will keep" }))} />
      </>}
      {report && <>
        <p role="status">{failed || retained ? "Removal incomplete." : "Removal complete."} {removed} removed · {retained} retained · {failed} failed.</p>
        <RemovalItems items={report.items} />
      </>}
      <div className={styles.confirmActions}>
        <button type="button" className={styles.inspectButton} autoFocus disabled={removing} onClick={onClose}>{report ? "Close report" : "Cancel"}</button>
        {plan && !report && <button type="button" className={`${styles.inspectButton} ${styles.dangerAction}`} disabled={loading || removing} onClick={() => { void confirm(); }}>{removing ? "Removing…" : `Remove ${target.kind}`}</button>}
      </div>
    </div>}
  </dialog>;
}

function RemovalItems({ items }: { items: { kind: string; id: string; name: string; status: string; reason?: string }[] }) {
  return <ul className={styles.removalList} aria-label="Resources">
    {items.map((item) => <li key={`${item.kind}:${item.id}`} className={styles.removalItem} data-status={item.status}>
      <div><strong>{item.kind}</strong><span className={styles.removalStatus}>{item.status}</span></div>
      <code>{item.name || item.id}</code>
      {item.reason && <small>{item.reason}</small>}
    </li>)}
  </ul>;
}

const checkIntervals = [5, 15, 30, 60, 360] as const;

function SettingsDrawer({ open, csrfToken, onClose }: { open: boolean; csrfToken: string; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const dismiss = useCallback(() => dialogRef.current?.close(), []);
  useDrawerSwipe(dialogRef, "left", open, dismiss);
  const [status, setStatus] = useState<SelfUpdateStatus | null>(null);
  const [automatic, setAutomatic] = useState(false);
  const [intervalMinutes, setIntervalMinutes] = useState(15);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [checkingNow, setCheckingNow] = useState(false);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    if (!open && dialog.open) dialog.close();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    setStatus(null);
    setError("");
    let active = true;
    let initialized = false;
    const controller = new AbortController();
    async function refresh() {
      if (document.visibilityState === "hidden") return;
      try {
        const current = await getSelfUpdateStatus(controller.signal);
        if (active) {
          setStatus(current);
          setError("");
          if (!initialized) {
            setAutomatic(current.automatic);
            setIntervalMinutes(current.intervalMinutes);
            initialized = true;
          }
        }
      } catch (cause) {
        if (active && !(cause instanceof DOMException && cause.name === "AbortError")) {
          setError(cause instanceof Error ? cause.message : "Unable to load update status.");
        }
      }
    }
    void refresh();
    const interval = window.setInterval(() => { void refresh(); }, 20_000);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      active = false;
      controller.abort();
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [open]);

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSaving(true);
    setError("");
    try {
      setStatus(await saveSelfUpdateSettings({ automatic, intervalMinutes }, csrfToken));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to save update setting.");
    } finally {
      setSaving(false);
    }
  }

  async function checkNow() {
    setCheckingNow(true);
    setError("");
    try {
      setStatus(await checkSelfUpdateNow(csrfToken));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "Unable to start update check.");
    } finally {
      setCheckingNow(false);
    }
  }

  const labels: Record<SelfUpdateStatus["status"], string> = {
    not_checked: "Not checked",
    checking: "Checking",
    up_to_date: "Up to date",
    updating: "Updating",
    update_failed: "Update failed",
  };

  const busy = saving || checkingNow || status?.status === "checking" || status?.status === "updating";
  const unchanged = status?.automatic === automatic && status?.intervalMinutes === intervalMinutes;

  return <dialog
    id="settings-drawer"
    ref={dialogRef}
    className={styles.settingsDrawer}
    aria-labelledby="settings-title"
    onClose={onClose}
    onClick={dismissDrawerBackdrop}
    {...{ closedby: "any" }}
  >
    <div className={styles.settingsBody}>
      <header className={styles.settingsHeader}>
        <div>
          <span className={styles.sectionLabel}>PREFERENCES</span>
          <h2 id="settings-title">Settings</h2>
        </div>
        <button type="button" className={styles.closeButton} autoFocus onClick={() => dialogRef.current?.close()} aria-label="Close settings">×</button>
      </header>
      <div className={styles.settingsContent}>
        <section aria-labelledby="updates-title" className={styles.settingsSection}>
          <h3 id="updates-title">NoX Yard updates</h3>
          <p>Check the public GHCR image and install new builds automatically.</p>
          <form action="/api/self-update" method="post" onSubmit={(event) => { void save(event); }} className={styles.settingsForm}>
            <label className={styles.settingsCheckbox}>
              <input type="checkbox" name="automatic" checked={automatic} disabled={!status || busy} onChange={(event) => setAutomatic(event.target.checked)} />
              <span>Automatic updates <strong>{automatic ? "On" : "Off"}</strong></span>
            </label>
            <fieldset className={styles.intervalFieldset} disabled={!status || busy}>
              <legend>Check for updates every</legend>
              <div className={styles.intervalChoices}>
                {checkIntervals.map((minutes) => <label key={minutes} className={styles.intervalChoice}>
                  <input type="radio" name="intervalMinutes" value={minutes} checked={intervalMinutes === minutes} onChange={() => setIntervalMinutes(minutes)} />
                  <span>{minutes === 360 ? "6 hours" : minutes === 60 ? "1 hour" : `${minutes} min`}</span>
                </label>)}
                <button type="button" className={styles.checkNowButton} disabled={!status || busy} onClick={() => { void checkNow(); }}>
                  Check now
                </button>
              </div>
            </fieldset>
            <button type="submit" className={styles.primaryButton} disabled={!status || busy || unchanged}>
              {saving ? "Saving…" : "Save settings"}
            </button>
          </form>
        </section>
        <section aria-label="Update status" className={styles.settingsSection}>
          <h3>Current status</h3>
          <dl className={styles.updateFacts} aria-live="polite">
            <div><dt>Status</dt><dd>{status ? labels[status.status] : "Loading"}</dd></div>
            <div><dt>Last checked</dt><dd>{status?.lastChecked ? new Date(status.lastChecked).toLocaleString() : "Never"}</dd></div>
            {status?.currentBuildSHA && <div><dt>Current build</dt><dd title={status.currentBuildSHA}>{status.currentBuildSHA.slice(0, 12)}</dd></div>}
          </dl>
          {status?.status === "update_failed" && status.error && <p className={styles.updateError} role="alert">{status.error}</p>}
          {error && <p className={styles.updateError} role="alert">{error}</p>}
        </section>
      </div>
    </div>
  </dialog>;
}

function dismissDrawerBackdrop(event: MouseEvent<HTMLDialogElement>) {
  if (event.target !== event.currentTarget) return;
  const bounds = event.currentTarget.getBoundingClientRect();
  if (event.clientX < bounds.left || event.clientX > bounds.right ||
      event.clientY < bounds.top || event.clientY > bounds.bottom) {
    event.currentTarget.close();
  }
}

function Metric({ label, value }: { label: string; value: string }) {
  return <span className={styles.metric}><span>{label}</span><strong>{value}</strong></span>;
}

function ContainerRow({ container }: { container: Container }) {
  return <article className={styles.containerRow}>
    <div className={styles.containerHeading}>
      <div><h3>{container.service || container.name}</h3>{container.service && <span>{container.name}</span>}</div>
      <span className={`${styles.statusBadge} ${statusClass(container.state)}`}>{container.state}</span>
    </div>
    <p className={styles.imageName} title={container.image}>{container.image}</p>
    <p className={styles.containerHealth}>Health: {healthLabel(container.health)}</p>
    <div className={styles.containerMetrics}>
      <Metric label="CPU" value={formatCPU(container.cpuPercent)} />
      <Metric label="Memory" value={formatBytes(container.memoryBytes)} />
      <Metric label="Uptime" value={formatUptime(container.uptimeSeconds)} />
      <Metric label="Network ↓ / ↑" value={`${formatBytes(container.networkRxBytes)} / ${formatBytes(container.networkTxBytes)}`} />
    </div>
  </article>;
}

function ContainerInspectionView({
  inspection, revealed, revealing, onReveal, onHide,
}: {
  inspection: ContainerInspection;
  revealed: boolean;
  revealing: boolean;
  onReveal: () => void;
  onHide: () => void;
}) {
  const sectionID = useId();
  return <>
    <p className={styles.containerID}>Container ID <code>{inspection.id}</code></p>
    <section className={styles.inspectSection} aria-labelledby={`${sectionID}-ports-title`}>
      <h3 id={`${sectionID}-ports-title`}>Ports</h3>
      {inspection.ports.length === 0 ? <p>None configured</p> : <ul className={styles.inspectList}>
        {inspection.ports.map((port, index) => <li key={`${port.containerPort}-${port.hostIP}-${port.hostPort}-${index}`}>
          <strong>{port.containerPort}</strong>
          <span>{port.hostPort ? `${port.hostIP || "All interfaces"}:${port.hostPort}` : "Not published"}</span>
        </li>)}
      </ul>}
    </section>
    <section className={styles.inspectSection} aria-labelledby={`${sectionID}-mounts-title`}>
      <h3 id={`${sectionID}-mounts-title`}>Volumes and mounts</h3>
      {inspection.mounts.length === 0 ? <p>None configured</p> : <ul className={styles.inspectList}>
        {inspection.mounts.map((mount, index) => <li key={`${mount.destination}-${index}`}>
          <strong>{mount.destination}</strong>
          <span>{mount.type} · {mount.source || "Temporary"} · {mount.readOnly ? "Read only" : "Read/write"}</span>
        </li>)}
      </ul>}
    </section>
    <section className={styles.inspectSection} aria-labelledby={`${sectionID}-networks-title`}>
      <h3 id={`${sectionID}-networks-title`}>Networks</h3>
      {inspection.networks.length === 0 ? <p>None attached</p> : <ul className={styles.inspectList}>
        {inspection.networks.map((network) => <li key={network.name}>
          <strong>{network.name}</strong>
          <span>{[network.ipv4, network.ipv6].filter(Boolean).join(" · ") || "No assigned IP"}</span>
        </li>)}
      </ul>}
    </section>
    <section className={styles.inspectSection} aria-labelledby={`${sectionID}-environment-title`}>
      <div className={styles.inspectSectionHeading}>
        <h3 id={`${sectionID}-environment-title`}>Environment variables</h3>
        {inspection.environment.length > 0 && <button type="button" className={styles.inspectButton} disabled={revealing} onClick={revealed ? onHide : onReveal}>
          {revealing ? "Revealing…" : revealed ? "Hide values" : "Reveal values"}
        </button>}
      </div>
      {inspection.environment.length === 0 ? <p>None configured</p> : <dl className={styles.environmentList}>
        {inspection.environment.map((variable, index) => <div key={`${variable.name}-${index}`}>
          <dt>{variable.name}</dt>
          <dd><code>{revealed ? variable.value || "(empty)" : "••••••"}</code></dd>
        </div>)}
      </dl>}
    </section>
  </>;
}

function statusClass(state: string): string {
  if (state === "running") return styles.statusRunning;
  if (state === "stopped" || state === "exited" || state === "dead") return styles.statusStopped;
  return styles.statusPartial;
}

function healthLabel(health: string): string {
  return health === "none" ? "No check" : health;
}

function formatCPU(value: number | null): string {
  return value === null ? "—" : `${value.toFixed(1)}%`;
}

function formatBytes(value: number | null): string {
  if (value === null) return "—";
  if (value < 1024) return `${value} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)) - 1, units.length - 1);
  return `${(value / 1024 ** (index + 1)).toFixed(1)} ${units[index]}`;
}

function formatUptime(seconds: number | null): string {
  if (seconds === null) return "—";
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ${Math.floor((seconds % 3600) / 60)}m`;
  return `${Math.floor(seconds / 86400)}d ${Math.floor((seconds % 86400) / 3600)}h`;
}
