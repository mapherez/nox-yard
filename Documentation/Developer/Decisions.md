# Technical decisions

Record decisions here when they change the architecture, security model, product behavior, or development conventions. Add a date, decision, reason, and consequences; do not silently replace an earlier decision. The initial decisions were agreed during planning on **2026-09-24**; later entries carry their own dates.

## D-001 — English-only project

All UI text, API messages, code comments, and documentation are in English. There is no localization framework or language switch in the MVP. This keeps copy, design, and maintenance simple for the intended audience.

## D-002 — Go service and React frontend

Use Go for the local Docker-facing service and React/TypeScript/Vite for the browser. Serve production frontend assets from Go. SQLite and a persistent directory avoid a separate database service. Build Linux `arm64` and `amd64` images. The canonical Go module path is `github.com/mapherez/nox-yard`.

## D-003 — Docker Engine is the runtime source of truth

Discover Compose projects from Docker labels and standalone containers from the remaining Engine inventory. Do not require access to an existing Compose file. Stored NoX Yard metadata augments, but does not override, live Docker state.

## D-004 — Two levels of project management

External projects support safe operations possible through the Docker Engine, including update/recreation when their configuration can be reconstructed. New/imported/adopted projects store Compose source and support Compose-level operations. Optional adoption is explicit and follows a preview of differences. Unsupported external recreation is blocked rather than attempted with known configuration loss.

## D-005 — First-run administrator setup

The first browser visit while no administrator exists presents username/password creation. The setup remains available until the first account is atomically committed, then is permanently replaced by login. There is no initial token or time window. The deployment must initially be reachable only by trusted LAN/VPN users.

## D-006 — URL import and duplicate handling

The MVP imports Compose files from public HTTPS URLs only. The fetched source is stored; normal image updates do not silently replace its structure. Reusing a known URL offers a separate copy, explicit sync/update of an associated project after preview, or cancellation.

## D-007 — Persistence and deletion

Managed YAML, variables, credentials, settings, and job history persist across container restarts. Volume deletion is an explicit, initially unselected removal option. External volume deletion only targets identifiable exclusive volumes; shared resources are preserved.

## D-008 — Auto-update and self-management

Auto-update is opt-in per project, initially checked daily at 03:00 server-local time. NoX Yard appears in inventory and can restart/update after confirmation. An independent temporary job container survives replacement of the web service. The UI does not offer stop/remove for its own service.

## D-009 — Dark-only design and canonical docs

Use semantic CSS variables, reusable components, and responsive layouts from the first UI change. No light theme is built. `Documentation/Developer/` is the primary technical reference and is updated with implementation changes.

## D-010 — Session storage and first deployment boundary

Phase 1 stores only the administrator and hashed session tokens in SQLite. Session cookies are HttpOnly and SameSite Strict; HTTPS public origins use Secure cookies. The initial Compose installation omits the Docker socket until Docker inventory is implemented, so the current dashboard does not imply that host inventory is connected.

## D-011 — Manual GHCR publication and image-only host installation

Publish `ghcr.io/mapherez/nox-yard:latest` for Linux `amd64` and `arm64` only when the GitHub Actions workflow is started manually from `master`. The host Compose file references that image and contains no build context, allowing deployment with `docker compose pull` and `docker compose up -d`. The package must be public for anonymous pulls; a private installation requires GHCR authentication on the host. Manual publication keeps new pushes from changing the deployable image unexpectedly.

## D-012 — Target architecture must match the executable

The first published `arm64` image contained an `amd64` executable because the Go build stage defaulted its `TARGETARCH` argument to `amd64`. Build stages now use `BUILDPLATFORM`, import BuildKit's `TARGETOS` and `TARGETARCH` without defaults, and verify the Go binary's recorded architecture. Manual publication bypasses the build cache so a corrected image is rebuilt before it replaces `latest`.

## D-013 — Shared pre-push checks and gated automatic publication (2026-09-25)

This supersedes the manual publication policy in D-011 and the manual-publication detail in D-012. A clone-local pre-push hook calls repository scripts for formatting, vet, tests, frontend build, optional frontend checks, and Compose validation. GitHub repeats these checks, builds `amd64` and `arm64` images, and runs the ARM64 executable under QEMU with a `/healthz` smoke test. Pull requests never publish. The automatic branch publication policy originally adopted here is superseded by D-019. Normal CI still cancels older runs on the same ref; official publication now follows formal release tags only.

## D-014 — Temporary Engine-based self-update worker (2026-09-25)

The app checks the public GHCR `latest` manifest for its platform and uses the image config digest as the comparison ID. On change it pulls through the Engine API and starts a disposable helper from the exact old image ID. The helper preserves the original Compose container until the replacement passes `/healthz`, with a SQLite snapshot for rollback. Automatic checks are opt-in and run every six hours. This avoids a permanent updater service and Docker CLI in the runtime image.

## D-015 — Dedicated settings drawer and configurable check interval (2026-09-25)

This supersedes the fixed six-hour interval in D-014. Global update settings belong in a sidebar drawer so the dashboard remains focused on projects. The persisted interval defaults to 15 minutes and can be set to 5, 15, 30, 60, or 360 minutes. Saving enabled settings triggers an immediate check; scheduled checks are due within about one minute of the selected interval. Rounded UI surfaces use only `--radius-sm`.

## D-016 — Public Compose source intake (2026-09-28)

New managed projects may start from pasted YAML, an uploaded file, or a public HTTPS URL. Source intake limits the file to 1 MiB. URL retrieval uses port 443, rejects private and special-purpose DNS results, disables proxy use and redirects, and limits request duration. This restriction keeps the web service from fetching host-local or internal-network resources through the Docker-capable API. Compose validation and deployment remain separate steps after source intake.

## D-017 — Independent versioned machine Control API (2026-10-03)

Expose machine inventory, inspection, start/stop/restart, and image pulls under `/v1` on the existing HTTP server. Keep `/api/*` as the browser interface with its session, Origin, CSRF, streaming, and terminal behavior. Machine routes are disabled by default and use an explicit independent Bearer key; public health/info remain available. Separate DTOs and stable error codes protect clients from internal model changes. Shared operation helpers reuse inventory metadata, lifecycle implementations, and notifications. Generic project operations remain Engine-backed, including existing managed containers; they do not deploy missing services through Compose.

Preserve `/healthz` and its SQLite-only readiness for Docker healthchecks and self-update. Add independent application `buildVersion` metadata with runtime override, exact Git tag/full-SHA build fallback, and `dev` default. Keep `buildSHA` separate for self-update. The feature requires no new port, SQLite migration, CLI onboarding, or change to publication policy. See [Control API](Control-API.md) for the contract.

## D-018 — Embedded LAN MCP and shared orchestration (2026-10-05)

Expose an always-on, unauthenticated Streamable HTTP `/mcp` endpoint on the existing server and port for the private homelab LAN. Keep browser sessions, the opt-in Bearer `/v1` API, readiness, bind address and server timeouts unchanged. Use the latest normally resolved `github.com/mapherez/nox-mcp` Go module; updates use `go get github.com/mapherez/nox-mcp@latest` followed by `go mod tidy` and repository checks.

HTTP and MCP share a lightweight `internal/application` facade and the existing managers, with one inventory notifier for pending states and browser SSE. The facade propagates adapter contexts without new deadlines; existing asynchronous manager jobs keep their independent lifetimes. MCP configures a 60-second global runtime timeout, 32 concurrent requests and 8 MiB payloads. Long Compose/update work returns jobs/status. Engine pulls remain synchronous; a timeout leaves their outcome unconfirmed. Tool annotations explicitly describe reads, mutations and idempotence, including manual self-update with automatic updates disabled. Interactive terminal and continuous subscriptions stay in their existing interfaces. See [Embedded MCP](MCP.md).

## D-019 — Formal SemVer releases and a stable update channel (2026-10-06)

This supersedes the publication policy in D-013. `npm run release -- X.Y.Z` validates a clean repository, updates only `VERSION`, runs shared checks and a local Docker build, creates a release commit and annotated tag, and atomically pushes the current branch and tag. Pre-commit failures restore VERSION exactly; legitimate local release commits and tags survive later failures with explicit recovery commands.

Only the tag-triggered release workflow publishes official images and creates GitHub Releases. It repeats source checks on the tagged commit, refuses existing releases, and publishes both architectures with the explicit release tag, full commit SHA, and OCI metadata. Stable versions publish their version tag and `latest`; prereleases publish only their version tag. Publication is serialized and refuses to move latest behind a newer published stable release. Installation and self-update retain their existing latest channel and runtime implementation. Normal branch/PR CI retains validation builds and the ARM64 smoke test without registry publication. See [Releases](Releases.md).

## D-020 — Verified managed outcomes and explicit one-shot services (2026-10-08)

Compose CLI success is not application readiness. C1 verifies active services through Docker within two minutes after Compose returns, inside the existing overall operation deadline. Long-running containers must remain running for five seconds without restarts; configured healthchecks must be healthy. Missing, unhealthy, dead or unexpectedly exited services fail verification. An intentional one-shot service must exit zero and be declared by `nox-yard.lifecycle: oneshot` in its service labels, or referenced by a `depends_on` condition of `service_completed_successfully`. Inactive profiles and zero-replica services are excluded; active replica counts must be accounted for. Service names/status reasons may be returned; environment values and raw command output are not readiness errors.

Before C3, a failed update explicitly reports that automatic rollback was not performed. Health verification is a bounded observation, not a guarantee of indefinite uptime. This contract applies to deploy, sync, start, restart and update; pull/removal retain their own success semantics.

## D-021 — Adoption preserves the original directory and existing files (2026-10-08)

C1 resolves a common absolute host directory from live Compose working-directory labels, with an explicit user-supplied fallback if metadata is absent. Conflicting metadata or a mismatching supplied directory is rejected. Adoption persists the directory and materializes `compose.yml`/supplied env files there without recreating containers. Existing files must match the confirmed source; no existing file is overwritten by adoption. Preview/submit recheck runtime configuration and file identities; ambiguous configurations are blocked. Relative binds retain the original directory as their base. Raw runtime configuration and secret values stay internal; comparison reports field changes, not values.

Comparison is limited to supported observable settings. Unknown source fields, advanced mounts/networks, anonymous volumes and unsupported runtime overrides are blocked rather than omitted. Image defaults must be present locally. Legacy records missing their directory fail start/restart/update/sync safely instead of resolving relative binds against a temporary directory; migration/recovery requires separate upgrade validation.

Adoption also requires service identity labels that Compose recognizes: config hash, non-one-off marker and a positive container number. The working-directory label alone may use an explicit fallback. [Compose container discovery](https://github.com/docker/compose/blob/main/pkg/compose/containers.go) filters by config-hash and one-off labels; accepting otherwise labelled containers could leave originals unmanaged while creating duplicates. Hook runners and one-off containers are excluded from managed runtime verification.

Adoption verifies the absence of unprovided declared env files, including implicit `.env`. Existing files must be supplied and matched so later operations cannot load a different definition. Global `.env` cannot override reserved Compose/Docker controls; the same namespaces are already blocked in submitted interpolation variables. Separate service env files can still supply application values with those names.

## D-022 — Durable outcomes, supported updates and scheduling contracts (2026-10-08)

These are planned contracts for C2–C5. Preserve managed job status compatibility (`running`, `succeeded`, `failed`) and add stage/result fields for unchanged, verified replacement, successful rollback and recovery-required outcomes. A rolled-back update remains failed. Reconcile durable worker/resource state after interruption; never assume success or blindly replay mutation. Conflicting operations share resource ownership across adapters. Self-update retains its dedicated SQLite snapshot/recovery path.

External reconstruction supports only configurations that can be preserved and verified; unknown dependencies/settings are rejected before mutation. Retain original images/configuration until verification finishes. Runtime rollback does not revert shared-volume writes, application migrations or discarded writable container layers. File/metadata consistency for managed sync is part of C3.

Project schedules are separate from Yard self-update, disabled by default, and initially daily at 03:00 server-local time with the effective timezone shown explicitly. Skip stopped/unsupported/protected targets; deduplicate scheduled occurrences across restart/clock changes, coalesce missed checks, and use the same unchanged-image/health/rollback path as manual updates. Suppress automatic retries of a failed image until a new image or explicit retry. These capabilities remain planned until their acceptance gates pass.
