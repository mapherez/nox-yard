import { useCallback, useEffect, useId, useRef, useState, type FormEvent, type MouseEvent, type ReactNode } from "react";
import {
  checkSelfUpdateNow,
  createAdministrator,
  getBootstrap,
  getContainerInspection,
  getProjects,
  getSelfUpdateStatus,
  saveSelfUpdateSettings,
  revealContainerEnvironment,
  signIn,
  signOut,
  type Bootstrap,
  type Container,
  type ContainerInspection,
  type Project,
  type SelfUpdateStatus,
} from "./api";
import styles from "./App.module.css";
import { useDrawerSwipe } from "./useDrawerSwipe";

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
  const [isMobile, setIsMobile] = useState(() => window.matchMedia("(max-width: 47.99rem)").matches);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() => {
    try { return window.localStorage.getItem("nox-yard-sidebar-collapsed") === "true"; }
    catch { return false; }
  });
  const compactSidebar = !isMobile && sidebarCollapsed;

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
      <aside className={styles.sidebar} id="primary-sidebar">
        <div className={styles.sidebarHeader}>
          <div className={styles.sidebarBrand} aria-label="NoX Yard">
            <span className={styles.brandMark} aria-hidden="true">N</span>
            {!compactSidebar && <span className={styles.brandName}>NoX Yard</span>}
          </div>
          <button type="button" className={`${styles.sidebarToggle} ${compactSidebar ? styles.sidebarExpand : ""}`} aria-label={isMobile ? "Close menu" : compactSidebar ? "Expand sidebar" : "Hide sidebar"} title={isMobile ? "Close menu" : compactSidebar ? "Expand sidebar" : "Hide sidebar"} aria-expanded={!compactSidebar} onClick={() => isMobile ? setMobileOpen(false) : changeSidebar(!sidebarCollapsed)}>
            <SidebarIcon collapsed={compactSidebar} />
          </button>
        </div>
        <nav aria-label="Primary" className={styles.sidebarNav}>
          <a href="#projects" aria-current="page" className={styles.navLink} aria-label="Projects" title={compactSidebar ? "Projects" : undefined} onClick={() => setMobileOpen(false)}>
            <GridIcon />{!compactSidebar && <span>Projects</span>}
          </a>
        </nav>
        <div className={styles.sidebarFooter}>
          <button type="button" className={styles.navLink} aria-label="Settings" title={compactSidebar ? "Settings" : undefined} aria-haspopup="dialog" aria-controls="settings-drawer" aria-expanded={settingsOpen} onClick={() => { setMobileOpen(false); setSettingsOpen(true); }}>
            <SettingsIcon />{!compactSidebar && <span>Settings</span>}
          </button>
          <div className={styles.accountRow}>
            {compactSidebar
              ? <div className={styles.accountCompact}>
                <span className={styles.accountAvatar} aria-hidden="true">{displayName.charAt(0)}</span>
                <button type="button" onClick={handleSignOut} disabled={pending} className={styles.compactSignOut} aria-label={`Sign out ${displayName}`} title="Sign out"><SignOutIcon /></button>
              </div>
              : <>
                <span className={styles.accountAvatar} aria-hidden="true">{displayName.charAt(0)}</span>
                <span className={styles.accountName} title={displayName}>{displayName}</span>
                <button type="button" onClick={handleSignOut} disabled={pending} className={styles.signOutButton} aria-label="Sign out" title="Sign out"><SignOutIcon /></button>
              </>}
          </div>
        </div>
      </aside>

      <div ref={dashboardBodyRef} className={styles.dashboardBody}>
        <main id="content" tabIndex={-1} className={styles.content}>
          <div className={styles.pageHeading} id="projects">
            <h1 className={styles.visuallyHidden}>Projects</h1>
            <button type="button" className={styles.mobileMenuButton} aria-label="Open menu" aria-controls="primary-sidebar" aria-expanded={mobileOpen} onClick={() => setMobileOpen(true)}><MenuIcon /></button>
            <button type="button" className={styles.refreshButton} onClick={() => setRefreshKey((key) => key + 1)} disabled={refreshing}>
              {refreshing ? "Refreshing…" : "Refresh"}
            </button>
          </div>

          {error && <p className={styles.formError} role="alert">{error}</p>}
          {inventoryError && <p className={styles.inventoryError} role="alert">{inventoryError}</p>}
          {projects === null && !inventoryError && <div className={styles.emptyPanel} role="status">Loading Docker projects…</div>}
          {projects?.length === 0 && <section className={styles.emptyPanel} aria-labelledby="empty-title">
            <div className={styles.emptyIcon}><GridIcon /></div>
            <h2 id="empty-title">No containers found</h2>
            <p>Compose projects and standalone containers on this host will appear here.</p>
          </section>}
          {projects && projects.length > 0 && <>
            <div className={styles.inventoryMeta}>
              <span>{projects.length} {projects.length === 1 ? "project" : "projects"} · {projects.reduce((sum, project) => sum + project.containers.length, 0)} containers</span>
              <span>{collectedAt && `Updated ${new Date(collectedAt).toLocaleTimeString()}`}</span>
            </div>
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
      <ProjectDrawer project={selected} csrfToken={csrfToken} onClose={() => setSelectedID(null)} />
      <SettingsDrawer open={settingsOpen} csrfToken={csrfToken} onClose={() => setSettingsOpen(false)} />
    </div>
  );
}

function ProjectDrawer({ project, csrfToken, onClose }: { project?: Project; csrfToken: string; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  useDrawerSwipe(dialogRef, "right");

  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (project && !dialog.open) dialog.showModal();
    if (!project && dialog.open) dialog.close();
  }, [project]);

  return <dialog
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
        <div>
          <span className={styles.sectionLabel}>PROJECT DETAILS</span>
          <h2 id="project-drawer-title">{project.name}</h2>
        </div>
        <button type="button" className={styles.closeButton} autoFocus onClick={() => dialogRef.current?.close()} aria-label="Close project details">×</button>
      </header>
      <div className={`${styles.settingsContent} ${styles.projectDrawerContent}`}>
        <p className={styles.detailSummary}>
          {project.containers.length} {project.containers.length === 1 ? "container" : "containers"} · {project.state} · Health: {healthLabel(project.health)}
        </p>
        <div className={styles.containerList}>
          {project.containers.map((container) => <ContainerDetails key={container.id} container={container} csrfToken={csrfToken} />)}
        </div>
      </div>
    </div>}
  </dialog>;
}

function ContainerDetails({ container, csrfToken }: { container: Container; csrfToken: string }) {
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

const checkIntervals = [5, 15, 30, 60, 360] as const;

function SettingsDrawer({ open, csrfToken, onClose }: { open: boolean; csrfToken: string; onClose: () => void }) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  useDrawerSwipe(dialogRef, "left");
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

function GridIcon(): ReactNode {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <rect x="3" y="3" width="7" height="7" rx="1.5" />
      <rect x="14" y="3" width="7" height="7" rx="1.5" />
      <rect x="3" y="14" width="7" height="7" rx="1.5" />
      <rect x="14" y="14" width="7" height="7" rx="1.5" />
    </svg>
  );
}

function SettingsIcon(): ReactNode {
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M3 6h6m6 0h6M3 12h10m6 0h2M3 18h2m6 0h10" />
      <circle cx="12" cy="6" r="3" />
      <circle cx="16" cy="12" r="3" />
      <circle cx="8" cy="18" r="3" />
    </svg>
  );
}

function SidebarIcon({ collapsed }: { collapsed: boolean }): ReactNode {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <rect x="3" y="3" width="18" height="18" rx="2" />
    <path d="M9 3v18" />
    {collapsed ? <path d="m13 9 3 3-3 3" /> : <path d="m16 9-3 3 3 3" />}
  </svg>;
}

function MenuIcon(): ReactNode {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" aria-hidden="true">
    <path d="M4 6h16M4 12h16M4 18h16" />
  </svg>;
}

function SignOutIcon(): ReactNode {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <path d="M10 4H5v16h5M7 12h11m-4-4 4 4-4 4" />
  </svg>;
}
