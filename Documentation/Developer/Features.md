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

The Go service serves the Vite production build and exposes `/healthz`, which checks SQLite access. The React UI has responsive setup/login screens, a compact dashboard sidebar, sign-out, loading/error states, and an inventory placeholder. It does not currently query Docker or show real projects. UI styles use the semantic tokens in `web/src/styles/tokens.css` and feature CSS Modules.

## Verification and limits

Go endpoint tests cover setup persistence and concurrency, login, logout CSRF, origin checks, secure cookies, and password reset session invalidation. A browser run covered setup, logout, and login at desktop and mobile widths. The Docker Compose file parses, but no Linux Docker runtime check has been performed yet because the daemon was unavailable in the development environment.
