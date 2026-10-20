
# Yard

**Your Docker homelab, in view.**

Yard is a self-hosted dashboard for managing Docker containers and Compose projects. See what's running, find a failing service, deploy a stack, and manage updates from one place.

Part of the NoX environment, Yard also works independently with its own local account. No external account or service is required.

**Self-hosted · Docker-native · Source-available · Linux AMD64 & ARM64**

[Quickstart](Documentation/User/Quickstart.md) · [User guide](Documentation/User/Guide.md) · [Configuration](Documentation/User/Configuration.md) · [What's new](CHANGELOG.md) · [Releases](https://github.com/mapherez/nox-yard/releases)

## One place for everyday Docker work

| | What you can do |
| --- | --- |
| **See your homelab** | Discover existing Compose projects and standalone containers automatically. Check status, health, CPU, memory, and uptime. |
| **Find things quickly** | Switch between grid and compact table views, search projects and containers, and drag items into your preferred order. |
| **Manage workloads** | Start, stop, restart, pull images, and recreate supported projects and containers, with previews and confirmations. |
| **Deploy Compose stacks** | Supply a public HTTPS URL, paste YAML, or upload a file. Configure variables and review the deployment before it runs. |
| **Investigate problems** | Inspect ports, mounts, networks, and environment variables. Search live logs, export them, or open a container terminal. |
| **Stay up to date** | Choose optional daily image updates per project, track operation history, and update Yard itself from Settings. |

Yard manages **one local Docker host**. Existing workloads appear without importing or moving them; supported Compose projects can be adopted for managed operations.

## Get started

You need a **Linux host with Docker Engine and the Compose plugin**, on `linux/amd64` or `linux/arm64`. Yard ships as a prebuilt container image.

Save [compose.yaml](compose.yaml) and [.env.example](.env.example) as `compose.yaml` and `.env` in your installation directory. For LAN access, set `NOX_BIND_ADDRESS=0.0.0.0` in `.env`; adjust `NOX_PORT` if needed.

```sh
docker compose up -d
```

Open **http://YOUR_SERVER_IP:8095**, create your administrator account, and your existing Docker workloads appear automatically. The unchanged loopback binding is available at **http://127.0.0.1:8095** on the host.

**[Follow the Quickstart →](Documentation/User/Quickstart.md)** for copy-and-paste installation commands and your first deployment.

## Updates that you control

Use **Pull → Recreate** to update a workload manually. Pull downloads images; Recreate applies saved configuration and cached images. Restart only restarts existing containers.

Automatic project updates and Yard self-updates are **opt-in**. Schedules use your configured server timezone. Yard's built-in updater verifies the official image signature before installation; release notes also include instructions for manual verification.

See the [user guide](Documentation/User/Guide.md#updates-and-maintenance) for update controls and backup guidance.

## Optional integrations

Connect compatible MCP clients through Yard's separate MCP endpoint, or use the HTTP Control API for automation. Both are **disabled by default**.

Enable them in **Settings → MCP & access**, generate a Yard token, and choose inspection, write, and sensitive-read permissions. Yard and MCP have independent host ports and bindings.

- [MCP setup and tool reference](Documentation/Developer/MCP.md)
- [Control API reference](Documentation/Developer/Control-API.md)

NoX Gateway integration is planned; it is not required to use Yard or its direct MCP endpoint.

## Your host, your data

Yard is intended for trusted homelab networks and private VPNs. Its Docker socket access grants control over the host: protect dashboard access accordingly.

Application state lives in `./data`. Back up that directory, your Compose/environment files, and workload data separately. Container recovery cannot undo changes an application makes to persistent data.

## Documentation

| Looking for… | Start here |
| --- | --- |
| Installation and first login | [Quickstart](Documentation/User/Quickstart.md) |
| Deployments, logs, terminal, and updates | [User guide](Documentation/User/Guide.md) |
| Ports, timezone, reverse proxy, and integration access | [Configuration](Documentation/User/Configuration.md) |
| Changes between versions | [Changelog](CHANGELOG.md) and [GitHub Releases](https://github.com/mapherez/nox-yard/releases) |
| Supported operations and limitations | [Feature reference](Documentation/Developer/Features.md) |
| Development, architecture, and release verification | [Developer documentation](Documentation/Developer/README.md) |

## License & Attribution

This project is licensed under the **Creative Commons Attribution-NonCommercial-ShareAlike 4.0 International (CC BY-NC-SA 4.0)** license.

### How to Credit This Project

If you use, fork, or modify this code, you **must** provide attribution by linking back to my GitHub profile. Please include the following credit notice prominently in your repository or project documentation:

> **Based on [nox-yard](https://github.com/mapherez/nox-yard) by [mapherez](https://github.com/mapherez)**
> This project includes code from [nox-yard](https://github.com/mapherez/nox-yard), used under the CC BY-NC-SA 4.0 license.

---

*For more details on what you can and cannot do, please refer to the [LICENSE](LICENSE) file in this repository.*

## Disclaimer of Warranty

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
