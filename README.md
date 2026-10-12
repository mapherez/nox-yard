
# NoX Yard

**Your Docker homelab, in view.**

NoX Yard is a self-hosted web application for managing Docker containers and Compose projects from a single, focused dashboard.

Built for personal homelabs, it brings container monitoring, project deployment, lifecycle controls, live logs, interactive terminals, image updates, and automated maintenance into one interface — without requiring you to manage everything through the command line.

Designed for Linux hosts, with Raspberry Pi and x86-64 support.

**Self-hosted · Docker-native · Lightweight · Open source**

---

## What is NoX Yard?

Running a homelab often means managing multiple Docker applications, checking their health, inspecting logs, updating images, and maintaining Compose configurations.

NoX Yard brings these everyday operations together.

It connects directly to your local Docker Engine, automatically discovers existing workloads, and presents them through a responsive web interface.

You can monitor an existing installation, deploy a new Compose project, investigate a failing container, or manage updates without leaving the dashboard.

NoX Yard focuses on **one Docker host at a time**, keeping the experience straightforward and local to your infrastructure.

## Features

### Docker dashboard

Get an overview of the Docker workloads running on your host.

- Automatically discover Compose projects and standalone containers.
- View container states, health information, and project status.
- Monitor CPU usage, memory consumption, and uptime.
- See containers grouped by their Compose project.
- Receive live inventory updates when Docker changes, including changes made outside NoX Yard.
- Access individual projects and containers through contextual detail panels.

The dashboard updates automatically, with manual refresh available whenever needed.

### Project and container management

Manage existing workloads directly from the interface.

- Start, stop, and restart projects or individual containers.
- Pull container images from their configured registries.
- Update images or recreate containers for supported workloads.
- Remove containers and projects through reviewed confirmation dialogs.
- Inspect detailed container configurations and runtime information.

NoX Yard distinguishes between three types of workloads:

| Type | Description |
| --- | --- |
| **Managed Compose** | Projects deployed or adopted by NoX Yard, with saved Compose definitions and additional management capabilities. |
| **External Compose** | Existing Compose projects automatically discovered from Docker, without requiring import. |
| **Standalone Container** | Individual Docker containers that are not part of a Compose project. |

Existing workloads can be monitored and controlled immediately. Supported external Compose projects can also be adopted for managed operations.

### Deploy Compose projects

Deploy new Docker applications directly from NoX Yard.

Select **New Project** from the dashboard and provide a Docker Compose definition using one of three methods:

- **HTTPS URL** — Load a Compose definition from a public HTTPS address.
- **Paste YAML** — Paste a Compose configuration directly into the application.
- **Upload file** — Import a local Compose YAML file.

NoX Yard reads the definition, identifies required variables and environment files, and lets you configure their values before deployment.

Before anything is created, a validation and preview step displays the proposed services, images, ports, mounts, networks, and relevant changes.

Once confirmed, NoX Yard prepares the project files on the Docker host, pulls the required images, and deploys the stack.

Managed projects retain their Compose source and configuration for subsequent operations.

**Note:** Compose deployments have validation requirements. Image builds and unresolved local file dependencies are not supported through the new-project workflow. See the [feature documentation](Documentation/Developer/Features.md) for the supported configurations.

### Live logs and interactive terminal

Investigate running containers without opening a separate SSH session.

**Live logs**

- View recent container output.
- Follow new log entries in real time.
- Inspect standard output and error messages.
- Switch between containers within a project.

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

NoX Yard provides more than a basic image pull.

For supported projects and containers, you can request an image update or recreation with configuration checks and a review of the proposed operation.

**Update images** pulls the configured images and compares their actual Docker image identities with the deployed versions.

If nothing has changed, the existing containers are preserved.

When replacements are required, NoX Yard uses an assessed workflow to preserve supported runtime configuration, verify the resulting containers, and attempt rollback if the operation fails.

**Recreate containers** replaces supported containers using their existing deployed images, without requiring a new image pull.

For managed Compose projects, image updates use the saved Compose configuration. Source synchronization is a separate, explicitly reviewed operation.

Some configurations cannot be safely reconstructed and will be rejected rather than silently modified.

> **Important:** Container rollback does not reverse writes or database migrations made to persistent application data. Back up important workloads before updating applications that may modify their data.

### Automatic project updates

Keep supported workloads updated through an optional per-project schedule.

Inside a project's **Details** panel:

1. Find **Automatic image updates**.
2. Enable automatic updates for the project.
3. Select the daily check time.
4. Save the configuration.

NoX Yard checks for changed images and uses its supported update, verification, and rollback workflow when replacements are needed.

Automatic project updates are disabled by default and require an eligible, healthy running workload.

Schedules follow the server's configured timezone.

### Operation history and recovery

Track maintenance operations from the same interface used to manage your containers.

Each project includes a contextual **Operations** history, covering recent actions and their results.

NoX Yard records operation progress, completed outcomes, failures, rollback attempts, and situations requiring manual recovery.

Long-running management jobs are persisted independently of the browser session, so closing or refreshing the page does not erase their recorded state.

When an interrupted operation requires host inspection, NoX Yard can retain the affected resources under recovery protection until that situation is reviewed.

### NoX Yard self-updates

NoX Yard can also maintain its own installation.

From **Settings**, you can:

- View application build and update status.
- Check for a new stable release manually.
- Enable or disable automatic updates.
- Configure the automatic update-check interval.

Self-updates use the published stable `latest` image from GitHub Container Registry.

The update process uses a temporary Docker worker to replace the application and verify its health, with an attempt to restore the previous installation if replacement fails.

Automatic self-updates are disabled by default.

## Getting started

### Requirements

NoX Yard is designed to run on a Linux host with:

- Docker Engine
- Docker Compose plugin
- `linux/amd64` or `linux/arm64` architecture
- Access to the local Docker socket

Raspberry Pi 5 is the primary target platform.

### Installation

NoX Yard is distributed as a Docker image through GitHub Container Registry.

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

This creates or starts the NoX Yard container in the background, using the supplied Compose configuration. Its persistent application data is stored in `./data`.

Open the dashboard in your browser:

**http://YOUR_HOST_IP:8095**

On your first visit, NoX Yard guides you through creating the administrator account.

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

NoX Yard stores the managed Compose files under the configured host directory and displays the new project on the dashboard.

You can then use its detail panel to manage containers, inspect logs, check operation history, and configure automatic updates.

## Configuration

NoX Yard is configured through the supplied Docker Compose file and optional environment variables.

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

NoX Yard is intended for trusted personal networks, such as a home LAN or private VPN.

Access to the Docker socket (`/var/run/docker.sock`) effectively grants control over the Docker host. Protect access to the application accordingly.

NoX Yard provides a local administrator account, password-based authentication, protected browser sessions, and confirmation workflows for destructive operations.

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

NoX Yard includes an embedded Model Context Protocol (MCP) endpoint for integration with compatible clients and automation workflows.

```text
http://YOUR_HOST_IP:8095/mcp
```

The MCP interface provides tools for inspecting Docker workloads, performing supported operations, retrieving job information, and interacting with NoX Yard's management capabilities.

The endpoint is always enabled and currently does **not require authentication**. It must remain restricted to a trusted private network.

See the [MCP documentation](Documentation/Developer/MCP.md) for available tools, behaviors, and limitations.

### Control API

NoX Yard also offers an optional HTTP Control API for external applications and automation tools.

Unlike the embedded MCP endpoint, this API uses independent Bearer-token authentication.

The API exposes supported inventory, inspection, container lifecycle, and image-pull operations.

It is disabled by default and can be enabled through deployment configuration.

See the [Control API documentation](Documentation/Developer/Control-API.md) for setup instructions and endpoint contracts.

## Technology

NoX Yard combines a Go backend with a modern React interface.

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

NoX Yard is under active development.

Its functionality includes local Docker discovery and management, managed Compose deployments, container inspection, logs, terminal access, image maintenance, operation history, and optional scheduled updates.

Support for certain Docker configurations is intentionally restricted where an operation cannot be safely assessed or reconstructed.

For implementation progress, outstanding validation work, and known limitations, refer to the [project progress documentation](Documentation/Developer/Progress.md).
