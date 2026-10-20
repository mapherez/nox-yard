
# Yard

**Your Docker homelab, in view.**

Yard is a self-hosted web application in the NoX environment for managing Docker containers and Compose projects from a single, focused dashboard.

Built for personal homelabs, it brings container monitoring, project deployment, lifecycle controls, live logs, interactive terminals, image updates, and automated maintenance into one interface — without requiring you to manage everything through the command line.

Designed for Linux hosts, with Raspberry Pi and x86-64 support.

**Self-hosted · Docker-native · Lightweight · Open source**

---

## What is Yard?

Running a homelab often means managing multiple Docker applications, checking their health, inspecting logs, updating images, and maintaining Compose configurations.

Yard brings these everyday operations together.

It connects directly to your local Docker Engine, automatically discovers existing workloads, and presents them through a responsive web interface.

You can monitor an existing installation, deploy a new Compose project, investigate a failing container, or manage updates without leaving the dashboard.

Yard focuses on **one Docker host at a time**, keeping the experience straightforward and local to your infrastructure.

## Features

### Docker dashboard

Get an overview of the Docker workloads running on your host.

- Automatically discover Compose projects and standalone containers.
- View container states, health information, and project status.
- Monitor CPU usage, memory consumption, and uptime.
- See containers grouped by their Compose project.
- Switch between a card grid and a compact table with the same project information and horizontal scrolling on narrow screens.
- Search project, container and service names as you type; container matches keep their parent project visible.
- Drag projects into a custom order or sort names A to Z / Z to A; the browser remembers the view and order.
- Receive live inventory updates when Docker changes, including changes made outside Yard.
- Access individual projects and containers through contextual detail panels.

The dashboard updates automatically, with manual refresh available whenever needed.

### Project and container management

Manage existing workloads directly from the interface.

- Start, stop, and restart projects or individual containers.
- Pull container images from their configured registries.
- Pull images, then recreate supported workloads to apply them.
- Edit supported Compose and environment files directly in the project drawer.
- Remove containers and projects through reviewed confirmation dialogs.
- Inspect detailed container configurations and runtime information.

Yard distinguishes between three types of workloads:

| Type | Description |
| --- | --- |
| **Managed Compose** | Projects deployed or adopted by Yard, with saved Compose definitions and additional management capabilities. |
| **External Compose** | Existing Compose projects automatically discovered from Docker, without requiring import. |
| **Standalone Container** | Individual Docker containers that are not part of a Compose project. |

Existing workloads can be monitored and controlled immediately. Supported external Compose projects can also be adopted for managed operations.

### Deploy Compose projects

Deploy new Docker applications directly from Yard.

Select **New Project** from the dashboard and provide a Docker Compose definition using one of three methods:

- **HTTPS URL** — Load a Compose definition from a public HTTPS address.
- **Paste YAML** — Paste a Compose configuration directly into the application.
- **Upload file** — Import a local Compose YAML file.

Yard reads the definition, identifies required variables and environment files, and lets you configure their values before deployment.

Before anything is created, a validation and preview step displays the proposed services, images, ports, mounts, networks, and relevant changes.

Once confirmed, Yard prepares the project files on the Docker host, pulls the required images, and deploys the stack.

Managed projects retain their original host file locations for subsequent operations. Current Compose and environment files are authoritative; SQLite holds application metadata, history, and private recovery snapshots. Legacy database configuration is exported through a verified migration.

**Note:** Compose deployments have validation requirements. Image builds and unresolved local file dependencies are not supported through the new-project workflow. See the [feature documentation](Documentation/Developer/Features.md) for the supported configurations.

### Live logs and interactive terminal

Investigate running containers without opening a separate SSH session.

**Live logs**

- View recent container output.
- Follow new log entries in real time.
- Inspect standard output and error messages.
- Switch between containers within a project.
- Search the displayed logs with highlighted matches.
- Confirm an export of the current log buffer to `BASE/PROJECT_NAME/logs` on the Docker host, with the latest export time visible below the output.

**Interactive terminal**

- Open a shell inside a running container.
- Execute commands directly from your browser.
- Use an interactive, resizable terminal.
- Reconnect when a terminal session ends.

Terminal availability depends on the selected container having `/bin/sh`. Sessions are closed when you leave the terminal view.

### Container inspection

Explore the details of a container through its **Details** panel.

Available information includes:

- Container image and service identity.
- Current state and health.
- CPU, memory, network activity, and uptime.
- Published and exposed ports.
- Mounted volumes and bind mounts.
- Connected Docker networks.
- Environment variable names.

Environment variable values are hidden by default and can be explicitly revealed when needed.

### Image updates and container recreation

**Pull** downloads the images referenced by the current Compose files without replacing containers. Standalone containers and Compose projects whose files cannot be located use their runtime image references.

**Recreate** applies current saved configuration and locally cached images without pulling or building. It deliberately replaces containers even when the image has not changed. To update manually, use **Pull → Recreate** and review the confirmation.

In a Compose container's details, Recreate targets that service and all its replicas, leaving dependencies and initializers untouched. At project level it targets the whole project.

Supported Compose projects expose inline environment editing and a collapsed **Compose and environment files** editor. **Save** validates and writes files; use Recreate to apply the changes. Restart only restarts existing containers.

The assessed workflow preserves supported configuration, verifies resulting containers, and attempts rollback after ordinary replacement failures. If Compose files are missing, the supported Engine reconstruction path remains available. URL source synchronization is a separate, explicitly reviewed operation.

Some configurations cannot be safely reconstructed and will be rejected rather than silently modified.

> **Important:** Container rollback does not reverse writes or database migrations made to persistent application data. Back up important workloads before updating applications that may modify their data.

### Automatic project updates

Keep supported workloads updated through an optional per-project schedule.

Inside a project's **Details** panel:

1. Find **Automatic image updates**.
2. Enable automatic updates for the project.
3. Select the daily check time.
4. Save the configuration.

Yard checks for changed images and uses its supported update, verification, and rollback workflow when replacements are needed.

Automatic project updates are disabled by default and require an eligible, healthy running workload.

Schedules follow the server's configured timezone.

### Operation history and recovery

Track maintenance operations from the same interface used to manage your containers.

Each project includes a contextual **Operations** history, covering recent actions and their results.

Yard records operation progress, completed outcomes, failures, rollback attempts, and situations requiring manual recovery.

Long-running management jobs are persisted independently of the browser session, so closing or refreshing the page does not erase their recorded state.

When an interrupted operation requires host inspection, Yard can retain the affected resources under recovery protection until that situation is reviewed.

### Yard self-updates

Yard can also maintain its own installation.

From **Settings**, view build/update status and select **Check now** to check and install immediately, even when automatic updates are disabled.

Enable automatic updates and choose the daily check time in Yard's own project **Details** drawer, using the same schedule controls as other projects. The initial time is 03:00 in the server timezone; the former interval timer no longer runs.

Self-updates use the published stable `latest` image from GitHub Container Registry.

The update process uses an independent Docker worker to apply supported current host configuration and verify health, with runtime and SQLite recovery after ordinary replacement failures. The runtime-based self-update path remains available when host files cannot be located. See [Self-update](Documentation/Developer/Self-Update.md) for pinned image references and recovery limits.

Automatic self-updates are disabled by default.

## Getting started

### Requirements

Yard is designed to run on a Linux host with:

- Docker Engine
- Docker Compose plugin
- `linux/amd64` or `linux/arm64` architecture
- Access to the local Docker socket

Raspberry Pi 5 is the primary target platform.

### Installation

Yard is distributed as a Docker image through GitHub Container Registry.

Create a directory for the application and copy the repository's [compose.yaml](compose.yaml) into it.

The supplied Compose configuration uses the stable image:

`ghcr.io/mapherez/nox-yard:latest`

From that directory, pull the published image:

```bash
docker compose pull
```

This downloads the image without replacing existing containers.

Then start the application:

```bash
docker compose up -d
```

This creates or starts the Yard container in the background, using the supplied Compose configuration. Its persistent application data is stored in `./data`.

Open the dashboard in your browser:

**http://YOUR_HOST_IP:8095**

On your first visit, Yard guides you through creating the administrator account.

A password of at least 12 characters is required.

Once authenticated, your existing Docker projects and containers appear automatically.

> The default host port is `8095`. It can be changed using the optional `.env` configuration.

### Deploy your first managed project

After signing in:

1. Open **Settings** and configure the **Projects directory**. This must be an existing absolute directory on the Docker host.
2. Return to **Projects** and select **New Project**.
3. Provide a Compose file using a URL, pasted YAML, or an upload.
4. Fill in the required environment variables or environment files.
5. Select **Validate and preview**.
6. Review the proposed deployment and confirm.

Yard stores the managed Compose files under the configured host directory and displays the new project on the dashboard.

You can then use its detail panel to manage containers, inspect logs, check operation history, and configure automatic updates.

## Configuration

Yard is configured through the supplied Docker Compose file and optional environment variables.

The repository includes an [.env.example](.env.example) with the available deployment settings.

Common options include:

| Variable | Default | Purpose |
| --- | --- | --- |
| `NOX_PORT` | `8095` | HTTP port exposed on the host. |
| `NOX_BIND_ADDRESS` | `0.0.0.0` | Host interface used for the published port. |
| `NOX_PUBLIC_URL` | Empty | Browser-facing origin when using a reverse proxy. |
| `NOX_TIMEZONE` | `Etc/UTC` | Server timezone used by project schedules. |

When using an HTTPS reverse proxy, configure `NOX_PUBLIC_URL` with the exact public origin.

For additional configuration, maintenance, development, and recovery information, see the [Development Guide](Documentation/Developer/Development.md).

## Security and data

Yard is intended for trusted personal networks, such as a home LAN or private VPN.

Access to the Docker socket (`/var/run/docker.sock`) effectively grants control over the Docker host. Protect access to the application accordingly.

Yard provides a local administrator account, password-based authentication, protected browser sessions, and confirmation workflows for destructive operations.

**Important considerations:**

- Do not expose the dashboard directly to an untrusted network.
- Complete administrator setup immediately after the first deployment.
- Keep the application's persistent `./data` directory private and backed up.
- Back up managed Compose source files and application volumes or bind-mounted data separately.
- Review update and removal previews before confirmation.
- Docker volumes are retained by default during removal unless eligible volume deletion is explicitly selected.

The application does not require a third-party account for its standard browser management experience.

## Integrations

### MCP

Yard includes an embedded Model Context Protocol (MCP) endpoint for integration with compatible clients and automation workflows.

```text
http://YOUR_HOST_IP:8095/mcp
```

The MCP interface provides tools for inspecting Docker workloads, performing supported operations, retrieving job information, and interacting with Yard's management capabilities.

The endpoint is always enabled and currently does **not require authentication**. It must remain restricted to a trusted private network.

See the [MCP documentation](Documentation/Developer/MCP.md) for available tools, behaviors, and limitations.

### Control API

Yard also offers an optional HTTP Control API for external applications and automation tools.

Unlike the embedded MCP endpoint, this API uses independent Bearer-token authentication.

The API exposes supported inventory, inspection, container lifecycle, and image-pull operations.

It is disabled by default and can be enabled through deployment configuration.

See the [Control API documentation](Documentation/Developer/Control-API.md) for setup instructions and endpoint contracts.

## Technology

Yard combines a Go backend with a modern React interface.

| Component | Technology |
| --- | --- |
| Backend | Go |
| Frontend | React, TypeScript, Vite |
| Database | SQLite |
| Container integration | Docker Engine API |
| Deployment | Docker Compose |
| Distribution | GitHub Container Registry |
| Architectures | Linux ARM64 and AMD64 |

The application runs as a single production service, serving both the web interface and backend API.

## Documentation

Detailed implementation and maintenance documentation lives in [`Documentation/Developer/`](Documentation/Developer/README.md).

Useful references:

- [Features](Documentation/Developer/Features.md) — Detailed application capabilities and current behavior.
- [Architecture](Documentation/Developer/Architecture.md) — Application architecture and Docker integration.
- [Managed Updates](Documentation/Developer/Managed-Updates.md) — Managed Compose image updates, source synchronization, and rollback.
- [External Updates](Documentation/Developer/External-Updates.md) — Supported external container recreation and update workflows.
- [Project Schedules](Documentation/Developer/Project-Schedules.md) — Automatic image updates and scheduling behavior.
- [Development Guide](Documentation/Developer/Development.md) — Local development, configuration, maintenance, and recovery.
- [Releases](Documentation/Developer/Releases.md) — Release process and published image channels.

## Project status

Yard is under active development.

Its functionality includes local Docker discovery and management, managed Compose deployments, container inspection, logs, terminal access, image maintenance, operation history, and optional scheduled updates.

Support for certain Docker configurations is intentionally restricted where an operation cannot be safely assessed or reconstructed.

For implementation progress, outstanding validation work, and known limitations, refer to the [project progress documentation](Documentation/Developer/Progress.md).
