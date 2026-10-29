# Docker/Compose parity audit

## Baseline and scope

Review baseline: `96ec730` (v1.3.11), replacing the supplied static audit's v1.3.8 / `d9c8de92` reference. Implementation is an uncommitted source change on that baseline; this report does not establish publication or deployed versions. Existing drawer draft/inventory fixes remain in place and are covered by browser regressions.

Docker Engine remains runtime authority; native Compose resolves merges, interpolation, profiles and dependencies. Yard intentionally exposes a narrower workflow than the entire Docker CLI. The approved contract supersedes the original audit's request for automatic late override discovery: the displayed ordered file list is explicit, equivalent to `-f`, and project name is equivalent to `-p`. Terminal-added files are not silently selected.

## Findings matrix

Classification describes the baseline finding; the final column identifies the implemented resolution or retained boundary. Test names refer to repository suites, not a promise that every platform has been exercised. All P0/P1/P2 identifiers from the supplied analysis are accounted for.

| ID / priority | Finding and evidence | Classification | Resolution and regression evidence |
| --- | --- | --- | --- |
| P0-01 | `configuration_files.go` used registered labels/metadata, not later conventional discovery. Explicit native `-f` also suppresses discovery. | Intentional selection contract; missing override workflow confirmed | Ordered selection retained; `overrides.go` adds native combined review, collision-safe separate file and reviewed removal. `smoke-native-parity.py` tests excluded conventional overrides, addition/order/stale labels/conflicts/retention; browser `smoke-compose-parity.cjs` tests review/cancel/save. |
| P0-02 | Project create arguments included `--remove-orphans`; recovery could clean a broader group. | Confirmed problem | `configuration_scope.go`, `deployment.go`, `recreate/restore.go`: no implicit orphan removal; rollback deletes only job-labelled candidates in scope. Native parity keeps an existing orphan; service and interrupted-worker suites exercise recovery. |
| P0-03 | Engine project lifecycle loop did not delegate Compose dependency semantics. | Confirmed problem | `lifecycle_files.go` uses corresponding Compose start/stop/restart. Missing-source fallback shares assessed observable ordering through `composeorder`; ambiguous/missing/cyclic dependencies fail first. Native parity compares mixed lifecycle state and initializers. |
| P0-04 | Reduced model was used for inspection; the baseline already retained raw resolved JSON in important deployment paths. Separate validators/context could still drift. | Blanket field-loss claim is a false positive; context drift confirmed | `compose_context.go` resolves one snapshot and retains full JSON. Private transformations remove build, select profiles, pin images and rebase paths; reduced model never defines document preservation. Native unit/local dependency and runtime semantic comparisons cover these transformations. |
| P1-01 | Some import validators rejected `build` even beside `image`; file recreation already supported that combination. | Confirmed inconsistency; build-only is intentional | Shared resolver accepts image+build and uses published/local image only. Build-only fails with service/field diagnostic. Native interpolation/build test and service suite. |
| P1-02 | Import rejected valid local include/extends/configs/secrets/label_file/develop declarations. | Confirmed overrestriction, with capability limits | Local dependency snapshots and native resolution; preserve develop without executing it. `TestNativeComposeLocalDependenciesAndOriginalPaths`, include project-directory/environment tests, native config/secret mount fixture. Remote/outside-root inputs and unsupported recovery/platform settings remain explicit restrictions. |
| P1-03 | Regex treated unset optional interpolation as required and incompletely modeled native syntax. | Confirmed problem | Native `config --variables --format json` discovery and Compose resolution. `TestNativeComposeInterpolationAndBuildMetadata` covers optional, required, empty, defaults, nested, escaped and comment cases. Editing-only expression handling is not used to validate required variables. |
| P1-04 | Restricted server environment was deliberate, but interpolation intent/file selection needed durable context. | Confirmed context gap; unrestricted inheritance rejected intentionally | Persist ordered interpolation files, overrides and profiles. Explicit Yard name/files override environment controls. Reject destination/destructive controls. Native precedence/control tests and override variable/dependency upload fixture. |
| P1-05 | `expectedServices` omitted services merely because they declared profiles. Labels do not recover original profile intent. | Confirmed problem | Native active selection; preserve deployed explicitly selected services without activating profile companions; persist explicit choices. Verification/schedules use same snapshot. Native profile tests and runtime profile comparison. |
| P1-06 | Outside-root configuration files could not be read. | Intentional access boundary | Paths are not reinterpreted; local include bases are preserved and outside-root dependency access fails. Native local path and escape tests; adoption checks resolved absolute binds against the original runtime and rejects a changed base. Application bind paths remain subject to existing recovery checks. |
| P1-07 | Import's env-file checks were narrower than Compose and lost optional/long-syntax semantics. | Confirmed problem within supported root | Native order, optional `required`, raw `format`, interpolated dependency paths and includes. `TestNativeComposeLocalDependenciesAndOriginalPaths`, explicit interpolation precedence and include environment tests. |
| P1-08 | Restart skipped stopped containers. Native Compose restarts stopped and running services and does not apply config. | Confirmed problem | Engine individual restart accepts stopped/created/exited states; project delegates Compose. UI warns about initializers and stopped services; action result can carry warnings. Native mixed-state comparison and lifecycle unit tests. |
| P1-09 | Removal implicitly selected unused images, unlike native down defaults. | Confirmed problem | Separate false-default removeImages/removeVolumes in facade, HTTP and MCP; both fingerprint-bound; nonforced resource checks. External declarations supplement old ownership labels. Removal unit tests and native default/opt-in image/external-resource fixture. |
| P1-10 | Creation/import/file operations/adapters used different validation paths. | Confirmed inconsistency, intentional per-operation recovery limits | Shared snapshot resolver/facade; no second orchestrator. HTTP/MCP use existing jobs/auth formats plus additive fields/tools. Go adapter tests, MCP runtime suite, configuration/service/machine/schedule suites and browser regressions. Advanced recovery restrictions are evaluated before mutation. |
| P2-01 | Symlinked configuration paths rejected. | Intentional restriction | Retained confined no-symlink reads/writes; explicit file error, no fallback. Configuration confinement tests. Secure symlink support is future work. |
| P2-02 | 1 MiB per file differs from Compose. | Intentional operational bound | Retained, aggregate 8 MiB and bounded dependency traversal. Configuration source/size tests. Oversized sources require host management. |
| P2-03 | Advanced runtime/platform fields may fail adoption/recovery despite Docker support. | Intentional operation-specific restriction | Preserve full source model; assess old runtime before replacement, do not silently strip unsupported settings. Explicit errors for unsupported runtime/security/device/driver/namespace cases. Existing adoption/recreate assessment tests and native service namespace failure fixture. Broader recovery support is future work. |
| P2-04 | Engine fallback has group/grace bounds and incomplete source relationships. | Intentional restriction | Existing 16-container / 30-second limits retained. Missing sources only; invalid/partially missing sources never authorize fallback. Shared observed dependency assessment; existing recreate unit/runtime fixtures. |
| P2-05 | Terminal assumes `/bin/sh`. | Intentional product contract | Retained authentication/permissions and shell-presence check. Existing terminal/browser/session tests. Arbitrary exec/shell selection remains future work. |
| P2-06 | Latest 200 lines and silent 16 KiB splitting restricted history and altered boundaries. | Confirmed problem | Docker-backed time range/cursor pages, explicit transport continuation/end, 1,000 events / 4 MiB pages and 8 MiB display bound. No archive. Log decoder/history tests cover long/empty Unicode, repeated timestamps, streams and TTY; runtime fixture compares retained native log output. Rotation/removal limits remain visible. |
| P2-07 | Generic sanitized errors concealed useful context. | Confirmed usability gap; secret protection retained | Safe structured operation/service/file/field/reason where known, no bulk stderr/config/env/credential URL return. Required variable/file and build-only diagnostics plus auth/redaction tests. Unclassified native errors deliberately retain a safe generic reason. |
| P2-08 | Pull/Recreate separation differs from unrestricted up. | Intentional workflow | Save only writes; Pull downloads only; Recreate uses local immutable images with no build/pull and preserves stopped services; service Recreate has no dependencies; Restart never applies edits. Existing runtime suites and native differential fixture. |

## Operation contract

| Yard operation | Observable contract |
| --- | --- |
| Save / Add override | Validate and save only; frozen URL import, separate original/override files; journal before mutation. |
| Pull | Selected configuration images only; no build or container recreation. |
| Recreate | Apply saved configuration with local images, no pull/build; preserve previously stopped services and unrelated orphans. |
| Service Recreate | Selected service and its replicas; no dependent service mutation or recovery. |
| Start / Stop / Restart | Corresponding Compose command with active context when source is available; individual containers use Engine. Restart can start stopped services/rerun initializers. |
| Remove | Reviewed resource list; images/volumes kept unless independently selected. Shared/external/unverified resources protected; no force on images, volumes or networks. |

The frozen context includes directory, ordered source/dependency bytes, interpolation selection, profiles/active services and revision. Source and relevant runtime/image fingerprints are rechecked by existing operation workers, schedules, verification and recovery. Unknown configuration journal completion retains reservations and requires inspection; no blind replay or claim of atomic multi-file filesystem rollback. Ordinary replacement rollback restores inspected runtime settings and stopped state, not application-data writes. Reserved operation labels cannot be supplied by project sources.

## Evidence and limitations

Validation completed locally on 2026-10-29. All ten current-image suites have passing final executions after targeted corrections. This is consolidated per-suite evidence, **not one uninterrupted aggregate pass on a single final image**. Original failed attempts remain recorded; no unexecuted case is counted as passed. Docker Desktop Linux ARM64 evidence is separate from native Linux host acceptance, amd64 smoke/emulation and CI publication. A missing test or another platform is never marked passed. The v1.0.1 direct-upgrade fixture does not prove the supported v1.3.1 intermediate-conversion upgrade path after converter retirement.

The native parity fixture compares separate native/Yard projects and normalizes fixture identities/paths only. Other suites cover interruption, rollback, schedules, adapters and self-protection. The shared suite list is `scripts/runtime_suites.py`; acceptance and CI use it once. Fixture resources are manifest-scoped and cleaned on success or failure; no global prune is permitted.

See [configuration protocol](Configuration-Files.md), [Engine limitations](External-Updates.md), [MCP contract](MCP.md) and [native-host procedure](Host-Validation.md).

## Recorded validation

Host: macOS ARM64, Docker Desktop Engine 29.9.2 / Linux aarch64 VM. Host Compose 5.6.1; the bundled Compose used by Yard, its unit runner and differential reference is 2.40.3. Final source/image revision is the r9 local build; tags are disposable test references, not releases. The final network-context persistence change was verified by the machine suite; the adoption/input-validation changes were verified by MCP and Go tests. No source behavior changed after those checks.

| Check | Result / boundary |
| --- | --- |
| Go | `go test ./...`, `go vet ./...`, gofmt passed in Linux ARM64 Go 1.26 runner, including native Compose resolution tests. |
| Frontend | Typecheck/build and all ten browser fixtures passed; override contrast >= 4.5:1, 1440/768/390/320 layout, Escape/Keep editing, cancel/save and stable history navigation included. Existing bundle-size warning remains. |
| Release tooling | 63/63 passed in Linux Node 24. macOS command-entrypoint tests failed due to the local temporary-path environment; that run is not reported as a pass. No real remote push. |
| Static/build | actionlint 1.7.12, Compose config, Python compilation, Markdown local links and `git diff --check` passed. |
| Architectures | Final r9 linux/arm64 and linux/amd64 images built; health and bundled verifier passed. amd64 execution is emulated on ARM64, not native-host acceptance. |
| Remote/legacy/manual | Remote CI/publication, designated native Linux hosts, full manual host checklist and supported v1.3.1 intermediate legacy upgrade were not executed here. |

| Runtime suite | Final result | Tested local image revision |
| --- | --- | --- |
| `smoke-jobs.py` | PASS (75.05 s) | r7 |
| `smoke-managed.py` | PASS (114.31 s) | r7 |
| `smoke-recreate.py` | PASS (94.95 s) | r7 |
| `smoke-mcp.py` | PASS (88.69 s) | r8 |
| `smoke-schedules.py` | PASS (83.81 s) | r8 |
| `smoke-self-update.py` | PASS (25.2 s) | r8 |
| `smoke-configuration.py` | PASS (41.54 s) | r8 |
| `smoke-service-recreate.py` | PASS (45.11 s) | r7 |
| `smoke-machine.py` | PASS (18.92 s) | r9 |
| `smoke-native-parity.py` | PASS (95.66 s) | r8 |

Local JSON evidence is retained under `.tmp/`: `parity-validation-summary.json` links the exact reports and image IDs per suite; `parity-platform-r9-results.json` records the final architecture smokes; `parity-cleanup-results.json` records resource cleanup. The ignored evidence files are local review artifacts, not portable proof of remote CI or another host. Each fixture has a resource manifest; the original session inventory is preserved in cleanup evidence.

Failures corrected during validation included actual absolute-bind adoption protection and network-context fingerprint persistence, plus fixture-only ARM64 selection, restart/readiness synchronization, retired DB-YAML expectations, a malformed sibling insertion and a driver rotation setting. Desktop multi-process SQLite fixtures use Linux volume storage; the self-updater keeps its required bind contract by binding its disposable Linux volume directory. Production SQLite durability and recovery rules were not weakened. Restart event order is intentionally not asserted: native Compose can restart concurrently; multiplicity/state are compared, while Start/Stop dependency ordering is checked.

## Official references

- [Compose config](https://docs.docker.com/reference/cli/docker/compose/config/) — native model resolution and variable discovery.
- [Compose merge](https://docs.docker.com/compose/how-tos/multiple-compose-files/merge/) — default versus explicit file selection, merge order and base paths.
- [Compose restart](https://docs.docker.com/reference/cli/docker/compose/restart/) — stopped/running services and unchanged configuration.
- [Compose down](https://docs.docker.com/reference/cli/docker/compose/down/) — default resource removal and external ownership.

References reviewed on 2026-10-29; local behavior is validated against the bundled Compose version recorded with the runtime evidence, rather than assuming documentation establishes full compatibility.

## Cleanup result

Final inventory: **0 containers, 0 volumes, 0 image references, 0 build-cache entries and 0 build-history records** from this work. Only the three built-in Docker networks remain. Cleanup removed 17 remaining image references, two test-created Go cache volumes, 266 cache entries and 80 task build records. Fixtures had already removed their workload containers/networks/volumes; 24 resource manifests reported no declared resources left behind. Temporary fixture trees and task-specific host caches/debug files were removed. Reports and the responsive UI screenshot are retained for review.

Cleanup used the initial inventory, unique fixture identities and exact image/cache/build IDs, checked usage, preserved the default builder and used no global prune. Nonforced image/volume/network deletion retained the same safety boundary as the implementation. Detailed identities are in the local cleanup JSON; the counts describe this session only.

## Changed files

The implementation, tests, shared CI runner and documentation change the following files on the stated baseline. No dependency versions or release version were changed.

<details>
<summary>Full source file list (96)</summary>

```text
.github/workflows/ci.yml
CHANGELOG.md
Documentation/Developer/Architecture.md
Documentation/Developer/Configuration-Files.md
Documentation/Developer/Decisions.md
Documentation/Developer/Design-System.md
Documentation/Developer/Development.md
Documentation/Developer/Docker-Compose-Parity.md
Documentation/Developer/External-Updates.md
Documentation/Developer/Features.md
Documentation/Developer/Host-Validation.md
Documentation/Developer/MCP.md
Documentation/Developer/Managed-Updates.md
Documentation/Developer/Progress.md
Documentation/Developer/README.md
Documentation/User/Guide.md
cmd/nox-yard/main.go
internal/application/configuration.go
internal/application/errors.go
internal/application/jobs.go
internal/application/operations.go
internal/application/schedules_integration_test.go
internal/application/service.go
internal/application/service_test.go
internal/composeorder/order.go
internal/httpapi/configuration.go
internal/httpapi/configuration_test.go
internal/httpapi/control.go
internal/httpapi/control_dto.go
internal/httpapi/logs.go
internal/httpapi/maintenance.go
internal/httpapi/managed.go
internal/httpapi/server.go
internal/inventory/log_history.go
internal/inventory/log_history_test.go
internal/inventory/logs.go
internal/inventory/logs_test.go
internal/lifecycle/lifecycle.go
internal/lifecycle/removal.go
internal/lifecycle/removal_test.go
internal/managed/adoption.go
internal/managed/adoption_test.go
internal/managed/compose_context.go
internal/managed/compose_context_test.go
internal/managed/configuration_deployment.go
internal/managed/configuration_files.go
internal/managed/configuration_pull.go
internal/managed/configuration_scope.go
internal/managed/configuration_scope_test.go
internal/managed/configuration_test.go
internal/managed/deployment.go
internal/managed/deployment_files.go
internal/managed/env_files.go
internal/managed/env_files_test.go
internal/managed/host_compose.go
internal/managed/lifecycle_files.go
internal/managed/machine_network.go
internal/managed/manager.go
internal/managed/overrides.go
internal/managed/preview.go
internal/managed/readiness.go
internal/managed/removal_source.go
internal/managed/source_info.go
internal/managed/worker.go
internal/mcpapi/server.go
internal/mcpapi/server_test.go
internal/recreate/assessment.go
internal/recreate/restore.go
internal/store/configuration.go
internal/store/managed.go
scripts/acceptance.py
scripts/runtime_suites.py
scripts/smoke-compose-parity.cjs
scripts/smoke-configuration.cjs
scripts/smoke-configuration.py
scripts/smoke-jobs.py
scripts/smoke-machine.py
scripts/smoke-managed.py
scripts/smoke-mcp.py
scripts/smoke-native-parity.py
scripts/smoke-recreate.py
scripts/smoke-schedules.py
scripts/smoke-self-update.py
scripts/smoke-service-recreate.py
scripts/smoke-upgrade.py
scripts/smoke_support.py
scripts/test-browser.mjs
web/src/App.tsx
web/src/LogsPanel.module.css
web/src/LogsPanel.tsx
web/src/NewProjectDrawer.tsx
web/src/OverrideDrawer.module.css
web/src/OverrideDrawer.tsx
web/src/ProjectConfiguration.tsx
web/src/api.ts
web/src/styles/tokens.css
```

</details>
