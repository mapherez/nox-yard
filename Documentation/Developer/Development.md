# Development and maintenance guide

## Toolchains

- Go 1.26 for the root module `github.com/mapherez/nox-yard`.
- Node.js 24 for `web/` (`.nvmrc`), with npm and the committed `web/package-lock.json`.
- Docker Engine and the Compose plugin on a Linux integration host. Raspberry Pi 5 (`linux/arm64`) is the primary release target; `linux/amd64` is also intended.

## Repository layout

- `cmd/nox-yard/`: service entrypoint and interactive password reset command.
- `internal/auth/`: password validation and Argon2id hashes.
- `internal/store/`: SQLite schema, administrator, and sessions.
- `internal/httpapi/`: same-origin HTTP routes, session protection, and static asset serving.
- `internal/selfupdate/`: opt-in GHCR checks and the temporary Engine-based update worker; see [Self-Update](Self-Update.md).
- `web/src/`: typed API client, React views, and CSS Modules. Shared tokens and global rules are in `web/src/styles/`.
- `Dockerfile`, `compose.yaml`, `.env.example`: image build and single-service self-hosted installation.
- `scripts/check.sh`, `scripts/test.sh`, `scripts/ci-local.sh`: shared local and CI validation.
- `scripts/install-hooks.sh`: installs the local pre-push hook in a clone.
- `scripts/smoke-arm64.sh`: CI-only ARM64 executable and runtime health validation.
- `.github/workflows/ci.yml`: source checks, Docker builds, ARM64 smoke test, and gated GHCR publication.
- `Documentation/Developer/`: canonical architecture, implementation, style, decision, progress, and feature documentation.

Keep Go packages and frontend modules small and named for their responsibilities. Prefer typed application models at HTTP boundaries. Do not send Docker SDK structs, credentials, or host-only details to the browser unless a user decision requires them.

## Local run

Build the frontend first, because the Go server serves `web/dist` by default:

```sh
cd web
npm ci
npm run build
cd ..
NOX_LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/nox-yard
```

Open `http://127.0.0.1:8080`. On Windows PowerShell, set the address with `$env:NOX_LISTEN_ADDR='127.0.0.1:8080'` before `go run ./cmd/nox-yard`. `go run` compiles a temporary host executable during development; the Dockerfile builds a Linux binary inside the image. Stop the local process with Ctrl+C.

For frontend development, run `npm run dev` from `web/`. Vite proxies `/api` to the Go service at `http://127.0.0.1:8080`. Keep the Go service running for authentication and persistence. Local Docker inventory requires access to `/var/run/docker.sock` on Linux.

## Local validation and pre-push

Install Go 1.26, Node.js 24, Docker with the Compose plugin, and frontend dependencies. From the repository root:

```sh
npm ci --prefix web
sh scripts/install-hooks.sh
```

Run the installer from Git Bash on Windows, or from a POSIX shell on Linux. It installs a minimal `.git/hooks/pre-push` that calls `scripts/ci-local.sh`. Hooks are local to each clone, so repeat the installer in every new clone. The installer refuses to overwrite an existing pre-push hook or a custom `core.hooksPath`; in that case, add `sh "$(git rev-parse --show-toplevel)/scripts/ci-local.sh"` to the existing hook setup. The hook blocks a push when any check fails. It can be bypassed locally with `git push --no-verify`, but GitHub CI still runs.

The same checks can be run at any time with `sh scripts/ci-local.sh`. `check.sh` validates tracked Go files with `gofmt`, runs `go vet ./...`, and validates Compose with `docker compose config --quiet`. `test.sh` runs `go test ./...`, the frontend type-check/build, and optional frontend `test`, `lint`, and `stylelint` package scripts through `npm run --if-present`. Those optional scripts are not defined yet. Add non-interactive checks under these names when the relevant tooling is introduced. The local scripts do not build Docker images or start services. The frontend build creates the ignored `web/dist/` directory.

## GitHub CI and image publication

Pull requests into `master` run the shared source checks, explicit `linux/amd64` and `linux/arm64` Docker builds, and an ARM64 runtime smoke test. They do not publish images or change the host. QEMU runs the ARM64 container on the GitHub runner. The smoke script checks the executable's ELF architecture, then starts the image and requires `/healthz` to return `{"status":"ok"}`. It removes the test container on exit.

Pushes to `master` run the same gates. Only after all pass does the publish job build and push a multi-architecture image to GHCR with `latest` and `sha-<full commit SHA>` tags. Publication uses `GITHUB_TOKEN` with `packages: write` only in that job. There is no manual publication workflow or automatic release/version tag. Workflow concurrency cancels older runs for the same branch or pull request.

For branch protection, require **Source validation** and **Docker builds and ARM64 smoke test** before merging. Direct pushes to `master` are checked after the push; branch protection is needed if direct pushes must be disallowed. A failing check prevents the publish job from running.

The image package may be private. Set its visibility to public in GitHub package settings for an unauthenticated host pull. For a private package, authenticate the host to `ghcr.io` with a token that has `read:packages` access. The Compose file references the published image and has no local `build:` context, so the host needs only `compose.yaml` and optional `.env` configuration. Run `docker compose pull` followed by `docker compose up -d` to deploy the latest passing `master` image.

## Configuration, data, and recovery

`NOX_LISTEN_ADDR` defaults to `:8080`; use `127.0.0.1:8080` for a local development server. `NOX_DATA_DIR` defaults to `./data`, and `NOX_WEB_DIR` defaults to `./web/dist`. In Compose, these are `/data` and `/srv/nox-yard/web`. The optional `NOX_PUBLIC_URL` must be an exact HTTP(S) origin without a path. Set it to the browser-facing HTTPS origin behind a reverse proxy so sessions use Secure cookies and Origin checks compare against that origin.

`.env.example` configures the Compose host bind address and port. Compose defaults to host port 8095 mapped to container port 8080; `NOX_PORT` can override the host port. The Compose file mounts `./data` and `/var/run/docker.sock`. Keep the application on a trusted LAN/VPN, especially before the administrator account has been created. Do not commit `.env`, `data/`, credentials, sessions, or host Docker data. Back up `data/` before upgrades; SQLite is the only persisted application state at this stage.

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
