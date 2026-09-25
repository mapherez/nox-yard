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

The dashboard refreshes every 20 seconds while visible and offers manual refresh. Selecting a card opens a read-only container panel. Project and container lifecycle mutation endpoints are not yet exposed.

## NoX Yard self-update

Automatic updates are off by default. The authenticated dashboard can enable them and shows the last check, update state, error, and current build SHA when available. The updater compares the running image ID with the platform image ID behind GHCR `latest`, then uses a temporary worker to replace NoX Yard and restore the previous container and SQLite snapshot if the replacement fails health. See [Self-Update](Self-Update.md) for the operational contract.
