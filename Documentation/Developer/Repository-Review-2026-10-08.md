# Repository review — 2026-10-08

Reviewed commit: `5904a448bce10d421b2d965a6367173853dea404` (`master`, `v1.0.1`). The working tree was clean and matched the locally recorded `origin/master` at the start of this review; no remote fetch or GitHub workflow-status verification was performed. Current milestone status belongs in [Progress](Progress.md).

This review examined source, canonical documentation, release tooling and CI, ran local validations, and exercised disposable Docker Desktop Linux fixtures. The connected Yard instance was queried only through read-only MCP health/status tools. No deployed project, installation or setting was changed. Application code and dependencies were not changed.

## Validation evidence

| Check | Result | Limits |
| --- | --- | --- |
| Tracked Go formatting | Pass | `gofmt -l` returned no files. |
| `go vet ./...` | Pass | Windows Go 1.26.3. |
| `go test -count=1 -cover ./...` | Pass | Linux-only SQLite rollback test is skipped on Windows. |
| Release tooling | 43 tests passed | Temporary local Git repositories; no release or remote push of this repository. |
| Frontend typecheck and production build | Pass | Main JavaScript chunk is approximately 630 kB / 172 kB gzip; Vite warns about chunk size. |
| Production and development Compose configuration | Pass | Configuration validation only. |
| Linux amd64 and arm64 image builds | Pass | Separate local audit image tags. |
| ARM64 executable and `/healthz` smoke | Pass | Docker Desktop emulation; not a Pi performance or installation test. Windows `python` was used for the script's `python3` invocation. |
| `scripts/smoke-mcp.py` | Pass | Anonymous MCP, HTTP/Control auth separation, lifecycle, finite logs, masked/explicit environment reads, pull, self-protection, browser SSE, Compose preview/deploy/jobs/update/removal, removal fingerprints, self-restart and persisted session/settings. Disposable amd64 fixtures only. |
| Managed unhealthy-container probe | Finding reproduced | Both deployment and update returned `succeeded` while the resulting container became `unhealthy`. |
| npm dependency audit | One high advisory | `source-map-js@1.2.1`, a development/build dependency through Vite/PostCSS; not proof of an exploitable production HTTP endpoint. |
| Connected Yard instance | Ready; `v1.0.1`; Docker available | Cached inventory reported 11 projects, 14 containers and 12 running containers. This is a health observation, not acceptance testing on that host. |

All test containers, fixture networks and fixture volumes were cleaned up. Audit images and ignored diagnostic files under `.tmp/` remain available locally. Browser layout/keyboard checks, all three source-input methods, URL copy/sync, real adoption, password recovery on the Pi, interrupted-operation recovery and induced update rollback were not revalidated in this review. Previous recorded checks are not new evidence from this audit.

## Findings to address before closing the MVP

### 1. High: managed deployment/update success does not establish application health

In `internal/managed/manager.go`, deployment finishes after `docker compose up -d --no-build`; update pulls and runs `up -d --no-build --force-recreate`. A successful CLI exit sets the job to `succeeded`. There is no subsequent running/health verification, previous-container retention, or rollback for ordinary managed projects.

Reproduction used a disposable Alpine service running `sleep 600` with a healthcheck that always returns false. Both initial deploy and update reported `succeeded`; Docker reported `unhealthy`. This conflicts with the safe-update acceptance criteria. A successful pull or Compose command is insufficient evidence that the new application works.

Add bounded runtime/health verification, explicit unhealthy/failed results, and rollback for update failure. Define behavior for services without healthchecks and intentional one-shot services. An unchanged-image check is also missing: the current update path always forces recreation.

### 2. High: adoption does not preserve a host project directory

Source inspection shows that `Manager.Preview` assigns `ProjectDir` for `new`, `copy` and `sync`, but leaves it empty for `adopt`. Submission stores that value and completes adoption without writing host source files. Later operations with an empty directory use a new temporary Compose directory.

Relative bind mounts are accepted by validation. For an adopted source containing `./data:/data`, later Compose operations therefore resolve `./data` against a temporary directory rather than the original project's directory. This risks mounting the wrong data and makes adoption inconsistent with newly managed projects. This finding is based on code inspection; real adoption was not exercised against an existing user stack.

Resolve and preserve an explicit host project directory during adoption, or block relative binds with an actionable reason until that is supported. Expand the comparison beyond service presence, images and selected ports/mounts/networks: command, environment, removed bindings and other runtime differences are not a complete adoption diff today.

### 3. Required Phase 4 work: interrupted jobs are recorded as failures without reconciliation

`store.InterruptManagedJobs` changes every `running` managed job to `failed` when the manager starts, with a service-restarted message. Jobs are persisted, but the manager does not inspect whether the Docker operation actually finished, resume it, or provide an independent durable worker/result protocol. Detached helper activity and database state may disagree after interruption.

The self-update implementation has its own worker and recovery path; this does not implement general managed-job recovery. Self-restart helpers also do not persist their final lifecycle outcome, as already documented in Features.

Implement durable job reconciliation and expose recoverable job history/status across browser and service restarts before adding scheduled updates.

### 4. Required Phase 4 work: external recreation and project schedules are absent

External projects and standalone containers have Engine discovery, inspection, lifecycle, logs, terminal, pull and removal. There is no general safe reconstruction/recreation pipeline with rollback for external containers, and no persisted project-level auto-update opt-in or scheduler.

The global Yard self-update settings do not provide project auto-update. Complete the external supported-configuration checks, configuration preservation, replacement verification/rollback and opt-in unchanged-image schedule described in the implementation plan.

### 5. Medium: integration automation leaves important paths outside normal CI

The frontend defines build/typecheck scripts, but no test, lint or stylelint scripts. The optional invocations in `scripts/test.sh` are currently no-ops. Backend statement coverage in this Windows run was approximately 16% for managed operations and 9% for self-update; these figures are indicators of gaps, not comprehensive behavioral coverage measures. HTTP tests do exercise password/session behavior even though the auth package has no direct tests of its own.

Normal CI builds both architectures and runs the ARM64 health smoke. The real Docker MCP smoke script is not invoked by the CI or release workflow. Release builds both architectures but does not invoke the ARM64 runtime smoke itself.

Prioritize automated regressions for unhealthy updates, rollback, adoption/relative paths, interrupted jobs, URL copy/sync and failed pulls. Add deterministic frontend coverage for job-state transitions and errors. Integrate disposable Docker testing into an appropriate workflow.

### 6. Medium: development dependency has a known advisory

`web/package-lock.json` pins `source-map-js@1.2.1`, pulled in by PostCSS/Vite. npm audit reports a high-severity denial-of-service advisory for indexed source maps; the patched version is 1.2.2. See [GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q).

Update the compatible dependency/lockfile and rerun the frontend build and audit. The production image serves prebuilt assets through Go and does not run Vite/PostCSS; this finding concerns the development/build dependency path, not a demonstrated production server vulnerability. Go dependency vulnerability scanning was not performed.

### 7. Low: documentation mixes intended behavior with current implementation

`Architecture.md` presents safe external recreation and project auto-update as available, rejects relative binds despite the current implementation supporting them for new host-based projects, and places managed files under `/data` although new project files live under the configured host base directory. `Development.md` still describes managed validation/deployment as future work and backup guidance as SQLite-only.

Label planned functionality explicitly and document both SQLite and host project files/bind data in backup/recovery guidance. Keep the phase checkpoint solely in Progress.

## Suggested implementation order

1. Correct managed health verification and failure reporting; add regressions and rollback.
2. Fix adoption directory/configuration preservation and comparison.
3. Implement interrupted-job reconciliation and durable result/history access.
4. Add supported external recreation with rollback, followed by opt-in project scheduling and unchanged-image checks.
5. Close the remaining Pi/standalone/input-method/recovery acceptance cases, integrate Docker regressions into CI, refresh documentation and publish the completed milestone.

The dependency patch is small and can accompany the first implementation batch. Frontend code splitting and icon-font asset reduction are secondary improvements; neither blocks basic operation.

## Pause context

There is no explicit recorded reason for stopping development. Progress last identified managed Compose host acceptance checks as pending. Earlier architecture and Pi memory-accounting problems have documented resolutions. Recent commits continued with inventory events, Control API/MCP integration and formal releases, so the `v1.0.1` tag should not be treated as evidence that every original MVP acceptance case is complete.
