# Changelog

User-facing changes in Yard. Entries summarize tagged source changes; [GitHub Releases](https://github.com/mapherez/nox-yard/releases) is the record of published images and signatures.

## Unreleased

## 1.3.4

### Clearer Settings

- Settings shows the running Yard version alongside update status and build information.
- Yard updates now appear first, followed by the projects directory. MCP controls sit in a collapsed accordion with an always-visible enabled/disabled indicator.

### Easier to get started

- Reorganized the README around installation and everyday homelab use.
- Added a Quickstart, user guide, and configuration guide.
- Updated technical documentation and clarified upgrade requirements for older installations.

### More useful release notes

- Added a user-facing changelog with version-by-version summaries.
- GitHub Releases now put the matching changelog summary first, with image verification details in an expandable section.

[Release](https://github.com/mapherez/nox-yard/releases/tag/v1.3.4) · [Changes](https://github.com/mapherez/nox-yard/compare/v1.3.3...v1.3.4)

## 1.3.3

### Safer Yard updates

- Yard's built-in updater now verifies the official container image signature and release identity before installing an update.
- Added verification and recovery checks around Yard's self-update process.

This protection is provided by the new updater: installing this version does not retroactively add verification to older Yard installations.

[Release](https://github.com/mapherez/nox-yard/releases/tag/v1.3.3) · [Changes](https://github.com/mapherez/nox-yard/compare/v1.3.2...v1.3.3)

## 1.3.2

### Integration access under your control

- MCP is now opt-in, with inspection, write, and sensitive-read permissions configured in Settings.
- MCP and the Control API share a single Yard token. Generate, rotate, or revoke it from Settings, and inspect the latest ten token events.
- Choose separate host bindings and ports for Yard and MCP. Network changes remain pending until applied, with health verification and recovery handling.
- See the local MCP server's status in the sidebar.

### Release trust and housekeeping

- Older installations with DB-only project configuration must complete conversion on v1.3.1 before upgrading; already-converted installations are unaffected.
- Published container images now include keyless Cosign signatures and build provenance, with verification instructions in release notes.
- Removed the retired configuration-migration flow and its repeated network requests.
- Added the CC BY-NC-SA 4.0 license and attribution terms.

[Release](https://github.com/mapherez/nox-yard/releases/tag/v1.3.2) · [Changes](https://github.com/mapherez/nox-yard/compare/v1.3.1...v1.3.2)

## 1.3.1

### A more useful dashboard

- Switch between cards and a compact table, search projects and containers, and reorder items by dragging or sorting by name.
- View and order preferences are remembered in your browser.
- Fixed clipped metrics in dashboard cards.

### Clearer details and navigation

- Wider, reorganized drawers, stronger tabs, and more compact project details.
- Taller logs and terminal panels, highlighted log search, and confirmed log exports to the Docker host.
- Refreshed Settings to match the rest of the interface.
- Mobile-style sidebar navigation with swipe and outside-tap dismissal on tablets and small desktops.

[Release](https://github.com/mapherez/nox-yard/releases/tag/v1.3.1) · [Changes](https://github.com/mapherez/nox-yard/compare/v1.3.0...v1.3.1)

## Earlier versions

See [GitHub Releases](https://github.com/mapherez/nox-yard/releases) for earlier versions. This curated changelog starts at 1.3.1.
