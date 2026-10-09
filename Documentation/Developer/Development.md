# Development and maintenance guide

## Toolchains

- Go 1.26.9 or later in the 1.26 series for the root module `github.com/mapherez/nox-yard`; this patch floor addresses the standard-library advisories reported by the CI vulnerability scanner on 2026-10-09.
- Node.js 24 for `web/` (`.nvmrc`), with npm and the committed `web/package-lock.json`.
- Docker Engine and the Compose plugin on a Linux integration host. Raspberry Pi 5 (`linux/arm64`) is the primary release target; `linux/amd64` is also intended.

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
- `scripts/check.sh`, `scripts/test.sh`, `scripts/ci-local.sh`: explicitly requested full source checks; CI invokes its separate jobs.
- `scripts/install-hooks.mjs`, `scripts/push-check.mjs`: portable lightweight hook installation and pushed-snapshot checks.
- `scripts/smoke-arm64.sh`: CI executable/runtime health checks, supporting AMD64 and ARM64.
- `scripts/build-version.sh`: exact-commit Git tag/full-SHA version metadata for normal builds.
- `package.json`, `scripts/release*.mjs`: formal release version, command, shared validation, metadata, and tests.
- `.github/workflows/pipeline.yml`: push/PR entrypoint and release classification/cancellation; `ci.yml`: reusable source/image validation.
- `.github/workflows/release.yml`: reusable CI call, selected runtime acceptance, image-artifact publication and GitHub Releases.
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

## Local validation and pre-push

Install Git, Node.js 24 and Go 1.26.9. Push and release require neither Docker Desktop, frontend dependencies nor Chrome. From the repository root:

```sh
npm run hooks:install
```

This portable Node installer creates the clone-local pre-push hook or migrates the exact old Yard hook. It preserves custom hooks and `core.hooksPath`; integrate `node "$(git rev-parse --show-toplevel)/scripts/push-check.mjs" "$@"` into custom hooks, forwarding Git's stdin. The old `sh scripts/install-hooks.sh` entrypoint remains available. Repeat installation for each clone.

The hook validates committed snapshots, not working-tree edits: whitespace, `gofmt` for changed Go files, Node syntax for changed JS/MJS/CJS, package JSON and frontend manifest/lock agreement. Branch/tag updates targeting the same commit are checked once. Unknown remote objects fall back to checking the full tree. There are no network calls, automatic dependency installations, compilation, browser or Docker operations. Release preflight shares this validator; its own push reuses only an exact validated tree/base within that process. This local marker is never used as evidence by remote CI.

During development, run the smallest relevant regression checks. Real container behaviour needs an isolated Linux Engine, but a minor unrelated change does not require Docker or the full acceptance suite. `sh scripts/ci-local.sh` remains an explicitly requested full source check, requiring its documented Go/frontend/browser/Compose prerequisites; neither the hook nor release invokes it. It is also not the remote CI entrypoint.

The event entrypoint is `pipeline.yml`; `ci.yml` and `release.yml` accept reusable calls only. Normal CI runs vet/Go tests, delivery tests, vulnerability checks, frontend build/browser tests and two architecture health checks. Go and frontend validation run in parallel. Compiled binaries preserve executable permissions through a tar artifact; tested frontend assets and image archives are reused downstream. No validation is copied into the publishing job.

Configure branch protection to require **Pipeline result** instead of **Source validation** and **Docker builds and ARM64 smoke test**. The aggregate check handles documentation skips, normal CI and release gates; configuration is intentionally manual. Obsolete PR runs cancel automatically. A release explicitly cancels only ancestor normal CI in `master`, including the legacy CI during migration; other releases, newer/unrelated runs and completed runs remain intact.

## Explicit durable-job acceptance

These checks run against disposable fixtures, separately from the default source checks. Build a local image and run:

```sh
docker buildx build --platform linux/amd64 --load -t nox-yard:jobs-smoke .
python scripts/smoke-jobs.py --image nox-yard:jobs-smoke
python scripts/smoke-managed.py --image nox-yard:jobs-smoke
python scripts/smoke-recreate.py --image nox-yard:jobs-smoke
python scripts/smoke-mcp.py --image nox-yard:jobs-smoke
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

The script mocks API responses in the test browser only; it checks reload/reopening, action blocking, keyboard acknowledgement/conflict/retry/focus, rollback/cleanup details and 1440/768/390/320px layouts. Screenshots go to ignored `output/playwright/`. Real Docker interruption remains the jobs fixture's responsibility. Selected runtime gates are wired into release CI; confirming their first real GitHub execution remains C7 work.

## GitHub CI and image publication

PRs and normal master pushes call the reusable CI; documentation-only changes skip builds. Backend/frontend validation runs once and produces artifacts for image packaging. Both Linux image architectures receive executable and `/healthz` checks; ARM64 uses QEMU. Normal pushes publish nothing.

Formal releases use `npm run release -- X.Y.Z` from a clean master checkout. One pipeline calls the same CI, runs cumulative-diff-selected acceptance, then publishes the tested image artifacts. A release cancels only obsolete ancestor normal CI; new normal pushes cannot cancel releases. Stable tags promote `latest`; prereleases do not. See [Releases](Releases.md) for artifact identities, retries, queueing and the full-acceptance override.

Require **Pipeline result** for branch protection. Direct master pushes are checked after the push; use protection rules if they must be disallowed. Old source/Docker check names must be removed from required checks manually. Any failed applicable gate blocks the aggregate result and publication.

The image package may be private. Set its visibility to public in GitHub package settings for an unauthenticated host pull. For a private package, authenticate the host to `ghcr.io` with a token that has `read:packages` access. The Compose file references the published image and has no local `build:` context, so the host needs only `compose.yaml` and optional `.env` configuration. Run `docker compose pull` followed by `docker compose up -d` to deploy the latest stable release.

## Configuration, data, and recovery

The optional [Control API](Control-API.md) uses the same backend port. `NOX_YARD_API_ENABLED` defaults to false; when true, `NOX_YARD_API_KEY` must be an independent key without whitespace, commas, or control characters. Compose passes both variables through from `.env`. Public `/v1/health` and `/v1/info` remain available. Machine requests use only Bearer auth; browser sessions/Origin/CSRF remain unchanged. `NOX_YARD_VERSION` optionally overrides the incorporated build version; leave it empty to use the exact Git tag/full-SHA metadata, or `dev` for unversioned builds. `buildSHA` remains separate for self-update. No changes to bind address, reverse proxy, or `/healthz` healthchecks are required.

`NOX_LISTEN_ADDR` defaults to `:8080`; use `127.0.0.1:8080` for a local development server. `NOX_DATA_DIR` defaults to `./data`, and `NOX_WEB_DIR` defaults to `./web/dist`. In Compose, these are `/data` and `/srv/nox-yard/web`. The optional `NOX_PUBLIC_URL` must be an exact HTTP(S) origin without a path. Set it to the browser-facing HTTPS origin behind a reverse proxy so sessions use Secure cookies and Origin checks compare against that origin.

`.env.example` configures the Compose host bind address and port. Compose defaults to host port 8095 mapped to container port 8080; `NOX_PORT` can override the host port. The Compose file mounts `./data` and `/var/run/docker.sock`. Keep the application on a trusted LAN/VPN, especially before the administrator account has been created. Do not commit `.env`, `data/`, credentials, sessions, or host Docker data.

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

Do not mark a phase complete based on scaffolding or a successful build alone. Use the acceptance checkpoint in [Implementation Plan](Implementation-Plan.md).

## Updating NoX MCP

The embedded MCP adapter consumes the latest available NoX MCP through normal Go module resolution. Update with `go get github.com/mapherez/nox-mcp@latest`, then `go mod tidy`, and commit the generated `go.mod`/`go.sum` changes. Run the repository checks, MCP integration tests and both architecture builds. Do not manage dependency SHAs manually or use a local checkout replacement. See [MCP](MCP.md) for the transport and shared-facade contracts.

## Project schedule validation

Compose passes `NOX_TIMEZONE` (IANA name, default `Etc/UTC`) as `TZ`; the backend rejects an invalid timezone. A direct local server uses `TZ` itself, defaulting to UTC. Opt-in remains per project, independent of the Yard self-update settings.

```sh
python scripts/smoke-schedules.py --image nox-yard:local
# With local Vite and a Playwright CLI browser session:
playwright-cli run-code --filename scripts/smoke-schedules.cjs
```

The scheduling fixture uses a disposable registry, application images, named state volume and Linux runner containing the built Yard binary. Updates run through real independent workers with only state/socket mounts. Test code supplies the daily clock to `Tick`; production provides no clock override. Assertions cover default-off, unchanged/changed images, reopening storage during a running worker, standalone target lineage, failed-image suppression/manual retry, extra image-volume rejection, stopped/manual-busy targets, external Compose and managed comparison/rollback/source preservation. Store/time tests cover two SQLite connections, atomic admission, coalesced misses, civil-date duplication, DST and schema 7 migration. Browser checks cover opt-in/save/retry/reload, effective timezone, skipped history, unavailable-target disable and 320px/keyboard behavior. Cleanup uses only generated fixture identities and recorded anonymous volumes. Docker Desktop and ARM64 emulation do not replace C6 real-host acceptance.

## Host upgrade and recovery acceptance

See [Host validation](Host-Validation.md) for the prepared Pi 5 handoff, native-host suite/report and manual UI checklist. `scripts/smoke-upgrade.py --image CURRENT --legacy-image LEGACY` uses a real v1.0.1 image, disposable state/application storage, the actual interactive password CLI and a three-part backup/restore. `scripts/acceptance.py` runs all isolated runtime fixtures against native current/legacy images and records exact platform/image evidence. `scripts/smoke-self-update.py` checks real independent Yard replacement and failed replacement/SQLite recovery while redirecting only the fixture binary's restore tag; production GHCR discovery/pull is checked through the release path separately. No fixture uses existing user stacks.
