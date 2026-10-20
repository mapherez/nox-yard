# Technical decisions

Record decisions here when they change the architecture, security model, product behavior, or development conventions. Add a date, decision, reason, and consequences; do not silently replace an earlier decision. The initial decisions were agreed during planning on **2026-09-24**; later entries carry their own dates.

For push/release validation and concurrency, D-036 supersedes the local-hook and concurrency details in D-013/D-029. D-039 retires D-031's one-off converter. D-040 supersedes the earlier independent machine-authentication assumptions. D-041 defines release signing and the updater's image-trust boundary.

Entries preserve the decision made at their date. For current behavior, D-029 supersedes the publication/check/concurrency details in D-011–D-013/D-019; D-031 supersedes DB-active configuration and interval-based/separate Yard scheduling in D-004/D-007/D-014/D-015/D-022/D-026. D-033/D-034 extend initializer inventory and Recreate scope. Read these later entries together with the linked feature contracts, rather than treating every historical statement as current.

## D-001 — English-only project

All UI text, API messages, code comments, and documentation are in English. There is no localization framework or language switch in the MVP. This keeps copy, design, and maintenance simple for the intended audience.

## D-002 — Go service and React frontend

Use Go for the local Docker-facing service and React/TypeScript/Vite for the browser. Serve production frontend assets from Go. SQLite and a persistent directory avoid a separate database service. Build Linux `arm64` and `amd64` images. The canonical Go module path is `github.com/mapherez/nox-yard`.

## D-003 — Docker Engine is the runtime source of truth

Discover Compose projects from Docker labels and standalone containers from the remaining Engine inventory. Do not require access to an existing Compose file. Stored Yard metadata augments, but does not override, live Docker state.

## D-004 — Two levels of project management

External projects support safe operations possible through the Docker Engine, including update/recreation when their configuration can be reconstructed. New/imported/adopted projects store Compose source and support Compose-level operations. Optional adoption is explicit and follows a preview of differences. Unsupported external recreation is blocked rather than attempted with known configuration loss.

## D-005 — First-run administrator setup

The first browser visit while no administrator exists presents username/password creation. The setup remains available until the first account is atomically committed, then is permanently replaced by login. There is no initial token or time window. The deployment must initially be reachable only by trusted LAN/VPN users.

## D-006 — URL import and duplicate handling

The MVP imports Compose files from public HTTPS URLs only. The fetched source is stored; normal image updates do not silently replace its structure. Reusing a known URL offers a separate copy, explicit sync/update of an associated project after preview, or cancellation.

## D-007 — Persistence and deletion

Managed YAML, variables, credentials, settings, and job history persist across container restarts. Volume deletion is an explicit, initially unselected removal option. External volume deletion only targets identifiable exclusive volumes; shared resources are preserved.

## D-008 — Auto-update and self-management

Auto-update is opt-in per project, initially checked daily at 03:00 server-local time. Yard appears in inventory and can restart/update after confirmation. An independent temporary job container survives replacement of the web service. The UI does not offer stop/remove for its own service.

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

MCP exposure and authentication in this historical decision are superseded by D-040.

Expose an always-on, unauthenticated Streamable HTTP `/mcp` endpoint on the existing server and port for the private homelab LAN. Keep browser sessions, the opt-in Bearer `/v1` API, readiness, bind address and server timeouts unchanged. Use the latest normally resolved `github.com/mapherez/nox-mcp` Go module; updates use `go get github.com/mapherez/nox-mcp@latest` followed by `go mod tidy` and repository checks.

HTTP and MCP share a lightweight `internal/application` facade and the existing managers, with one inventory notifier for pending states and browser SSE. The facade propagates adapter contexts without new deadlines; existing asynchronous manager jobs keep their independent lifetimes. MCP configures a 60-second global runtime timeout, 32 concurrent requests and 8 MiB payloads. Long Compose/update work returns jobs/status. Engine pulls remain synchronous; a timeout leaves their outcome unconfirmed. Tool annotations explicitly describe reads, mutations and idempotence, including manual self-update with automatic updates disabled. Interactive terminal and continuous subscriptions stay in their existing interfaces. See [Embedded MCP](MCP.md).

## D-019 — Formal SemVer releases and a stable update channel (2026-10-06)

This supersedes the publication policy in D-013. `npm run release -- X.Y.Z` validates a clean repository, updates only `package.json`, runs shared checks and a local Docker build, creates a release commit and annotated tag, and atomically pushes the current branch and tag. Pre-commit failures restore `package.json` exactly; legitimate local release commits and tags survive later failures with explicit recovery commands.

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

These define contracts for C2–C5. C2 implements durable ownership, progress, verified/recovery outcomes and contextual history as specified in D-023. C3 implements unchanged-image detection and managed rollback as specified in D-024; C4 implements external recreation/removal as specified in D-025. The detailed scheduling contract is specified in D-026. Preserve managed job status compatibility (`running`, `succeeded`, `failed`); a rolled-back update remains failed. Self-update retains its dedicated SQLite snapshot/recovery path.

External reconstruction supports only configurations that can be preserved and verified; unknown dependencies/settings are rejected before mutation. Retain original images/configuration until verification finishes. Runtime rollback does not revert shared-volume writes, application migrations or discarded writable container layers. File/metadata consistency for managed sync is part of C3.

Project schedules are separate from Yard self-update, disabled by default, and initially daily at 03:00 server-local time with the effective timezone shown explicitly. Skip stopped/unsupported/protected targets; deduplicate scheduled occurrences across restart/clock changes, coalesce missed checks, and use the same unchanged-image/health/rollback path as manual updates. Suppress automatic retries of a failed image until a new image or explicit retry. Acceptance evidence and remaining host checkpoints are tracked in Progress.

## D-023 — Durable resource ownership and reviewed recovery (2026-10-08)

Use SQLite operation records plus unique resource reservations across adapters/managers. Managed Compose and Yard restart run in narrowly mounted independent containers from the exact current image, claim once and persist progress/results. Register worker identity before starting it; a lost create/start response retains ownership for observation. Self-update preserves its dedicated snapshot/rollback protocol while recording shared ownership/history.

Observe running workers and child helpers after web restart. Verify completed commands without replay; otherwise retain resources with a recovery-required failure. Interrupted synchronous Engine requests also retain uncertain reservations, including per-target timeout causes in partial results. Cleanup failures are distinct from operation failures. Recovery acknowledgement requires current revision, explicit host review and no active worker/helper; it releases reservations without changing application containers/files or turning failure into success. C3 permits cleanup of temporary rollback image references after acknowledgement.

Keep execution payloads and ownership tokens private, use safe public errors, and restore typed contextual history after drawer/browser/backend reload. Browser recovery uses existing session/Origin/CSRF protection; MCP exposes equivalent lookup/history/acknowledgement. Control API v1 retains its response and route contracts. See [Durable operations](Operation-Jobs.md).

Managed metadata writes/deletion, terminal outcome and reservation release commit atomically behind the operation's ownership/status guard. Late observations cannot overwrite a newer project's metadata. C3 adds the source journal and bounded rollback described below.

## D-024 — Immutable managed targets, verified source commit and rollback (2026-10-08)

Keep cache-only pull, stored-definition image update and previewed URL source sync separate. Compare all active deployed service/replica image IDs; unchanged updates preserve container IDs/state and report `unchanged`, without certifying pre-existing health. Replacement consumes the complete resolved Compose model over stdin, with inspected immutable IDs/platforms and pulls disabled. Preserve the original source image reference through reserved metadata for inventory/Engine pull compatibility, never for replacement identity. Preserve the original directory for relative binds and literal-dollar values; reject tilde binds instead of guessing a host home.

Journal prior source/env/variables and full inspections privately before mutation. Assess the old runtime against the supported adoption contract, refusing drift or settings/state that cannot be restored. Retain two temporary references per previous image until verification/recovery review; Yard's non-forced image removal cannot discard them during replacement. No user image tag is retargeted by rollback.

Keep old sync files active until target readiness and image verification pass. Commit owned files with restrictive, symlink-checked same-directory atomic renames, then finalize metadata/status/lock release in guarded SQLite. Persist `committing_files` because host multi-file writes and SQLite are not one transaction. Restore known old/candidate partial writes on ordinary failure; ambiguous files, vanished helpers/workers and failed rollback remain reserved for explicit recovery. Reconciliation may verify a completed replacement and finish a pristine source commit, but never replay replacement or an uncertain partial journal.

Rollback has its own three-minute bound, recreates old immutable configuration, restores running/stopped state and verifies supported settings/readiness. A restored failure records failed/rolled_back/restored; cleanup failures remain distinct and retryable. Named-volume/bind data survives; application writes/migrations and writable container layers are not undone. Successful one-shot recreation may execute again. Initial deploy has no prior app and reports partial resources plus retry/cleanup guidance. See [Managed updates](Managed-Updates.md) for details and acceptance limits.

## D-025 — Assessed Engine replacement and explicit removal (2026-10-08)

Standalone and complete external Compose targets use confirmed inspect snapshots, immutable image IDs and the existing independent-worker/reservation protocol. Unknown configured settings and unpreservable observable dependencies are refused. Update pulls and skips replacement when every image is unchanged; recreate uses deployed images. Changed groups retain all originals, replace in dependency order and restore original IDs/names/configuration/state on ordinary failure. Uncertain outcomes retain ownership and private recovery journals without replay. Stable lineage reservations keep standalone history across repeated replacements. Only verified success removes retained originals, without deleting data volumes.

Removal defaults to keeping volumes. Explicit selection and a matching current fingerprint permit only exclusive identifiable volumes; shared/external resources, binds and host source remain. Managed removal uses the same assessed Engine plan before guarded metadata deletion, including older adapter requests assessed at submission. Application cleanup failures remain distinct after helper cleanup. Generic Engine removal remains runtime-only for managed projects. Runtime rollback does not undo shared data writes/migrations or copy writable layers. See [External updates](External-Updates.md) for assessment limits, adapter compatibility and recovery.

## D-026 — Opt-in daily project scheduling and failed-image suppression (2026-10-08)

Use 03:00 in the configured server IANA timezone (`TZ`, UTC default; Compose maps `NOX_TIMEZONE`). Calculate civil dates with timezone/DST rules, never browser time or elapsed 24-hour intervals. New/existing projects remain disabled until explicit per-project opt-in. Enabling requires a supported healthy running target; a later stop, unsafe drift, missing target or protected Yard/helper skips the check without starting it. Declared successful one-shot dependencies are allowed when long-running services are running.

Add schema 8 schedule rows/occurrences and private candidate fingerprints to durable jobs. Consume a civil-date occurrence atomically with job admission/resource reservation. Reconcile interrupted jobs first, then coalesce missed slots to the latest eligible daily slot; never replay all missed days or the same civil date. Limit admissions to four due rows per tick and one automatic worker globally, including pending cleanup/recovery. Manual work keeps normal resource ownership; busy projects get a durable skipped reason, global contention defers without consuming the slot.

Execute through the existing managed/Engine update contracts. Compare immutable service/platform image sets independent of container IDs, persist the candidate before replacement, and suppress the last known failed set across restart/disable/re-enable. New images or explicit manual retries remain possible; a verified/unchanged successful manual update clears the failure fence. Rollback and recovery failures stay failed. Disabling is rechecked before pull and immediately before replacement; it cannot undo an already-started replacement. Successful removal disables matching settings, while standalone schedules follow guarded replacement lineage. Keep project settings/history contextual and expose equivalent browser/MCP adapters without changing Control API v1 or Yard self-update.

## D-027 — Managed init/logging comparison and effective rollback settings (2026-10-15)

Extend D-021/D-024's assessed comparison to Compose `init` and `logging`. Compare the init flag and declared logging overrides without exposing values. Logging fields omitted from source may inherit daemon defaults: Engine merges default options even for an explicitly selected matching driver, so additional observed options do not prove drift. Pin the complete inspected logging configuration and explicit init state in the private rollback definition, preserving Compose literal-dollar escaping. Refuse mixed replica settings before mutation and verify the restored effective settings against the original inspections, including inherited options. Unsupported fields remain blocked with a distinct unsupported-comparison error naming the service/field; malformed source retains invalid-source classification. See [Managed updates](Managed-Updates.md).

## D-028 — Managed tmpfs configuration comparison (2026-10-15)

Support service-level tmpfs and long-syntax tmpfs volumes in D-021/D-024's comparison. Docker may omit these mounts from its ordinary mount list; compare the two HostConfig representations instead. Keep option strings intact and compare long-syntax target, read-only, size and mode. Unknown advanced tmpfs settings remain blocked. Replacement and rollback preserve configuration using the full resolved definition and verify it through the same comparison; tmpfs data remains ephemeral. Display tmpfs targets in the existing mount preview. See [Managed updates](Managed-Updates.md) for semantics and acceptance coverage.

## D-029 — CI gates script-requested publication in one pipeline (2026-10-15)

Supersede D-019's independent tag-triggered publication. Every branch push runs CI; tag pushes and pull-request events do not independently repeat those runs. The release script creates a version-only commit with an explicit release trailer and matching annotated tag, then pushes both atomically. CI validates that identity and calls the reusable publisher only after source validation and both-architecture builds/ARM64 smoke pass. Publication stays on that exact commit, skips repeated source validation and imports CI's Docker caches. The script leaves validation to CI and any installed pre-push hook instead of explicitly repeating checks and a local image build. Failed/canceled CI cannot publish. Global publication serialization remains separate from validation concurrency; a matching completed publication is a successful no-op, while conflicts/downgrades remain blocked. See [Releases](Releases.md).

## D-030 — Sync retains saved inputs and checks occupied host ports (2026-10-15)

An omitted Sync input preserves its saved value while the variable/file remains declared by the new source. Explicit replacements, including empty strings, remain distinct from omission. New `.env` assignments supersede inherited interpolation values; explicit interpolation inputs have highest precedence. Keep effective inputs private, fingerprint their resolved configuration and persist the same inputs in the worker payload. New/copy requests do not inherit another project. The form sends edited empty values and omits untouched fields.

Check fixed host bindings against other active Docker containers during actionable preview/confirmation and immediately before replacement, allowing the target project's current bindings. Report the port/protocol, requesting service and occupying container. Also reject overlapping fixed candidate bindings across services. A late conflict fails without mutation or rollback. Docker remains authoritative for races, ephemeral/range allocation, arbitrary host processes and host-network listeners. Acceptance uses disposable real port owners to verify omission preserves a saved override despite an occupied source default, and that conflicts before/after confirmation preserve running containers and source. See [Managed updates](Managed-Updates.md).

## D-031 — Host configuration and uniform project management (2026-10-15)

Host Compose, `.env` and service environment files become authoritative. Reuse Docker's original paths and ordered Compose labels, retaining location metadata for stdin deployments. Inline variable editing and a collapsed full-file accordion validate and write with fingerprint concurrency checks; Save does not restart. Start/Stop/Restart affect existing containers; Pull only downloads; Recreate applies current files and cached images without pull/build, including unchanged images. Remove Update from menus while retaining API/schedule compatibility. Missing files keep assessed Engine management; invalid/inaccessible files must fail instead of selecting stale state.

Automatically export legacy active DB configuration on web startup. Preserve conflicting manual files, resume partial exports and block affected editing/recreation/automatic jobs until verified Ready. Persist an exact private native recovery snapshot before guarded SQL cleanup; keep job snapshots, accounts, preferences and history in SQLite. Ownership follows the host project/base owner without recursively changing application data. Expose authenticated, CSRF-protected, explicitly revealed reads and fingerprinted writes.

Yard uses the same drawer and daily schedule, default 03:00 server time with existing on/off retained. Disable the interval timer. Keep only status, last checked, build and Check now in Settings. Independent self-recreation applies host files and snapshots SQLite after stopping the web process, restoring it before restarting the previous runtime after failure. See [Configuration files](Configuration-Files.md) and [Self-update](Self-Update.md).

## D-032 — MCP host configuration parity (2026-10-15)

Expose host-file reads, whole-file saves and individual service-variable edits through three MCP tools calling the shared application facade directly. Reuse file validation, fingerprint checks, operation reservations and recovery; add no internal HTTP calls or parallel Docker implementation. Default reads and write responses hide file contents and variable values, while explicit reveal returns the current saved configuration. Saving remains separate from reviewed Recreate. One-time migration management stays outside the MCP catalog.

Resolve a container's current Compose membership before Pull and use only that service's current file reference. Project Pull retains project scope, and missing files alone allow assessed runtime fallback. Update tool descriptions for Recreate and Yard's daily schedule while retaining existing tool names and legacy self-update input fields. This adapter extension uses the existing NoX MCP runtime without a dependency upgrade. See [Embedded MCP](MCP.md).

## D-033 — Completed initializers in project inventory (2026-10-16)

Count successful one-shot initializers as satisfied members when aggregating a running project. Identify them from Compose's deployed dependency-condition labels or the existing explicit lifecycle label, scoped by project and excluding one-off runs/hooks/helpers. Docker's list status must explicitly report `Exited (0)`; unknown or failed exits cannot imply success. Retain raw container state, expose an optional `completed` marker to browser/MCP inventory, and display Completed in container badges. Keep all-stopped projects Stopped and ordinary stopped services Partial when peers run. Recompute on each list response without file access, extra Docker probes or stale completion caches. Healthcheck aggregation and lifecycle actions remain independent of this display correction.

## D-034 — Recreate one Compose service (2026-10-16)

Add optional `service` to the existing browser/MCP recreate request for Compose `recreate` operations. Container details select that service's replicas; project actions retain whole-project behavior. Bind scope, current host files and runtime state to the reviewed fingerprint, and refuse unknown services instead of widening scope. Reuse independent workers, shared reservations and private journals. Native execution uses local pinned images, `--no-build --pull never --no-deps` and no orphan removal. Recovery, image retention and verification target only the selected service; unselected container identities/state must remain unchanged. Missing-file Engine fallback assesses dependencies without including them in replacement or rollback. Explicit incoming namespace/link/volumes-from references block unsafe replacement, including references from other projects.

Permit development `build:` alongside an explicit `image:` by removing build metadata solely from the private resolved execution model. Host files are preserved, builds never run and build-only services still fail. Retain stopped service state and require consistent running/stopped states across replicas before scoped recreation. See [Configuration files](Configuration-Files.md) for the contract and validation fixture.

## D-035 — Yard product identity (2026-10-20)

The application is named **Yard** and belongs to the **NoX** environment. Use Yard in product copy, documentation, logs, errors and human-facing MCP titles/descriptions. Keep `nox-yard` for the repository, module/package names, executable, Docker images/services/labels, stored data and protocol identifiers, and retain existing `NOX_*` environment variables and release markers. This is a presentation change with no identifier/data migration. Use a text-only Yard wordmark and a white Y favicon on blue; see [Design system](Design-System.md).

## D-036 — CI-only automatic validation and a single pipeline lane (2026-10-20)

Supersede D-013's automatic full-suite pre-push hook and D-029's branch/SHA-specific job concurrency plus separate noncancelable publisher. Pushes run no project tests/builds locally. Retain focused/manual source checks; the complete existing source/frontend/release checks, workflow lint, both architecture builds and ARM64 smoke remain CI gates before publication. `scripts/install-hooks.sh` is a compatibility migrator that removes only its recognized old hook, preserving custom configurations. New clones install no hook.

Use one constant workflow-level concurrency group for the entire CI caller, including the reusable publisher. A newer branch push cancels the older pipeline and replaces an older pending run repository-wide. Chain source validation, Docker builds/smoke and conditional release through job dependencies; never run a separate tag-triggered publisher or reuse the parent's concurrency group inside the callee. Tests are not repeated by publication. Image publication retains the existing CI cache reuse and exact validated source SHA/tag.

The release command still checks a clean working tree/index, SemVer, Git identity and local/remote tag conflicts before preparing its version-only commit. Clean local commits ahead of origin are supported and travel in the same atomic push as the release commit/tag. Missing remote commits must be integrated first; no force push is added. A canceled publication cannot undo already written registry/GitHub resources; preserve the existing completed-publication identity and conflict checks on retry. See [Releases](Releases.md) and [Development](Development.md).

## D-037 — Searchable project grid/list and persistent ordering (2026-10-20)

Keep the dashboard's existing project grouping and details flow while adding a compact table as the List alternative to Grid. Keep approximately 44px rows at every viewport, with contained horizontal scrolling instead of stacked metadata. Search the existing inventory immediately by project/container/service name and show a matched container's parent project. Place the full-width search above the desktop/tablet header and immediately below the mobile header. Keep total inventory counts independent of the filter.

Use one shared order for both views, natural A–Z/Z–A sorting and insertion-style reordering with a dedicated pointer/touch grip, floating ghost, empty blue destination and temporary neighbor movement. Support keyboard arrows/Home/End, cancellation and reduced motion. Filtered reordering exchanges only visible slots; hidden projects retain their positions. Cancel a gesture if inventory membership/order changes, preserving it across metric updates. Persist layout, sort mode and manual IDs in origin-local storage scoped by administrator; fall back to in-memory state when unavailable. Search remains transient. No backend schema/API or Docker resource changes are required. See [Features](Features.md) and [Design system](Design-System.md).

## D-038 — Uniform drawers and explicit log snapshots (2026-10-20)

Use one width for Project, Settings and New Project drawers: half the viewport capped at 60rem on desktop, nearly fullscreen below 1280px, fullscreen below 768px. Strengthen the existing three tabs and section hierarchy while retaining the established palette. Every Compose project starts at the overview/container picker, regardless of container count; standalone targets start at container details. Keep immediate actions first, compact schedule controls below primary information, and row-major wrapping ports/environment fields. Replace symbolic interface controls with the existing Phosphor icon set. Logs and terminal fill the remaining height without changing terminal protocol behavior.

Search highlights literal case-insensitive matches in the current log buffer; it does not filter or fetch historical output. Explicit export freezes that buffer, requires confirmation and saves UTF-8 text through an authenticated Origin/CSRF-protected route and a confined, network-isolated host helper. Always use the configured `BASE/PROJECT_NAME/logs`, including external projects whose Compose files live elsewhere; standalone targets use `BASE/CONTAINER_NAME/logs`. Create missing named directories on write only, preserve host ownership and existing files, reject symbolic-link directories and do not move Compose files. Use server-minute timestamps with collision suffixes and derive Last export from the newest valid filename. No client-chosen filesystem paths or database migration are needed. See [Features](Features.md).

## D-039 — Retire the completed configuration migration (2026-10-20)

Remove D-031's one-off DB-to-file converter after the installed projects completed conversion. Delete its startup worker, recurring backend scan, global browser status/polling, per-project migration polling/gating/actions, retry/status APIs and migration-only tests. Read current file configuration on demand. Historical operation records and compatibility database columns remain data; ordinary SQLite schema initialization is independent of this removed feature.

Deploy/copy/Sync persist interpolation values directly in `.env` through the existing guarded file journal, and adoption saves them within its reviewed operation. Successful jobs atomically commit their result and location-only metadata; private job payloads retain execution/recovery data. File writes inherit host ownership without a later repair pass. File-backed update with unchanged images preserves container identities; explicit Recreate still applies saved configuration. Retain file validation, optimistic concurrency and operation reservations. Verify zero migration requests, direct deployment persistence, ownership and rollback using disposable fixtures. Installations with unconverted DB-only configuration must complete conversion on the preceding release before upgrading. See [Configuration files](Configuration-Files.md).

## D-040 — Opt-in MCP, local machine authorization and pending network changes (2026-10-20)

Serve MCP on its own listener/internal port 8081; keep browser/Control API on 8080. Default both host publications to loopback, MCP off, and local machine access to inspection. Offer explicit anonymous MCP independently of the always-authenticated optional Control API. Share one active random token, SHA-256 digest, transactional rotation/revocation and last-ten-events audit across both interfaces. Display the generated token once. Import an enabled legacy Control API key once without allowing stale environment configuration to resurrect it. Enforce write and sensitive-read grants at execution and filter the MCP catalog; explicit file reveal, environment values and logs require sensitive reads.

Persist desired network settings/revision in SQLite before creating a reserved independent network worker. Apply only the Yard service using its current immutable image, update original Compose/`.env`, and verify Docker health plus actual published bindings. Ordinary failure restores prior files/runtime and keeps requested settings pending; uncertain failures retain recovery reservations. Refuse ambiguous port merges, unsupported extra ports and missing healthchecks before writing. Apply now is the default, automatic recreation is opt-in, and UI-origin changes require reconnection. The sidebar displays local MCP availability and opens Settings. NoX Backend/Gateway registration, delegated credentials and gateway connectivity indicators remain outside this implementation until their shared contracts are ready.

## D-041 ? Signed releases and verified Yard updates (2026-10-20)

Sign the published multi-architecture OCI index by digest with keyless Cosign and GitHub Actions OIDC. Include BuildKit provenance, verify publication identity before creating the GitHub Release, and preserve the existing sequential pipeline and duplicate-publication marker. Version and latest tags share the signed digest at publication; Git tags are not cryptographically signed by this mechanism. See [Releases](Releases.md).

Yard verifies official release signatures before admitting an installation, including manual checks, daily updates and file-backed self-updates. Require the official release workflow on master, GitHub issuer/repository/push claims, matching signed version/source revision and a valid index/platform/config chain. Unsigned or unverifiable candidates fail closed without replacing Yard. The independent worker retains its network isolation and consumes the admitted immutable candidate; health verification and SQLite/runtime recovery remain separate requirements. Cached Recreate is not a release download. See [Self-update](Self-Update.md).

This establishes publisher identity and image integrity while retaining the single-host installation model. Older releases are not signed retroactively, and enforcement begins only after the new updater is installed. It does not claim a particular SLSA assurance level or reverse application-data migrations.
