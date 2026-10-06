# Embedded MCP

NoX Yard exposes an always-on Streamable HTTP MCP endpoint at `http://<host>:8095/mcp` in the default Compose installation. It uses the existing Go process and internal port 8080. Development clients can connect directly to `http://127.0.0.1:8080/mcp`.

The endpoint is intended for the existing private homelab LAN. It requires no credentials, cookies, Bearer key, OAuth or pairing, independently of browser administrator setup and `NOX_YARD_API_ENABLED`. Configure an HTTP-capable MCP client with the endpoint URL and no authorization headers. When a request includes Origin, it must match `NOX_PUBLIC_URL`, or the direct request origin when that setting is empty. There is no additional CORS policy.

Browser authentication and `/api/*`, the opt-in Bearer Control API `/v1/*`, and `/healthz` retain their existing contracts. Unknown `/mcp/*` paths return JSON errors instead of the frontend application. MCP exposes neither a stdio service nor the NoX desktop bridge.

## Shared implementation

`internal/application` is a lightweight orchestration facade. HTTP and MCP share the same inventory, lifecycle, managed Compose, self-update and storage instances. MCP does not call `/v1` over HTTP. Docker operations, source validation, locks, protected-target rules, confirmation fingerprints, jobs and persistence remain implemented by the existing managers.

The executable creates one `inventory.Notifier`. The facade, browser SSE and Compose manager use that same instance. Engine action/removal pending states are owned by the facade; Compose jobs retain their manager-owned pending states through completion. No operation emits duplicate Begin/finalization events merely because it originated through MCP.

Facade operations accept the adapter's context, introduce no deadlines and do not replace it with Background/WithoutCancel. Existing synchronous HTTP deadlines remain in the HTTP adapters; existing manager deadlines and asynchronous job contexts remain unchanged. Methods without a context API check cancellation before invoking their manager; an already accepted asynchronous operation is not retroactively canceled.

## Tools

The `yard_*` names remain the canonical MCP identifiers. Each tool also exposes `_meta.cli` through `tools/list` as additional metadata for human-facing clients; generic MCP clients may ignore it. The CLI paths below are local to the application. The future CLI is responsible for adding the `yard` prefix (for example, `container logs` becomes `yard container logs`); that prefix is not part of the metadata.

IDs must be copied from inventory: containers use full 64-character lowercase hexadecimal IDs; Engine projects use `container:<id>` or `compose:<name>`. Managed Compose operations take the saved project name instead. Engine and stored Compose operations remain distinct, matching existing Yard behavior.

| MCP tool | CLI (`_meta.cli`) | Inputs and behavior |
| --- | --- | --- |
| `yard_health` | `health` | No inputs. Storage readiness and version. |
| `yard_status` | `status` | No inputs. Docker availability and inventory counts. |
| `yard_projects` | `project list` | No inputs. Projects with nested containers and pending states. |
| `yard_metrics` | `metrics` | No inputs. Cached metrics. |
| `yard_container_inspect` | `container inspect` | `id`. Ports, mounts, networks and environment names, without values. |
| `yard_container_environment` | `container env` | `id`. Explicitly returns potentially sensitive environment values. |
| `yard_container_logs` | `container logs` | `id`. Last 20 decoded records, without following the stream. |
| `yard_container_action` | `container action` | `id`, `action`: `start`, `stop`, `restart`. Shared Engine lifecycle. |
| `yard_project_action` | `project action` | `id`, `action`: `start`, `stop`, `restart`. Shared Engine lifecycle. |
| `yard_container_pull` | `container pull` | `id`. Pull configured images without recreating containers. |
| `yard_project_pull` | `project pull` | `id`. Pull configured images without recreating containers. |
| `yard_container_remove_preview` | `container remove preview` | `id`. Current removal plan and fingerprint. |
| `yard_project_remove_preview` | `project remove preview` | `id`. Current removal plan and fingerprint. |
| `yard_container_remove` | `container remove` | `id`, `confirm:true`, `fingerprint`. Revalidate and execute existing removal rules. |
| `yard_project_remove` | `project remove` | `id`, `confirm:true`, `fingerprint`. Revalidate and execute existing removal rules. |
| `yard_compose_source` | `compose source` | Existing source fields: `kind`, optional `url`, `filename`, `yaml`. Inspect source and required variables/env files. |
| `yard_compose_preview` | `compose preview` | Existing managed request: `name`, `source`, `variables`, `envFiles`, `mode`, optional `fingerprint`. Modes: `new`, `copy`, `sync`, `adopt`. Return the preview/fingerprint. |
| `yard_compose_submit` | `compose submit` | Existing managed request: `name`, `source`, `variables`, `envFiles`, `mode`, optional `fingerprint`. Modes: `new`, `copy`, `sync`, `adopt`. Submission requires the current preview fingerprint and returns a job. |
| `yard_compose_operation` | `compose operation` | `name`, `operation`, `removeVolumes`. Operations: `start`, `stop`, `restart`, `pull`, `update`, `remove`. Returns a job. |
| `yard_compose_job` | `compose job` | `id`. Persisted job status. |
| `yard_projects_settings_get` | `settings projects get` | No inputs. Read the configured host projects base directory. |
| `yard_projects_settings_set` | `settings projects set` | `projectsBase`. Existing host-directory checks apply. |
| `yard_self_update_status` | `update status` | No inputs. Current update settings/state. |
| `yard_self_update_settings` | `update settings` | `automatic`, `intervalMinutes`. Enabling automatic updates can trigger a check/update/restart. |
| `yard_self_update_check_and_update` | `update now` | No inputs. Queue a manual check which can install an update and restart Yard **even when automatic updates are disabled**. Observe progress with `yard_self_update_status`. |

Required fields and output shapes are available through `tools/list`. Normal results use structured content plus a JSON text representation. Errors use `IsError` and a code/message envelope; operation errors retain counters and partial results under `details.result`. Accepted jobs and queued restart helpers are not reported as completed operations.

All tools explicitly declare ReadOnly, Destructive and Idempotent through NoX MCP. Reads/previews, including explicit environment reads, are `(true,false,true)`. Engine actions, pulls, removals, Compose submissions/operations and self-update mutations are conservatively `(false,true,false)`. Setting the projects directory is `(false,true,true)`. Multi-mode tool annotations cover all accepted modes.

## Limits and continuous features

The Yard configures the NoX MCP runtime with a global **60-second timeout**, 32 concurrent requests and an 8 MiB payload limit. Existing Compose source limits still apply. Neither the server's 130-second WriteTimeout nor the NoX MCP library's default timeout is changed.

Compose and self-update retain their existing asynchronous execution; query their job/status tools rather than waiting for completion. Engine pulls remain synchronous and can exceed the MCP timeout. A timeout or disconnect does not establish success or failure of already issued work; reconcile current state before retrying. There are no automatic mutation retries or newly introduced jobs.

Container log snapshots share Docker opening and stdout/stderr/TTY decoding with browser SSE. The browser retains its 200-record initial tail and continuous stream. Long lines retain the existing 16 KiB segmentation, and snapshots keep at most 20 decoded records.

Interactive terminal, continuous log/event subscriptions, administrator setup/login/logout and password recovery remain in their existing interfaces. MCP does not provide an alternate command-execution implementation.

## Updating the library

Use normal Go module resolution; do not manually select commits or add a local checkout replacement:

```sh
go get github.com/mapherez/nox-mcp@latest
go mod tidy
```

Commit the resolved `go.mod`/`go.sum` updates and run the existing repository checks, including MCP integration tests and multi-architecture builds. Go may resolve a pseudo-version when no release tag exists; that version is generated by Go and does not require manual SHA management.

## Validation

MCP tests use the official Go SDK over real HTTP with temporary SQLite and shared fake dependencies. They cover discovery, schemas/annotations, anonymous MCP versus existing HTTP authentication, origin validation, explicit environment reveal, partial failures, protected/stale removal, manual self-update, shared browser SSE/pending states and asynchronous Compose notification lifetimes. Facade tests cover context propagation, cancellation before dispatch and blocked log-reader cancellation. Existing manager tests continue to cover Docker transport, concurrency and operation rules.

The isolated Docker smoke test creates disposable containers, Compose projects and data volumes, publishes temporary loopback ports, and cleans up its own fixtures. It exercises real lifecycle operations, inspection/environment/logs, pulls, removal fingerprints, browser SSE, Compose jobs, persisted settings/session and queued Yard self-restart. An optional ARM64 image also checks runtime readiness and MCP discovery under emulation:

```sh
docker buildx build --platform linux/amd64 --load -t nox-yard:mcp-smoke-amd64 .
docker buildx build --platform linux/arm64 --load -t nox-yard:mcp-smoke-arm64 .
python scripts/smoke-mcp.py --image nox-yard:mcp-smoke-amd64 --arm64-image nox-yard:mcp-smoke-arm64
```

The script accepts `--docker-command`, `--docker-config` and `--docker-host` for Docker Desktop installations. It does not install a remote Yard update or publish/deploy the built images.
