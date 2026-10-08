# Developer documentation

This directory is the **canonical technical documentation** for NoX Yard. The root README gives a short overview; implementation details and decisions belong here.

| Document | Use it for |
| --- | --- |
| [Implementation Plan](Implementation-Plan.md) | MVP scope, phases, and acceptance criteria. |
| [MVP Completion Plan](Completion-Plan.md) | Ordered remaining implementation packages, dependencies, acceptance gates, and release closure. |
| [Architecture](Architecture.md) | System boundaries, data flow, Docker integration, and persistence. |
| [Durable operations](Operation-Jobs.md) | Worker lifetime, resource reservations, job/history contracts and host recovery review. |
| [MCP](MCP.md) | Embedded LAN MCP endpoint, tools, contexts, notifications, and dependency updates. |
| [Control API](Control-API.md) | Stable v1 machine endpoints, Bearer authentication, contracts, and version metadata. |
| [Design System](Design-System.md) | UI structure, style tokens, component rules, and responsive behavior. |
| [Decisions](Decisions.md) | Decisions that constrain later work and their reasons. |
| [Development Guide](Development.md) | Repository conventions, workflow, and maintenance instructions. |
| [Releases](Releases.md) | One-command formal releases, stable/prerelease channels, and recovery. |
| [Progress](Progress.md) | Single current phase checkpoint and remaining work. |
| [Features](Features.md) | Notes on implemented features and their operational behavior. |
| [Repository review — 2026-10-08](Repository-Review-2026-10-08.md) | Audit evidence, implementation gaps, and recommended next work. |

## Documentation rules

1. Write documentation, code comments, UI text, and API messages in English. Do not introduce localization infrastructure.
2. Update architecture, design, feature, decision, or maintenance guidance only when that guidance changes. Record milestone status only in [Progress](Progress.md); do not repeat checkpoints in other documents.
3. Treat the Docker Engine as the runtime source of truth. When documentation describes planned behavior, label it as planned until verified by implementation.
4. Keep this index accurate as documents are added or renamed. Avoid duplicate, conflicting specifications in other folders.
