# Configuration

The supplied [Compose file](../../compose.yaml) and [.env.example](../../.env.example) configure the installation. Save the example as `.env` beside `compose.yaml` and run `docker compose up -d` after deployment-setting changes.

## Host settings

| Variable | Default | Purpose |
| --- | --- | --- |
| `NOX_BIND_ADDRESS` | `127.0.0.1` | Host binding for the dashboard and Control API. Use `0.0.0.0` for LAN/VPN access. |
| `NOX_PORT` | `8095` | Dashboard and Control API host port. |
| `NOX_MCP_BIND_ADDRESS` | `127.0.0.1` | Independent MCP host binding. |
| `NOX_MCP_PORT` | `8096` | Independent MCP host port; MCP itself starts disabled. |
| `NOX_TIMEZONE` | `Etc/UTC` | Server timezone used by daily update schedules; use an IANA name such as `Europe/Lisbon`. |
| `NOX_PUBLIC_URL` | Empty | Exact browser-facing origin when using a reverse proxy. |

MCP and the dashboard can use different host interfaces and ports. Publishing the MCP port does not enable MCP.

## Reverse proxy

Set `NOX_PUBLIC_URL` to the exact external origin, for example `https://yard.example.com`. An HTTPS origin enables Secure session cookies. A containerized proxy can share Yard's Docker network; otherwise configure a host binding reachable by the proxy.

## Projects directory

Set **Projects directory** in Settings to an existing absolute directory on the Docker host. New deployments use `BASE/PROJECT_NAME/compose.yml`, with log exports in `BASE/PROJECT_NAME/logs`. For projects outside that base, exports use a project-named directory under the configured base; standalone containers use their container name.

Existing Docker workloads are discovered regardless of this setting. Changing the directory does not move existing projects.

## MCP and Control API

Both integrations are disabled by default. In **Settings → MCP & access**:

1. Enable the interface you need.
2. Generate a Yard token and copy it when shown; it cannot be retrieved later.
3. Choose permissions. Inspection is the default; writes and sensitive reads such as environment values, file contents, and logs are separate opt-ins.

The default MCP endpoint is `http://127.0.0.1:8096/mcp`. Configure the client with `Authorization: Bearer <TOKEN>`, adjusting the address for your binding and port.

There is one active Yard token shared by MCP and the Control API. Generating a replacement immediately invalidates the previous token for both interfaces, as does revocation. Tokens are stored as hashes; the **History** modal shows the latest ten token events.

MCP can explicitly allow anonymous access with the selected permissions. The Control API always requires the Yard token. The sidebar's MCP indicator reports the local server's operational state, not client connections or Gateway registration. NoX Gateway integration is not implemented yet.

### Apply network changes from Settings

Saving a binding or port change marks it **pending**. Select **Apply now** to recreate Yard with the current image and check its health and actual bindings. Automatic application is optional. This requires a supported Compose installation with accessible source files.

Reconnect at the new address if the dashboard binding or port changes. This operation changes networking; it does not upgrade Yard.

For tool and endpoint contracts, see [MCP](../Developer/MCP.md) and [Control API](../Developer/Control-API.md).
