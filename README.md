# NoX Yard

Your Docker homelab, in view.

NoX Yard is a browser dashboard for managing the Docker containers and Compose projects on your server. See what is running, inspect logs, change configuration and apply updates without switching between Docker commands.

Existing workloads appear automatically. You do not need to move or redeploy them to use Yard.

- Browse projects and containers, with health, resource usage and uptime.
- Start, stop and restart workloads.
- Read logs and open available container terminals.
- Deploy projects from a Compose file, pasted YAML or a supported URL.
- Edit Compose and environment files, preview changes and apply them.
- Pull images, recreate services and inspect operation history.
- Choose manual updates or opt-in update schedules.

Yard also installs participating NoX Suite products and reports their deployment state to Backend. It remains useful for ordinary Docker workloads; Backend and Gateway are not required for everyday Docker management.

## What you need

- A Linux Docker host with Docker Compose, on Intel/AMD or ARM64.
- [NoX Auth](https://github.com/mapherez/nox-auth) running on the same host, with your Google account allowed to sign in.
- Browser access to your server through your trusted network or VPN.

Yard controls Docker on that host. Keep access private. Its own browser address can use HTTP; you do not need a separate Google login configuration or certificate for Yard.

## Help

[Quickstart](Documentation/User/Quickstart.md) · [User guide](Documentation/User/Guide.md) · [Configuration](Documentation/User/Configuration.md)

For integrations and contributors: [MCP tools](Documentation/Developer/MCP.md) · [Suite integration](Documentation/Developer/Suite-Integration.md) · [Technical reference](Documentation/Developer/Architecture.md)

## Usage

### Install

On your Docker server, download the deployment files into a new folder:

```sh
mkdir nox-yard
cd nox-yard
curl -fsSLo compose.yaml https://raw.githubusercontent.com/mapherez/nox-yard/master/compose.yaml
curl -fsSLo .env https://raw.githubusercontent.com/mapherez/nox-yard/master/.env.example
```

Open `.env` in a text editor. For browser access from your private network, configure:

| Setting | What to enter |
| --- | --- |
| `NOX_BIND_ADDRESS` | `0.0.0.0` for LAN/VPN access, or keep `127.0.0.1` for access from the server itself. |
| `NOX_PORT` | An unused host port; the supplied default is **8095**. |
| `NOX_PUBLIC_URL` | The exact Yard address you will open in your browser, including its port when needed. |
| `NOX_AUTH_PUBLIC_URL` | The HTTPS address of your running NoX Auth service. |
| `NOX_TIMEZONE` | Your server's IANA timezone for scheduled updates. |

Google credentials and allowed accounts stay in Auth. Other settings can keep their defaults for a new installation using Auth's default volume names.

Start Yard:

```sh
docker compose pull
docker compose up -d
```

Open your configured Yard address and choose **NoX Login**. Sign in with your allowed Google account. Your existing projects and containers appear automatically. The `auth-setup` container exiting successfully is normal.

### Manage or deploy a project

Select a project to view its details, logs and available actions. **Pull** downloads an image; **Recreate** applies it to the running workload. Restarting alone does not apply a newly downloaded image.

To deploy a new project, set **Projects directory** in Settings to an existing directory on the Docker host. Choose **New Project**, provide its Compose definition, fill in any required environment values, then select **Validate and preview** before confirming.

To connect Yard to NoX Suite, add its address under **Service connections** in Backend, verify it and confirm the connection. Optional direct MCP and HTTP Control API access have separate settings under **Settings → MCP & access**.

### Update and preserve data

Use Yard's update controls, or run these commands from its installation folder:

```sh
docker compose pull
docker compose up -d
```

Keep the `data/` directory. Back up host Compose/environment files and each application's data separately. Rolling back a container does not undo writes that application made to its data. See the [user guide](Documentation/User/Guide.md) for details.

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
