# NoX Yard

NoX Yard is a lightweight, self-hosted Docker management web application for a personal homelab. Linux is the target platform, with Raspberry Pi 5 as the primary device.

## Stack

- Go 1.26 service with SQLite and server-side sessions.
- React 19, TypeScript 6, and Vite 8 frontend with a dark-only design system.
- Multi-stage Docker image and a single-service Compose installation. Formal releases publish `linux/arm64` and `linux/amd64` images to GHCR.

## CI and image publication

Push and release need Git, Node.js 24 and Go 1.26.9; Docker Desktop, frontend dependencies and Chrome are not required. Install or upgrade the lightweight hook in each clone with `npm run hooks:install`. It checks the pushed Git snapshots for whitespace, changed Go formatting, JavaScript syntax and package/lock consistency. It does not build, test, install dependencies or access the network.

A single GitHub pipeline handles pushes to `master` and pull requests. CI runs Go analysis/tests, delivery-tooling tests, Linux vulnerability analysis, frontend build/browser regressions, Compose validation and AMD64/ARM64 image health checks. Documentation-only changes skip builds. Nothing is published by a normal push or PR.

From a clean repository root on `master`, create a formal release with:

```sh
npm run release -- 1.2.0
```

The command updates only the root `package.json`, performs the lightweight checks, commits the version, creates an annotated tag and pushes `master` plus the tag atomically. One pipeline cancels superseded normal CI, calls the reusable CI, runs the Docker acceptance groups affected since the last published stable release, then publishes the exact tested images and creates the GitHub Release. Publication does not rebuild. Releases already in progress are protected against cancellation.

When you intend to release, commit the code and run the release command directly; a separate preceding push would start an unnecessary normal CI. GitHub branch protection should require **Pipeline result**, replacing the old source/Docker check names; this setting is not changed automatically.

| Release | Docker tags | GitHub |
| --- | --- | --- |
| Stable `v1.0.0` | `ghcr.io/mapherez/nox-yard:v1.0.0`, `ghcr.io/mapherez/nox-yard:latest` | Release `v1.0.0` |
| Prerelease `v1.1.0-rc.1` | `ghcr.io/mapherez/nox-yard:v1.1.0-rc.1` | Prerelease `v1.1.0-rc.1` |

Prereleases never change `latest`. Normal installation and self-update continue to follow the stable `latest` channel. See the [release guide](Documentation/Developer/Releases.md) for prerequisites and failure recovery.

## Run with Docker Compose

Formal stable releases publish `ghcr.io/mapherez/nox-yard:latest` for both `linux/amd64` and `linux/arm64`.

GHCR packages start private by default. After the first successful run, set the `nox-yard` package visibility to public in GitHub package settings to allow an unauthenticated pull, or authenticate the host to GHCR with a token that has `read:packages` access.

Copy `compose.yaml` to a directory on the Linux host with Docker Engine and the Compose plugin. An `.env` file is optional; copy `.env.example` too if you want to configure the bind address, port, or public URL. From that directory:

```sh
docker compose pull
docker compose up -d
```

Open `http://<host>:8095` and create the administrator account. The default host port is 8095; the application still listens on port 8080 inside the container. Restrict access to a trusted LAN or VPN. To bind only to a local reverse proxy, set `NOX_BIND_ADDRESS=127.0.0.1` in `.env`. Set `NOX_PUBLIC_URL` to the exact browser-facing origin when using a reverse proxy, such as `https://nox.example.test`; HTTPS enables Secure session cookies. The `./data` directory stores the administrator and sessions. Back it up and do not commit it.

The Compose file mounts `/var/run/docker.sock` so NoX Yard can discover local projects. Access to this socket effectively grants control of the Docker host; keep the app limited to a trusted LAN or VPN and protect the administrator account. Only the formal stable release workflow publishes or replaces the `latest` image.

## MCP connection

Connect an HTTP MCP client on the private homelab LAN to `http://<host>:8095/mcp`, without credentials. The always-on embedded endpoint shares the existing process and port. Its tools reuse Yard operations; browser login and the Bearer Control API remain independent. See [MCP documentation](Documentation/Developer/MCP.md) for tools, timeouts, explicit environment reads and self-update behavior.

## Local development

To use Vite with live containers from Docker Desktop, start Docker Desktop in Linux container mode, then run from the repository root:

```sh
docker compose -f compose.dev.yaml up -d --build
```

Open `http://127.0.0.1:5173`. Compose runs a Go API container and a Vite container that proxies `/api` to it. Frontend edits reload without a Docker build or a GitHub push. See the [development guide](Documentation/Developer/Development.md) for backend rebuilds and shutdown.

To run the Go server directly instead, install Go 1.26.9 or a later patch and Node.js 24. From `web/`, run `npm ci` and `npm run build`. Then, from the repository root, run:

```sh
NOX_LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/nox-yard
```

Open `http://127.0.0.1:8080`. Local data goes to `./data` by default. A local `go run` may compile a temporary executable; the Docker image uses a Linux executable built inside the image. See the [development guide](Documentation/Developer/Development.md) for configuration, frontend development, and password recovery.

## Documentation

[`Documentation/Developer/`](Documentation/Developer/README.md) is the source of truth for architecture, implementation plan, design rules, decisions, progress, features, and maintenance guidance. Keep it current with code changes.

All project files, code comments, UI copy, and documentation are written in English. Localization is outside the product scope.
