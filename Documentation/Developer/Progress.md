# Current checkpoint

**Reviewed:** 2026-10-20

**Source baseline:** `c777496e42cbde39a53d59363d80e225189992d5` (package version `1.3.3`).

The implementation is substantially complete for the agreed single-host homelab scope. Remaining work is mainly runtime automation and acceptance evidence, rather than the missing features described by the original MVP plans. This review changes documentation only; it does not change CI gates, application behavior, tags, or deployments.

## Implemented scope

| Area | Current behavior and reference |
| --- | --- |
| Workload management | Local Docker discovery, Compose/standalone lifecycle, deployment/adoption, host-file editing, explicit Pull → Recreate, supported replacement/rollback and contextual operation history. [Features](Features.md), [Configuration files](Configuration-Files.md), [Durable operations](Operation-Jobs.md). |
| Dashboard and drawers | Grid/table, live search, browser-local ordering/sort, drag-and-drop, consistent drawers, log highlighting/export, terminal sizing and tablet/mobile overlay navigation. Settings prioritizes updates, directory, then a collapsed MCP accordion. [Design system](Design-System.md). |
| Automation | Opt-in daily project/Yard updates, timezone-aware occurrence handling, recovery and failed-image suppression. [Schedules](Project-Schedules.md), [Self-update](Self-Update.md). |
| Machine access | MCP off by default; separate bindings/ports; one active hashed token shared with Control API; read/write/sensitive-read grants; ten-event token history; pending/apply network worker and local MCP indicator. [MCP](MCP.md), [Control API](Control-API.md). |
| Publication and trust | One cancel-replaced sequential CI lane, exact release requests, keyless Cosign signing, BuildKit provenance and signature enforcement by Yard's updater. User-facing version notes come from the changelog. [Releases](Releases.md). |
| Product documentation | Homelab-focused README, independent user guides and curated changelog. [User documentation](../User/README.md). |

The one-off configuration converter is retired. It is not pending implementation and must not be restored as recurring startup work. SQLite schema initialization remains a separate concern.

## Verification for this review

| Check | Result and boundary |
| --- | --- |
| Go tests and vet | `go test ./...` and `go vet ./...` passed on Windows amd64. Linux-only/opt-in runtime tests are not established by this result. |
| Release tooling | All 63 tests passed against disposable local Git remotes. No repository push or publication. |
| Frontend regressions | All eight browser fixtures passed: history, recreate, schedules, sessions, configuration, dashboard, drawers and machine settings. These use isolated API fixtures, not real Docker operations. |
| Frontend build | Typecheck and production build passed earlier in this session for this source. The existing main-chunk warning remains. |
| Documentation | Local Markdown links/anchors resolved after archiving; the active index and historical links were updated together. |
| Earlier runtime/signing evidence | Earlier implementation checks covered selected Docker Desktop amd64/ARM64 paths and verification of the published signed v1.3.2 image. They are historical evidence, not new native-host runs. |
| Current Docker/native hosts and remote pipeline | Not rerun or inspected by this documentation review. No claim of complete runtime coverage, current host deployment or fresh remote CI success. |

## Remaining implementation and acceptance

| Priority | Work still required | Evidence / completion condition |
| --- | --- | --- |
| 1 | Complete the runtime acceptance runner. | `scripts/acceptance.py` invokes seven suites but omits `smoke-configuration.py`, `smoke-service-recreate.py` and `smoke-machine.py`. Include those contracts in the aggregate report and verify cleanup/results on a disposable Linux Engine. |
| 2 | Broaden runtime publication gates. | CI currently executes `smoke-machine.py`, the bundled verifier version check and ARM64 runtime health. Jobs/interruption, managed/external rollback, schedules, upgrade/backup, configuration/service scope and self-update recovery are not all automated publication gates. Integrate the selected runtime coverage into the existing sequential pipeline, preserving cancellation and avoiding duplicate tests. |
| 3 | Validate the supported legacy upgrade path. | The converter was present in v1.3.1 and removed in v1.3.2. DB-only installations require the intermediate conversion before current releases. The existing upgrade harness starts at v1.0.1; explicitly verify its assumptions and add the supported intermediate-release path, preserving credentials, files, workload data and rollback boundaries. [Configuration contract](Configuration-Files.md#direct-deployment-persistence). |
| 4 | Close native Linux acceptance. | Run current-image acceptance and the manual checklist on real `linux/amd64` and `linux/arm64` hosts. Include public-HTTPS intake, proxy/session behavior, unavailable Docker, responsive/keyboard use, scheduling and backup/restore. Record exact source/image IDs and outcomes. [Host validation](Host-Validation.md). No particular hardware brand is required. |
| 5 | Strengthen Control API file-aware Pull coverage. | Application tests cover current-file service/project selection and invalid-vs-missing fallback. v1 HTTP tests mainly verify dispatch through controller doubles. Add explicit adapter-to-facade regressions for these cases and Yard protection boundaries. This is a coverage gap, not a reproduced API defect. |

Source references for the coverage findings: `.github/workflows/ci.yml`, `scripts/acceptance.py`, `scripts/smoke-upgrade.py`, `internal/application/pull_test.go` and `internal/httpapi/control_test.go`.

## Deferred and optional work

- **NoX Gateway:** registration, delegated identity and gateway connection indicators await the shared Backend/MCP/Gateway contract. This is intentionally outside the current implementation; direct Yard-token access is implemented.
- **Launch media:** real screenshots or a short GIF remain to be supplied for the README.
- **Performance:** reduce the large frontend chunk and unused icon asset formats when practical. This is an improvement, not an observed functional failure.
- **Security automation:** `scripts/security.sh` and npm audit are available, but are not automatic CI gates. Decide their CI cadence separately; no fresh vulnerability scan was performed in this review.

## Historical context

Earlier plans, reviews and checkpoints are available in Git history. Their missing-feature lists, test counts and release/deployment statements are not the current backlog. Current technical contracts remain in this directory, and current release changes belong in [CHANGELOG.md](../../CHANGELOG.md).
