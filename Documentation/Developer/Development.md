# Development and maintenance guide

## Toolchains

- Go 1.26.9 or later in the 1.26 series for the root module `github.com/mapherez/nox-yard`; this patch floor addresses the standard-library advisories found in the 2026-10-20 review. See the [official Go release history](https://go.dev/doc/devel/release#go1.26).
- Node.js 24 for `web/` (`.nvmrc`), with npm and the committed `web/package-lock.json`.
- Docker Engine and the Compose plugin on a Linux integration host. Release images support `linux/arm64` and `linux/amd64`; Yard targets homelabs across both architectures.

## Repository layout

- `cmd/nox-yard/`: service entrypoint and interactive password reset command.
- `internal/auth/`: password validation and Argon2id hashes.
- `internal/store/`: SQLite schema, administrator, and sessions.
- `internal/httpapi/`: browser HTTP routes, session protection, static assets, and the independent versioned machine Control API.
- `internal/selfupdate/`: opt-in GHCR checks and the temporary Engine-based update worker; see [Self-Update](Self-Update.md).
- `internal/managed/`: source intake, validation/preview, host project files, adoption and asynchronous managed Compose operations.
- `internal/jobs/`: independent worker transport, resource identity, interruption observation and reviewed recovery.
- `web/src/`: typed API client, React views, and CSS Modules. Shared tokens and global rules are in `web/src/styles/`.
- `Dockerfile`, `compose.yaml`, `compose.dev.yaml`, `.env.example`: production and local development images and Compose configurations.
- `scripts/check.sh`, `scripts/test.sh`, `scripts/ci-local.sh`: shared local and CI validation.
- `scripts/install-hooks.sh`: compatibility command that removes only the recognized legacy Yard pre-push hook; new clones install no hook.
- `scripts/smoke-arm64.sh`: CI-only ARM64 executable and runtime health validation.
- `scripts/build-version.sh`: exact-commit Git tag/full-SHA version metadata for normal builds.
- `package.json`, `scripts/release*.mjs`: formal release version, command, shared validation, metadata, and tests.
- `.github/workflows/ci.yml`: source checks, Docker builds and ARM64 smoke; calls the publisher only for a validated script release request.
- `.github/workflows/release.yml`: reusable publication workflow, request-identity recheck, multi-architecture GHCR publication and GitHub Releases.
- `Documentation/Developer/`: canonical architecture, implementation, style, decision, progress, and feature documentation.

Keep Go packages and frontend modules small and named for their responsibilities. Prefer typed application models at HTTP boundaries. Do not send Docker SDK structs, credentials, or host-only details to the browser unless a user decision requires them.

## Local run

For frontend work against real containers on a machine with Docker Desktop in Linux container mode, start the local API and Vite together:

```sh
docker compose -f compose.dev.yaml up -d --build
```

Open `http://127.0.0.1:5173` and create a local administrator account on first run. The `web` service runs Vite with hot reload and forwards `/api` to the `api` service. The `api` service builds the Go backend target from this checkout, binds to host loopback, stores its database in a Compose named volume, and mounts the Docker Engine socket. No image is pulled from GHCR or built on GitHub. Frontend edits reload automatically; after Go changes, run `docker compose -f compose.dev.yaml up -d --build api` to rebuild the API. Stop both services with `docker compose -f compose.dev.yaml down`; this keeps the local data and Node dependency volumes. The development backend has the app's normal Docker management permissions, so use it only on a trusted machine.

If the page reports that the local API is unavailable, check `docker compose -f compose.dev.yaml ps` and `docker compose -f compose.dev.yaml logs api web`. Docker Desktop must be running for the API to read inventory. Vite is fixed to port 5173 because the backend uses that origin for session and CSRF checks.

To run the Go server directly instead of using the development container, build the frontend first because the Go server serves `web/dist` by default:

```sh
cd web
npm ci
npm run build
cd ..
NOX_LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/nox-yard
```

Open `http://127.0.0.1:8080`. On Windows PowerShell, set the address with `$env:NOX_LISTEN_ADDR='127.0.0.1:8080'` before `go run ./cmd/nox-yard`. `go run` compiles a temporary host executable during development; the Dockerfile builds a Linux binary inside the image. Stop the local process with Ctrl+C.

For frontend development, run `npm run dev` from `web/`. Vite proxies `/api` to the Go service at `http://127.0.0.1:8080`. Keep the Go service running for authentication and persistence. Local Docker inventory requires access to `/var/run/docker.sock` on Linux.

Managed mutations and durable Yard restart require the container backend, its exact running image and a writable persistent state mount plus Docker socket. A host `go run` process cannot provide that independent-worker transport; use `compose.dev.yaml` for those flows.

## CI validation and optional local checks

Pushes run no project tests or builds locally. Ordinary Git pushes need Git and remote access; `npm run release` additionally needs Node.js 24 and a usable Git identity. Docker/Compose, Go, frontend dependencies and Chrome are not prerequisites for sending a push or release request. Keep the relevant development tools installed when working on the code itself.

The complete automatic checks run in CI after the push. Manual focused tests remain available while developing. To run the full source suite voluntarily, install Go 1.26.9+, Node.js 24, Docker CLI with Compose, frontend dependencies and Chrome, then run from the repository root:

```sh
npm ci --prefix web
sh scripts/ci-local.sh
```

`check.sh` checks tracked Go formatting, runs vet, validates both workflows with pinned actionlint 1.7.12 and validates Compose. `test.sh` runs Go and release-tooling tests, frontend typecheck/build and all eight deterministic browser fixtures. These cover history, recreate, schedules, sessions, configuration, dashboard search/view/sort/reordering (mouse, touch and keyboard), drawers/log search/export, and MCP/access settings. Each fixture gets a fresh browser context with isolated storage, init scripts, routes, listeners and viewport. Dashboard metric checks sweep 320–1920px, stress wider fonts and large values, and include expanded/collapsed desktop sidebars so Windows-only font success cannot hide a narrow-card layout problem. Compose configuration requires the CLI/plugin, but not a running daemon. Real Docker acceptance needs an Engine. The frontend runner starts an ephemeral loopback preview with pinned Playwright CLI 0.1.22/Chrome; CI installs Chrome before invoking it. Manual failures remain failures; no optional check is silently reported as passed. The frontend build creates ignored `web/dist/` output.

Older clones may still have the full-suite pre-push hook. Retire it once using Git Bash on Windows or a POSIX shell:

```sh
sh scripts/install-hooks.sh
```

Despite its historical name, this compatibility command creates no hook. It removes only the exact old Yard hook (LF or CRLF), is safe to repeat, and refuses custom hooks, symlinks and custom `core.hooksPath` configurations. Remove a Yard validation call from a custom hook manually. New clones need no hook setup. Do not add `ci-local.sh` back to pre-push; it remains a CI/manual entry point.

## Explicit durable-job acceptance

These checks run against disposable fixtures, separately from the default source checks. Build a local image and run:

```sh
docker buildx build --platform linux/amd64 --load -t nox-yard:jobs-smoke .
python scripts/smoke-jobs.py --image nox-yard:jobs-smoke
python scripts/smoke-managed.py --image nox-yard:jobs-smoke
python scripts/smoke-recreate.py --image nox-yard:jobs-smoke
python scripts/smoke-mcp.py --image nox-yard:jobs-smoke
python scripts/smoke-machine.py --image nox-yard:jobs-smoke
python scripts/smoke-schedules.py --image nox-yard:jobs-smoke
python scripts/smoke-self-update.py --image nox-yard:jobs-smoke
```

The jobs fixture gates changed-image pull/replacement in a derived image, interrupts web/worker containers and checks resource ownership, truthful recovery, no replay and secret-free history. The managed fixture uses a real disposable registry plus a Linux Go runner to verify immutable/unchanged images, cache-only pull, failed pull, unhealthy update/sync rollback, stopped state, source/SQL/file-journal consistency and volume/bind preservation. Its URL loader is injected only inside the test to exercise copy/sync/cancel without publishing fixture files or weakening production public-address checks. The recreation fixture verifies standalone/external group replacement/rollback, fixed ports/static networking, anonymous/named volumes, binds, unsupported image volumes and unchanged external source. MCP also tests real external worker/web restart and removal choices/shared-volume protection. Fixtures use random identities and clean up only their resources. Linux tests, including SQLite self-update restore, should run on Linux; `go test -race ./internal/store ./internal/application ./internal/jobs ./internal/managed ./internal/recreate ./internal/selfupdate` checks persistence/orchestration boundaries when a C compiler is available.

For deterministic browser feedback tests, start local Vite at port 5173, then run:

```sh
npx --yes --package @playwright/cli playwright-cli -s=history open http://127.0.0.1:5173 --headed
npx --yes --package @playwright/cli playwright-cli -s=history run-code --filename scripts/smoke-history.cjs
npx --yes --package @playwright/cli playwright-cli -s=history run-code --filename scripts/smoke-recreate.cjs
npx --yes --package @playwright/cli playwright-cli -s=history close
```

The scripts mock API responses in the test browser only; they check reload/reopening, action blocking, keyboard acknowledgement/conflict/retry/focus, rollback/cleanup details and responsive layouts down to 320px. Screenshots go to ignored `output/playwright/`. The mandatory runner covers eight fixtures: history, recreate, schedules, session expiry, file configuration, dashboard, drawers and MCP/access settings. Real Docker interruption remains the jobs fixture's responsibility. CI runs the MCP/access/network Docker fixture and ARM64 runtime smoke; the broader runtime-gate gaps are recorded in [Progress](Progress.md#remaining-implementation-and-acceptance).

## GitHub CI and image publication

Every branch push starts the CI pipeline with the shared source checks, explicit `linux/amd64` and `linux/arm64` Docker builds, the bundled amd64 Cosign executable check, and the real-Engine MCP/access/network recovery fixture. QEMU runs the ARM64 runtime smoke on the GitHub runner: executable architecture, application health and its bundled verifier are checked. Tag pushes have no independent workflow trigger, and there is no parallel pull-request run for a branch push. The other Docker acceptance suites are not all CI gates; see [Progress](Progress.md#remaining-implementation-and-acceptance).

Formal releases use `npm run release` from the clean repository root: the script shows the current package version and prompts for the new one. Explicit `npm run release -- X.Y.Z` arguments remain available for automation. The script pushes a marked release commit and its annotated tag. Only after both CI jobs succeed does CI call the reusable [release workflow](Releases.md), using the exact validated SHA/tag. Ordinary pushes run CI and skip publication. The publisher reuses CI's Docker build caches and does not repeat source checks. Stable releases publish the version tag and `latest`; prereleases publish only their version tag.

One repository-wide workflow concurrency group covers source checks, Docker builds/smoke and the called publisher. A newer branch push cancels the previous pipeline, including its publication stage if active, and replaces any pending run. Branch/SHA-specific job groups are not used. Within a run, source validation must pass before Docker starts, and both gates must pass before publication. The reusable publisher has no independent trigger or competing concurrency group; an identical completed publication is skipped successfully. Release tooling tests run through `scripts/test.sh` in local checks and CI. For branch protection, require **Source validation** and **Docker builds and ARM64 smoke test** before merging. A failed or canceled CI gate prevents publication.

The image package may be private. Set its visibility to public in GitHub package settings for an unauthenticated host pull. For a private package, authenticate the host to `ghcr.io` with a token that has `read:packages` access. The Compose file references the published image and has no local `build:` context, so the host needs only `compose.yaml` and optional `.env` configuration. Run `docker compose pull` followed by `docker compose up -d` to deploy the latest stable release.

## Configuration, data, and recovery

The optional [Control API](Control-API.md) uses the UI port and shares one SQLite-backed Yard token with MCP. Enable it and generate/rotate/revoke the token in Settings. Legacy `NOX_YARD_API_ENABLED=true` with a valid `NOX_YARD_API_KEY` imports the key only on first initialization; later token changes are authoritative in SQLite. Public `/v1/health` and `/v1/info` remain available. Anonymous MCP does not open Control API. Browser sessions/Origin/CSRF remain unchanged. `NOX_YARD_VERSION` optionally overrides the incorporated build version; leave it empty to use the exact Git tag/full-SHA metadata, or `dev` for unversioned builds. `buildSHA` remains separate for self-update.

Outside Docker, `NOX_LISTEN_ADDR` defaults to `127.0.0.1:8080` and `NOX_MCP_LISTEN_ADDR` to `127.0.0.1:8081`. Containers explicitly listen on `:8080` and `:8081`; the host publications decide network exposure. `NOX_DATA_DIR` defaults to `./data`, and `NOX_WEB_DIR` defaults to `./web/dist`. In Compose, these are `/data` and `/srv/nox-yard/web`. The optional `NOX_PUBLIC_URL` must be an exact HTTP(S) origin without a path. Set it to the browser-facing HTTPS origin behind a reverse proxy so sessions use Secure cookies and Origin checks compare against that origin. A separate MCP reverse-proxy origin must respect that same Origin policy.

`.env.example` and Compose both default to host loopback: Yard 8095→8080 and MCP 8096→8081. `NOX_BIND_ADDRESS`/`NOX_PORT` and `NOX_MCP_BIND_ADDRESS`/`NOX_MCP_PORT` independently control publications. Explicitly choose `0.0.0.0` for LAN/VPN access. MCP starts disabled, including on upgrades; enable it in Settings after configuring its token or explicit anonymous mode. Network Settings persist a desired revision before Apply now launches a separate worker. Supported automatic changes update host Compose/`.env`, use the current image without pull/build, verify health/bindings and attempt recovery; unsupported installations show a manual-configuration explanation. Existing deployments retain observed host bindings as initial desired values. The Compose file mounts `./data` and `/var/run/docker.sock`. Do not commit `.env`, `data/`, credentials, sessions, or host Docker data.

Back up SQLite with a consistent SQLite backup/snapshot method, the configured host projects directory (Compose and environment files), and the applications' named-volume/bind data. These are separate storage locations; copying `/data` alone does not back up deployed application data or host source files. Restore matching source/configuration and metadata before resuming management. Container/configuration rollback does not undo application data writes or schema migrations inside shared volumes.

Coordinate backups/restores with active independent workers as well as the web process. Operation payloads contain sensitive project inputs. See [Durable operations](Operation-Jobs.md) for uncertain-job inspection and recovery acknowledgement.

Run `sh scripts/security.sh` to scan Linux amd64 and arm64 call paths with the pinned official `govulncheck` tool. The scanner is installed into a temporary directory and does not change `go.mod`. Database/network failures are failures, not a clean scan. `npm audit --prefix web` checks frontend dependencies. Update only the affected compatible dependency or toolchain patch, then repeat the relevant checks.

`scripts/fixtures/managed-acceptance.yaml` supplies healthy, unhealthy, one-shot, volume/bind and failed-pull examples. Run only with a unique disposable Compose project name and a copied temporary fixture directory; profiles select intentional failures. A standalone fixture is the same Alpine sleep service created without Compose labels; external Compose fixtures use these definitions outside Yard ownership. Never use an existing user stack as a fixture.

To reset a forgotten password, use an interactive terminal attached to the same data directory:

```sh
docker compose exec -it nox-yard nox-yard reset-admin-password
```

For a local run, stop the server and run `go run ./cmd/nox-yard reset-admin-password` from the repository root. The command prompts for confirmation and signs out all existing sessions.

## Change workflow

1. Read the relevant architecture, design, and feature notes. Add a decision entry if a new choice changes an earlier one.
2. Implement behavior across the relevant backend, API, frontend, and documentation. Keep errors actionable and in English.
3. Verify the smallest relevant checks. Docker lifecycle and recovery need a real Linux Engine; UI work needs desktop, tablet, mobile, and keyboard checks where relevant.
4. Update [Features](Features.md) when product behavior changes. Update the single [checkpoint](Progress.md) when a phase milestone or its remaining work changes.

Do not mark a phase complete based on scaffolding or a successful build alone. Use the current [acceptance procedure](Host-Validation.md) and [remaining-work list](Progress.md).

## Updating NoX MCP

The embedded MCP adapter consumes the latest available NoX MCP through normal Go module resolution. Update with `go get github.com/mapherez/nox-mcp@latest`, then `go mod tidy`, and commit the generated `go.mod`/`go.sum` changes. Run the repository checks, MCP integration tests and both architecture builds. Do not manage dependency SHAs manually or use a local checkout replacement. See [MCP](MCP.md) for the transport and shared-facade contracts.

## Project schedule validation

Compose passes `NOX_TIMEZONE` (IANA name, default `Etc/UTC`) as `TZ`; the backend rejects an invalid timezone. A direct local server uses `TZ` itself, defaulting to UTC. Opt-in and editable daily time are per project, including Yard; its legacy self-update enabled setting follows the same daily row.

```sh
python scripts/smoke-schedules.py --image nox-yard:local
# With local Vite and a Playwright CLI browser session:
playwright-cli run-code --filename scripts/smoke-schedules.cjs
```

The scheduling fixture uses a disposable registry, application images, named state volume and Linux runner containing the built Yard binary. Updates run through real independent workers with only state/socket mounts. Test code supplies the daily clock to `Tick`; production provides no clock override. Assertions cover default-off, unchanged/changed images, reopening storage during a running worker, standalone target lineage, failed-image suppression/manual retry, extra image-volume rejection, stopped/manual-busy targets, external Compose and managed comparison/rollback/source preservation. Store/time tests cover two SQLite connections, atomic admission, coalesced misses, civil-date duplication, DST and schema 7 migration. Browser checks cover opt-in/save/retry/reload, effective timezone, skipped history, unavailable-target disable and 320px/keyboard behavior. Cleanup uses only generated fixture identities and recorded anonymous volumes. Docker Desktop and ARM64 emulation do not replace C6 real-host acceptance.

## Host upgrade and recovery acceptance

See [Host validation](Host-Validation.md) for a fresh native Linux handoff, suite/report and manual UI checklist. `scripts/smoke-upgrade.py --image CURRENT --legacy-image LEGACY` starts from a real v1.0.1 image with disposable application storage, interactive password recovery and a three-part backup/restore; its upgrade assumptions need verification against the retired converter and the supported v1.3.1 intermediate path. `scripts/acceptance.py` runs seven isolated runtime suites and records exact platform/image evidence. Run `smoke-configuration.py`, `smoke-service-recreate.py` and `smoke-machine.py` separately as well; they are not included in the aggregate report. `scripts/smoke-self-update.py` checks independent Yard replacement and failed replacement/SQLite recovery while redirecting only the fixture binary's restore tag; production GHCR discovery/signature admission and pull are separate from that internal worker-protocol fixture. No fixture uses existing user stacks.

## File configuration verification

Run `python scripts/smoke-service-recreate.py --image IMAGE` to validate optional service scope through the real MCP/shared facade and independent workers. The generated local project has two app replicas, a running dependency, a completed initializer and Yard. Assertions cover `image:` with a deliberately unavailable development build context, current env edits, unavailable unrelated image references, scope-bound confirmations, unknown services, incoming shared namespaces from outside the project, service-only failed-start recovery, stopped replicas and missing-file Engine fallback after retained-container cleanup. Dependency IDs/start times and the initializer run count must remain unchanged. Fixtures never connect to a homelab Yard.

Run `python scripts/smoke-configuration.py --image IMAGE` with a freshly built Linux Yard image to test authenticated file reveal/save, no mutation on Save, current-file self-recreation, daily schedule/session persistence and SQLite/runtime recovery after invalid Yard startup. Resources have generated local fixture names and do not target a running homelab.

`smoke-managed.py` additionally covers direct .env persistence and location-only metadata, inline environment edits, stale fingerprints, cached-only Recreate with the fixture registry stopped, original multi-file merge order, terminal edits, stopped services, source-preserving runtime rollback, host-file ownership, source journal recovery and repair of invalid local YAML. `smoke-recreate.py` verifies the assessed Engine fallback when files are unavailable.

The browser suite includes `smoke-configuration.cjs`: zero migration/background configuration requests, inline edit/cancel/save, retained conflict drafts, pending values, initially collapsed Compose editing, consistent Yard actions/daily controls, compact Settings and desktop/mobile layouts. `node scripts/test-browser.mjs` runs it with the other existing deterministic browser regressions against the built frontend. Screenshots are written under ignored `output/playwright/`. The drawer fixture also checks overlay navigation below 1280px using real Chromium touch input, outside taps, Escape, focus containment/return, canceled/vertical gestures, Settings navigation and responsive transitions that preserve the desktop sidebar preference.

`smoke-drawers.cjs` checks shared drawer widths at 1920/1440/1280/1024/768/390/320px, centered Phosphor close controls, consistent single/multi-container Compose entry, row-major ports, private environment values, full-height logs/terminal, literal log highlighting/navigation, export cancel/failure/retry/focus, exact text snapshots, persisted last-export status and the standalone destination. Go tests in `internal/logexport` and `internal/httpapi` cover confined paths, invalid input, same-minute/concurrent no-overwrite publication, newest-file selection and session/Origin/CSRF/confirmation/member boundaries. Export uses the configured host base even for external Compose projects; see [Features](Features.md). Generated `logs/` directories are ignored by Git and Docker builds.

## Compose parity validation

The current-image gates are aggregated by `python3 scripts/acceptance.py --image CURRENT --skip-upgrade`; CI uses the same `scripts/runtime_suites.py` inventory. The report names every selected suite and records exact image/Engine/host identities. Use repeated `--suite SCRIPT` only for diagnostics; a subset is not complete acceptance. Legacy upgrade evidence remains separate and follows the supported intermediate conversion path described in [Host validation](Host-Validation.md).

`smoke-native-parity.py` compares native Compose against Yard on distinct disposable projects: effective container configuration, profiles, dependencies, replica/state preservation, explicit overrides, orphan safety, mount contents, Docker log pagination and removal choices. IDs and fixture-specific paths are normalized; configuration and behavior are compared. Native Compose unit tests also cover interpolation and local dependency resolution. Browser fixtures exercise the reviewed override flow and bounded log pages. See [audit and evidence](Docker-Compose-Parity.md).

Every fixture must use a unique identity, record its resources and clean only those resources, including failures. Use `smoke_support.cleanup_fixture` for the final source-tree removal after stopping/removing fixture containers. Native Linux retains root-owned bind directories that a non-root CI runner cannot delete. On `PermissionError`, cleanup uses the supplied local image (`--pull never`) in a read-only, network-isolated helper with only the exact fixture child mounted writable. It refuses symlink roots and observable remaining mounts; it neither chowns the checkout nor ignores failures. Symlinks inside the fixture are deleted without following their targets. Portable regressions run with the shared source checks. For the actual Linux ownership/mount regressions, run `NOX_CLEANUP_TEST_IMAGE=LOCAL_IMAGE python3 -m unittest discover -s scripts -p test_smoke_support.py`; the Docker daemon must see the same fixture paths. These two tests are explicitly skipped when the opt-in/Linux prerequisites are absent. Never use global prune. On Docker Desktop, multi-process SQLite fixtures use a named Linux volume for Yard state; source/app bind mounts still exercise host paths. The dedicated self-update fixture preserves its bind-only requirement by binding the directory of its disposable Linux volume. This avoids filesystem-locking differences of macOS shared SQLite binds without changing production durability/recovery semantics. Desktop alias mount checks compare actual source identities inside read-only network-isolated helpers.
