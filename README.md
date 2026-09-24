# NoX Yard

NoX Yard is a planned, lightweight, self-hosted Docker management web application for a personal homelab. Linux is the target platform, with Raspberry Pi 5 as the primary device.

## Current status

This repository currently contains **project scaffolding and technical documentation only**. There is no runnable NoX Yard application, Docker Compose deployment, or implemented feature yet. The next development checkpoint starts the application foundation.

## Planned stack

- Go backend using the Docker Engine API, SQLite, and a persistent job system.
- React, TypeScript, and Vite frontend with a dark-only design system.
- Docker Compose installation and multi-architecture Linux images (`arm64` and `amd64`).

The root Go module and `web/` frontend manifest establish the toolchains. The module name `nox-yard` is provisional until the repository has a canonical remote import path. Frontend dependencies have not been installed or locked yet. The local environment used for this checkpoint has Go but no Node.js installation.

## Documentation

[`Documentation/Developer/`](Documentation/Developer/README.md) is the source of truth for implementation, architecture, design rules, decisions, progress, and maintenance guidance. Keep it current whenever behavior or technical direction changes.

## Repository layout

| Path | Purpose |
| --- | --- |
| `cmd/` | Future Go entrypoints. |
| `internal/` | Future backend packages. |
| `web/` | Frontend toolchain and future application source. |
| `deploy/` | Future Docker Compose and release assets. |
| `Documentation/Developer/` | Canonical technical documentation. |

All project files, code comments, UI copy, and documentation are written in English. Localization is outside the product scope.
