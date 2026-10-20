# User guide

[Install Yard](Quickstart.md) · [Configuration](Configuration.md) · [What's new](../../CHANGELOG.md)

## Dashboard

Yard discovers Compose projects and standalone containers on its Docker host, including changes made outside Yard. The dashboard shows status, health, CPU usage, memory, and uptime.

Choose a card grid or compact table, search by project/container/service name, and drag items into your preferred order. A matching container keeps its parent project visible. You can also sort names A–Z or Z–A. Your view and order are remembered in that browser, including after refresh or logout; they do not sync between browsers.

## Workload types

| Type | Meaning |
| --- | --- |
| Managed Compose | Deployed or adopted by Yard, with saved Compose definitions and additional management capabilities. |
| External Compose | An existing Compose project discovered automatically, without import. Supported projects can be adopted. |
| Standalone container | A Docker container outside a Compose project. |

Available actions depend on the workload and whether its configuration can be safely read and reproduced. Yard rejects unsupported replacements instead of guessing at missing configuration. See the [feature reference](../Developer/Features.md) for detailed limits.

## Deploy and edit projects

Set **Projects directory** in Settings to an existing absolute host directory before deploying. **New Project** accepts public HTTPS URLs, pasted YAML, or uploaded Compose files. Fill in variables, then validate and review services, images, ports, mounts, and networks before confirming.

New projects use `BASE/PROJECT_NAME/compose.yml`. Existing managed projects retain their host file locations. Host Compose and environment files remain the configuration source of truth.

Supported projects expose environment editing and a **Compose and environment files** editor in Details. **Save** validates and writes the files; **Recreate** applies them. Restart does not apply configuration changes. Synchronizing a URL source is a separate reviewed action.

## Details, logs, and terminal

**Details** contains lifecycle actions, state and health, resource usage, image information, ports, mounts, networks, and environment variables. Environment values are hidden until explicitly revealed. At project level, use **Operations** to inspect action progress, results, and recovery information; jobs retain their recorded state when you close or refresh the browser.

**Logs** lets you switch containers, follow output, and search with highlighted matches. Export saves the currently displayed buffer as a `.txt` file on the Docker host, rather than downloading it to your browser. Files use `BASE/PROJECT_NAME/logs/YYYY-MM-DD-HH-mm.txt`; projects outside the configured base and standalone containers use a named directory under that base. The latest export time appears below the logs.

**Terminal** opens an interactive `/bin/sh` in the selected running container. The container must provide that shell. Leaving the terminal view closes the session.

## Updates and maintenance

### Update a workload manually

1. Select **Pull** to download the images referenced by the current configuration.
2. Select **Recreate**, review the confirmation, and apply the replacement.

Pull does not replace containers. Recreate uses locally cached images and saved configuration, without pulling or building. At project level it targets the project; from a Compose container's details it targets that service and its replicas, leaving dependencies and initializers untouched.

Yard verifies supported replacements and attempts recovery after ordinary replacement failures. Recovery cannot reverse application writes or database migrations in persistent data. Back up important workload data before upgrades.

### Schedule project updates

In the project's **Details**, enable **Automatic image updates**, choose the daily time, and save. This is disabled by default and requires an eligible, healthy running workload. Times follow the server timezone configured through `NOX_TIMEZONE`.

### Update Yard

In **Settings**, **Check now** checks for and installs an available update immediately, even when automatic updates are disabled. For daily automatic checks, enable the schedule in Yard's own project Details. Automatic self-updates are off by default.

The built-in updater verifies the official image signature before installation. The default Compose file tracks the stable `latest` channel. To manage versions manually, use a published version tag or verified digest; see [image selection and verification](../Developer/Releases.md#choose-an-update-policy-in-compose). A pinned Compose image is respected by the updater.

### Backups and removal

Back up three things separately: Yard's `./data` directory, project Compose/environment files, and workload volumes or bind-mounted data. Let active operations finish, then stop Yard before copying its data directory for a consistent offline backup. See the [maintenance guide](../Developer/Development.md) for backup, restore, and administrator password recovery.

Removal is reviewed before confirmation. Docker volumes are retained by default unless eligible volume deletion is explicitly selected. Check operation results and any recovery instructions before retrying a failed action.
