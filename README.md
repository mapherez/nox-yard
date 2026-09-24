# NoX Yard

NoX Yard is a lightweight, self-hosted Docker management web application for a personal homelab. Linux is the target platform, with Raspberry Pi 5 as the primary device.

## Stack

- Go 1.26 service with SQLite and server-side sessions.
- React 19, TypeScript 6, and Vite 8 frontend with a dark-only design system.
- Multi-stage Docker image and a single-service Compose installation. Passing pushes to `master` publish `linux/arm64` and `linux/amd64` images to GHCR.

## CI and image publication

The pre-push hook runs shared Go, frontend, and Compose checks locally without a Docker build. Install it in each clone with `sh scripts/install-hooks.sh` after `npm ci --prefix web` (use Git Bash on Windows). GitHub CI repeats those checks, builds both architectures, and runs the ARM64 container under QEMU to verify its executable and `/healthz`. Pull requests publish nothing. Passing pushes to `master` publish `latest` and `sha-<commit>` tags. See the [development guide](Documentation/Developer/Development.md) for details.

## Run with Docker Compose

After CI succeeds on `master`, GitHub publishes `ghcr.io/mapherez/nox-yard:latest` and `ghcr.io/mapherez/nox-yard:sha-<commit>` for both `linux/amd64` and `linux/arm64`.

GHCR packages start private by default. After the first successful run, set the `nox-yard` package visibility to public in GitHub package settings to allow an unauthenticated pull, or authenticate the host to GHCR with a token that has `read:packages` access.

Copy `compose.yaml` to a directory on the Linux host with Docker Engine and the Compose plugin. An `.env` file is optional; copy `.env.example` too if you want to configure the bind address, port, or public URL. From that directory:

```sh
docker compose pull
docker compose up -d
```

Open `http://<host>:8095` and create the administrator account. The default host port is 8095; the application still listens on port 8080 inside the container. Restrict access to a trusted LAN or VPN. To bind only to a local reverse proxy, set `NOX_BIND_ADDRESS=127.0.0.1` in `.env`. Set `NOX_PUBLIC_URL` to the exact browser-facing origin when using a reverse proxy, such as `https://nox.example.test`; HTTPS enables Secure session cookies. The `./data` directory stores the administrator and sessions. Back it up and do not commit it.

The Compose file mounts `/var/run/docker.sock` so NoX Yard can discover local projects. Access to this socket effectively grants control of the Docker host; keep the app limited to a trusted LAN or VPN and protect the administrator account. Only a passing `master` CI run publishes or replaces the `latest` image.

## Local development

Install Go 1.26 and Node.js 24. From `web/`, run `npm ci` and `npm run build`. Then, from the repository root, run:

```sh
NOX_LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/nox-yard
```

Open `http://127.0.0.1:8080`. Local data goes to `./data` by default. A local `go run` may compile a temporary executable; the Docker image uses a Linux executable built inside the image. See the [development guide](Documentation/Developer/Development.md) for configuration, frontend development, and password recovery.

## Documentation

[`Documentation/Developer/`](Documentation/Developer/README.md) is the source of truth for architecture, implementation plan, design rules, decisions, progress, features, and maintenance guidance. Keep it current with code changes.

All project files, code comments, UI copy, and documentation are written in English. Localization is outside the product scope.
