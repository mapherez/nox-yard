# MVP implementation plan

**Status:** approved direction; Phase 1 is in progress. This plan records the MVP scope and acceptance checkpoints. See [Progress](Progress.md) for what is implemented and verified.

## Outcome and constraints

Deliver an English-only, dark-only, responsive Docker management application for a single local Linux host. Raspberry Pi 5 is the primary target. Install through Docker Compose; use public registry images for the MVP. Reach the UI on a trusted LAN/VPN, with HTTPS supplied by a user-managed reverse proxy when needed.

The MVP includes project and standalone-container discovery, state and metrics, lifecycle operations, logs, inspect, terminal, creation from URL/paste/upload, validation and preview, image pulls, update/recreation, removal, local login, and opt-in auto-update. The NoX Yard service appears in the dashboard and can restart or update itself with confirmation.

## Delivery phases

### 1. Foundation

- Establish repository configuration and the canonical documentation in `Documentation/Developer/`.
- Implement the Go service, React/TypeScript/Vite application shell, SQLite migrations, production asset serving, Docker Compose installation, and Linux `arm64`/`amd64` image build.
- Add first-run username/password creation, persistent administrator record, login/logout, session protection, and local password recovery.
- Create dark-mode tokens, reusable layout and controls, responsive navigation, error states, and accessibility foundations.

**Checkpoint:** an empty but authenticated dashboard runs after `docker compose up`, survives a restart, and works on Pi 5 and amd64 Linux.

### 2. Inventory and direct Docker management

- Discover all containers through the Docker Engine; group Compose containers by project label and show standalone containers separately. Include running and stopped containers and NoX Yard itself.
- Add aggregate project cards, container details, CPU/memory/network sampling, uptime, health, ports, mounts, networks, and masked environment values.
- Implement group and container start/stop/restart, logs, inspect, interactive terminal, and Engine-backed image pull and confirmed removal.
- Stream changes and logs; handle Docker disconnection, missing containers, partial group failures, and stale browser sessions.

**Checkpoint:** an existing Compose project and a standalone container can be inspected and managed without access to their Compose file.

### 3. Managed Compose projects

- Accept public HTTPS URLs, pasted YAML, and uploaded files. Collect interpolation variables, validate with Compose, and preview name, services, images, ports, volumes, and networks before deployment.
- Support public prebuilt images, named volumes, and absolute host bind mounts. Reject builds, relative bind mounts, and unresolved local-file dependencies in new projects.
- Persist YAML, variables, and source metadata. Implement pull, deploy, start/stop/restart, update/recreate, logs, inspect, and removal with an optional volume deletion choice.
- Detect repeated source URLs and offer separate copy, sync with diff/confirmation, or cancel. Permit voluntary adoption of an external project only after comparison and confirmation.

**Checkpoint:** each input method deploys the same sample stack; invalid or unsupported input is rejected before any host change; managed configuration survives restart.

### 4. Safe update and scheduled operations

- Add durable job state, progress, restart recovery, and temporary independent job containers for operations that may stop the web service.
- For external containers, reconstruct only configurations that can be safely represented through Engine inspect. Preserve mounts, networks, ports, environment, and labels; verify replacements and roll back on failure. Block unsupported cases with a specific reason.
- Implement manual pull/update and project-level opt-in auto-update, initially scheduled daily at 03:00 server-local time. Check for a changed image before recreating; record every result.
- Support confirmed NoX Yard restart/update through the independent job path. For external project removal, delete containers and optionally exclusive identifiable volumes; leave shared resources intact.

**Checkpoint:** update succeeds for representative managed and external stacks, recovers from an induced failure, and can update the NoX Yard container without losing job history.

### 5. Validation and release

- Test Docker integration on real Linux Engine for running/stopped projects, standalone containers, health, logs, terminal, mounts and volumes, network attachment, failed pulls, rollback, and self-update.
- Test first-run concurrency, login rate limits, sessions, CSRF, URL destination checks, and secret masking.
- Verify responsive views and keyboard use. Publish versioned multi-architecture images and document install, upgrade, backup, recovery, and limits.

**Checkpoint:** a clean Raspberry Pi 5 installation completes first-run setup, deploys and updates a sample project, handles a pre-existing project, and retains data across restart.

## Interface contracts to define during implementation

Expose typed project, container, and job models through same-origin REST endpoints. Use SSE for read-only events/logs and an authenticated, origin-checked WebSocket for terminal sessions. Long operations return a job ID; clients obtain progress and final status from that ID. Internal Docker SDK structs and raw credentials are not API contracts.

The project model distinguishes managed Compose, external Compose, and standalone container views. Docker is authoritative for runtime state; SQLite is authoritative for NoX Yard ownership, source URLs, credentials, settings, and job history. Managed projects remain listed with zero containers. External projects with zero containers cannot be rediscovered without a stored source.

## Acceptance cases

- Two simultaneous first-run submissions create exactly one administrator; later visits show login only.
- Discovery groups Compose services correctly and does not misclassify standalone containers or temporary NoX Yard jobs.
- All three creation inputs produce a validated preview and require confirmation before pull/deploy; URL duplicates show all three agreed choices.
- A Compose update preserves named volumes by default; removal deletes volumes only when explicitly selected and safe.
- An external recreation retains observable Docker configuration and rolls back when replacement startup fails; unsupported containers receive an actionable reason.
- Auto-update never runs without project opt-in and does not recreate when the image is unchanged.
- Refreshing the browser or restarting NoX Yard does not lose the outcome of a long-running job.
- The dashboard and its contextual panels remain usable on desktop, tablet, and mobile with keyboard navigation.
