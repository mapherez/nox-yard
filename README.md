# NoX Yard

Your Docker homelab, in view.

NoX Yard is a self-hosted dashboard for managing Docker containers and Compose projects. It brings running workloads, configuration, logs and updates into one interface, so you can see what is happening and act without switching between separate Docker commands.

Yard discovers existing workloads directly from Docker. You do not have to move or redeploy them just to see them in the dashboard.

## What you can do

| Capability | What it provides |
| --- | --- |
| View your host | Browse Compose projects and standalone containers with status, health, resource usage and uptime. |
| Find workloads | Search, switch between grid and table views, and arrange projects in your preferred order. |
| Manage containers | Start, stop, restart, pull images and recreate supported workloads. |
| Deploy projects | Paste Compose YAML, upload a file or supply a supported source URL, then review the deployment. |
| Edit configuration | Work with host Compose and environment files, review changes and apply them explicitly. |
| Investigate problems | Read and search logs, export a snapshot or open an available container terminal. |
| Manage updates | Choose manual updates or opt-in schedules, inspect operation history and update Yard itself. |

Pulling an image, recreating a container and restarting a service are separate actions. Yard shows previews and operation results, and preserves supported recovery state when an operation fails. Container recovery does not undo changes an application has written to persistent data.

## How it fits into NoX Suite

Yard manages one local Docker host. It remains useful for ordinary workloads that do not participate in NoX Suite.

For participating products, Yard reuses its deployment workflow to prepare the local communication channel, initiate registration with Backend and report lifecycle changes. Gateway and CLI discover the products through the shared contracts; Yard does not maintain a fixed list of supported NoX applications.

Dashboard sign-in uses **NoX Login** through Auth. Direct MCP and the HTTP Control API are optional interfaces with their own access settings. Native Docker management does not require Backend or Gateway.

## Documentation

- [Quickstart](Documentation/User/Quickstart.md)
- [Everyday use](Documentation/User/Guide.md)
- [Configuration and access](Documentation/User/Configuration.md)
- [NoX Login](Documentation/Developer/NoX-Login.md)
- [Suite integration](Documentation/Developer/Suite-Integration.md)
- [MCP tools](Documentation/Developer/MCP.md)
- [HTTP Control API](Documentation/Developer/Control-API.md)
- [Technical reference](Documentation/Developer/Architecture.md)
- [Developer documentation](Documentation/Developer/README.md)
- [Changelog](CHANGELOG.md)

## Usage

Run NoX Auth on the same Docker host. Save [compose.yaml](compose.yaml) and [.env.example](.env.example) in a deployment directory, naming the environment file `.env`.

Configure your browser-facing Yard address, Auth origin, host binding and any host ports you want to use. The [Quickstart](Documentation/User/Quickstart.md) explains those settings; no address from another installation is required.

Start Yard:

```sh
docker compose pull
docker compose up -d
```

Open your configured Yard address and choose **NoX Login**. Existing Compose projects and standalone containers appear automatically. To deploy a new project, configure the host projects directory in Settings, supply a Compose definition, and review the preview before confirming.

Use **Pull → Recreate** to apply an image update. Automatic updates are opt-in. Optional direct MCP and Control API access are configured under **Settings → MCP & access**.

Yard controls the Docker host, so keep it on trusted networks or private VPN access. Back up Yard's data, host Compose/environment files and application data separately. See the [user guide](Documentation/User/Guide.md) for update and recovery behavior.

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
