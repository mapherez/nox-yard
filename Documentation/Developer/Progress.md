# Current Yard checkpoint — November 10, 2026

The current source consumes NoX MCP 0.7.3 and publishes EN-US human-menu descriptors
through Suite NoX discovery. The 38 existing tool contracts are unchanged; ordinary
`tools/list` omits CLI metadata. Native management uses NoX Login; direct MCP,
Control API and scoped Suite delegation remain independent. See [MCP](MCP.md),
[NoX Login](NoX-Login.md) and [Suite setup](Suite-Integration.md).

Local validation passed the full Go race suite, format/vet/workflow/Compose checks,
Linux ARM64 image build and Linux AMD64 cross-build, all 38 forms in the actual CLI interaction engine, configuration/override
revision and frozen-source bindings with masked variable reviews, and
real Auth v0.0.2 / Backend v1.2.2 / candidate Yard+Gateway acceptance. The routed
check exercises definitions, status and guarded removal of a disposable container.
No installed host, personal credential, commit or release was operated by this work.
The ten frontend browser regressions, 172 CI/release tooling tests and Python
fixture regressions also passed. The owner subsequently rejected the current CLI interaction design because some
operations still require manual IDs/action strings and JSON. Menu work is deferred
at the owner's direction; these local checks do not constitute owner acceptance.
The optional Gateway/lifecycle fixtures remain optional; no additional CI/release
gates or full-Suite test round are scheduled by this documentation update.

## Historical checkpoints

The dated records below preserve earlier evidence and pending work as of those
checkpoints. They are not current setup instructions or a claim that older SDK,
login or CI descriptions still apply. Current procedures live in the linked guides.

# NoX Login update — November 3, 2026

The working tree now replaces dashboard password entry with NoX Login, consuming Auth v0.0.1. It preserves existing data/legacy administrator records in additive tables, uses an automatic one-shot enrollment component with a separate admin mount, and retains independent direct MCP/Control API behavior. See [NoX Login](NoX-Login.md). Real installed Google/Yard acceptance remains pending publication/deployment; source tests are not that acceptance. Suite pairing/lifecycle work was subsequently resumed; see the current checkpoint below. No release or commit is performed by this work.

## Same-host Suite points 7 + 8 — November 4, 2026

The adapter now starts Yard's private listener, completes foundation pairing,
prepares marked services through existing deployment previews, and reconciles
registration/lifecycle/removal from existing durable jobs. Settings exposes safe
integration state and retry. Backend's corresponding change delegates only an
enrolled installer's own canonical local deployment associations; it conveys no
personal token or account/key administration. See [Suite integration](Suite-Integration.md).

Local real-service acceptance passed installation/ACK/registration, volume
isolation, stop/start, declaration refresh, controller restart, removal/reinstall,
and installation during Backend outage with recovery and no Docker-job replay.
The tests also exposed and corrected SQLite private-state scanning and a removal
confirmation fingerprint that changed when the worker froze observation metadata.
Native removal still binds actual resources, files and semantic source context.

Backend vet/full Go race tests passed. Yard source/race, frontend build, ten browser
regressions (including Suite status/retry), workflow/Compose/Python and CI/release
tooling checks passed. Final packaged lifecycle acceptance also passed on Linux ARM64/Docker Desktop
using Yard image `sha256:6e696408105fea734ff5cc2891ab9c3d46f5e041b1aca3507c2768bc1b23c822`
and Backend candidate `sha256:b2796141c86c016f46a512fb5d927dd4a0ce7e03757909b92f5729d58bd597a3`.
The same Yard image passed foundation pairing against published Backend v1.2.0
and Auth v0.0.2 through the shared runtime inventory; evidence is
`.tmp/suite-foundation-final.json`. Its preceding image passed published Auth
Compose enrollment/rollback and the complete existing managed-deployment fixture.
Only the final active-job Suite reconciliation guard changed afterward; its
regressions and final real-service lifecycle test passed. The final lifecycle
fixture also recovers a committed registration from its pre-response journal.
Auth v0.0.2/SDK v0.7.2 are unchanged. Backend must be published/updated first.
Foundation pairing with published dependencies remains in the shared runtime CI
inventory; full lifecycle acceptance uses the updated Backend candidate image.
No remote CI, release, installed product adaptation, CLI or point-12 acceptance is
claimed. The owner already confirmed Google login/global logout on installed apps.


## Local verification of NoX Login

November 3, 2026: shared source checks passed (Go vet/tests, workflow/Compose and Python syntax validation, CI/release tooling, frontend build and all ten browser regressions). `go test -race ./...` also passed. The three current-image CI groups passed all **eleven** runtime suites using Linux ARM64 on Docker Desktop, Engine 29.9.2. Reports are `.tmp/nox-login-final-group-{1,2,3}.json`, all against image `sha256:7c3aad38e4f1122b9ff472a9ae75557d860283bfbd816a5c1864a7478a53561b`.

Those checks found a Docker Desktop bind-alias issue during restoration: `/Users/...` and the daemon's `/host_mnt/Users/...` can identify the same directory. The existing device/inode/type proof is now shared by managed operations and Engine restoration/comparison. Only proven bind sources are aligned for comparison; recovery inputs and checks of type, destination, access, propagation and other options remain intact. Regression tests reject unproven aliases and changed mount properties. Temporary diagnostic copies/overlays were removed.

A final guard additionally makes failed/empty `stat` results refuse identity instead of comparing two empty strings. Its actual shell expression passed positive/negative tests; affected packages passed race tests. After that guard, the rebuilt image `sha256:343f8365183ca08b6ba6ea03a940d6a37776291c9710fbb163bcd16cc7e4181e` passed published Auth enrollment (including independent Yard client identities), HTTP private-origin/Google redirects, real v1.3.13 rollback and the complete managed-deployment/rollback suite again.

Real Google-account acceptance on the installed Yard remains pending deployment. This run does not claim new remote CI, designated native Linux host acceptance, or legacy v1.0.1 converter/upgrade evidence. It does not complete the earlier unfinished Suite pairing/lifecycle work. No commit, release or homelab deployment was performed.

The records below describe earlier delivered work and its identified verification, not a claim that all newly changed Auth paths were included in those historical runs.

# Historical checkpoint — October 29, 2026

**Reviewed:** 2026-10-29. The parity implementation began on `96ec730` (v1.3.11); subsequent source commits are `712518f`, cleanup correction `058ca18` and fingerprint correction `3512b7d`. The current fixture-output and SQLite startup corrections are uncommitted working-tree changes. No commit, push, release or production deployment was performed by this follow-up.

## Implemented scope

The Docker/Compose parity work extends the existing managers/facade, workers and recovery protocol. It adds explicit reviewed overrides, persistent ordered file/environment/profile context, native resolution/lifecycle, full JSON preservation, operation-scoped recovery, independent opt-in image/volume deletion and direct bounded Docker history. The [audit](Docker-Compose-Parity.md) classifies every P0/P1/P2 finding with source and tests; [Configuration files](Configuration-Files.md) is the active technical contract.

The shared runtime inventory now includes configuration, service recreation, machine recovery and native parity. CI uses the same ten current-image suites once, with optional legacy upgrade separately. Prior omissions of those suites and the narrower CI runtime gate are resolved in source. Remote CI has not been run from this workspace.

## Verification

The results and exact image/Engine identities are recorded in [Docker/Compose parity](Docker-Compose-Parity.md#evidence-and-limitations). Go tests/vet, frontend build, ten browser fixtures, Linux release-tool tests, actionlint/Compose validation and both architecture builds/smokes have passed locally. The initial runtime checks spanned r7/r8/r9 images. The CI fingerprint follow-up below adds one uninterrupted aggregate pass of all ten current-image suites on the corrected image. The audit distinguishes these runs and their boundaries. Cleanup verified zero remaining task containers, volumes, images, build cache or build records; only built-in networks remain. A failed or unexecuted case is not a pass.

Docker Desktop uses a Linux ARM64 VM. amd64 smoke is emulated. Neither establishes designated native Linux host/manual acceptance. The existing frontend chunk-size warning remains an improvement item.

## CI cleanup follow-up (2026-10-29)

[Run 40854166306](https://github.com/mapherez/nox-yard/actions/runs/40854166306), source `712518f`, passed source validation, the amd64 image build, the jobs fixture and every managed-update behavior check. The runtime gate then failed in `smoke-managed.py` while the non-root GitHub runner attempted to delete root-owned bind data (`PermissionError` in `shutil.rmtree`); later suites and ARM64 build were not executed in that run.

All eleven runtime fixtures now share confined directory cleanup with an explicit Docker helper fallback for Linux ownership. The machine fixture also stops hiding directory-cleanup errors. Eight regressions passed locally in Linux Python 3.14, including real root-owned mode-000 bind data removed by a UID-1001 caller, actual mounted-target refusal and sibling/symlink preservation. That local validation did not establish a remote CI pass. The subsequent run below confirms that the managed fixture now completes its cleanup.

## CI fingerprint follow-up (2026-10-29)

[Run 40858385274, job 123395931345](https://github.com/mapherez/nox-yard/actions/runs/40858385274/job/123395931345), source `058ca18`, passed source validation, the amd64 image build, jobs and the entire managed fixture including cleanup. It failed at the first external/standalone update with “Target configuration changed during pull; review again.” Later runtime suites and the ARM64 build were not reached.

The Engine recreation fingerprint included the raw `Mounts` slice, whose inspection order can vary without a configuration change. A deterministic regression failed on the original source after merely permuting identical mounts. Fingerprinting now sorts a copy by destination and retains all fields, for selected targets and observed unselected dependencies. Regression coverage checks all 24 permutations of four mounts in both scopes, input immutability, and real changes to each mount field.

`go test ./...` and `go vet ./...` passed in Linux ARM64 Go 1.26 with Compose installed. Both architecture images built; AMD64 health and bundled Cosign smoke passed under emulation. All ten current-image runtime suites then passed in one acceptance invocation on ARM64 image `sha256:a0f76b6c3d22330863583579903a8a3efc4570e13fc0777d41cdf43c29462136`, using Docker Desktop Engine 29.9.2, from 10:50:35 to 11:02:09 UTC. This includes the unchanged-image case that failed remotely, rollback, interruption, self-update, configuration, service recreation, machine recovery and native Compose comparisons. Legacy upgrade and designated native-host acceptance were not run.

Local evidence: `.tmp/ci-drift-validation.json`, `.tmp/ci-drift-acceptance-results.json`, `.tmp/ci-drift-amd64-smoke.json` and `.tmp/ci-drift-cleanup-results.json`. The earlier isolated-builder attempt could not resolve local fixture images and was interrupted; it is retained separately and is not a pass. Final cleanup verified the initial inventory: zero containers, volumes, image references, build-cache entries and build-history records; the same three built-in networks and the original builder remain. Temporary fixture directories, including the interrupted attempt, were removed. No global prune was used. A new remote run still requires pushing the correction.

## CI private runner output and SQLite startup follow-up (2026-10-29)

[Run 40863302085, job 123412551895](https://github.com/mapherez/nox-yard/actions/runs/40863302085/job/123412551895), source `3512b7d`, passed source validation, the AMD64 build, jobs, managed updates and all Engine recreation checks. Python then failed reading the root-created `0600` anonymous-volume ledger in the recreation fixture's cleanup, before the directory cleanup helper. Later suites and the ARM64 build were not reached. The previous Docker Desktop host run did not reproduce this Linux ownership boundary.

The fixture audit found the same private-output contract in recreation, schedules and self-update. The host now exclusively creates the two ledger filenames as private files before starting the root producer; the Go producers append without creating or replacing them. Ownership and `0600` remain intact. Linux acceptance now runs real ownership/mount/output regressions before the runtime suites, so CI exercises this boundary on every current-image acceptance run.

The Linux run also exposed an application startup race: a scheduled managed worker exited with `configure database: database is locked (5) (SQLITE_BUSY)`, leaving the fixture's job at `launching`. `store.Open` configured journal mode before installing its busy handler. It now installs the existing five-second busy timeout first, then enables WAL and foreign keys. A regression with a real exclusive SQLite lock failed before the change in both WAL and DELETE modes; both cases and twelve concurrent openers passed ten repeated executions afterward. The tests also check WAL, foreign keys and integrity after releasing the lock. Job replay, ownership, confirmation and recovery guards are retained. The schedule fixture now reports safe job state/stage/worker/error on timeout rather than only a generic message.

Final validation passed in one uninterrupted current-image acceptance invocation, 12:18:52–12:32:13 UTC on 2026-10-29: all ten runtime suites and the three Linux ownership/mount/output checks. The runner used Python 3.14.10 with real/effective UID 1001 on a Linux filesystem and private Docker Engine 29.9.2 inside Docker Desktop's ARM64 VM, with working CPU/memory/pids controllers. Yard/reference Compose was 2.40.3. The exact ARM64 image was `sha256:b883caa8346979517e74f00ebca0eb45a762d676bfc58467d25ae15f8cf37e8a`. Schedules also passed two additional independent executions (79.60 and 78.55 seconds), making three successful executions after the SQLite correction. `go test ./...` with Compose installed, `go vet ./...`, Go formatting and the portable fixture regressions passed. Both architecture builds passed; AMD64 health/Cosign smoke passed under ARM64 emulation and its container was removed.

Evidence: `.tmp/ci-linux-validation.json`, `.tmp/ci-linux-acceptance-results.json`, `.tmp/ci-linux-runner-proof.json`, `.tmp/ci-linux-source-match.json`, `.tmp/ci-linux-schedules-fixed-results.json`, `.tmp/ci-linux-sqlite-before.log`, `.tmp/ci-linux-sqlite-after.log`, `.tmp/ci-linux-go-final.log` and `.tmp/ci-linux-amd64-smoke.json`. Failed shared-network/cgroup harness attempts and pre-correction SQLite failures are retained separately and never counted as passes. A Go run without the required Compose executable failed; the final Go run with Compose passed. These Linux filesystem/UID checks strengthen local evidence; designated native hosts, legacy upgrade, manual acceptance and a remote CI rerun remain separate and unexecuted.

Changed runtime code is confined to `internal/store/store.go`. The new SQLite regression is `internal/store/open_test.go`; the three Go Docker acceptance producers and three Python smoke entry points share the private-ledger contract through `scripts/smoke_support.py`. `scripts/test_smoke_support.py` covers ownership/confinement and `scripts/acceptance.py` now enforces the native Linux preflight. Development instructions and this checkpoint document the actual boundary. Final cleanup evidence is `.tmp/ci-linux-cleanup-results.json`.

Final cleanup independently restored the initial inventory: zero containers, volumes, image references, build-cache entries and build-history records; the original builder and same three built-in networks remain. Both private daemons, their complete nested test state/data volumes, the earlier shared-filesystem workspace and temporary harness/import archives were removed. Only review evidence remains under ignored `.tmp/`. No global prune, commit, push, release or production deployment was performed.

## Remaining acceptance outside this local environment

- Designated native Linux amd64 and ARM64 hosts, plus manual public-HTTPS intake, proxy/session behavior, unavailable daemon, actual scheduling boundary and backup/restore checks. [Procedure](Host-Validation.md).
- Supported legacy upgrade through v1.3.1 conversion. The old direct v1.0.1 fixture is not proof after converter retirement; it was not counted as a current-image parity gate.
- Remote CI/publication and signed official GHCR deployment. Published-image management and signature enforcement remain implemented; local fixture tests do not establish a new release.

These are evidence boundaries, not claims that all Docker capabilities are supported. Build-only services, remote/outside-root configuration dependencies, symlinks, size limits, `/bin/sh` terminal and assessed advanced Engine recovery limits remain intentional and documented.

## Deferred and optional work

- NoX Gateway registration/delegation awaits its shared Backend/MCP/Gateway contract.
- Launch screenshots/media and frontend bundle/icon-size optimization remain optional product work.
- Security automation cadence remains separate from this parity intervention.

Historical checkpoints remain in Git history. The one-off configuration converter remains retired; DB-only installations must convert on v1.3.1 before upgrading to v1.3.2 or later.
