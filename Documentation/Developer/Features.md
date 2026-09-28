# Implemented features

This file records current behavior. Future Docker management capabilities are specified in the [Implementation Plan](Implementation-Plan.md).

## First-run administrator and local login

- `GET /api/bootstrap` reports whether setup is needed or a valid session exists. The browser displays setup, login, or the dashboard accordingly.
- `POST /api/setup` creates the only administrator. Username must contain 3–32 ASCII letters, digits, dots, underscores, or hyphens; password must contain 12–128 characters. The database's single administrator row prevents concurrent setup requests from creating two accounts. Setup issues a session on success.
- `POST /api/login` validates the stored Argon2id password. Five failed attempts from one remote IP block further attempts for 15 minutes in the current server process. A successful login clears that IP's attempts.
- Session tokens are random, stored only as SHA-256 hashes in SQLite, and expire after seven days. Cookies are HttpOnly and SameSite Strict. `NOX_PUBLIC_URL` with an HTTPS origin enables a Secure `__Host-` cookie. Browser mutations require an exact Origin match; logout also requires the session CSRF token.
- `POST /api/logout` deletes the session and clears its cookie. `nox-yard reset-admin-password` asks for a new password in an interactive terminal and invalidates all sessions. There is no email or remote password recovery.

Authentication state lives in `data/nox-yard.sqlite`. Keep `data/` persistent and private. The first-run account remains claimable by anyone who can reach the application until setup is completed, so restrict initial network access.

## Application shell

The Go service serves the Vite production build and exposes `/healthz`, which checks SQLite access. The React UI has responsive setup/login screens, a compact dashboard sidebar, and sign-out. UI styles use the semantic tokens in `web/src/styles/tokens.css` and feature CSS Modules.

## Docker inventory

`GET /api/projects` requires a valid session. It lists all local Docker containers, groups those with `com.docker.compose.project` labels into Compose projects, and presents other containers individually. Each project includes its containers, state, health, CPU, memory, and uptime. Container details also include image, service name, network totals, and individual metrics. Missing stats are represented as `null`; Docker errors return `503` without blocking login or `/healthz`.

The dashboard refreshes every 20 seconds while visible and offers manual refresh. Selecting a Compose card opens a right drawer with one project action row, a summary, and a compact container list. Selecting a container replaces that view with its action row and details; **Back to project** restores the list. A standalone card opens its container details directly. The drawer has a fixed header and a Details tab; Logs and Terminal are visibly unavailable until their APIs exist. Only the tab content scrolls. Authenticated `GET /api/containers/{id}` returns its published and exposed ports, mounts, attached networks, and environment variable names. Values are omitted by default. **Reveal values** uses an Origin- and CSRF-protected `POST /api/containers/{id}/environment`; **Hide values** clears them from the displayed state. Missing containers return 404. The project grid keeps its full width. The left sidebar can expand to show labels or collapse to icons on desktop and tablet, with Settings and the account at its bottom. Mobile uses a menu button to open the wide sidebar over the page.

Project and container drawers provide **Start**, **Stop**, and **Restart** actions. Stop and restart require confirmation. `POST /api/projects/{id}/actions` and `POST /api/containers/{id}/actions` accept `{ "action": "start|stop|restart" }` with a valid session, matching Origin, and CSRF token. Compose group membership is read from current Docker labels before each action; no Compose source file is needed. A group action attempts each eligible container and returns succeeded, skipped, failed, and queued counts plus Docker errors for failures. A stale target returns 404. Stop of NoX Yard itself and manual actions against its temporary helpers are blocked. Restarting NoX Yard queues a temporary helper from the exact running image ID; the helper restarts the service, waits for Docker health, then removes itself. Helper outcome is not yet persisted as a lifecycle job, so a queued response does not mean the restart has passed health.

## NoX Yard self-update

Automatic updates are off by default. The Settings drawer controls them and their check interval, and shows status and build details. The updater compares the running image ID with the platform image ID behind GHCR `latest`, then uses a temporary worker to replace NoX Yard and restore the previous container and SQLite snapshot if the replacement fails health. See [Self-Update](Self-Update.md) for the operational contract.
