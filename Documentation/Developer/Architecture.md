# Architecture

## System boundary

NoX Yard manages one local Linux Docker Engine. The browser talks only to the NoX Yard web service; Docker socket access stays on the server side. The product is designed for a trusted LAN or VPN and may sit behind a user-managed HTTPS reverse proxy. Docker socket access gives the application broad control of the host, so the installation must not be exposed to untrusted users or networks.

```text
Browser (React)
  | REST, SSE, WebSocket
  v
NoX Yard service (Go) ---- SQLite (/data)
  |                       managed source/env files (host projects directory)
  | Docker Engine SDK
  v
Local Docker Engine ---- containers, images, networks, volumes
  ^
  | temporary job container (Compose and self-update operations)
```

The multi-stage Dockerfile targets Linux `arm64` and `amd64`. Frontend and Go builder stages run on BuildKit's build platform. The Go compiler receives the target OS and architecture, and the build checks the resulting binary's `GOARCH` before copying it into the target-platform Alpine image. Normal CI validates both architectures and runs an ARM64 smoke test without publication. Formal release tags publish a multi-platform image to GHCR after tagged-source checks; only stable releases update `latest`. The host's Docker Compose installation pulls that image and does not build locally. The `./data:/data` mount holds SQLite, including managed source/variables and job records. Newly managed source/env files are also written under the configured host projects directory, outside `/data`. The Docker Unix socket is mounted for local inventory access. The backend serves the built frontend, so production does not need a separate web server.

## Backend boundaries

- **Application facade:** shared transport-independent orchestration over the existing managers. A single inventory notifier connects HTTP/MCP operations, pending states and browser SSE. Adapter contexts are propagated without facade deadlines; existing asynchronous jobs retain their own lifetimes.
- **API/auth:** browser HTTP endpoints with session, Origin, and CSRF checks, plus an independent `/v1` machine boundary with Bearer authentication and stable DTOs/errors. An always-on unauthenticated LAN `/mcp` adapter shares that server/port. All management adapters use the application facade and existing managers. No client request reaches the Docker socket directly.
- **Inventory:** a read model built from all Docker containers, with Compose projects grouped by `com.docker.compose.project` and standalone containers represented individually. Docker events trigger refreshes; periodic reconciliation handles missed events. Stored metadata supplements the live inventory but never replaces runtime state.
- **Docker adapter:** Engine API negotiation, inspect, stats, logs, exec, image pulls, and container lifecycle operations.
- **External replacement:** assessed inspect snapshots, immutable platform images, independent workers, retained originals, dependency order, verified configuration/readiness and bounded rollback in `internal/recreate`.
- **Compose adapter:** validation and operations for projects whose source was created, imported, or voluntarily adopted in NoX Yard. The source YAML, environment files and interpolation variables are stored persistently. Managed replacement uses immutable images/platforms and a complete resolved definition; private snapshots, verified file/metadata commit and bounded rollback preserve the old version on ordinary failure. See [Managed updates](Managed-Updates.md).
- **Jobs:** durable operation records and atomic resource reservations coordinate managed, Engine and Yard mutations across adapters. Managed Compose and self-restart use independent workers; startup observes live workers/helpers and verifies completed work or retains uncertain reservations for host review. Self-update keeps its dedicated snapshot/rollback protocol and mirrors progress/results into history. See [Durable operations](Operation-Jobs.md).
- **Project scheduler:** daily server-clock slots, schema 8 opt-in settings/occurrences, one automatic worker globally and durable skipped/deferred results. Admission consumes an occurrence atomically with its existing update job/reservations; replacement remains in the managed/Engine managers.
- **Storage:** SQLite for the initial administrator, hashed sessions, managed-project metadata, URL sources, update settings, and job history. Secrets and Compose variables remain server-side with restrictive file permissions.

New Go packages under `internal/` should follow these boundaries. Keep Docker SDK types out of public HTTP responses; map them to stable application models.

## Project model

The inventory exposes a common `Project` view with identity, kind (`managed-compose`, `external-compose`, or `standalone`), containers, aggregate state, health, CPU, memory, network usage, and uptime. A managed project remains visible when no containers exist because its source is stored. An external project without any remaining containers cannot be rediscovered from Compose metadata alone.

External Compose projects require no host file mounts or adoption for Engine actions: container and group start/stop/restart, logs, inspect, terminal, image pull, assessed update/recreation and confirmed removal. Per-project opt-in auto-update uses these same verified update paths; see [Project schedules](Project-Schedules.md). Editing/synchronizing the Compose structure or adding/removing services requires a stored Compose definition.

C4 snapshots Engine-visible configuration, preserves assessed mounts/networks/ports/environment/labels and resource/security settings, verifies immutable replacements and retains original containers until success. Unsupported configurations are blocked before mutation. Dependency-aware rollback restores original IDs/names and running/stopped state; standalone history follows old/new identities. Explicit volume choice defaults off and shared resources remain protected across Engine and managed removal. See [External updates](External-Updates.md) for supported fields, bounds and recovery. Writable container layers are not persistent data. Managed projects use Docker Compose followed by bounded Docker runtime verification. Active replicas must remain running for five seconds and configured healthchecks must be healthy; declared one-shot services must exit zero. C3 snapshots supported prior configuration and restores old immutable images/configuration and running/stopped state after ordinary update/sync failure; uncertain restoration retains reservations for review.

NoX Yard appears in the normal inventory. Its restart or update runs through an independent temporary job container, because the web service may stop mid-operation. Stop and remove actions for NoX Yard are unavailable in the UI.

The implemented self-update flow, rollback snapshot, and operational preconditions are documented in [Self-Update](Self-Update.md).

## API and live data

The [embedded MCP endpoint](MCP.md) exposes discrete inventory, lifecycle, Compose, removal, settings and self-update operations plus finite log snapshots on `/mcp`. It uses the NoX MCP Go library with a 60-second global runtime timeout. Interactive terminal and continuous browser streams keep their existing interfaces.

The stable [Control API v1](Control-API.md) provides machine inventory, inspection, lifecycle, and image pulls on the same port. Protected routes are opt-in through explicit configuration. Public health/info do not require Docker availability. `/healthz` retains its SQLite-only contract. Machine routes do not expose streaming, terminal, managed editing/deployment, or self-update mutations. The `internal/application` facade retains the inventory overlay and operation orchestration used by all management adapters; their authentication, DTOs and error presentation remain independent.

- REST: bootstrap state, setup/login/logout, inventory and detail reads, import preview/commit/sync, actions, job status, and Yard self-update settings. Per-project update schedules expose opt-in, timezone and last/next result.
- SSE: inventory/metrics invalidations through `/api/projects/events`, and live container logs. Managed job progress is read through its job endpoint.
- WebSocket: authenticated, origin-checked bidirectional container terminal sessions.

Managed mutations return a job ID. The contextual drawer reads typed job history after reload/reopening, including progress, recovery and cleanup results. Engine pulls remain synchronous and also persist operation records; uncertain interruption retains reservations instead of permitting blind retry. API payloads never return raw Docker SDK structs or secrets by default; sensitive values require an explicit reveal action.

### Inventory and metrics lifecycle

`GET /api/projects` makes one Docker `ContainerList(All: true)` call, groups the summaries, and joins in-memory metrics and pending operations plus managed metadata. It does not inspect containers, collect stats, or probe their filesystems. Health comes from the list response. Detailed inspection is requested when opening a container's details or explicitly revealing its environment. Logs, terminal, and lifecycle operations retain their own on-demand Docker checks.

The inventory reader owns two independent background workers. The metrics worker samples running containers with at most six concurrent stats requests, a four-second per-container timeout, a twenty-second cycle limit, and a five-second pause between cycles. The cache retains recent successful samples across transient failures and reports unknown values after thirty seconds. `/api/metrics` reads only this cache. No Docker I/O runs while holding the cache lock. Lifecycle events invalidate old samples and prevent in-flight responses from restoring them. Uptime uses observed start events or a lazy details inspection; an already-running container has unknown uptime after service startup until inspected or restarted.

The Docker event worker watches container create/start/stop/die/destroy/restart, health, pause/unpause, rename, update, kill, and OOM events. It reconnects with bounded backoff and a timestamp cursor, and requests reconciliation on reconnect because Docker event history is bounded. Notifications coalesce without blocking producers. The authenticated SSE endpoint sends invalidations, an initial reconciliation, and heartbeat comments; reconnecting clients fetch current state instead of relying on replay. Session validity is rechecked on heartbeats. Reverse proxies must permit streaming without buffering.

React coalesces event bursts for 100 ms and queues another inventory refresh if an event arrives during an in-flight request. Metrics notifications fetch only `/api/metrics`. Local actions expose pending states immediately and refresh inventory after their response, including failures; server-side pending states remain visible to other clients and last until managed jobs finish. Hidden tabs close SSE and resume with a fresh snapshot. A 15-second timer polls while disconnected, or reconciles after at least 60 seconds without an inventory refresh while connected. Polling is a recovery mechanism, not the normal update path.

The `/bin/sh` capability check runs only when opening a terminal. Known presence/absence is cached for five minutes and invalidated by lifecycle events; other Docker errors are not cached as missing shells.

## Compose import flow

1. Accept HTTPS public URL, pasted YAML, or uploaded YAML. Restrict URL fetches by scheme, destination, redirects, timeout, and size.
2. Collect interpolation variables/env files and validate using `docker compose config`; reject builds and unresolved local dependencies. New host-based projects support relative bind mounts resolved from their stored host project directory.
3. Show a preview of the name, services, images, ports, volumes, and networks. Nothing is deployed before confirmation.
4. Persist submitted source/variables for initial deployment and launch an independent durable worker. Pull images before writing initial active source files; apply complete resolved definitions with immutable IDs/platforms and verify Docker readiness. Sync keeps old files/metadata until verification, then journals the file commit before guarded metadata finalization. Supported update/sync failures restore old source/runtime; uncertain workers or partial commits retain reservations without replay.
5. A repeated source URL offers a separate copy, sync of an existing associated project with diff and confirmation, or cancellation. A source matching an external project's name can be adopted only after comparison and explicit confirmation. Adoption resolves the original host directory from Compose metadata or an explicit fallback, rechecks runtime/file fingerprints, and saves source/env files without recreating containers or overwriting existing files. Relative binds retain that original base. Unsupported adoption configurations are blocked.

Pull downloads images without replacing containers. Managed update pulls from the stored definition without fetching its URL, preserves container IDs when images are unchanged, and verifies changed immutable targets with bounded rollback. URL source changes remain explicit previewed sync. External update uses confirmed runtime snapshots and retains originals until verification; recreate uses current deployed images without a pull. Project auto-update is opt-in and runs daily at 03:00 in the explicit server timezone (UTC by default). Durable occurrences coalesce missed checks, serialize automatic workers and suppress known failed immutable image sets; see [Project schedules](Project-Schedules.md). Yard self-update retains its separate implemented settings and worker.

## Authentication and failure handling

While no administrator exists, the first-run screen accepts a username and password. The database's single administrator row prevents two accounts from being created concurrently. After creation, only login is available. This intentionally leaves setup open until completed; initial access must be restricted to a trusted LAN/VPN. Passwords use Argon2id, sessions use opaque server-side tokens, and browser mutations require an exact Origin match; logout additionally requires a CSRF token. A local interactive command resets the administrator password and invalidates sessions. Future browser mutations must apply the same session, Origin, and CSRF protections. Machine mutations authenticate exclusively by Bearer and do not accept session cookies.

If Docker is unavailable, show the inventory as unavailable instead of stale success. Jobs must report partial failure, preserve enough state to retry or roll back, and never claim an update succeeded solely because the web request returned. SQLite and managed source files require persistent storage across restarts.

## References

- [Docker Engine API and SDK compatibility](https://docs.docker.com/reference/api/engine/)
- [Compose project and service labels](https://docs.docker.com/reference/compose-file/services/)
- [Compose config validation and rendering](https://docs.docker.com/reference/cli/docker/compose/config/)
- [Docker daemon access security](https://docs.docker.com/engine/security/)
