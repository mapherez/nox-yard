# MVP completion plan

**Prepared:** 2026-10-08

This is the planned execution path for completing the existing MVP, based on the [original implementation plan](Implementation-Plan.md), the [repository review](Repository-Review-2026-10-08.md), and outstanding acceptance checks in [Progress](Progress.md). It does not declare any milestone complete. Implementation status and new validation evidence remain exclusively in Progress.

The original plan remains the scope contract. This document breaks its remaining work into ordered implementation packages with dependencies, deliverables and acceptance gates. Each package should be delivered in focused changes that include the relevant backend, adapters, frontend, tests and documentation.

## Scope and constraints

Keep the single local Linux Docker host, Raspberry Pi 5 priority, amd64 support, English-only copy, dark-only responsive UI, public prebuilt-image MVP, and trusted LAN/VPN boundary. Reuse the application facade and notifier across browser HTTP and MCP. Preserve the independent authentication and contracts of browser HTTP, Control API v1 and MCP.

Keep management in the existing project drawer and its Details, Logs and Terminal tabs. Job progress/history should be contextual; this plan does not add a new dashboard or duplicate action rows. Preserve stable release tags, the `latest` channel and the existing opt-in Yard self-update behavior.

Frontend bundle/font optimization is optional follow-up work. Multi-host management, registry-credential storage, localization, a new CLI and a UI redesign are outside the completion scope.

## Traceability to the original plan

| Original phase | Remaining implementation or evidence | Completion packages |
| --- | --- | --- |
| 1. Foundation | Pi account/session persistence, interactive password recovery, clean Linux installation; regression checks for setup/session security | C6 |
| 2. Inventory/direct management | Standalone-container acceptance; disconnected Docker, stale targets, partial failures, expired sessions; realtime and lazy shell checks on the Pi | C6 |
| 3. Managed Compose | Correct health outcomes, adoption directory/diff, URL copy/sync, all three inputs, env files, relative binds, restart persistence | C1–C3, C6 |
| 4. Safe updates/schedules | Durable jobs, restart reconciliation, managed rollback, external reconstruction/rollback, unchanged-image detection, per-project opt-in scheduling, durable Yard restart outcome, explicit safe volume deletion | C2–C5 |
| 5. Validation/release | Automated Docker/frontend regressions, release runtime gates, real Pi/amd64 acceptance, backup/recovery guidance and final versioned release | C0–C7 |

Review finding 1 is addressed by C1/C3; finding 2 by C1; finding 3 by C2; finding 4 by C3–C5; finding 5 by tests throughout and C6/C7; finding 6 by C0; finding 7 by C0 and documentation updates throughout.

## Execution order

| Package | Outcome | Depends on |
| --- | --- | --- |
| C0 | Establish an accurate baseline and remove the known build dependency advisory | Existing reviewed source |
| C1 | Correct managed health reporting and adoption paths | C0 |
| C2 | Make operation state, ownership and recovery durable | C1 |
| C3 | Complete safe managed deploy/sync/update with rollback | C1, C2 |
| C4 | Add supported external recreation and finish safe removal contracts | C2, C3 |
| C5 | Add opt-in project auto-update using the verified update paths | C3, C4 |
| C6 | Close integration, security, UI and real-host acceptance gaps | C1–C5 |
| C7 | Gate publication, complete operational documentation and release | C6 |

C0 and C1 are the first implementation batch. C2 is a prerequisite for durable rollback/recovery, so full managed rollback is delivered in C3 rather than implemented twice. Tests should accompany each change; C6 consolidates remaining acceptance work rather than postponing all testing until the end.

## C0 — Baseline, contracts and dependency hygiene

**Deliverables**

- Update the compatible `source-map-js` dependency resolution/lockfile to a patched version; verify with a fresh frontend install, build and npm audit. Record other findings separately rather than running an uncontrolled dependency upgrade.
- Correct architecture/development guidance that presents external updates, project schedules or complete job recovery as implemented. Document the configured host project directory and its relationship to SQLite and backup data.
- Record operation contracts in Decisions before the dependent implementation: health/no-health/one-shot behavior, job outcome and progress compatibility, supported external configurations, rollback boundaries and project scheduling policy.
- Establish reproducible fixtures for healthy/unhealthy services, volumes/binds, standalone containers, external Compose, failed pulls and one-shot services. Keep every integration fixture isolated and cleanup limited to recorded fixture IDs.
- Add a dependency vulnerability scan for Go to the validation work; triage findings and their runtime relevance. A scan result is not a substitute for application tests.

**Acceptance gate:** the reviewed behavior and planned behavior are distinguished in documentation; the known npm advisory is cleared; existing source checks continue to pass; the fixture and contract definitions support the next packages.

**Primary areas:** `web/package-lock.json`, validation scripts, canonical architecture/development/decision notes.

## C1 — Managed health reporting and adoption correctness

Deliver health verification and adoption as separate focused changes.

**Deliverables**

- Verify deployed services through Docker after Compose returns. Wait for configured healthchecks within a bounded deadline; require sustained running state for services without healthchecks. Treat successful declared one-shot services separately from unexpectedly exited long-running services.
- Report unhealthy, missing, failed and timed-out services accurately. Do not label a deployment/update successful solely because `compose up -d` exited successfully. Until C3 supplies rollback, explicitly report that a failed update has not been restored.
- Require adoption to resolve and persist an explicit host project directory. Use existing Compose metadata when reliable, otherwise request the directory in the existing adoption flow. Reject ambiguous or unresolvable paths before ownership or filesystem mutation.
- Keep the original relative-bind base when storing adopted source and env files. Define safe handling of existing files: no unreviewed overwrite, and the stored source must match the confirmed preview. Adopt only after comparison/confirmation; adoption itself does not recreate containers.
- Expand the comparison to added/removed ports, bind/volume options, networks, image, command/entrypoint, environment and other supported runtime fields. Mask secret values while still identifying changes; block configurations that cannot be compared reliably.
- Make preview fingerprints reflect the relevant current source/target state and recheck before mutation.

**Acceptance gate:** the unhealthy fixture from the review no longer returns success; healthy and one-shot fixtures follow the defined contract; adopted relative binds keep the same host data before and after restart/update; stale previews and ambiguous adoption are rejected before mutation.

**Primary areas:** `internal/managed`, `internal/application`, `internal/store`, browser/MCP adapters, `NewProjectDrawer`, managed job feedback.

## C2 — Durable jobs, recovery and contextual history

**Deliverables**

- Persist target identity, operation, source/target image IDs, progress stage, worker identity, execution ownership, timestamps, final outcome and rollback information. Migrations preserve existing managed and self-update records.
- Retain existing managed status compatibility (`running`, `succeeded`, `failed`) where possible; expose richer stage/outcome fields additively. A rolled-back update is a failed update with an explicit restored result, not an update success.
- Replace blanket failure of running jobs on startup with reconciliation against worker and Docker state. Resume observation of live workers; verify completed results; recover or retain resources with an actionable error when the outcome is uncertain. Never blindly replay destructive operations.
- Use temporary independent workers for operations that must survive stopping/replacing the web service. Workers must persist their outcome, have narrowly defined mounts, and be hidden/protected from ordinary inventory actions. Persist self-restart outcome while retaining the existing self-update mechanism and recovery contract.
- Coordinate overlapping managed/Engine/manual/scheduled operations on the same actual resources. Prevent duplicate submissions and restarts from launching concurrent replacements. Reconcile inventory and release pending state only when the operation reaches a verified final result.
- Provide typed job lookup/history through the application facade and browser/MCP adapters as needed. Restore progress after browser reload, drawer reopening and backend restart, without depending on an in-memory frontend job ID. Keep Control API v1 behavior stable; expanding that API is not required by the MVP.
- Redact secrets from progress/errors/history and distinguish cleanup failures from replacement failures.

**Acceptance gate:** browser reload and service restart retain a verifiable operation outcome; interruption during pull, replacement and verification does not create duplicate replacements; a vanished worker produces reconciliation/recovery rather than an assumed result; concurrent conflicting calls are rejected consistently across adapters.

**Primary areas:** storage migrations/job records, a focused job orchestration boundary, managed/lifecycle/self-update integration, application facade, contextual drawer feedback.

## C3 — Safe managed deploy, sync and update

**Deliverables**

- Separate pull, image update and source sync. Image updates use the stored definition; they do not silently fetch replacement YAML from a source URL.
- Resolve target images for the current platform, compare image IDs/digests with the deployed services, and use immutable verified targets during replacement. An image-only update with unchanged images returns an unchanged result and does not recreate containers. Source changes remain an explicit previewed sync.
- Snapshot the previous source, env files, variables, image IDs, container state and relevant runtime configuration before replacement. Retain the images/configuration needed to restore the prior version until verification finishes.
- Stage host source/env-file writes and metadata changes so a failed sync cannot leave SQLite describing one version while the host file contains another. Commit the confirmed version only after verification or persist an explicit recoverable intermediate state.
- Implement bounded service verification and rollback for failed update/sync. Restore prior image IDs/configuration and original running/stopped state; report any rollback or cleanup failure precisely. Initial deployment has no previous application to restore: report partial resources and a safe retry/cleanup path.
- Preserve named volumes and bind data. Runtime rollback restores container/configuration state; it does not undo application data writes or database migrations inside shared volumes. Document this boundary and the backup requirements for applications that migrate their data.
- Verify URL copy/sync/cancel, env-file handling, all input methods and unsupported-source rejection. Capture actionable Docker errors without exposing supplied secrets.

**Acceptance gate:** changed images replace successfully; unchanged images retain container IDs; failed pull leaves the old stack intact; unhealthy replacement restores the prior image/configuration; failed source sync keeps host files and SQLite consistent; named-volume contents and binds survive successful update and rollback.

**Primary areas:** managed execution and host-file helpers, durable job workers/results, storage, managed preview and contextual UI.

## C4 — External recreation and safe removal

**Deliverables**

- Introduce a supported-configuration assessment and preview for standalone and external Compose targets. Explicitly reject configurations whose dependencies cannot be preserved; never silently drop unknown or unsupported runtime settings.
- Snapshot and preserve observable ports, mounts, networks/aliases, environment, labels, command/entrypoint, user, working directory, healthchecks, restart policy, resource/security settings and other supported fields. Show meaningful changes without revealing secrets.
- Recheck the target fingerprint immediately before mutation. Retain originals until replacements pass verification; implement dependency-aware group ordering and rollback for partial group failures. Restore previously stopped/running states and report per-container outcomes.
- Reuse C2 job ownership/reconciliation and C3 image/health semantics through the application facade. Track old/new target identity so job history and later schedules still refer to the correct standalone replacement.
- Expose update/recreate in the existing contextual management controls with confirmation. Keep pull as a cache-only operation and Yard self-update on its dedicated path.
- Audit removal against the original explicit-volume-choice requirement. Supply an initially unselected volume-deletion choice and a current preview; delete only identifiable exclusive resources selected for removal. Preserve shared/external resources, host binds and source files. Review adapter compatibility deliberately when extending existing removal requests/tools.

**Acceptance gate:** representative standalone and external Compose fixtures retain supported configuration/data; unsupported targets receive a specific reason before any change; failed group replacement restores originals; stale previews abort; unchecked volume deletion retains volumes and selected shared volumes are still protected.

**Primary areas:** lifecycle/Engine update and removal logic, job orchestration, facade/adapters, project/container drawer actions.

## C5 — Opt-in project auto-update

**Deliverables**

- Persist per-project opt-in and scheduling state for eligible managed, external Compose and standalone project views. Default every existing/new project to disabled. Keep this independent of global Yard self-update settings.
- Start with daily checks at 03:00 server-local time, as specified originally. Define/document the server timezone explicitly and show the effective schedule/timezone in the existing project settings context; never infer it from the browser's timezone.
- Use the same supported-configuration checks, immutable image comparison, durable jobs, verification and rollback as manual updates. A disabled project, protected helper/Yard target, unsupported configuration or stopped project must not be started/updated automatically.
- Persist each scheduled occurrence/result so restart, clock changes and concurrent manual actions cannot execute the same occurrence twice. Reconcile interrupted work first; coalesce missed checks into at most one catch-up check per eligible project rather than replaying every missed interval.
- Bound work/concurrency for the Pi. Skip/defer busy projects with a recorded reason. Prevent repeated automatic attempts of a known failed image until a new image arrives or an explicit manual retry is requested.
- Expose last/next check and result, including unchanged/skipped/failure/rollback, through contextual history and the applicable adapters.

**Acceptance gate:** disabled projects never update; unchanged images never recreate; an opted-in fixture updates at the effective scheduled time; restart/missed schedules do not duplicate work; conflicting manual actions remain safe; failed-image suppression and explicit retry work.

**Primary areas:** project settings/schedule persistence, scheduler, durable jobs/application facade, project drawer settings/history.

## C6 — Acceptance and regression closure

Carry out these checks against isolated fixtures first, then designated sample stacks on the real Linux hosts. A disposable Docker Desktop test or ARM64 emulation does not close a Raspberry Pi acceptance checkpoint. Record exact version/platform, result and remaining limits in Progress.

| Acceptance group | Required evidence |
| --- | --- |
| Foundation/security | Clean Pi first-run installation; exactly one winner for concurrent setup; account/session persistence; interactive password reset revokes sessions; rate limiting, expiry, Origin/CSRF and secure-cookie behavior |
| Inventory/lifecycle | Running/stopped external Compose and standalone targets; metrics/health/uptime; realtime/lazy shell behavior; missing `/bin/sh`; Docker disconnection/reconnection; stale targets, partial failures and expired browser sessions |
| Import/adoption | Same sample stack through paste, upload and public HTTPS URL; preview/confirmation; variables/env files, named volumes and relative binds; URL copy/sync/cancel; adoption; unsupported/invalid sources rejected before host mutation |
| Safe updates/jobs | Healthy/unhealthy/no-health/one-shot services; changed/unchanged images; failed pull; named-volume/bind preservation; induced managed and external rollback; browser/backend/worker interruption; truthful partial outcomes and persisted history |
| Scheduling/self-management | Opt-in/default-off, effective timezone, missed-check/restart behavior, concurrent manual actions, failed-image suppression; verified Yard restart/self-update success and failed replacement recovery |
| Removal | Explicit volume choice, shared resources retained, changed fingerprints rejected, protected Yard/helpers and truthful partial-cleanup reports |
| UI/adapters | Desktop/tablet/mobile and 320px layout; keyboard, Escape/focus return and reduced motion; recoverable job feedback; deterministic frontend regressions for failures/reload/history; HTTP/MCP consistency and existing Control API contracts |
| Recovery/architectures | Real Pi 5 and Linux amd64 installation/runtime; upgrade from existing v1.0.1 data; backup and restore of SQLite, host source/env files and representative application data |

Add regression tests to the shared validation scripts as they become available. Direct Go auth/store tests should cover important boundary cases missing from existing HTTP coverage. Frontend tests must verify user-visible state transitions rather than mirror implementation details. Introduce lint/stylelint only if useful; optional no-op scripts are not evidence of completed checks.

**Acceptance gate:** the original five phase checkpoints and eight acceptance cases have evidence, or a clearly documented supported-configuration limit consistent with the original plan. No unresolved high-impact defect can be waived by a successful build or release tag.

## C7 — CI gates, operational docs and release

**Deliverables**

- Run disposable real-Docker regressions, including the existing MCP smoke and new update/recovery cases, in CI on Linux. Ensure Linux-only tests actually execute. Keep fixture IDs/cleanup isolated and secrets out of logs.
- Make formal releases run runtime gates against the tagged source before registry publication: amd64 integration and ARM64 executable/readiness checks. Keep both image architectures, serialized stable publication and prerelease isolation.
- Complete install/upgrade/backup/restore/password-recovery, failed-job/manual-recovery, supported external configurations and rollback/data limits documentation. Update architecture, Features, Development, MCP and other affected contracts as behavior is delivered.
- Validate a clean Pi installation and upgrade of an existing fixture from v1.0.1; check persistence and the final sample deployment/update/external-project workflow.
- Select the SemVer version according to actual compatibility changes, create the formal release through the existing tooling, and verify published manifests/version metadata and the documented installation path. The existing v1.0.1 tag does not close MVP acceptance.

**Acceptance gate:** all C6 evidence is recorded, release gates pass before publication, both published architectures work, operational recovery is documented/tested, and Progress explicitly closes the original MVP milestones.

## Working rules and definition of completion

Implement one package at a time, keeping contracts/migrations, execution logic, adapters/UI and meaningful regressions in reviewable changes. Update Progress after each acceptance gate; passing code checks alone does not close a host checkpoint. Record design decisions when their contracts change, and keep planned claims labelled until implemented.

Every destructive integration test must use disposable fixtures or a specifically designated test stack. Existing deployed user projects are not acceptance fixtures. Use the current real instance for read-only observations during preparation, and perform lifecycle acceptance on designated disposable sample stacks.

The MVP is complete only when the original acceptance cases are evidenced end to end: valid creation from every input, truthful operation results, preserved configuration/data, recoverable jobs, safe updates and opted-in schedules, usable responsive controls, and tested installation/upgrade/recovery on the target platforms. Optional frontend asset optimization can follow that milestone.
