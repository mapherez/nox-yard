# Repository review — 2026-10-20

Reviewed source: `ed6d7b35c26b4b90305ed816ce636b22839dacf8` (`master`, `v1.2.1`, root package version `1.2.1`). The checkout was clean initially and matched the locally recorded `origin/master`; no fetch, remote CI, GHCR publication or deployed Yard query was performed. This review covers source, all 21 tracked Markdown documents, deployment examples, tests and CI/release scripts. It updates documentation and the Go patch floor; no push, release or host deployment occurred. Current milestone status belongs in [Progress](Progress.md).

## Findings and changes

### High: the minimum Go patch had reachable security advisories

The module required Go 1.26.6. A fresh pinned govulncheck 1.8.0 scan of Linux amd64 reported 10 reachable standard-library vulnerabilities: GO-2026-6603, 6605, 6607, 6608, 6609, 6610, 6611, 6612, 6613 and 6617. These are static call-path findings, not proof of exploitation or of the toolchain used by an already published image. The old C0 scan's clean result cannot describe today's advisory database.

Raised `go.mod` to **Go 1.26.9**, the patch identified by the scanner and the [official Go release history](https://go.dev/doc/devel/release#go1.26). For example, the [official GO-2026-6617 advisory](https://pkg.go.dev/vuln/GO-2026-6617) records the HTTP/2 fix. Tests/vet passed again with Go 1.26.9; post-change scans for Linux amd64 and arm64 report zero reachable and zero imported-package vulnerabilities. The existing module-only GO-2026-5932 OpenPGP advisory remains outside imported packages. This is not an advisory-free module graph.

CI reads the minimum from `go.mod`; Docker uses the Go 1.26 Alpine builder and must satisfy that minimum. Existing images/binaries are not patched by changing this checkout: rebuild, validate and publish/deploy through the documented process.

### Medium: current status and product instructions mixed multiple generations

`Progress.md` still described C6 and host-file work as uncommitted and not released, although the clean source already carried `v1.2.1`. It also repeated former DB persistence, fixed/independent scheduling and older deployment claims. Replaced that accumulation with a current checkpoint, concise historical evidence and an ordered remaining-work list. Tags are distinguished from verified registry publication/deployment.

Corrected README/Features/Architecture and related guides to match D-029–D-034: host Compose/env files are authoritative after verified migration; Save writes files only; manual image maintenance uses Pull → Recreate; Recreate uses cached images and optional service scope; Yard uses daily scheduling in its own drawer, with compact Settings and Check now. Uptime includes background backfill. Updated schema references to 9 where describing current upgrade behavior.

Historical decisions, original plans and the 2026-10-08 audit remain available with explicit supersession/context notices. Their former missing features are implemented; those old findings must not become a new backlog by being read without their baseline.

### Medium: disposable Docker acceptance is still outside CI publication gates

The source job runs Go tests/vet/formatting, 53 release-tooling tests, frontend build and five browser fixtures. The Docker job builds amd64/arm64 and executes the ARM64 health smoke. The reusable publisher follows only a validated release request and successful CI dependencies. These parts of C7 exist.

However, neither workflow calls `scripts/acceptance.py` or the Docker jobs/managed/recreate/MCP/schedules/upgrade/self-update fixtures. Thus a release can pass its existing gates without exercising real update/rollback/interruption contracts. Linux Go tests do run in source CI, but do not replace the real-Engine fixtures. Integrate isolated runtime gates and retain reports before publication; the local Docker daemon was unavailable for implementing/verifying that CI expansion in this review.

### Medium: the acceptance aggregator omits two newer runtime suites

`scripts/acceptance.py` runs seven suites. It does not invoke `smoke-configuration.py` or `smoke-service-recreate.py`, despite current host-file management, Yard SQLite recovery and service scope depending on them. A successful aggregate report alone therefore does not cover those latest contracts. Host-validation guidance now requires both extra commands explicitly. Extend the aggregator/CI together when a disposable Linux Engine is available, then validate cleanup, native platforms and report contents.

### Medium: old Pi handoff instructions and ignored evidence were stale

Host-validation instructions claimed that the current source was an uncommitted C6 tree and pointed to dated local images/archives. Those artifacts are ignored and may not exist in another checkout or correspond to new source. Replaced the claim with fresh versioned handoff instructions, explicit image/source alignment and current schema 6→9/migration behavior. Existing archive filenames are retained because `prepare-host-validation.py` still emits them. Rebuild a fresh bundle; no previous `.tmp/` report or emulated ARM64 result closes native-host acceptance.

### Medium: Control API Pull changed through the shared facade

The old v1 guide promised generic runtime-only image pulls and blocked Yard Pull. Current `controlPull` → `runPull` → `application.Pull` can read host files, select one Compose service and pull Yard through its file path. Missing files alone fall back to runtime; invalid/inaccessible existing files fail. This behavior is shared with browser/MCP and covered at the application layer, but deserves explicit v1 adapter regression coverage because D-017 originally promised stable semantics.

Corrected the guide to describe actual image selection and protection boundaries. Routes, authentication and DTOs retain their existing shapes. A follow-up should exercise current-file project/service references, invalid versus missing files, empty managed targets and Yard protections through v1 HTTP before making further compatibility claims.

### Low: index omissions and asset size

The developer index omitted Managed Updates, External Updates, Project Schedules and Self-Update despite those being canonical contracts. Added all four, linked this review and identified historical sources. Corrected a malformed D-031 heading and an outdated UI label in design guidance.

The frontend build still warns about a 651.44 kB JavaScript chunk (177.86 kB gzip). Multiple icon font formats also remain in the build output. Asset/chunk optimization is optional follow-up; passing this review does not imply it was completed.

## Verification performed

| Check | Result | Boundary |
| --- | --- | --- |
| Tracked Go formatting | Pass; no unformatted files | `gofmt -l` |
| `go test -count=1 ./...` | Pass before and after the Go patch | Windows amd64; Linux-only runtime/ownership tests require Linux |
| `go vet ./...` | Pass before and after the patch | Go 1.26.9 final run |
| Linux backend cross-builds | amd64 and arm64 passed; binary metadata confirms Go 1.26.9/target architectures | No runtime or container startup |
| Release tooling | 53 passed | Disposable local Git remotes; no actual repository push |
| Frontend typecheck/build | Pass | Existing large-chunk warning |
| `npm test --prefix web` | Five fixtures passed | Browser API mocks; history/recreate/schedules/sessions/configuration; no Docker operations |
| npm audit | Zero vulnerabilities | Existing frontend lockfile; no broad dependency upgrade |
| govulncheck 1.8.0 | Go 1.26.6: 10 reachable; Go 1.26.9: zero reachable/imported on Linux amd64 and arm64 | One unimported module-only advisory remains |
| Compose config | Production, development and host-validation passed | Placeholder validation image for interpolation; no startup |
| Docker integration / ARM64 runtime | Not rerun | Local Docker daemon unavailable |
| Native Pi/Linux hosts, remote CI/publication | Not inspected | No current acceptance/deployment claim |

Scanner diagnostics and build/browser outputs are ignored local artifacts. This tracked report records their outcomes without depending on those artifacts being present in future clones. [Progress](Progress.md) lists the next acceptance work.

## Document inventory and disposition

The initial tracked set was the root README plus 20 files in `Documentation/Developer/`; this review adds one report. No tracked Word/PDF or separate documentation tree was found. Deployment/toolchain configuration was also cross-checked against the prose.

| Document | Disposition |
| --- | --- |
| [Root README](../../README.md) | Corrected manual image flow, file editing/storage and Yard schedule/Settings instructions. |
| [Developer index](README.md) | Added omitted contracts, latest audit and guidance on source/deployment evidence. |
| [Architecture](Architecture.md) | Corrected publication pipeline and active host-file persistence. |
| [Completion plan](Completion-Plan.md) | Preserved acceptance gates; labelled baseline assumptions superseded by later decisions. |
| [Configuration files](Configuration-Files.md) | Reviewed against file handlers, migration and service scope; retained current contract. |
| [Control API](Control-API.md) | Corrected file-aware Pull and protection boundaries; adapter regression follow-up recorded. |
| [Decisions](Decisions.md) | Added supersession map; preserved dated decisions; repaired D-031 heading. |
| [Design system](Design-System.md) | Corrected Recreate label and added configuration/schedule style ownership. |
| [Development](Development.md) | Updated Go floor, workflow responsibilities and five-fixture coverage. |
| [External updates](External-Updates.md) | Reviewed runtime fallback, cached images, service scope, rollback and removal; retained. |
| [Features](Features.md) | Corrected old Update/Pull/Yard/uptime claims and separated inventory from configuration notes. |
| [Host validation](Host-Validation.md) | Replaced stale bundle/source assumptions; corrected schema/migration and extra suite requirements. |
| [Implementation plan](Implementation-Plan.md) | Preserved original scope; added links to current superseding contracts. |
| [MCP](MCP.md) | Reviewed 35-tool catalog, file disclosure, scope and compatibility fields; retained. |
| [Managed updates](Managed-Updates.md) | Reviewed legacy sync versus current file protocol, supported comparison and recovery; retained. |
| [Operation jobs](Operation-Jobs.md) | Distinguished schema 7 job introduction, schema 8 schedules and current schema 9. |
| [Progress](Progress.md) | Replaced stale cumulative status with current baseline, evidence and remaining gates. |
| [Project schedules](Project-Schedules.md) | Corrected obsolete independence claim for Yard settings. |
| [Releases](Releases.md) | Reviewed current interactive command/request identity and reusable publisher; retained. |
| [2026-10-08 review](Repository-Review-2026-10-08.md) | Preserved historical evidence; added explicit historical/current links. |
| [Self-update](Self-Update.md) | Reviewed current worker/daily model; repaired Settings navigation text. |

No functional defect was reproduced in the checks that ran. Outstanding real-Engine/native-host/CI coverage limits remain material; source tests and documentation alignment do not close the MVP acceptance gates.
