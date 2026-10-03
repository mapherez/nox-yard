# Architecture

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

The multi-stage Dockerfile targets Linux `arm64` and `amd64`. Frontend and Go builder stages run on BuildKit's build platform. The Go compiler receives the target OS and architecture, and the build checks the resulting binary's `GOARCH` before copying it into the target-platform Alpine image. Passing pushes to `master` publish a multi-platform image to GHCR after explicit builds and an ARM64 runtime smoke test. The host's Docker Compose installation pulls that image and does not build locally. The `./data:/data` mount holds the SQLite database and, later, managed Compose sources. The Docker Unix socket is mounted for local inventory access. The backend serves the built frontend, so production does not need a separate web server.

## Backend boundaries

- **API/auth:** browser HTTP endpoints with session, Origin, and CSRF checks, plus an independent `/v1` machine boundary with Bearer authentication and stable DTOs/errors. Both use the existing server and shared operation helpers. No client request reaches the Docker socket directly.
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

The implemented self-update flow, rollback snapshot, and operational preconditions are documented in [Self-Update](Self-Update.md).

## API and live data

The stable [Control API v1](Control-API.md) provides machine inventory, inspection, lifecycle, and image pulls on the same port. Protected routes are opt-in through explicit configuration. Public health/info do not require Docker availability. `/healthz` retains its SQLite-only contract. Machine routes do not expose streaming, terminal, managed editing/deployment, or self-update mutations. Helpers in `internal/httpapi/operations.go` retain the same inventory overlay and lifecycle controller used by browser handlers; only adapters' auth, DTOs, and errors differ.

- REST: bootstrap state, setup/login/logout, inventory and detail reads, import preview/commit/sync, actions, job status, and auto-update settings.
- SSE: inventory/metrics invalidations through `/api/projects/events`, and live container logs. Managed job progress is read through its job endpoint.
- WebSocket: authenticated, origin-checked bidirectional container terminal sessions.

Mutating calls return a job ID for long-running work. The frontend can reconnect and recover progress after a page or service restart. API payloads never return raw Docker SDK structs or secrets by default; sensitive values require an explicit reveal action.

### Inventory and metrics lifecycle

`GET /api/projects` makes one Docker `ContainerList(All: true)` call, groups the summaries, and joins in-memory metrics and pending operations plus managed metadata. It does not inspect containers, collect stats, or probe their filesystems. Health comes from the list response. Detailed inspection is requested when opening a container's details or explicitly revealing its environment. Logs, terminal, and lifecycle operations retain their own on-demand Docker checks.

The inventory reader owns two independent background workers. The metrics worker samples running containers with at most six concurrent stats requests, a four-second per-container timeout, a twenty-second cycle limit, and a five-second pause between cycles. The cache retains recent successful samples across transient failures and reports unknown values after thirty seconds. `/api/metrics` reads only this cache. No Docker I/O runs while holding the cache lock. Lifecycle events invalidate old samples and prevent in-flight responses from restoring them. Uptime uses observed start events or a lazy details inspection; an already-running container has unknown uptime after service startup until inspected or restarted.

The Docker event worker watches container create/start/stop/die/destroy/restart, health, pause/unpause, rename, update, kill, and OOM events. It reconnects with bounded backoff and a timestamp cursor, and requests reconciliation on reconnect because Docker event history is bounded. Notifications coalesce without blocking producers. The authenticated SSE endpoint sends invalidations, an initial reconciliation, and heartbeat comments; reconnecting clients fetch current state instead of relying on replay. Session validity is rechecked on heartbeats. Reverse proxies must permit streaming without buffering.

React coalesces event bursts for 100 ms and queues another inventory refresh if an event arrives during an in-flight request. Metrics notifications fetch only `/api/metrics`. Local actions expose pending states immediately and refresh inventory after their response, including failures; server-side pending states remain visible to other clients and last until managed jobs finish. Hidden tabs close SSE and resume with a fresh snapshot. A 15-second timer polls while disconnected, or reconciles after at least 60 seconds without an inventory refresh while connected. Polling is a recovery mechanism, not the normal update path.

The `/bin/sh` capability check runs only when opening a terminal. Known presence/absence is cached for five minutes and invalidated by lifecycle events; other Docker errors are not cached as missing shells.

## Compose import flow

1. Accept HTTPS public URL, pasted YAML, or uploaded YAML. Restrict URL fetches by scheme, destination, redirects, timeout, and size.
2. Collect interpolation variables and validate using `docker compose config`; reject unsupported local-file dependencies, builds, and relative bind mounts for newly managed projects.
3. Show a preview of the name, services, images, ports, volumes, and networks. Nothing is deployed before confirmation.
4. Persist the source and variables, pull public prebuilt images, then run Compose deployment as a durable job.
5. A repeated source URL offers a separate copy, sync of an existing associated project with diff and confirmation, or cancellation. A source matching an external project's name can be adopted only after comparison and explicit confirmation.

Pull downloads images without replacing containers. Update uses the stored Compose definition for managed projects or a safe Engine-based recreation for external projects. Auto-update is opt-in per project and runs on a configurable server-local schedule; the initial default is daily at 03:00.

## Authentication and failure handling

While no administrator exists, the first-run screen accepts a username and password. The database's single administrator row prevents two accounts from being created concurrently. After creation, only login is available. This intentionally leaves setup open until completed; initial access must be restricted to a trusted LAN/VPN. Passwords use Argon2id, sessions use opaque server-side tokens, and browser mutations require an exact Origin match; logout additionally requires a CSRF token. A local interactive command resets the administrator password and invalidates sessions. Future browser mutations must apply the same session, Origin, and CSRF protections. Machine mutations authenticate exclusively by Bearer and do not accept session cookies.

If Docker is unavailable, show the inventory as unavailable instead of stale success. Jobs must report partial failure, preserve enough state to retry or roll back, and never claim an update succeeded solely because the web request returned. SQLite and managed source files require persistent storage across restarts.

## References

- [Docker Engine API and SDK compatibility](https://docs.docker.com/reference/api/engine/)
- [Compose project and service labels](https://docs.docker.com/reference/compose-file/services/)
- [Compose config validation and rendering](https://docs.docker.com/reference/cli/docker/compose/config/)
- [Docker daemon access security](https://docs.docker.com/engine/security/)
