# Technical decisions

Record decisions here when they change the architecture, security model, product behavior, or development conventions. Add a date, decision, reason, and consequences; do not silently replace an earlier decision. All entries below were agreed during planning on **2026-09-24**.

## D-001 — English-only project

All UI text, API messages, code comments, and documentation are in English. There is no localization framework or language switch in the MVP. This keeps copy, design, and maintenance simple for the intended audience.

## D-002 — Go service and React frontend

Use Go for the local Docker-facing service and React/TypeScript/Vite for the browser. Serve production frontend assets from Go. SQLite and a persistent directory avoid a separate database service. Build Linux `arm64` and `amd64` images. The canonical Go module path is `github.com/mapherez/nox-yard`.

## D-003 — Docker Engine is the runtime source of truth

Discover Compose projects from Docker labels and standalone containers from the remaining Engine inventory. Do not require access to an existing Compose file. Stored NoX Yard metadata augments, but does not override, live Docker state.

## D-004 — Two levels of project management

External projects support safe operations possible through the Docker Engine, including update/recreation when their configuration can be reconstructed. New/imported/adopted projects store Compose source and support Compose-level operations. Optional adoption is explicit and follows a preview of differences. Unsupported external recreation is blocked rather than attempted with known configuration loss.

## D-005 — First-run administrator setup

The first browser visit while no administrator exists presents username/password creation. The setup remains available until the first account is atomically committed, then is permanently replaced by login. There is no initial token or time window. The deployment must initially be reachable only by trusted LAN/VPN users.

## D-006 — URL import and duplicate handling

The MVP imports Compose files from public HTTPS URLs only. The fetched source is stored; normal image updates do not silently replace its structure. Reusing a known URL offers a separate copy, explicit sync/update of an associated project after preview, or cancellation.

## D-007 — Persistence and deletion

Managed YAML, variables, credentials, settings, and job history persist across container restarts. Volume deletion is an explicit, initially unselected removal option. External volume deletion only targets identifiable exclusive volumes; shared resources are preserved.

## D-008 — Auto-update and self-management

Auto-update is opt-in per project, initially checked daily at 03:00 server-local time. NoX Yard appears in inventory and can restart/update after confirmation. An independent temporary job container survives replacement of the web service. The UI does not offer stop/remove for its own service.

## D-009 — Dark-only design and canonical docs

Use semantic CSS variables, reusable components, and responsive layouts from the first UI change. No light theme is built. `Documentation/Developer/` is the primary technical reference and is updated with implementation changes.

## D-010 — Session storage and first deployment boundary

Phase 1 stores only the administrator and hashed session tokens in SQLite. Session cookies are HttpOnly and SameSite Strict; HTTPS public origins use Secure cookies. The initial Compose installation omits the Docker socket until Docker inventory is implemented, so the current dashboard does not imply that host inventory is connected.

## D-011 — Manual GHCR publication and image-only host installation

Publish `ghcr.io/mapherez/nox-yard:latest` for Linux `amd64` and `arm64` only when the GitHub Actions workflow is started manually from `master`. The host Compose file references that image and contains no build context, allowing deployment with `docker compose pull` and `docker compose up -d`. The package must be public for anonymous pulls; a private installation requires GHCR authentication on the host. Manual publication keeps new pushes from changing the deployable image unexpectedly.

## D-012 — Target architecture must match the executable

The first published `arm64` image contained an `amd64` executable because the Go build stage defaulted its `TARGETARCH` argument to `amd64`. Build stages now use `BUILDPLATFORM`, import BuildKit's `TARGETOS` and `TARGETARCH` without defaults, and verify the Go binary's recorded architecture. Manual publication bypasses the build cache so a corrected image is rebuilt before it replaces `latest`.

## D-013 — Shared pre-push checks and gated automatic publication (2026-09-25)

This supersedes the manual publication policy in D-011 and the manual-publication detail in D-012. A clone-local pre-push hook calls repository scripts for formatting, vet, tests, frontend build, optional frontend checks, and Compose validation. GitHub repeats these checks, builds `amd64` and `arm64` images, and runs the ARM64 executable under QEMU with a `/healthz` smoke test. Pull requests never publish. Passing pushes to `master` publish `latest` and a full-commit `sha-` tag to GHCR. Concurrent runs on the same ref cancel older runs. Versioned releases remain a separate future decision.

## D-014 — Temporary Engine-based self-update worker (2026-09-25)

The app checks the public GHCR `latest` manifest for its platform and uses the image config digest as the comparison ID. On change it pulls through the Engine API and starts a disposable helper from the exact old image ID. The helper preserves the original Compose container until the replacement passes `/healthz`, with a SQLite snapshot for rollback. Automatic checks are opt-in and run every six hours. This avoids a permanent updater service and Docker CLI in the runtime image.
