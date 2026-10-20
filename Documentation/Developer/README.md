# Developer documentation

This directory is the **canonical technical documentation** for Yard. The root README introduces the product; implementation details and decisions belong here. Start with Progress for current status, Features for implemented behavior, and the latest repository review for audit evidence. Plans and dated reviews preserve their original scope; later decisions can supersede them.

| Document | Use it for |
| --- | --- |
| [Implementation Plan](Implementation-Plan.md) | MVP scope, phases, and acceptance criteria. |
| [MVP Completion Plan](Completion-Plan.md) | Original ordered completion packages, dependencies and acceptance gates; consult Progress for what remains. |
| [Architecture](Architecture.md) | System boundaries, data flow, Docker integration, and persistence. |
| [Durable operations](Operation-Jobs.md) | Worker lifetime, resource reservations, job/history contracts and host recovery review. |
| [Configuration files](Configuration-Files.md) | Host source of truth, inline editing, migration, file API, ownership and uniform actions. |
| [MCP](MCP.md) | Embedded LAN MCP endpoint, tools, contexts, notifications, and dependency updates. |
| [Control API](Control-API.md) | Stable v1 machine endpoints, Bearer authentication, contracts, and version metadata. |
| [Design System](Design-System.md) | UI structure, style tokens, component rules, and responsive behavior. |
| [Decisions](Decisions.md) | Decisions that constrain later work and their reasons. |
| [Development Guide](Development.md) | Repository conventions, workflow, and maintenance instructions. |
| [Releases](Releases.md) | One-command formal releases, stable/prerelease channels, and recovery. |
| [Host validation](Host-Validation.md) | Isolated Pi 5/Linux acceptance, upgrade/password/backup recovery and native-host evidence. |
| [Progress](Progress.md) | Single current phase checkpoint and remaining work. |
| [Features](Features.md) | Notes on implemented features and their operational behavior. |
| [Managed updates](Managed-Updates.md) | Managed image comparison, explicit source sync and rollback. |
| [External updates](External-Updates.md) | Assessed Engine fallback, replacement and removal limits. |
| [Project schedules](Project-Schedules.md) | Daily project/Yard updates, timezone, opt-in and failed-image suppression. |
| [Self-update](Self-Update.md) | Yard Check now, daily scheduling, current-file replacement and SQLite recovery. |
| [Repository review — 2026-10-20](Repository-Review-2026-10-20.md) | Latest source/document comparison, verification results and remaining gaps. |
| [Repository review — 2026-10-08](Repository-Review-2026-10-08.md) | Historical v1.0.1 audit; its findings are not a statement of current missing features. |

## Documentation rules

1. Write documentation, code comments, UI text, and API messages in English. Do not introduce localization infrastructure.
2. Update architecture, design, feature, decision, or maintenance guidance only when that guidance changes. Record milestone status only in [Progress](Progress.md); do not repeat checkpoints in other documents.
3. Treat the Docker Engine as the runtime source of truth. When documentation describes planned behavior, label it as planned until verified by implementation.
4. Keep this index accurate as documents are added or renamed. Avoid duplicate, conflicting specifications in other folders.
5. Distinguish committed/tagged source, verified registry publication, deployed host versions and host acceptance. A local tag or an ignored `.tmp/` report is not proof of the other states. Mark superseded decisions and old handoff examples explicitly.
