import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import {
  createAdministrator,
  getBootstrap,
  signIn,
  signOut,
  type Bootstrap,
} from "./api";
import styles from "./App.module.css";

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
    <div className={styles.dashboard}>
      <a href="#content" className={styles.skipLink}>Skip to content</a>
      <aside className={styles.sidebar}>
        <div className={styles.sidebarBrand}><span className={styles.brandMark} aria-label="NoX Yard">N</span></div>
        <nav aria-label="Primary" className={styles.sidebarNav}>
          <a href="#projects" aria-current="page" className={styles.navLink}>
            <GridIcon /><span>Projects</span>
          </a>
        </nav>
      </aside>

      <div className={styles.dashboardBody}>
        <header className={styles.topbar}>
          <Brand compact />
          <div className={styles.account}>
            <span className={styles.accountName}>{username}</span>
            <button type="button" onClick={handleSignOut} disabled={pending} className={styles.signOutButton}>Sign out</button>
          </div>
        </header>
        <main id="content" tabIndex={-1} className={styles.content}>
          <div className={styles.pageHeading} id="projects">
            <div>
              <span className={styles.sectionLabel}>OVERVIEW</span>
              <h1>Projects</h1>
              <p>Your Docker workspace, all in one place.</p>
            </div>
          </div>

          {error && <p className={styles.formError} role="alert">{error}</p>}
          <section className={styles.emptyPanel} aria-labelledby="empty-title">
            <div className={styles.emptyIcon}><GridIcon /></div>
            <h2 id="empty-title">Your project view is taking shape</h2>
            <p>Docker projects and containers will appear here when inventory is connected.</p>
          </section>
        </main>
      </div>
    </div>
  );
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
