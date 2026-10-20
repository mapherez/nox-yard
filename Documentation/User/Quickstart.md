# Quickstart

Get from a Docker host to the Yard dashboard in a few minutes. This guide assumes Docker Engine and the Compose plugin are already installed on Linux (`amd64` or `arm64`).

## 1. Get the Compose configuration

Run on the Docker host in a new installation directory:

```sh
mkdir yard
cd yard
curl -fsSLo compose.yaml https://raw.githubusercontent.com/mapherez/nox-yard/master/compose.yaml
curl -fsSLo .env https://raw.githubusercontent.com/mapherez/nox-yard/master/.env.example
```

Alternatively, save [compose.yaml](../../compose.yaml) and [.env.example](../../.env.example) into that directory, naming the latter `.env`.

## 2. Choose your access settings

Edit `.env`. For dashboard access over your trusted LAN/VPN, set:

```dotenv
NOX_BIND_ADDRESS=0.0.0.0
NOX_PORT=8095
NOX_TIMEZONE=Europe/Lisbon
```

Choose your own timezone and an unused host port. Keep `NOX_BIND_ADDRESS=127.0.0.1` if you want local-only access. MCP stays disabled, with a separate loopback binding.

Using an HTTPS reverse proxy? Set `NOX_PUBLIC_URL` to the exact browser-facing origin. See [configuration](Configuration.md#reverse-proxy).

## 3. Start Yard

```sh
docker compose up -d
```

Compose pulls the published stable image and starts Yard. Application state is stored in `./data`.

Open **http://YOUR_SERVER_IP:8095** with the LAN binding, or **http://127.0.0.1:8095** locally. Use your chosen port if you changed it.

Create the administrator account with a password of at least 12 characters. Existing Compose projects and standalone containers appear automatically; no import is required.

## 4. Explore or deploy

Open an existing project to see **Details**, **Logs**, and **Terminal**. Switch between the dashboard's grid and table views, or search for a project or container.

To deploy a new project:

1. In **Settings**, set **Projects directory** to an existing absolute directory on the Docker host, such as `/srv/compose`.
2. Select **New Project** and provide a Compose definition by HTTPS URL, pasted YAML, or file upload.
3. Fill in the required variables and environment files.
4. Select **Validate and preview**, review the proposed services and resources, then confirm.

New projects are saved under `BASE/PROJECT_NAME/compose.yml`. The deployment workflow uses prebuilt images; image builds and unresolved local file dependencies are not supported.

## If Yard does not start

```sh
docker compose ps
docker compose logs --tail=100 nox-yard
```

If a published port is occupied, change it in `.env` and run `docker compose up -d` again. If inventory is unavailable, check that Docker is running and the supplied Docker socket mount is present.

Next: [everyday use](Guide.md) · [configuration](Configuration.md) · [release changes](../../CHANGELOG.md)
