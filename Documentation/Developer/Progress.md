# Current checkpoint

**Reviewed:** 2026-10-29. **Baseline:** `96ec730` (v1.3.11), with uncommitted parity implementation. No commit, push, release or production deployment was performed.

## Implemented scope

The Docker/Compose parity work extends the existing managers/facade, workers and recovery protocol. It adds explicit reviewed overrides, persistent ordered file/environment/profile context, native resolution/lifecycle, full JSON preservation, operation-scoped recovery, independent opt-in image/volume deletion and direct bounded Docker history. The [audit](Docker-Compose-Parity.md) classifies every P0/P1/P2 finding with source and tests; [Configuration files](Configuration-Files.md) is the active technical contract.

The shared runtime inventory now includes configuration, service recreation, machine recovery and native parity. CI uses the same ten current-image suites once, with optional legacy upgrade separately. Prior omissions of those suites and the narrower CI runtime gate are resolved in source. Remote CI has not been run from this workspace.

## Verification

The final results and exact image/Engine identities are recorded in [Docker/Compose parity](Docker-Compose-Parity.md#evidence-and-limitations). Go tests/vet, frontend build, ten browser fixtures, Linux release-tool tests, actionlint/Compose validation and both architecture builds/smokes have passed locally. All ten current-image runtime suites have passing final per-suite executions after targeted corrections. These span r7/r8/r9 images and do not constitute one uninterrupted aggregate pass on a single image. The audit records exact boundaries and local evidence. Cleanup verified zero remaining task containers, volumes, images, build cache or build records; only built-in networks remain. A failed or unexecuted case is not a pass.

Docker Desktop uses a Linux ARM64 VM. amd64 smoke is emulated. Neither establishes designated native Linux host/manual acceptance. The existing frontend chunk-size warning remains an improvement item.

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
