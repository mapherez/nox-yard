# NoX Yard

NoX Yard is a lightweight, self-hosted Docker management web application for a personal homelab. Linux is the target platform, with Raspberry Pi 5 as the primary device.

## Current status

The Phase 1 foundation is implemented: a Go service serves a React application with first-run administrator setup, login, logout, SQLite persistence, and an authenticated dashboard shell. Docker inventory and management are planned for the next phases; the dashboard does not yet show host containers. The Compose installation has been configured but has not been run against a Linux Docker Engine yet.

## Stack

- Go 1.26 service with SQLite and server-side sessions.
- React 19, TypeScript 6, and Vite 8 frontend with a dark-only design system.
- Multi-stage Docker image and a single-service Compose installation. Linux `arm64` and `amd64` are intended targets; runtime validation is pending.

## Run with Docker Compose

On a Linux host with Docker Engine and the Compose plugin:

```sh
cp .env.example .env
docker compose up --build -d
```

Open `http://<host>:8080` and create the administrator account. Restrict access to a trusted LAN or VPN. To bind only to a local reverse proxy, set `NOX_BIND_ADDRESS=127.0.0.1` in `.env`. Set `NOX_PUBLIC_URL` to the exact browser-facing origin when using a reverse proxy, such as `https://nox.example.test`; HTTPS enables Secure session cookies. The `./data` directory stores the administrator and sessions. Back it up and do not commit it.

The current Compose file does not mount the Docker socket. Docker access will be added with the inventory implementation.

## Local development

Install Go 1.26 and Node.js 24. From `web/`, run `npm ci` and `npm run build`. Then, from the repository root, run:

```sh
NOX_LISTEN_ADDR=127.0.0.1:8080 go run ./cmd/nox-yard
```

Open `http://127.0.0.1:8080`. Local data goes to `./data` by default. A local `go run` may compile a temporary executable; the Docker image uses a Linux executable built inside the image. See the [development guide](Documentation/Developer/Development.md) for configuration, frontend development, and password recovery.

## Documentation

[`Documentation/Developer/`](Documentation/Developer/README.md) is the source of truth for architecture, implementation plan, design rules, decisions, progress, features, and maintenance guidance. Keep it current with code changes.

All project files, code comments, UI copy, and documentation are written in English. Localization is outside the product scope.
