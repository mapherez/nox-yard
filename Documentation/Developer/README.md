# Developer documentation

Technical contracts and maintenance guidance for Yard. For installation and everyday use, start with the [user documentation](../User/README.md).

Start here:

- [Development](Development.md) — toolchains, local workflow, checks and recovery.
- [Technical reference](Architecture.md) — architecture, operating contracts, security decisions, backup/recovery and isolated validation.
- [Features](Features.md) — implemented behavior and supported limits.
- [Releases](Releases.md) — publication, signatures, verification and failed-release recovery.

## Technical references

| Document | Responsibility |
| --- | --- |
| [Configuration files](Configuration-Files.md) | Host source of truth, file editors, concurrency, ownership and uniform actions. |
| [Durable operations](Operation-Jobs.md) | Worker lifetime, reservations, history and reviewed recovery. |
| [Managed updates](Managed-Updates.md) | Compose deployment, image comparison, source sync and rollback. |
| [External updates](External-Updates.md) | Assessed Engine fallback, replacement, removal and recovery limits. |
| [Project schedules](Project-Schedules.md) | Daily project/Yard updates, timezone and failed-image suppression. |
| [Self-update](Self-Update.md) | Yard image trust, installation, health checks and SQLite/runtime recovery. |
| [Suite integration](Suite-Integration.md) | Same-host pairing, participating services, scoped deployment registration, lifecycle and recovery. |
| [MCP](MCP.md) | Direct/Suite access, unchanged tools, private human menus and shared-library maintenance. |
| [Control API](Control-API.md) | Versioned machine endpoints, Bearer access and response contracts. |
| [Design system](Design-System.md) | UI conventions, tokens, responsive behavior and visual QA. |

## Documentation rules

1. Write documentation, code comments, UI text and API messages in English.
2. Keep installation and everyday-use instructions in `Documentation/User/`. Use this directory for technical contracts, development and operations.
3. Update each technical contract when its behavior changes. Keep current architectural/operational rules in the [technical reference](Architecture.md) and user-facing changes in [CHANGELOG.md](../../CHANGELOG.md); do not create another implementation-progress log.
4. Treat Docker Engine as runtime truth. Label planned behavior explicitly; archived plans do not establish implementation.
5. Distinguish source/tag state, registry publication, deployed versions and native-host acceptance. Ignored local reports are not portable proof of any other state.
6. Maintain current architectural/security decisions in the [technical reference](Architecture.md). Keep procedures in the relevant feature guide; do not accumulate superseded decision logs or task transcripts.
7. Update links and this index when moving or adding documents. Add a new document only for a distinct contract or procedure; avoid parallel specifications.
