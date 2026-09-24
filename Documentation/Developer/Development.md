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
- `web/src/`: typed API client, React views, and CSS Modules. Shared tokens and global rules are in `web/src/styles/`.
- `Dockerfile`, `compose.yaml`, `.env.example`: image build and single-service self-hosted installation.
- `.github/workflows/ci.yml`: read-only Go, frontend, and Compose checks on pull requests and pushes to `master`.
- `.github/workflows/publish-image.yml`: manual multi-architecture publication to GHCR.
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

For frontend development, run `npm run dev` from `web/`. Vite proxies `/api` to the Go service at `http://127.0.0.1:8080`. Keep the Go service running for authentication and persistence. The UI has no Docker data yet.

Relevant commands:

```sh
go test ./...
cd web && npm run build
docker compose config
```

The final command validates Compose syntax without starting a service. A real Linux Engine is needed to validate `docker compose pull`, `docker compose up -d`, data persistence, and target architectures.

CI runs the Go tests, frontend type-check/build, and Compose validation on pull requests into `master` and pushes to `master`. It has only `contents: read` permission and does not publish an image. To block merging a failing change, require the **Go tests** and **Frontend build and Compose config** status checks in the repository's branch protection settings. A direct push to `master` is checked after the push; the workflow alone cannot prevent that push.

## Image publication

The **Publish Docker image** workflow has only a `workflow_dispatch` trigger. It runs only when started in GitHub Actions with `master` selected, and publishes `ghcr.io/mapherez/nox-yard:latest` for `linux/amd64` and `linux/arm64`. It uses the repository's `GITHUB_TOKEN` with `packages: write`; no personal token is needed for publication. No push or tag starts the workflow automatically. Concurrent manual runs queue instead of canceling an active publication. Publication builds without cache. The Dockerfile cross-compiles the Go executable for each target architecture and fails the build if its recorded `GOARCH` differs from the target.

The image package may be private. Set its visibility to public in GitHub package settings for an unauthenticated host pull. For a private package, authenticate the host to `ghcr.io` with a token that has `read:packages` access. The Compose file references the published image and has no local `build:` context, so the host needs only `compose.yaml` and optional `.env` configuration. Run `docker compose pull` followed by `docker compose up -d` to deploy a manually published version.

## Configuration, data, and recovery

`NOX_LISTEN_ADDR` defaults to `:8080`; use `127.0.0.1:8080` for a local development server. `NOX_DATA_DIR` defaults to `./data`, and `NOX_WEB_DIR` defaults to `./web/dist`. In Compose, these are `/data` and `/srv/nox-yard/web`. The optional `NOX_PUBLIC_URL` must be an exact HTTP(S) origin without a path. Set it to the browser-facing HTTPS origin behind a reverse proxy so sessions use Secure cookies and Origin checks compare against that origin.

`.env.example` configures the Compose host bind address and port. Compose defaults to host port 8095 mapped to container port 8080; `NOX_PORT` can override the host port. The current Compose file mounts `./data` but does not yet mount the Docker socket. Keep the application on a trusted LAN/VPN, especially before the administrator account has been created. Do not commit `.env`, `data/`, credentials, sessions, or host Docker data. Back up `data/` before upgrades; SQLite is the only persisted application state at this stage.

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
