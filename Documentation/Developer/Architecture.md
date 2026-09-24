# Architecture

**Status:** Phase 1 service, authentication, persistence, and UI shell are implemented. Docker integration, inventory, streaming, Compose management, and jobs below remain planned.

## System boundary

NoX Yard manages one local Linux Docker Engine. The browser talks only to the NoX Yard web service; Docker socket access stays on the server side. The product is designed for a trusted LAN or VPN and may sit behind a user-managed HTTPS reverse proxy. Docker socket access gives the application broad control of the host, so the installation must not be exposed to untrusted users or networks.

```text
Browser (React)
  | REST, SSE, WebSocket
  v
NoX Yard service (Go) ---- SQLite + managed Compose files (/data)
  | Docker Engine SDK
  v
Local Docker Engine ---- containers, images, networks, volumes
  ^
  | temporary job container (Compose and self-update operations)
```

The multi-stage Dockerfile is configured for Linux `arm64` and `amd64`; those runtime builds still need validation on Linux. A manually dispatched GitHub Actions workflow publishes a multi-platform image to GHCR. The host's Docker Compose installation pulls that image and does not build locally. The current `./data:/data` mount holds the SQLite database. Managed Compose sources will also live there. The Docker Unix socket will be mounted when inventory is implemented; it is absent from the current Compose file. The backend serves the built frontend, so production does not need a separate web server.

## Implemented Phase 1 paths

- `cmd/nox-yard/main.go` loads environment configuration, opens SQLite, starts the HTTP server, handles shutdown, and provides the interactive `reset-admin-password` command.
- `internal/store` owns schema migration, administrator data, and sessions. SQLite uses WAL, a busy timeout, and foreign keys. The database schema version is 1.
- `internal/auth` validates passwords and creates/verifies Argon2id hashes.
- `internal/httpapi` serves `/healthz`, `/api/bootstrap`, `/api/setup`, `/api/login`, `/api/logout`, and built frontend assets. Mutations check the request Origin; logout also checks a CSRF token. Session cookies are HttpOnly and SameSite Strict, with Secure cookies for HTTPS public origins.
- `web/src` contains the typed API client and React setup, login, and empty authenticated dashboard views. `web/src/styles/tokens.css` defines palette and semantic design tokens.

## Backend boundaries

- **API/auth:** same-origin HTTP endpoints, session and CSRF checks, input validation, and stream authorization. No browser request reaches the Docker socket directly.
- **Inventory:** a read model built from all Docker containers, with Compose projects grouped by `com.docker.compose.project` and standalone containers represented individually. Docker events trigger refreshes; periodic reconciliation handles missed events. Stored metadata supplements the live inventory but never replaces runtime state.
- **Docker adapter:** Engine API negotiation, inspect, stats, logs, exec, image pulls, and container lifecycle operations.
- **Compose adapter:** validation and operations for projects whose source was created, imported, or voluntarily adopted in NoX Yard. The source YAML and interpolation variables are stored persistently.
- **Job runner:** durable operations for pulls, deploys, recreation, updates, and removal. A temporary helper container continues operations if the web service is restarted or updates itself. Jobs record progress, outcome, and recoverable errors.
- **Storage:** SQLite for the initial administrator, hashed sessions, managed-project metadata, URL sources, update settings, and job history. Secrets and Compose variables remain server-side with restrictive file permissions.

New Go packages under `internal/` should follow these boundaries. Keep Docker SDK types out of public HTTP responses; map them to stable application models.

## Project model

The inventory exposes a common `Project` view with identity, kind (`managed-compose`, `external-compose`, or `standalone`), containers, aggregate state, health, CPU, memory, network usage, and uptime. A managed project remains visible when no containers exist because its source is stored. An external project without any remaining containers cannot be rediscovered from Compose metadata alone.

External Compose projects require no host file mounts or adoption to be managed. Engine-backed actions include container and group start/stop/restart, logs, inspect, terminal, image pull, safe recreation/update, opt-in auto-update, and confirmed removal. Editing/synchronizing the Compose structure or adding/removing services requires a stored Compose definition.

For external recreation, snapshot the Engine-visible container configuration, preserve mounts/networks/ports/environment/labels, start replacements, verify running or healthy state, and retain originals for rollback until the job succeeds. Block recreation with a clear reason when the configuration cannot be reproduced safely. Writable container layers are not persistent data and are not promised to survive recreation. A managed project uses Docker Compose for equivalent lifecycle operations.

NoX Yard appears in the normal inventory. Its restart or update runs through an independent temporary job container, because the web service may stop mid-operation. Stop and remove actions for NoX Yard are unavailable in the UI.

## API and live data

- REST: bootstrap state, setup/login/logout, inventory and detail reads, import preview/commit/sync, actions, job status, and auto-update settings.
- SSE: inventory changes, metrics, job progress, and live logs where one-way streaming is sufficient.
- WebSocket: authenticated, origin-checked bidirectional container terminal sessions.

Mutating calls return a job ID for long-running work. The frontend can reconnect and recover progress after a page or service restart. API payloads never return raw Docker SDK structs or secrets by default; sensitive values require an explicit reveal action.

## Compose import flow

1. Accept HTTPS public URL, pasted YAML, or uploaded YAML. Restrict URL fetches by scheme, destination, redirects, timeout, and size.
2. Collect interpolation variables and validate using `docker compose config`; reject unsupported local-file dependencies, builds, and relative bind mounts for newly managed projects.
3. Show a preview of the name, services, images, ports, volumes, and networks. Nothing is deployed before confirmation.
4. Persist the source and variables, pull public prebuilt images, then run Compose deployment as a durable job.
5. A repeated source URL offers a separate copy, sync of an existing associated project with diff and confirmation, or cancellation. A source matching an external project's name can be adopted only after comparison and explicit confirmation.

Pull downloads images without replacing containers. Update uses the stored Compose definition for managed projects or a safe Engine-based recreation for external projects. Auto-update is opt-in per project and runs on a configurable server-local schedule; the initial default is daily at 03:00.

## Authentication and failure handling

While no administrator exists, the first-run screen accepts a username and password. The database's single administrator row prevents two accounts from being created concurrently. After creation, only login is available. This intentionally leaves setup open until completed; initial access must be restricted to a trusted LAN/VPN. Passwords use Argon2id, sessions use opaque server-side tokens, and mutations require an exact Origin match; logout additionally requires a CSRF token. A local interactive command resets the administrator password and invalidates sessions. Future mutations must apply the same session and CSRF protections.

If Docker is unavailable, show the inventory as unavailable instead of stale success. Jobs must report partial failure, preserve enough state to retry or roll back, and never claim an update succeeded solely because the web request returned. SQLite and managed source files require persistent storage across restarts.

## References

- [Docker Engine API and SDK compatibility](https://docs.docker.com/reference/api/engine/)
- [Compose project and service labels](https://docs.docker.com/reference/compose-file/services/)
- [Compose config validation and rendering](https://docs.docker.com/reference/cli/docker/compose/config/)
- [Docker daemon access security](https://docs.docker.com/engine/security/)
