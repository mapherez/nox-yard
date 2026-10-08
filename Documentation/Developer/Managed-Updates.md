# Managed deployment, image updates and source sync

Managed operations use the C2 independent worker and resource reservations. This contract applies to stored Compose projects; external reconstruction and schedules are separate work packages. Yard's own update keeps its dedicated protocol.

## Separate operations

- **Pull** validates the stored definition and populates the image cache. Its `cached` result changes no deployed containers or host source files.
- **Update images** uses saved YAML, variables and environment files, without fetching the source URL. It pulls active services, inspects their local immutable image IDs/platforms and compares every deployed service/replica. Identical targets return `unchanged` and preserve container IDs and running/stopped state. This means no image change; it does not certify an already unhealthy application as healthy.
- **Sync** fetches the matching managed URL, presents source/configuration changes and requires a fresh confirmation fingerprint. Preview/cancel never changes the stack. A separate URL copy has a new project name/directory. Sync may apply changed services, mounts, environment and images.

Replacement uses the complete validated Compose JSON model over stdin, with immutable IDs, explicit inspected platforms and pull disabled. It preserves fields rather than reconstructing a reduced DTO. Relative binds are resolved against the original host directory. The serialized model keeps Compose's literal-dollar escapes; runtime comparisons decode them. Tilde bind paths are rejected; use explicit absolute host paths or ordinary project-relative paths.

Pinned containers retain their original image reference in the reserved `nox-yard.image-reference` label. Inventory and cache-only Engine pulls use that reference; image verification, replacement and rollback still use actual immutable IDs. Engine pull history records the newly cached ID without changing deployed containers. Source definitions cannot supply the reserved label.

Compose helpers mount host source read-only; pull works from the parent directory so it cannot create a missing project directory through its working-directory mount. Start of a complete stack starts the existing containers instead of running `up` against mutable cache tags. Stop/Start after rollback therefore preserves the restored version even while the failed target remains cached. Start/restart also check saved source/env files for host drift.

## Snapshot and supported replacement

Before update/sync replacement, the private job payload journals the old source, variables/env files, full container inspections, service images/platforms and running/stopped state. Two temporary references per previous image under `nox-yard-rollback/JOB_ID` retain cache content and prevent Yard's non-forced image-ID deletion from discarding an unused rollback image during verification. User image tags are never moved by rollback.

The old runtime must match its saved source within the assessed adoption contract: standard networks, explicit named volumes/binds, replicas, ports, aliases, environment, labels, command/entrypoint, user/workdir, healthcheck, restart and stop settings. Missing/extra services, mixed image/running-state replicas, paused/restarting containers, anonymous volumes, unsupported advanced settings and source/runtime drift fail before replacement with a reason. New deployment can validate a broader Compose definition; that does not promise a later safe update for settings the assessment cannot verify. See D-021 and the adoption notes in [Features](Features.md).

Host files must match the old saved bytes, with unprovided declared env files absent. Candidate-only paths must be absent. Sync checks this during preview, before execution and again after pull. External Docker/file changes cannot be prevented by Yard's reservations and may require a fresh review or explicit recovery.

## File and metadata commit

The old active YAML/env files remain untouched while sync pulls, applies and verifies the candidate from its resolved in-memory definition. Only after readiness and image-ID verification does the worker commit the owned source/env paths. Each file uses a same-directory temporary file and atomic rename, with restrictive permissions and symlink checks. Removed tracked env files are removed; application data and unrelated files are never part of the journal.

A multi-file host commit and SQLite cannot share one filesystem transaction. The durable payload and `committing_files` stage identify this intermediate state. On an ordinary failure the old files are restored, including removed/added env paths. If a worker/helper disappears or an external write makes restoration unsafe, `recovery_required` retains ownership and snapshots; there is no blind replacement or partial-file replay. Metadata, verified terminal outcome and lock release use one guarded SQLite transaction.

An exited worker that left a healthy replacement can be reconciled without repeating Compose: verify exact targets/readiness and matching files, or finish a still-pristine source commit from its journal. Partial commits remain reserved for review. Cleanup trouble is reported separately and retried after worker exit; recovery-required image references remain until host review/acknowledgement. Acknowledgement restores no application containers/files and never converts failure to success.

Initial deployment has no prior application. It prepares the directory, pulls before writing active source files, then deploys and verifies. Metadata keeps the same submitted definition visible for retry. Failed initial application verification reports partial resources: inspect, use Start after correcting the cause, or Remove with volume deletion unchecked. An interrupted initial file commit requires host review rather than assuming files are complete.

## Verification and rollback

Changed targets use the bounded C1 readiness check (two minutes, five seconds stable running, healthchecks, replicas and explicit successful one-shot services) and exact image-ID verification. Failure after replacement starts a separate bounded three-minute rollback only when no detached helper can still mutate the target.

Rollback applies the old complete definition with old immutable image IDs/platforms without deleting volumes. It creates containers first, stops services that were originally stopped, starts originally running services and verifies old images, replica counts, running/stopped state, supported configuration and readiness. A recreated successful one-shot may need to execute again to restore its completed state. The failed operation records `outcome: rolled_back`, `rollback: restored`; it remains `status: failed`. Failure to prove restoration records `recovery_required` / `rollback: failed`, retaining snapshots/images and reservations.

Named volumes and host bind contents survive update and rollback. **Runtime rollback does not undo application data writes, database migrations or discarded writable container layers.** Back up application data before updating software that migrates it; some migrations require a manual data restore or application-specific recovery. Health verification is bounded observation, not indefinite availability.

## Acceptance fixtures

`scripts/smoke-managed.py --image IMAGE` builds a disposable registry, healthy/changed/unhealthy app images and Linux Go runner. It verifies cache-only/unchanged behavior, Engine pull compatibility with pinned containers and cached-image history, changed image replacement, failed pulls, unhealthy update/source rollback, stopped-state restoration, source/SQL consistency, partial file journals, env paths, data preservation, URL copy/sync/cancel business behavior and secret-free history. The injected URL loader is test-only; it does not publish fixture sources or bypass production HTTPS/public-address checks. Paste/upload and URL security validation retain unit coverage. A local TLS response fixture covers successful fetch, HTTP/redirect rejection, size/encoding/empty-body errors and cancellation. Real public-URL and Pi-host intake acceptance remain host checkpoints.

`smoke-jobs.py` tests the real independent worker/observer across interruption using changed-image fixture pulls. `smoke-mcp.py` covers adapter/auth/health/adoption/lifecycle regressions. `smoke-history.cjs` covers accessible contextual results, update confirmation, keyboard focus and responsive layouts. See [Development](Development.md) for commands and [Progress](Progress.md) for evidence and remaining host limits.
