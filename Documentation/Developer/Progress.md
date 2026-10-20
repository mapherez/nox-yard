# Current checkpoint

**Updated:** 2026-10-20

**Phase:** C6 native-host acceptance and C7 runtime publication gates remain open.

The current local source baseline is `33f201151a4bb61808de42a32f24f6fe01f40133`, the `v1.3.0` release request after the Yard product rename. The earlier 2026-10-20 audit reviewed `ed6d7b3` (`v1.2.1`); its historical evidence remains in the [repository review](Repository-Review-2026-10-20.md). Source tags and local package versions do not establish completed remote publication or deployment.

The working tree now contains D-036's CI-only automatic validation and single global pipeline lane. This change does not push, create a release or deploy to a host. The [completion plan](Completion-Plan.md) retains its original native-host/runtime acceptance gates; later decisions refine implementation assumptions.

## Implemented behavior

- **C0–C1:** dependency hygiene; bounded Docker readiness, health and successful one-shot verification; original-directory adoption with configuration/file fingerprints and data preservation.
- **C2:** durable operation records, independent workers, shared resource reservations, observation without replay, contextual history and reviewed recovery. Uncertain jobs remain reserved; acknowledgement does not mutate containers or turn failure into success.
- **C3–C4:** immutable image comparison, unchanged updates, managed source journaling and bounded rollback; assessed external/standalone replacement, lineage/history, explicit volume-deletion choice and shared-resource protection. Runtime rollback never reverses shared application writes or migrations.
- **C5:** opt-in daily updates, timezone/DST and occurrence deduplication, serialized automatic workers, persisted skips/deferrals and failed-image suppression. Current schema is 9; schedules initially use 03:00 and support editing the time.
- **C6 local preparation:** session revocation across SSE/WebSockets, direct auth/store boundary regressions, five deterministic browser suites, upgrade/password/backup fixtures and native-host handoff tooling.
- **D-029–D-034:** one CI pipeline with gated script-requested publication; current host Compose/env files as the active configuration; resumable verified migration with native recovery snapshots and host ownership; MCP file-editing parity (35 tools); completed initializer display; service-scoped Recreate with dependencies left untouched.

Current manual actions are **Save → Recreate** for configuration and **Pull → Recreate** for images. Restart only affects existing containers. Recreate uses cached images without pull/build, even when unchanged; a Compose container action targets its service's replicas. URL Sync remains explicit. Yard uses its own project drawer's daily schedule and independent SQLite/runtime recovery; Settings contains status and Check now. Legacy update and interval-setting APIs remain compatible without restoring the old interval timer. See [Configuration files](Configuration-Files.md), [Self-update](Self-Update.md), [MCP](MCP.md) and [Features](Features.md).

## Verification in this review

| Check | Result and limits |
| --- | --- |
| Go source | `go test -count=1 ./...` and `go vet ./...` passed with Go 1.26.9 on Windows amd64; tracked `gofmt -l` was empty. Linux-only runtime/ownership tests were not executed here. |
| Linux compilation | Backend cross-builds for Linux amd64 and arm64 passed; binary metadata confirms Go 1.26.9 and the intended architectures. This is compilation evidence, not runtime acceptance. |
| Release tooling | 53 tests passed against temporary local Git remotes; no repository push or registry publication. |
| Frontend | Typecheck/production build and all five browser fixtures passed: history, recreate, schedules, sessions and configuration. Existing main-chunk warning remains (651.44 kB / 177.86 kB gzip). |
| Compose | Production, development and isolated host-validation configurations validated; a placeholder validation image was supplied for interpolation only. No containers started. |
| Dependencies | npm audit reported zero vulnerabilities. The original Go 1.26.6 scan found 10 reachable standard-library advisories. After raising the floor to 1.26.9, pinned govulncheck 1.8.0 reported zero reachable and zero imported-package vulnerabilities for Linux amd64 and arm64. The module-only OpenPGP advisory GO-2026-5932 remains; Yard does not import it. |
| Docker runtime / native hosts | Not rerun: the local Docker daemon was unavailable. No inference of Pi/amd64-host acceptance, current deployment version or remote CI success is made from the source checks. |

## Yard product identity

The product is named **Yard**, part of the **NoX** environment (D-035). Product prose, browser titles, accessible labels, errors/logs and MCP display copy now use Yard. The login uses a larger text-only wordmark, with extra separation above the mobile form. Both expanded and collapsed sidebars retain the Yard name; the former N badge is removed. The favicon uses a white Y on the existing blue background. Technical repository/runtime identifiers, environment variables and persisted data are unchanged.

Validation passed Go tests/vet, 53 release-tooling tests, frontend typecheck/build and all five existing browser fixtures. Separate visual QA covered login at 1440/768/390/320 px, setup at 320 px, expanded/collapsed and mobile sidebars, Tab/Enter expansion, Escape dismissal, titles, overflow and favicon rendering. Screenshots were inspected. No former product-name references remain in tracked text. This presentation change does not close native-host acceptance or publish/deploy a release.

## CI-only push validation

D-036 removes automatic project tests/builds from push. The recognized legacy Yard pre-push hook was retired in this clone; the compatibility installer safely handles older clones and preserves custom hooks. Full checks remain available manually and mandatory in CI. Pinned actionlint is now part of those source checks. One workflow-level repository-wide lane covers source validation → Docker builds/ARM64 smoke → conditional publication, with a newer push canceling/replacing the preceding run. The publisher adds no independent lane or repeated source suite.

All 59 release/hook tests passed, including clean unpushed commits traveling with the release, LF/CRLF hook migration, repeated cleanup and custom-hook preservation. Both workflows passed actionlint 1.7.12, changed shell scripts passed syntax checks and diff whitespace checks passed. These are local implementation checks using temporary Git remotes, not a live observation of GitHub cancellation/publication; the updated policy takes effect remotely only after these source changes are pushed. No release or push was performed here. Native-host acceptance remains open.

## Previously recorded evidence

Earlier checkpoints recorded Windows/Linux tests, selected Linux race checks, disposable Linux Docker Desktop suites for jobs, managed/external updates and rollback, MCP/adoption/removal, schedules, upgrade/password/backup restoration and Yard SQLite recovery. The C0–C5 checkpoint commits include C2 `4ad8caa`, C3 `9daace6`, C4 `8462a5d` and C5 `05f1752`. Later committed configuration/service fixtures cover migration conflicts, host ownership, current-file Yard recovery, stopped replicas, dependencies and missing-file Engine fallback.

Those are historical results, not new executions in this review. Ignored `.tmp/` reports, images and the old C6 Pi bundle may describe older source and are not portable current acceptance evidence. Rebuild matching images/source archives before transfer. The [2026-10-08 review](Repository-Review-2026-10-08.md) preserves its v1.0.1 findings; their former missing features must not be reclassified as current gaps.

The user previously confirmed Pi installation, administrator setup, inventory/metrics, Compose lifecycle/pulls/removal, logs and terminal. Those observations do not close the later full native-host suite or establish what version runs there now.

## Remaining work in order

1. **Fresh runtime acceptance:** build current native amd64/arm64 images with the patched Go floor. Run the seven `scripts/acceptance.py` suites plus `smoke-configuration.py` and `smoke-service-recreate.py`, which are not yet included by that aggregator. Retain exact revision/toolchain/image IDs and results. Exercise only disposable fixtures.
2. **C6 native hosts:** repeat on the physical Pi 5 and designated Linux amd64 host. Complete original input-method/public-HTTPS/copy-sync-adoption checks, standalone inventory/actions, Docker disconnect/stale/partial failures, sessions/proxy cookies, daily schedule boundary, responsive/keyboard/performance and installation/upgrade/password/backup recovery. Follow [Host validation](Host-Validation.md); mark each case evidenced or pending.
3. **C7 CI runtime gates:** integrate disposable Docker regressions (including file configuration and service scope) before registry publication. The existing CI already runs source/frontend checks, both architecture builds and ARM64 readiness; it does not run the Docker acceptance suites.
4. **Release/deployment evidence:** inspect successful CI and the official GHCR manifests/version metadata, then verify clean install/upgrade through the documented release path. A tag alone does not close these gates. Existing deployed binaries require a rebuilt release to receive the Go security patch.
5. **Compatibility regression:** add explicit Control API v1 coverage for current-file project/service Pull, invalid/missing-file behavior and Yard protection boundaries; the shared facade now differs from the old runtime-only pull description. Current behavior is documented in [Control API](Control-API.md).

Frontend chunk/icon-asset optimization remains a lower-priority improvement. The MVP is not declared complete until the original host and release acceptance gates have evidence.
