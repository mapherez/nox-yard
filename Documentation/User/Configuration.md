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
| `NOX_PUBLIC_URL` | Required | Exact Yard browser-facing HTTP(S) origin; used for the approved NoX Login callback and Origin/cookie policy. |
| `NOX_AUTH_PUBLIC_URL` | Required | HTTPS origin of the running NoX Auth service; no Google secrets are configured in Yard. |
| `NOX_SUITE_LOCAL_VOLUME_NAME` | `nox-suite-local` | Shared local transport volume; must match Auth. |
| `NOX_AUTH_ADMIN_VOLUME_NAME` | `nox-auth-admin` | Existing administrative volume, mounted only by the one-shot setup service. |

MCP and the dashboard can use different host interfaces and ports. Publishing the MCP port does not enable MCP.

## NoX Login setup

Update the Compose as well as the image when adopting NoX Login. The one-shot `auth-setup` component registers the configured callback automatically and exits; the normal Yard service has no Auth administration mount. Keep the existing data directory. See [NoX Login deployment and rollback](../Developer/NoX-Login.md).

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

MCP can explicitly allow anonymous access with the selected permissions. The Control API always requires the Yard token. The sidebar's MCP indicator reports the local server's operational state, not client connections or Gateway registration. Suite MCP is a separate, same-host path authenticated by Backend delegations; it is not enabled or disabled by this direct MCP switch. See [Suite setup](../Developer/Suite-Integration.md).

### Apply network changes from Settings

Saving a binding or port change marks it **pending**. Select **Apply now** to recreate Yard with the current image and check its health and actual bindings. Automatic application is optional. This requires a supported Compose installation with accessible source files.

If this changes the browser-facing Yard origin, update `NOX_PUBLIC_URL` and recreate the Auth setup/service with Compose so the approved callback matches. A reverse proxy can keep the canonical browser URL unchanged while the upstream port changes. Reconnect at the configured canonical address. This operation changes networking; it does not upgrade Yard.

For tool and endpoint contracts, see [MCP](../Developer/MCP.md) and [Control API](../Developer/Control-API.md).

## NoX Suite and CLI menus

Run Auth, Backend, Yard and Gateway on the same Docker host with the matching
local runtime volume. In Backend, save Yard's native HTTP/HTTPS origin, **Verify**,
then **Connect**. No certificate copying, per-app Google secrets or manual pairing
files are required. Private HTTP product origins are supported.

The local `/mcp/suite` endpoint exposes Yard's existing 38 tools with Backend's
scope checks. Gateway is the client entry point; the CLI uses its private LAN/VPN
address and the user's Backend API key. The direct port 8096 and Yard token remain
independent. Do not send a personal Backend key directly to `/mcp/suite`.

After updating Yard, refresh the CLI catalog. Human menu descriptors are delivered
only through negotiated NoX discovery. Project selectors, read-only previews,
fingerprints and local confirmations use the existing operations; they do not
change tool arguments, grant permissions or replay writes. See [MCP](../Developer/MCP.md#human-menus-through-nox-discovery)
for supported inputs and [Suite integration](../Developer/Suite-Integration.md)
for deployment recovery. This SDK/menu update adds no Compose or environment setting.
