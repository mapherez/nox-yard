# Current checkpoint

**Reviewed:** 2026-10-29. The parity implementation began on `96ec730` (v1.3.11); subsequent source commits are `712518f` and cleanup correction `058ca18`. The current fingerprint correction is an uncommitted working-tree change. No commit, push, release or production deployment was performed by this follow-up.

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
