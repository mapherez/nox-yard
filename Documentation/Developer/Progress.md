# Progress and checkpoints

Update this file when a checkpoint is reached or the next step changes. Keep status claims tied to what has actually been verified.

## 2026-09-24 — Initial repository scaffold

**Status:** complete. Created root project configuration, toolchain manifests, placeholder source directories, and the canonical developer documentation.

## 2026-09-24 — Phase 1 foundation in progress

Implemented:

- Go HTTP service with health endpoint and production frontend asset serving.
- SQLite schema version 1 for the administrator and hashed sessions, with WAL enabled and a persistent `data/` directory.
- First-run administrator creation, Argon2id passwords, login, logout, session cookies, origin and CSRF protection, rate limiting, and interactive local password reset.
- React setup/login screens and responsive authenticated dashboard shell, with centralized dark tokens and CSS Modules.
- Multi-stage Dockerfile, local Compose configuration, example environment, and frontend lockfile.

**Verification:** `go test ./...` passed, `npm run build` passed, and `docker compose config` parsed the deployment. Browser checks covered setup, logout, and login at desktop and mobile widths; the browser reported no console errors after the username pattern correction. The temporary Go server and browser were stopped, their test data was removed, and port 8080 was free. The Docker daemon was unavailable, so image build and Compose runtime behavior on Linux remain unverified.

**Next checkpoint:** run the Phase 1 image and Compose installation on Linux `arm64` and `amd64`, verify persistence across restart and password recovery, then begin Phase 2 Docker inventory and direct management. The current dashboard deliberately shows an inventory placeholder.

## 2026-09-24 — Manual image publication prepared

Added a manually dispatched GitHub Actions workflow that publishes a multi-platform `latest` image to GHCR from `master`. Updated the Compose installation to pull that image without a local build context, and documented host deployment and package visibility. No workflow has run and no image has been published by this change. The first GitHub Actions build and Linux host pull remain to be verified.

## Phase status

| Phase | Status |
| --- | --- |
| Repository preparation and developer documentation | Complete |
| 1. Foundation application and authentication | In progress; Linux Compose runtime checkpoint pending |
| 2. Inventory and direct Docker management | Not started |
| 3. Managed Compose projects | Not started |
| 4. Safe update and scheduled operations | Not started |
| 5. Validation and release | Not started |
