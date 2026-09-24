# Developer documentation

This directory is the **canonical technical documentation** for NoX Yard. The root README gives a short overview; implementation details and decisions belong here.

| Document | Use it for |
| --- | --- |
| [Implementation Plan](Implementation-Plan.md) | MVP scope, phases, and acceptance criteria. |
| [Architecture](Architecture.md) | System boundaries, data flow, Docker integration, and persistence. |
| [Design System](Design-System.md) | UI structure, style tokens, component rules, and responsive behavior. |
| [Decisions](Decisions.md) | Decisions that constrain later work and their reasons. |
| [Development Guide](Development.md) | Repository conventions, workflow, and maintenance instructions. |
| [Progress](Progress.md) | Completed checkpoints, current work, and next steps. |
| [Features](Features.md) | Notes on implemented features and their operational behavior. |

## Documentation rules

1. Write documentation, code comments, UI text, and API messages in English. Do not introduce localization infrastructure.
2. Update the relevant document in the same change as an implementation or architectural decision. Record completed work in [Progress](Progress.md) and implemented behavior in [Features](Features.md).
3. Treat the Docker Engine as the runtime source of truth. When documentation describes planned behavior, label it as planned until verified by implementation.
4. Keep this index accurate as documents are added or renamed. Avoid duplicate, conflicting specifications in other folders.

**Status:** Phase 1 foundation is implemented in part. See [Progress](Progress.md) for verified behavior and the pending Linux Compose runtime checkpoint.
