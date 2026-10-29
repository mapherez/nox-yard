import { useCallback, useEffect, useId, useRef, useState, type FormEvent, type MouseEvent, type ReactNode } from "react";
import { useProjectConfiguration, ComposeConfiguration, EnvironmentValue, type ConfigurationControl } from "./ProjectConfiguration";
import {
  checkSelfUpdateNow,
  createAdministrator,
  getBootstrap,
  getContainerInspection,
	getManagedSettings,
  getSelfUpdateStatus,
  pullContainerImage,
  pullProjectImages,
  previewRemoveContainer,
  previewRemoveProject,
  removeContainer,
  removeProject,
	saveManagedSettings,
  revealContainerEnvironment,
  runContainerAction,
  runProjectAction,
  runManagedOperation,
  signIn,
  signOut,
  sessionExpiredEvent,
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
import settingsStyles from "./SettingsDrawer.module.css";
import { MachineAccessSettings, MCPIndicator } from "./MachineSettings";
import { getMachineStatus, type MachineStatus } from "./api";
import { useDrawerSwipe } from "./useDrawerSwipe";
import { TerminalPanel } from "./TerminalPanel";
import { LogsPanel } from "./LogsPanel";
import { NewProjectDrawer } from "./NewProjectDrawer";
import { useProjects } from "./useProjects";
import { useOperationHistory } from "./useOperationHistory";
import { OperationHistory } from "./OperationHistory";
import { ProjectSchedule } from "./ProjectSchedule";
import { RecreateConfirmation, type RecreateTarget } from "./RecreateConfirmation";
import { ConfirmationDialog } from "./ConfirmationDialog";
import { ProjectBrowser } from "./ProjectBrowser";
import { useInterfaceUpdates } from "./useInterfaceUpdates";

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
  useInterfaceUpdates();

  useEffect(() => {
    if (view.kind !== "dashboard") return;
    const expire = () => setView({ kind: "login" });
    window.addEventListener(sessionExpiredEvent, expire);
    return () => window.removeEventListener(sessionExpiredEvent, expire);
  }, [view.kind]);

  useEffect(() => {
    const titles: Record<View["kind"], string> = {
      loading: "Loading | Yard",
      setup: "Create administrator | Yard",
      login: "Sign in | Yard",
      dashboard: "Projects | Yard",
      error: "Unavailable | Yard",
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
            message: error instanceof Error ? error.message : "Unable to load Yard.",
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
        <p className={styles.muted}>Loading Yard…</p>
      </main>
    );
  }

  if (view.kind === "error") {
    return (
      <main className={styles.centerScreen}>
        <Brand />
        <h1>Unable to open Yard</h1>
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

function Brand() {
  return (
    <div className={styles.brand}>
      <span className={styles.brandName}>Yard</span>
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
          <div className={styles.authMobileBrand}><Brand /></div>
          <span className={styles.sectionLabel}>{isSetup ? "FIRST-RUN SETUP" : "WELCOME BACK"}</span>
          <h1>{isSetup ? "Create your admin account" : "Sign in to Yard"}</h1>
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
  const { projects, inventoryError, collectedAt, refreshing, refresh } = useProjects();
  const [selectedProject, setSelectedProject] = useState<Project | null>(null);
  const closeProject = useCallback(() => setSelectedProject(null), []);
  const selectedID = selectedProject?.id ?? null;
  const selectProject = (id: string) => setSelectedProject(projects?.find(project => project.id === id) ?? null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [focusMCP, setFocusMCP] = useState(false);
  const [machineStatus, setMachineStatus] = useState<MachineStatus | null>(null);
  const [machineError, setMachineError] = useState("");
  const machineRequest = useRef<AbortController | null>(null);
  const refreshMachine = useCallback(async () => {
    machineRequest.current?.abort(); const controller = new AbortController(); machineRequest.current = controller;
    try { const current = await getMachineStatus(controller.signal); if (!current.settings) throw new Error("Machine settings are unavailable."); if (!controller.signal.aborted) { setMachineStatus(current); setMachineError(""); } }
    catch (cause) { if (!controller.signal.aborted) { setMachineStatus(null); setMachineError(cause instanceof Error ? cause.message : "Unable to read MCP status."); } }
  }, []);
  useEffect(() => {
    const refresh = () => { if (document.visibilityState !== "hidden") void refreshMachine(); };
    refresh(); document.addEventListener("visibilitychange", refresh);
    return () => { machineRequest.current?.abort(); document.removeEventListener("visibilitychange", refresh); };
  }, [refreshMachine]);
  useEffect(() => {
    if (!machineStatus?.settings.mcpEnabled && !machineStatus?.networkStatus?.pending && machineStatus?.networkStatus?.job?.status !== "running") return;
    const interval = window.setInterval(() => { if (document.visibilityState !== "hidden") void refreshMachine(); }, machineStatus?.networkStatus?.job?.status === "running" ? 3000 : 30000);
    return () => window.clearInterval(interval);
  }, [machineStatus?.settings.mcpEnabled, machineStatus?.networkStatus?.pending, machineStatus?.networkStatus?.job?.status, refreshMachine]);
  const [newProjectOpen, setNewProjectOpen] = useState(false);
  const newProjectButtonRef = useRef<HTMLButtonElement>(null);
  const skipLinkRef = useRef<HTMLAnchorElement>(null);
  const dashboardBodyRef = useRef<HTMLDivElement>(null);
  const sidebarRef = useRef<HTMLElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const menuCloseRef = useRef<HTMLButtonElement>(null);
  const [overlayNavigation, setOverlayNavigation] = useState(() => window.matchMedia("(max-width: 79.99rem)").matches);
  const [menuOpen, setMenuOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try { return window.localStorage.getItem("nox-yard-sidebar-collapsed") === "true"; }
    catch { return false; }
  });
  const compactSidebar = !overlayNavigation && sidebarCollapsed;
  const closeMenu = useCallback(() => {
    setMenuOpen(false);
    requestAnimationFrame(() => {
      if (!document.querySelector("dialog[open]") && menuButtonRef.current?.getClientRects().length) menuButtonRef.current.focus();
    });
  }, []);
  useDrawerSwipe(sidebarRef, "left", overlayNavigation && menuOpen, closeMenu);

  const changeSidebar = useCallback((collapsed: boolean) => {
    setSidebarCollapsed(collapsed);
    try { window.localStorage.setItem("nox-yard-sidebar-collapsed", String(collapsed)); }
    catch { /* The layout still works when browser storage is unavailable. */ }
  }, []);

  useEffect(() => {
    const media = window.matchMedia("(max-width: 79.99rem)");
    const sync = () => {
      setOverlayNavigation(media.matches);
      setMenuOpen(false);
    };
    media.addEventListener("change", sync);
    return () => media.removeEventListener("change", sync);
  }, []);

  useEffect(() => {
    const content = dashboardBodyRef.current;
    const skipLink = skipLinkRef.current;
    if (!content) return;
    content.inert = menuOpen;
    if (skipLink) skipLink.inert = menuOpen;
    if (menuOpen) menuCloseRef.current?.focus();
    const onKeyDown = (event: KeyboardEvent) => {
      if (!menuOpen || document.querySelector("dialog[open]")) return;
      if (event.key === "Escape") { event.preventDefault(); closeMenu(); }
      if (event.key === "Tab") {
        const controls = sidebarRef.current?.querySelectorAll<HTMLElement>("a[href], button:not(:disabled)");
        const first = controls?.[0], last = controls?.[controls.length - 1];
        if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
        else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      content.inert = false;
      if (skipLink) skipLink.inert = false;
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [menuOpen, closeMenu]);

  const liveSelection = projects?.find((project) => project.id === selectedID);
  // Inventory reconciliation must not discard the open editor or its drafts.
  const selected = liveSelection ?? selectedProject ?? undefined;
  useEffect(() => {
    if (liveSelection) setSelectedProject(liveSelection);
  }, [liveSelection]);
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
    <div className={styles.dashboard} data-collapsed={compactSidebar} data-menu-open={menuOpen}>
      <a ref={skipLinkRef} href="#content" className={styles.skipLink}>Skip to content</a>
      {menuOpen && <button type="button" className={styles.sidebarScrim} aria-label="Close menu" tabIndex={-1} onClick={closeMenu} />}
      <aside ref={sidebarRef} className={styles.sidebar} id="primary-sidebar" role={overlayNavigation && menuOpen ? "dialog" : undefined} aria-modal={overlayNavigation && menuOpen ? true : undefined} aria-label={overlayNavigation && menuOpen ? "Navigation menu" : undefined}>
        <div className={styles.sidebarHeader}>
          <div className={styles.sidebarBrand} aria-label="Yard">
            <span className={styles.brandName}>Yard</span>
          </div>
          <button ref={menuCloseRef} type="button" className={`${styles.sidebarToggle} ${compactSidebar ? styles.sidebarExpand : ""}`} aria-label={overlayNavigation ? "Close menu" : compactSidebar ? "Expand sidebar" : "Hide sidebar"} title={overlayNavigation ? "Close menu" : compactSidebar ? "Expand sidebar" : "Hide sidebar"} aria-expanded={!compactSidebar} onClick={() => overlayNavigation ? closeMenu() : changeSidebar(!sidebarCollapsed)}>
            <i className={`ph-bold ${compactSidebar ? "ph-sidebar-simple" : "ph-sidebar"}`} aria-hidden="true" />
          </button>
        </div>
        <nav aria-label="Primary" className={styles.sidebarNav}>
          <a href="#projects" aria-current="page" className={styles.navLink} aria-label="Projects" title={compactSidebar ? "Projects" : undefined} onClick={() => { if (overlayNavigation) closeMenu(); }}>
            <i className="ph-fill ph-squares-four" aria-hidden="true" />{!compactSidebar && <span>Projects</span>}
          </a>
        </nav>
        <div className={styles.sidebarFooter}>
          <button type="button" className={styles.navLink} aria-label="Settings" title={compactSidebar ? "Settings" : undefined} aria-haspopup="dialog" aria-controls="settings-drawer" aria-expanded={settingsOpen} onClick={() => { setMenuOpen(false); setFocusMCP(false); setSettingsOpen(true); void refreshMachine(); }}>
            <i className="ph-bold ph-sliders-horizontal" aria-hidden="true" />{!compactSidebar && <span>Settings</span>}
          </button>
          <MCPIndicator status={machineStatus} compact={compactSidebar} onClick={() => { setMenuOpen(false); setFocusMCP(true); setSettingsOpen(true); void refreshMachine(); }} />
          <div className={styles.accountRow}>
            {compactSidebar
              ? <div className={styles.accountCompact}>
                <span className={styles.accountAvatar} aria-hidden="true">{displayName.charAt(0)}</span>
                <button type="button" onClick={handleSignOut} disabled={pending} className={styles.compactSignOut} aria-label={`Sign out ${displayName}`} title="Sign out"><i className="ph-bold ph-sign-out" aria-hidden="true" /></button>
              </div>
              : <>
                <span className={styles.accountAvatar} aria-hidden="true">{displayName.charAt(0)}</span>
                <span className={styles.accountInfo}><span className={styles.accountName} title={displayName}>{displayName}</span><span className={styles.accountRole}>Administrator</span></span>
                <button type="button" onClick={handleSignOut} disabled={pending} className={styles.signOutButton} aria-label="Sign out" title="Sign out"><i className="ph-bold ph-sign-out" aria-hidden="true" /></button>
              </>}
          </div>
        </div>
      </aside>

      <div ref={dashboardBodyRef} className={styles.dashboardBody}>
        <main id="content" tabIndex={-1} className={styles.content}>
          <ProjectBrowser projects={projects} username={username} selectedID={selectedID} onSelect={selectProject} rowCells={project => <>
            <td><span className={`${styles.statusBadge} ${statusClass(project.operation || project.state)}`}>{project.operation || project.state}</span></td>
            <td><span>{healthLabel(project.health)}</span></td>
            <td><span className={styles.cardKind}><i className={`ph-bold ${project.kind === "standalone" ? "ph-cube" : "ph-stack"}`} aria-hidden="true" />{project.kind === "standalone" ? "Container" : project.kind === "managed-compose" ? "Managed Compose" : "Compose"}</span></td>
            <td><span>{project.containers.length}</span></td>
            <td><span>{formatCPU(project.cpuPercent)}</span></td>
            <td><span>{formatBytes(project.memoryBytes)}</span></td>
            <td><span>{formatUptime(project.uptimeSeconds)}</span></td>
          </>} header={
          <div className={styles.dashboardHeader}>
            <div className={styles.pageHeading} id="projects">
              <button ref={menuButtonRef} type="button" className={styles.menuButton} aria-label="Open menu" aria-haspopup="dialog" aria-controls="primary-sidebar" aria-expanded={menuOpen} onClick={() => setMenuOpen(true)}><i className="ph-bold ph-list" aria-hidden="true" /></button>
              <div className={styles.headingCopy}>
                <h1>Projects</h1>
              </div>
              <div className={styles.headingActions}>
                <button type="button" className={styles.refreshButton} aria-label={refreshing ? "Refreshing projects" : "Refresh projects"} title={refreshing ? "Refreshing projects" : "Refresh projects"} onClick={refresh} disabled={refreshing}>
                  <i className="ph-bold ph-arrows-clockwise" aria-hidden="true" />
                </button>
                <button ref={newProjectButtonRef} type="button" className={`${styles.refreshButton} ${styles.newProjectButton}`} aria-label="New Project" title="New Project" aria-haspopup="dialog" aria-controls="new-project-drawer" aria-expanded={newProjectOpen} onClick={() => setNewProjectOpen(true)}>
                  <i className="ph-bold ph-plus" aria-hidden="true" />
                </button>
              </div>
            </div>
            {projects && projects.length > 0 && <div className={styles.inventoryMeta}>
              <span>{projects.length} {projects.length === 1 ? "project" : "projects"}<span className={styles.metaSeparator} aria-hidden="true">/</span>{projects.reduce((sum, project) => sum + project.containers.length, 0)} containers</span>
              <span>{collectedAt && `Updated ${new Date(collectedAt).toLocaleTimeString()}`}</span>
            </div>}
          </div>
          } notices={<>
          {error && <p className={styles.formError} role="alert">{error}</p>}
          {inventoryError && <p className={styles.inventoryError} role="alert">{inventoryError}</p>}
          {projects === null && !inventoryError && <div className={styles.emptyPanel} role="status">Loading Docker projects…</div>}
          {projects?.length === 0 && <section className={styles.emptyPanel} aria-labelledby="empty-title">
            <div className={styles.emptyIcon}><i className="ph-fill ph-squares-four" aria-hidden="true" /></div>
            <h2 id="empty-title">No containers found</h2>
            <p>Compose projects and standalone containers on this host will appear here.</p>
          </section>}
          </>}>
              {(project) => <>
                <span className={styles.cardTopline}>
                  <span className={styles.cardKind}><i className={`ph-bold ${project.kind === "standalone" ? "ph-cube" : "ph-stack"}`} aria-hidden="true" />{project.kind === "standalone" ? "Container" : project.kind === "managed-compose" ? "Managed Compose" : "Compose"}</span>
                  <span className={`${styles.statusBadge} ${statusClass(project.operation || project.state)}`}>{project.operation || project.state}</span>
                </span>
                <span className={styles.cardName} title={project.name}>{project.name}</span>
                <span className={styles.cardSubline}>{project.containers.length} {project.containers.length === 1 ? "container" : "containers"} · Health: {healthLabel(project.health)}</span>
                <span className={styles.cardMetrics}>
                  <Metric label="CPU" value={formatCPU(project.cpuPercent)} />
                  <Metric label="Memory" value={formatBytes(project.memoryBytes)} />
                  <Metric label="Uptime" value={formatUptime(project.uptimeSeconds)} />
                </span>
              </>}
          </ProjectBrowser>
        </main>
      </div>
      <ProjectDrawer project={selected} missing={Boolean(selected && !liveSelection)} csrfToken={csrfToken} onChanged={refresh} onClose={closeProject} />
      <NewProjectDrawer open={newProjectOpen} csrfToken={csrfToken} onChanged={refresh} onClose={() => { setNewProjectOpen(false); newProjectButtonRef.current?.focus(); }} />
      <SettingsDrawer open={settingsOpen} csrfToken={csrfToken} machineStatus={machineStatus} machineError={machineError} onMachineRefresh={refreshMachine} focusMCP={focusMCP} onClose={() => { setSettingsOpen(false); if (overlayNavigation) menuButtonRef.current?.focus(); }} />
    </div>
  );
}

type RemovalTarget = { kind: "project" | "container"; id: string; name: string; managedName?: string };

function ProjectDrawer({ project, missing, csrfToken, onChanged, onClose }: { project?: Project; missing: boolean; csrfToken: string; onChanged: () => void; onClose: () => void }) {
  const operations = useOperationHistory(project?.id, onChanged);
	const configuration=useProjectConfiguration(project?.id,csrfToken,project?.operation);
  const dialogRef = useRef<HTMLDialogElement>(null);
  const lastFocusedRef = useRef<HTMLElement | null>(null);
  const backdropPointer = useRef(false);
  const contentRef = useRef<HTMLDivElement>(null);
  const detailsTabRef = useRef<HTMLButtonElement>(null);
  const logsTabRef = useRef<HTMLButtonElement>(null);
  const terminalTabRef = useRef<HTMLButtonElement>(null);
  const backButtonRef = useRef<HTMLButtonElement>(null);
  const containerButtonsRef = useRef<Record<string, HTMLButtonElement | null>>({});
  const lastSelectionRef = useRef<string | null>(null);
  const tabsID = useId();
  const [discardOpen, setDiscardOpen] = useState(false);
  const dismiss = useCallback(() => {
    if (configuration.hasUnsavedChanges) setDiscardOpen(true);
    else onClose();
  }, [configuration.hasUnsavedChanges, onClose]);
  const [selectedContainerID, setSelectedContainerID] = useState<string | null>(null);
  const [retainedContainer, setRetainedContainer] = useState<{ projectID: string; container: Container } | null>(null);
  const [activeTab, setActiveTab] = useState<"details" | "logs" | "terminal">("details");
  const [busyTarget, setBusyTarget] = useState<string | null>(null);
  const [actionMessage, setActionMessage] = useState("");
  const [actionError, setActionError] = useState("");
  const [removalTarget, setRemovalTarget] = useState<RemovalTarget | null>(null);
  const [recreateTarget, setRecreateTarget] = useState<RecreateTarget | null>(null);
  useDrawerSwipe(dialogRef, "right", Boolean(project) && !discardOpen, dismiss);

  useEffect(() => {
    setActionMessage("");
    setActionError("");
    setSelectedContainerID(null);
    setActiveTab("details");
    lastSelectionRef.current = null;
    setDiscardOpen(false);
  }, [project?.id]);

  useEffect(() => {
    if (selectedContainerID) backButtonRef.current?.focus();
    else if (lastSelectionRef.current) containerButtonsRef.current[lastSelectionRef.current]?.focus();
  }, [selectedContainerID]);

  const liveContainer = project?.kind === "standalone" ? project.containers[0]
    : project?.containers.find((container) => container.id === selectedContainerID);
  const activeContainer = liveContainer ?? (retainedContainer?.projectID === project?.id &&
    (project?.kind === "standalone" || retainedContainer?.container.id === selectedContainerID) ? retainedContainer?.container : undefined);
  useEffect(() => {
    if (project && liveContainer) setRetainedContainer({ projectID: project.id, container: liveContainer });
  }, [project?.id, liveContainer]);
  const terminalUnavailable = Boolean(project && (project.containers.length === 0 || project.containers.every((container) => container.terminalAvailable === false)));

  useEffect(() => {
    if ((terminalUnavailable && activeTab === "terminal") || (project?.containers.length === 0 && activeTab === "logs")) {
      setActiveTab("details");
      detailsTabRef.current?.focus();
    }
  }, [terminalUnavailable, activeTab, project?.containers.length]);

  function selectTab(tab: "details" | "logs" | "terminal", focus = false) {
    if (tab === "terminal" && terminalUnavailable || tab === "logs" && project?.containers.length === 0) return;
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
      setActionMessage(result.queued && !result.succeeded ? "Yard restart queued. Check its health after reconnecting." : `${summary}${result.queued ? " Yard restart queued." : ""}${result.warnings?.length ? " "+result.warnings.join(" ") : ""}`);
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
    if (project && !dialog.open) { dialog.style.animation = ""; dialog.showModal(); }
    if (!project && dialog.open) dialog.close();
  }, [project?.id]);

  return <><dialog
    id="project-drawer"
    ref={dialogRef}
    className={`${styles.settingsDrawer} ${styles.projectDrawer}`}
    aria-labelledby="project-drawer-title"
    onClose={event => {
      if (event.target !== event.currentTarget || !project || event.currentTarget.open) return;
      // Selection owns the drawer lifetime. A native/programmatic close is
      // not permission to discard the editor; only explicit dismissal is.
      const focused = lastFocusedRef.current;
      event.currentTarget.style.animation = "none";
      event.currentTarget.showModal();
      if (focused?.isConnected) focused.focus({ preventScroll: true });
      if (configuration.hasUnsavedChanges) setDiscardOpen(true);
    }}
    onCancel={event => { event.preventDefault(); dismiss(); }}
    onPointerDown={event => {
      const bounds = event.currentTarget.getBoundingClientRect();
      backdropPointer.current = event.target === event.currentTarget &&
        (event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom);
    }}
    onClick={event => { if (backdropPointer.current) dismissDrawerBackdrop(event, dismiss); backdropPointer.current = false; }}
    onFocusCapture={event => { if (event.target instanceof HTMLElement && event.target !== event.currentTarget) lastFocusedRef.current = event.target; }}
    {...{ closedby: "closerequest" }}
  >
    {project && <div className={styles.settingsBody}>
      <header className={styles.settingsHeader}>
        <h2 id="project-drawer-title">{project.name}</h2>
        <button type="button" className={styles.closeButton} autoFocus onClick={dismiss} aria-label="Close project details"><i className="ph-bold ph-x" aria-hidden="true" /></button>
      </header>
      {missing && <p className={styles.detailSummary} role="status">This project is missing from the latest inventory. Your unsaved edits are kept.</p>}
      {!missing && activeContainer && !liveContainer && <p className={styles.detailSummary} role="status">This container is missing from the latest inventory. Your unsaved edits are kept.</p>}
      <div className={styles.projectTabs} role="tablist" aria-label={`${project.name} sections`} onKeyDown={(event) => {
        if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        const tabs: ("details" | "logs" | "terminal")[] = project.containers.length === 0 ? ["details"] : terminalUnavailable ? ["details", "logs"] : ["details", "logs", "terminal"];
        const index = tabs.indexOf(activeTab);
        selectTab(event.key === "Home" ? "details" : event.key === "End" ? tabs[tabs.length - 1] : tabs[(index + (event.key === "ArrowRight" ? 1 : tabs.length - 1)) % tabs.length], true);
      }}>
        <button ref={detailsTabRef} id={`${tabsID}-details-tab`} type="button" role="tab" aria-selected={activeTab === "details"} aria-controls={`${tabsID}-details-panel`} tabIndex={activeTab === "details" ? 0 : -1} className={styles.projectTab} onClick={() => selectTab("details")}>Details</button>
        <button ref={logsTabRef} id={`${tabsID}-logs-tab`} type="button" role="tab" aria-selected={activeTab === "logs"} aria-controls={`${tabsID}-logs-panel`} tabIndex={activeTab === "logs" ? 0 : -1} className={styles.projectTab} disabled={project.containers.length === 0} onClick={() => selectTab("logs")}>Logs</button>
        <button ref={terminalTabRef} id={`${tabsID}-terminal-tab`} type="button" role="tab" aria-selected={activeTab === "terminal"} aria-controls={`${tabsID}-terminal-panel`} tabIndex={activeTab === "terminal" ? 0 : -1} className={styles.projectTab} disabled={terminalUnavailable} title={terminalUnavailable ? "Terminal unavailable: /bin/sh is missing" : undefined} onClick={() => selectTab("terminal")}>Terminal</button>
      </div>
      {terminalUnavailable && <p className={styles.terminalUnavailable} role="status">Terminal unavailable: /bin/sh is missing from this {project.containers.length === 1 ? "container" : "project's containers"}.</p>}
      <div ref={contentRef} id={`${tabsID}-${activeTab}-panel`} role="tabpanel" aria-labelledby={`${tabsID}-${activeTab}-tab`} className={`${styles.settingsContent} ${styles.projectDrawerContent} ${activeTab !== "details" ? styles.consoleContent : ""}`}>
        {activeTab === "details" ? <>
        {project.kind === "managed-compose" && !activeContainer && <ManagedProjectControls key={`managed:${project.id}`} project={project} csrfToken={csrfToken} onChanged={onChanged} busy={operations.blocked} onAccepted={operations.refresh} onRecreate={() => setRecreateTarget({id:project.id,name:project.name,operation:"recreate"})} onRemove={() => setRemovalTarget({kind:"project",id:project.id,name:project.name,managedName:project.name})} />}
        {activeContainer && project.kind !== "standalone" && <button ref={backButtonRef} type="button" className={styles.backButton} onClick={() => showContainer(null)}><i className="ph-bold ph-arrow-left" aria-hidden="true" />Back to project</button>}
        {!activeContainer && <>
          {project.kind === "external-compose" && <LifecycleControls
            name={project.name}
            state={project.state}
            selfTarget={project.containers.some((container) => project.name === "nox-yard" && container.service === "nox-yard")}
            busy={busyTarget !== null || Boolean(project.operation) || operations.blocked}
            onRun={(action) => runAction("project", project.id, action)}
            pullLabel="Pull"
            removeLabel="Remove project"
            onPull={() => { void runMaintenance("project", project.id); }}
            onRemove={() => setRemovalTarget({ kind: "project", id: project.id, name: project.name })}
            onRecreate={() => setRecreateTarget({id:project.id,name:project.name,operation:"recreate"})}
          />}
          <section className={styles.projectOverview} aria-label="Project overview">
            <h3>Overview</h3>
            <p className={styles.detailSummary}>
              {project.containers.length} {project.containers.length === 1 ? "container" : "containers"} · {project.operation || project.state} · Health: {healthLabel(project.health)}
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
              configuration={configuration}
              csrfToken={csrfToken}
              busy={busyTarget !== null || Boolean(activeContainer.operation) || operations.blocked}
              selfTarget={project.name === "nox-yard" && activeContainer.service === "nox-yard"}
              helperTarget={activeContainer.name.startsWith("nox-yard-update-")}
              onRun={(action) => runAction("container", activeContainer.id, action)}
              onRecreate={() => setRecreateTarget({id:project.id,name:project.name,service:project.id.startsWith("compose:") ? activeContainer.service : undefined,operation:"recreate"})}
              pullLabel="Pull"
              removeLabel={project.kind === "external-compose" && project.containers.length === 1 ? "Remove project" : "Remove container"}
              onPull={() => { void runMaintenance(project.kind === "external-compose" ? "project" : "container", project.kind === "external-compose" ? project.id : activeContainer.id); }}
              onRemove={() => setRemovalTarget(project.kind === "external-compose" && project.containers.length === 1
                ? { kind: "project", id: project.id, name: project.name }
                : { kind: "container", id: activeContainer.id, name: activeContainer.name })}
            />
          : <section className={styles.containerPicker} aria-labelledby={`${tabsID}-containers-title`}>
              <h3 id={`${tabsID}-containers-title`}>Containers</h3>
              <div className={styles.containerList}>
                {project.containers.map((container) => <button key={container.id} ref={(element) => { containerButtonsRef.current[container.id] = element; }} type="button" className={styles.containerChoice} onClick={() => showContainer(container.id)}>
                  <span className={styles.containerChoiceName}><strong>{container.service || container.name}</strong>{container.service && <small>{container.name}</small>}</span>
                  <span className={`${styles.statusBadge} ${statusClass(containerStatus(container))}`}>{containerStatus(container)}</span>
                  <i className={`ph-bold ph-caret-right ${styles.containerChoiceArrow}`} aria-hidden="true" />
                </button>)}
              </div>
            </section>}
        <div className={styles.projectSupplemental}>
          {(!activeContainer || project.kind === "standalone") && <ProjectSchedule key={`schedule:${project.id}`} target={project.id} revision={operations.history.map(job => `${job.id}:${job.updatedAt}:${job.outcome}`).join("|")} csrfToken={csrfToken} onChanged={onChanged} />}
          <ComposeConfiguration key={`configuration:${project.id}`} control={configuration} />
        </div>
        <OperationHistory key={`history:${project.id}`} history={operations.history} error={operations.error} loading={operations.loading} csrfToken={csrfToken} onRefresh={operations.refresh} />
        </> : activeTab === "logs"
          ? <LogsPanel key={project.id} project={project} preferredContainerID={activeContainer?.id} csrfToken={csrfToken} onSelectContainer={setSelectedContainerID} />
          : <TerminalPanel key={project.id} project={project} preferredContainerID={activeContainer?.id} csrfToken={csrfToken} onSelectContainer={setSelectedContainerID} />}
      </div>
    </div>}
  </dialog>
    <ConfirmationDialog open={discardOpen && Boolean(project)} title="Unsaved changes" onClose={() => setDiscardOpen(false)} returnFocusRef={lastFocusedRef} actions={<>
      <button type="button" className={styles.inspectButton} autoFocus onClick={() => setDiscardOpen(false)}>No</button>
      <button type="button" className={styles.inspectButton} onClick={() => { setDiscardOpen(false); onClose(); }}>Yes</button>
    </>}>
      <p>There are unsaved changes. Do you want to proceed?</p>
    </ConfirmationDialog>
    <RemoveConfirmation target={removalTarget} csrfToken={csrfToken} onClose={() => setRemovalTarget(null)} onChanged={onChanged} />
    <RecreateConfirmation target={recreateTarget} csrfToken={csrfToken} onClose={() => setRecreateTarget(null)} onAccepted={() => { operations.refresh(); onChanged(); }} />
  </>;
}

function ManagedProjectControls({ project, csrfToken, onChanged, busy: historyBusy, onAccepted, onRemove, onRecreate }: { project: Project; csrfToken: string; onChanged: () => void; busy: boolean; onAccepted: () => void; onRemove: () => void; onRecreate: () => void }) {
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [confirm, setConfirm] = useState<"stop" | "restart" | null>(null);
  const menuRef = useRef<HTMLDetailsElement>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const confirmInvoker = useRef<HTMLElement | null>(null);
  useEffect(() => { if (confirm) cancelRef.current?.focus(); }, [confirm]);
  function ask(operation: NonNullable<typeof confirm>) {
    confirmInvoker.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (menuRef.current) menuRef.current.open = false;
    setConfirm(operation);
  }
  function cancelConfirmation() {
    setConfirm(null);
    if (confirmInvoker.current?.closest("details") === menuRef.current) menuRef.current?.querySelector("summary")?.focus();
    else confirmInvoker.current?.focus();
  }
  const busy = submitting || historyBusy || Boolean(project.operation);
  async function run(operation: "start" | "stop" | "restart" | "pull") {
    setError(""); setConfirm(null); setSubmitting(true);
    try { await runManagedOperation(project.name, operation, false, csrfToken); onAccepted(); onChanged(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "Unable to start operation."); }
    finally { setSubmitting(false); }
  }
  return <div className={styles.lifecycleControls}>
    <div className={styles.actionRow} aria-label={`${project.name} actions`}>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Start ${project.name}`} title="Start" disabled={busy || project.state === "running"} onClick={() => { void run("start"); }}><i className="ph-fill ph-play" aria-hidden="true" /></button>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Stop ${project.name}`} title="Stop" disabled={busy} onClick={() => ask("stop")}><i className="ph-fill ph-stop" aria-hidden="true" /></button>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Restart ${project.name}`} title="Restart" disabled={busy} onClick={() => ask("restart")}><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" /></button>
      <details ref={menuRef} className={styles.maintenanceMenu}><summary className={styles.inspectButton} aria-label="More actions"><i className="ph-bold ph-dots-three" aria-hidden="true" /></summary><div className={styles.maintenanceOptions}>
        <button type="button" disabled={busy} onClick={() => { if (menuRef.current) menuRef.current.open = false; void run("pull"); }}>Pull</button>
        <button type="button" disabled={busy} onClick={() => { if (menuRef.current) menuRef.current.open = false; onRecreate(); }}>Recreate</button>
        <button type="button" className={styles.maintenanceRemove} disabled={busy} onClick={() => { if(menuRef.current) menuRef.current.open=false; onRemove(); }}>Remove project</button>
      </div></details>
    </div>
    {confirm && <div className={styles.actionConfirm} role="group" aria-label={`Confirm ${confirm}`}><p>{confirm === "restart" ? "Restart" : "Stop"} <strong>{project.name}</strong>?</p>{confirm === "restart" && <p>Restart includes stopped services. Completed initializers may run again. Saved configuration changes are not applied.</p>}
      <div className={styles.actionRow}><button ref={cancelRef} type="button" className={styles.inspectButton} onClick={cancelConfirmation}>Cancel</button><button type="button" className={styles.inspectButton} disabled={busy} onClick={() => { void run(confirm); }}>Confirm {confirm}</button></div>
    </div>}
    {error && <p className={styles.inventoryError} role="alert">{error}</p>}
  </div>;
}

function ContainerDetails({ container, csrfToken, busy, selfTarget, helperTarget, onRun, pullLabel, removeLabel, onPull, onRemove, onUpdate, onRecreate, hideActions = false, configuration }: {
  configuration?: ConfigurationControl;
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
  onUpdate?: () => void;
  onRecreate?: () => void;
  hideActions?: boolean;
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
      if(configuration?.configuration?.available) await configuration.reload(true);
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
    {!hideActions && <LifecycleControls name={container.service || container.name} state={container.state} selfTarget={selfTarget} helperTarget={helperTarget} busy={busy} onRun={onRun} pullLabel={pullLabel} removeLabel={removeLabel} onPull={onPull} onRemove={onRemove} onUpdate={onUpdate} onRecreate={onRecreate} />}
    <ContainerRow container={container} />
    {loading && <p className={styles.detailSummary} role="status">Loading container details…</p>}
    {detailError && <p className={styles.inventoryError} role="alert">{detailError}</p>}
    {inspection?.id === container.id && <ContainerInspectionView
      inspection={inspection}
      configuration={configuration}
      service={container.service || ""}
      csrfToken={csrfToken}
      revealed={revealed}
      revealing={revealing}
      onReveal={() => { void revealEnvironment(); }}
      onHide={hideEnvironment}
    />}
  </section>;
}

function LifecycleControls({ name, state, selfTarget = false, helperTarget = false, busy, onRun, pullLabel, removeLabel, onPull, onRemove, onUpdate, onRecreate }: {
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
  onUpdate?: () => void;
  onRecreate?: () => void;
}) {
  const [confirming, setConfirming] = useState<"stop" | "restart" | null>(null);
  const running = state === "running" || state === "partial";

  return <div className={styles.lifecycleControls}>
    <div className={styles.actionRow} aria-label={`${name} actions`}>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Start ${name}`} title={`Start ${name}`} disabled={busy || state === "running"} onClick={() => { void onRun("start"); }}><i className="ph-fill ph-play" aria-hidden="true" /></button>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Stop ${name}`} title={`Stop ${name}`} disabled={busy || !running || selfTarget || helperTarget} onClick={() => setConfirming("stop")}><i className="ph-fill ph-stop" aria-hidden="true" /></button>
      <button type="button" className={`${styles.inspectButton} ${styles.iconAction}`} aria-label={`Restart ${name}`} title={`Restart ${name}`} disabled={busy || helperTarget} onClick={() => setConfirming("restart")}><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" /></button>
      <MaintenanceMenu busy={busy} pullLabel={pullLabel} removeLabel={removeLabel} canPull={!helperTarget} canRemove={!helperTarget && !selfTarget} onPull={onPull} onRemove={onRemove} onUpdate={onUpdate} onRecreate={onRecreate} />
    </div>
    {selfTarget && <p className={styles.actionHint}>Yard cannot stop or remove itself.</p>}
    {helperTarget && <p className={styles.actionHint}>Maintenance helpers are managed automatically.</p>}
    {confirming && <div className={styles.actionConfirm} role="group" aria-label={`Confirm ${confirming}`}>
      <p>{confirming === "stop" ? "Stop" : "Restart"} <strong>{name}</strong>?</p>{confirming === "restart" && <p>Restart includes stopped containers. Completed initializers may run again. Saved configuration changes are not applied.</p>}
      <div className={styles.actionRow}>
        <button type="button" className={styles.inspectButton} disabled={busy} onClick={() => setConfirming(null)}>Cancel</button>
        <button type="button" className={`${styles.inspectButton} ${confirming === "stop" ? styles.dangerAction : ""}`} disabled={busy} onClick={() => { const action = confirming; if (!action) return; setConfirming(null); void onRun(action); }}>Confirm {confirming}</button>
      </div>
    </div>}
  </div>;
}

function MaintenanceMenu({ busy, pullLabel, removeLabel, canPull, canRemove, onPull, onRemove, onRecreate }: {
  busy: boolean;
  pullLabel: string;
  removeLabel: string;
  canPull: boolean;
  canRemove: boolean;
  onPull: () => void;
  onRemove: () => void;
  onUpdate?: () => void;
  onRecreate?: () => void;
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
    <summary className={styles.inspectButton} aria-label="More actions" aria-disabled={busy} onClick={(event) => { if (busy) event.preventDefault(); }} title="More actions"><i className="ph-bold ph-dots-three" aria-hidden="true" /></summary>
    <div className={styles.maintenanceOptions}>
      {onRecreate && <button type="button" disabled={busy || !canPull} onClick={() => { if(ref.current) ref.current.open=false; onRecreate(); }}>Recreate</button>}
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
  const invoker = useRef<HTMLElement | null>(null);
  const titleID = useId();
  const [plan, setPlan] = useState<RemovalPlan | null>(null);
  const [report, setReport] = useState<RemovalReport | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [removing, setRemoving] = useState(false);
  const [removeVolumes, setRemoveVolumes] = useState(false);
 const [removeImages,setRemoveImages]=useState(false);
  const [revision, setRevision] = useState(0);
  const volumeChoiceID = useId();
  useEffect(() => { setRemoveVolumes(false);setRemoveImages(false); }, [target]);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (target && !dialog.open) {
      const active=document.activeElement;
      invoker.current=active instanceof HTMLElement ? active.closest("details")?.querySelector("summary") ?? active : null;
      dialog.showModal();
    }
    if (!target) { if(dialog.open) dialog.close(); invoker.current?.focus(); }
  }, [target]);

  useEffect(() => {
    if (!target) return;
    let current = true;
    setPlan(null);
    setReport(null);
    setError("");
    setLoading(true);
    const load = target.kind === "project" ? previewRemoveProject(target.id, removeVolumes,removeImages) : previewRemoveContainer(target.id, removeVolumes,removeImages);
    void load.then((result) => { if (current) setPlan(result); })
      .catch((cause: unknown) => { if (current) setError(cause instanceof Error ? cause.message : "Unable to inspect Docker resources."); })
      .finally(() => { if (current) setLoading(false); });
    return () => { current = false; };
  }, [target, removeVolumes,removeImages, revision]);

  async function confirm() {
    if (!target || !plan || removing) return;
    setRemoving(true);
    setError("");
    try {
      if (target.managedName) {
        await runManagedOperation(target.managedName,"remove",removeVolumes,csrfToken,plan.fingerprint,removeImages);
        onChanged(); onClose(); return;
      }
      const result = target.kind === "project"
        ? await removeProject(target.id, plan.fingerprint, csrfToken, removeVolumes,removeImages)
        : await removeContainer(target.id, plan.fingerprint, csrfToken, removeVolumes,removeImages);
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
      {!report && <label htmlFor={volumeChoiceID}><input id={volumeChoiceID} type="checkbox" checked={removeVolumes} disabled={removing} onChange={(event) => { setPlan(null); setRemoveVolumes(event.target.checked); }} /> Delete exclusive project volumes</label>}
      {!report && <label><input type="checkbox" checked={removeImages} disabled={removing} onChange={event=>{setPlan(null);setRemoveImages(event.target.checked);}} /> Remove unused images</label>}
      {plan && !report && <>
        <p>Yard will stop and remove the selected containers, then attempt to delete the volumes, networks, and images marked "will remove" below. Items marked "will keep" remain on the host for the stated reason.</p>
        <RemovalItems items={plan.items.map((item) => ({ ...item, status: item.action === "remove" ? "will remove" : "will keep" }))} />
      </>}
      {report && <>
        <p role="status">{failed ? "Removal completed with failures." : "Selected resources removed."} {removed} removed · {retained} retained · {failed} failed.</p>
        <RemovalItems items={report.items} />
      </>}
      <div className={styles.confirmActions}>
        <button type="button" className={styles.inspectButton} autoFocus disabled={removing} onClick={onClose}>{report ? "Close report" : "Cancel"}</button>
        {error && !report && <button type="button" className={styles.inspectButton} disabled={loading || removing} onClick={() => setRevision((value) => value + 1)}>Review again</button>}
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

function SettingsDrawer({ open, csrfToken, onClose, machineStatus, machineError, onMachineRefresh, focusMCP }: { open: boolean; csrfToken: string; onClose: () => void; machineStatus: MachineStatus | null; machineError: string; onMachineRefresh: () => Promise<void>; focusMCP: boolean }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const dismiss = useCallback(() => dialogRef.current?.close(), []);
  useDrawerSwipe(dialogRef, "left", open, dismiss);
  useEffect(() => { if (open && focusMCP) { const frame = requestAnimationFrame(() => { const panel = document.getElementById("machine-access-settings"); if (panel instanceof HTMLDetailsElement) { panel.open = true; panel.scrollIntoView({ block: "start" }); } }); return () => cancelAnimationFrame(frame); } }, [open, focusMCP]);
  const [status, setStatus] = useState<SelfUpdateStatus | null>(null);
	const [projectsBase, setProjectsBase] = useState("");
	const [savedProjectsBase, setSavedProjectsBase] = useState("");
	const [savingProjectsBase, setSavingProjectsBase] = useState(false);
	const [projectsError, setProjectsError] = useState("");
  const [error, setError] = useState("");
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
	let settingsActive = true;
	setProjectsError("");
	void getManagedSettings().then((settings) => { if (settingsActive) { setProjectsBase(settings.projectsBase); setSavedProjectsBase(settings.projectsBase); } }).catch((cause: unknown) => { if (settingsActive) setProjectsError(cause instanceof Error ? cause.message : "Unable to load projects directory."); });
    let active = true;
    const controller = new AbortController();
    async function refresh() {
      if (document.visibilityState === "hidden") return;
      try {
        const current = await getSelfUpdateStatus(controller.signal);
        if (active) {
          setStatus(current);
          setError("");

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
	  settingsActive = false;
      controller.abort();
      window.clearInterval(interval);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [open]);

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

	async function saveProjectsDirectory(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		setSavingProjectsBase(true);
		setProjectsError("");
		try {
			const settings = await saveManagedSettings(projectsBase, csrfToken);
			setProjectsBase(settings.projectsBase);
			setSavedProjectsBase(settings.projectsBase);
		} catch (cause) {
			setProjectsError(cause instanceof Error ? cause.message : "Unable to save projects directory.");
		} finally { setSavingProjectsBase(false); }
	}

  const labels: Record<SelfUpdateStatus["status"], string> = {
    not_checked: "Not checked",
    checking: "Checking",
    up_to_date: "Up to date",
    updating: "Updating",
    update_failed: "Update failed",
  };

  const busy = checkingNow || status?.status === "checking" || status?.status === "updating";

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
        <button type="button" className={styles.closeButton} autoFocus onClick={() => dialogRef.current?.close()} aria-label="Close settings"><i className="ph-bold ph-x" aria-hidden="true" /></button>
      </header>
      <div className={`${styles.settingsContent} ${settingsStyles.content}`}>
        <section aria-labelledby="updates-title" className={settingsStyles.section}>
          <div className={settingsStyles.heading}>
            <h3 id="updates-title"><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" />Yard updates</h3>
            <button type="button" className={`${styles.inspectButton} ${settingsStyles.action}`} disabled={!status || busy} onClick={() => { void checkNow(); }}><i className="ph-bold ph-arrows-clockwise" aria-hidden="true" />Check now</button>
          </div>
          <p className={settingsStyles.description}>Check now installs an available update. Manage automatic checks in Yard’s project drawer.</p>
          <dl className={settingsStyles.facts} aria-live="polite">
            <div><dt>Current version</dt><dd><code>{status?.currentVersion || (status ? "Unavailable" : "Loading")}</code></dd></div>
            <div><dt>Status</dt><dd><span className={`${styles.statusBadge} ${status?.status === "up_to_date" ? styles.statusRunning : status?.status === "update_failed" ? styles.statusStopped : busy ? styles.statusPartial : ""}`}>{status ? labels[status.status] : "Loading"}</span></dd></div>
            <div><dt>Last checked</dt><dd>{status?.lastChecked ? <time dateTime={status.lastChecked}>{new Date(status.lastChecked).toLocaleString(undefined, { dateStyle: "short", timeStyle: "short" })}</time> : "Never"}</dd></div>
            {status?.currentBuildSHA && <div><dt>Current build</dt><dd><code title={status.currentBuildSHA}>{status.currentBuildSHA.slice(0, 12)}</code></dd></div>}
          </dl>
          {status?.status === "update_failed" && status.error && <p className={settingsStyles.error} role="alert">{status.error}</p>}
          {error && <p className={settingsStyles.error} role="alert">{error}</p>}
        </section>
        <section aria-labelledby="projects-directory-title" className={settingsStyles.section}>
          <div className={settingsStyles.heading}>
            <h3 id="projects-directory-title"><i className="ph-bold ph-folder" aria-hidden="true" />Projects directory</h3>
          </div>
          <p id="projects-base-help" className={settingsStyles.description}>Choose an existing directory on the Docker host for new projects and exported logs.</p>
          <form onSubmit={(event) => { void saveProjectsDirectory(event); }} className={settingsStyles.form}>
            <div className={settingsStyles.field}>
              <label htmlFor="projects-base">Absolute directory on Docker host</label>
              <input id="projects-base" name="projectsBase" className={settingsStyles.input} type="text" value={projectsBase} onChange={(event) => setProjectsBase(event.target.value)} placeholder="/home/username/projects" required autoComplete="off" autoCapitalize="none" spellCheck={false} aria-describedby={`projects-base-help${projectsError ? " projects-base-error" : ""}`} />
            </div>
            <button type="submit" className={`${styles.inspectButton} ${settingsStyles.action}`} disabled={savingProjectsBase || projectsBase.trim() === savedProjectsBase}><i className="ph-bold ph-check" aria-hidden="true" />{savingProjectsBase ? "Saving…" : "Save directory"}</button>
            {projectsError && <p id="projects-base-error" className={settingsStyles.error} role="alert">{projectsError}</p>}
          </form>
          <dl className={settingsStyles.paths}>
            <div><dt>New projects</dt><dd><code>BASE/PROJECT_NAME/compose.yml</code></dd></div>
            <div><dt>Exported logs</dt><dd><code>BASE/PROJECT_NAME/logs</code></dd></div>
          </dl>
          <p className={settingsStyles.hint}>Existing Compose files stay in their current location. Relative bind mounts use that location. Standalone log folders use the container name.</p>
        </section>
        {open && <MachineAccessSettings status={machineStatus} error={machineError} csrfToken={csrfToken} onRefresh={onMachineRefresh} />}
      </div>
    </div>
  </dialog>;
}

function dismissDrawerBackdrop(event: MouseEvent<HTMLDialogElement>, dismiss = () => event.currentTarget.close()) {
  if (event.target !== event.currentTarget) return;
  const bounds = event.currentTarget.getBoundingClientRect();
  if (event.clientX < bounds.left || event.clientX > bounds.right ||
      event.clientY < bounds.top || event.clientY > bounds.bottom) {
    dismiss();
  }
}

function Metric({ label, value }: { label: ReactNode; value: string }) {
  return <span className={styles.metric}><span>{label}</span><strong>{value}</strong></span>;
}

function ContainerRow({ container }: { container: Container }) {
  return <article className={styles.containerRow}>
    <div className={styles.containerHeading}>
      <div><h3>{container.service || container.name}</h3>{container.service && <span>{container.name}</span>}</div>
      <span className={`${styles.statusBadge} ${statusClass(containerStatus(container))}`}>{containerStatus(container)}</span>
    </div>
    <p className={styles.imageName} title={container.image}>{container.image}</p>
    <p className={styles.containerHealth}>Health: {healthLabel(container.health)}</p>
    <div className={styles.containerMetrics}>
      <Metric label="CPU" value={formatCPU(container.cpuPercent)} />
      <Metric label="Memory" value={formatBytes(container.memoryBytes)} />
      <Metric label="Uptime" value={formatUptime(container.uptimeSeconds)} />
      <Metric label={<span aria-label="Network received / sent">Network <i className="ph-bold ph-arrow-down" aria-hidden="true" /> / <i className="ph-bold ph-arrow-up" aria-hidden="true" /></span>} value={`${formatBytes(container.networkRxBytes)} / ${formatBytes(container.networkTxBytes)}`} />
    </div>
  </article>;
}

function ContainerInspectionView({
  inspection, revealed, revealing, onReveal, onHide, configuration, service, csrfToken,
}: {
  configuration?: ConfigurationControl;
  service: string;
  csrfToken: string;
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
          <dd><EnvironmentValue name={variable.name} value={variable.value} revealed={revealed} service={service} containerID={inspection.id} csrfToken={csrfToken} control={configuration} /></dd>
        </div>)}
      </dl>}
    </section>
  </>;
}

function containerStatus(container: Container): string {
  return container.operation || (container.completed ? "completed" : container.state);
}

function statusClass(state: string): string {
  if (state === "running" || state === "completed") return styles.statusRunning;
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
