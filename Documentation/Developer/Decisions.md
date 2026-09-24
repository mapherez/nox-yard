# Technical decisions

Record decisions here when they change the architecture, security model, product behavior, or development conventions. Add a date, decision, reason, and consequences; do not silently replace an earlier decision. All entries below were agreed during planning on **2026-09-24**.

## D-001 — English-only project

All UI text, API messages, code comments, and documentation are in English. There is no localization framework or language switch in the MVP. This keeps copy, design, and maintenance simple for the intended audience.

## D-002 — Go service and React frontend

Use Go for the local Docker-facing service and React/TypeScript/Vite for the browser. Serve production frontend assets from Go. SQLite and a persistent directory avoid a separate database service. Build Linux `arm64` and `amd64` images. The root Go module currently uses the provisional local name `nox-yard` because no canonical Git remote exists.

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
