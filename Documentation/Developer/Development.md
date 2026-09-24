# Development and maintenance guide

**Current state:** scaffolding only. The commands below describe the intended workflow; there is no runnable application or deployment yet.

## Toolchains

- Go 1.26 or newer compatible 1.26 patch release for the root module.
- Node.js 24 LTS for the frontend (`.nvmrc`). Use npm and commit `web/package-lock.json` when dependencies are first installed during application implementation.
- Docker Engine and the Docker Compose plugin on a Linux development or integration host. Raspberry Pi 5 (`linux/arm64`) is the primary release target; `linux/amd64` is also supported.

The current machine has Go 1.26.3 but no Node.js executable. This checkpoint therefore does not install frontend dependencies or run a frontend build. The root `go.mod` is intentionally minimal and its `nox-yard` module path is provisional until the canonical repository path is known.

## Repository conventions

- `cmd/`: small Go entrypoints only. Keep business logic in `internal/`, separated into HTTP/auth, inventory, Docker adapter, Compose adapter, jobs, and storage packages as described in [Architecture](Architecture.md).
- `web/src/`: future React source. Put shared tokens and global layout rules under `styles/`; put feature-specific CSS Modules next to the components that use them. Consult [Design System](Design-System.md) before adding or changing UI patterns.
- `deploy/`: future Compose installation files and release configuration. Do not commit local credentials or generated `/data` contents.
- `Documentation/Developer/`: source of truth for technical behavior. Update [Features](Features.md), [Decisions](Decisions.md), and [Progress](Progress.md) in the same change as related code.

Keep Go packages and frontend modules small and named for their responsibilities. Prefer typed application models at HTTP boundaries. Do not expose Docker socket paths, raw Engine structs, credentials, or host-only details to the browser unless needed for a user decision.

## Configuration and secrets

`.gitignore` excludes local `.env` files, generated databases, persistent `data/`, build output, logs, and dependencies. Future example configuration may use `.env.example` with placeholders only. Never commit real passwords, Compose interpolation secrets, registry credentials, session tokens, or Docker host data.

The planned Docker installation mounts the local Engine socket and persistent data. Limit browser access to a trusted LAN/VPN and use a user-managed HTTPS reverse proxy for HTTPS. While the first-run account is unclaimed, anyone who can reach the setup screen can create it, by the chosen product design.

## Change workflow

1. Read the relevant architecture, design, and feature notes before a change. Update a decision entry if a new choice supersedes an earlier one.
2. Implement one behavior across its backend, API, frontend, documentation, and meaningful tests. Keep application and deployment errors actionable and in English.
3. Verify the smallest relevant unit/integration checks. Docker lifecycle and recovery behavior require integration checks against a real Linux Engine; UI behavior requires browser checks at desktop/tablet/mobile widths and with keyboard navigation.
4. Update [Progress](Progress.md) with completed work, verification, and the next checkpoint. Add operational notes to [Features](Features.md) only after behavior is implemented.

Do not mark a phase complete based on scaffolding alone. A checkpoint requires its acceptance behavior to run and be verified.

## Documentation references

- [Implementation Plan](Implementation-Plan.md) defines the MVP milestones and acceptance cases.
- [Architecture](Architecture.md) defines system boundaries and data flow.
- [Design System](Design-System.md) defines UI rules and token conventions.
- [Decisions](Decisions.md) records product and technical choices.
