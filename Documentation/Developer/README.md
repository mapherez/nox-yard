# Developer documentation

Technical contracts and maintenance guidance for Yard. For installation and everyday use, start with the [user documentation](../User/README.md).

Start here:

- [Development](Development.md) — toolchains, local workflow, checks and recovery.
- [Architecture](Architecture.md) — boundaries and data flow.
- [Features](Features.md) — implemented behavior and supported limits.
- [Progress](Progress.md) — current review, verification boundaries and remaining work.
- [Releases](Releases.md) — publication, signatures, verification and failed-release recovery.

## Technical references

| Document | Responsibility |
| --- | --- |
| [Docker/Compose parity](Docker-Compose-Parity.md) | Findings P0/P1/P2, intentional contracts, differential tests and recorded validation. |
| [Configuration files](Configuration-Files.md) | Host source of truth, file editors, concurrency, ownership and uniform actions. |
| [Durable operations](Operation-Jobs.md) | Worker lifetime, reservations, history and reviewed recovery. |
| [Managed updates](Managed-Updates.md) | Compose deployment, image comparison, source sync and rollback. |
| [External updates](External-Updates.md) | Assessed Engine fallback, replacement, removal and recovery limits. |
| [Project schedules](Project-Schedules.md) | Daily project/Yard updates, timezone and failed-image suppression. |
| [Self-update](Self-Update.md) | Yard image trust, installation, health checks and SQLite/runtime recovery. |
| [MCP](MCP.md) | Direct access, authorization, tools and shared-library maintenance. |
| [Control API](Control-API.md) | Versioned machine endpoints, Bearer access and response contracts. |
| [Design system](Design-System.md) | UI conventions, tokens, responsive behavior and visual QA. |
| [Decisions](Decisions.md) | Architectural decisions and explicit supersession of earlier choices. |
| [Host validation](Host-Validation.md) | Isolated Linux acceptance, manual checks, upgrade and backup recovery. |

## Documentation rules

1. Write documentation, code comments, UI text and API messages in English.
2. Keep installation and everyday-use instructions in `Documentation/User/`. Use this directory for technical contracts, development and operations.
3. Update each technical contract when its behavior changes. Keep the current remaining-work list and verification status in [Progress](Progress.md); put user-facing version changes in [CHANGELOG.md](../../CHANGELOG.md).
4. Treat Docker Engine as runtime truth. Label planned behavior explicitly; archived plans do not establish implementation.
5. Distinguish source/tag state, registry publication, deployed versions and native-host acceptance. Ignored local reports are not portable proof of any other state.
6. Record architectural/security decisions in [Decisions](Decisions.md), retaining explicit supersession links. Archive dated reports when superseded; do not accumulate task-by-task transcripts in the current checkpoint.
7. Update links and this index when moving or adding documents. Add a new document only for a distinct contract or procedure; avoid parallel specifications.
