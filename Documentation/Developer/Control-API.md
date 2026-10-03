# Control API v1

The Control API is the stable HTTP interface for machine clients, including a future standalone NoX CLI. It uses the existing NoX Yard process and port (`http://<host>:8095` in the default installation). Clients communicate with Yard, never directly with Docker.

The React browser API remains under `/api/*`, with sessions, cookies, Origin checks, CSRF, SSE, and terminal WebSockets. It is not the public machine contract. `/v1/*` uses independent DTOs, Bearer authentication, and stable error codes. Both adapters call the same inventory and lifecycle implementations.

## Enable and authenticate

```dotenv
NOX_YARD_API_ENABLED=false
NOX_YARD_API_KEY=
NOX_YARD_VERSION=
```

Protected routes are disabled by default. Set `NOX_YARD_API_ENABLED=true` and configure an independent random key, then recreate the service. For example, generate a key with `openssl rand -hex 32`. Do not reuse the administrator password. An enabled API without a key fails HTTP startup; invalid boolean values also fail startup. Keys must contain no whitespace, commas, or control characters. A key alone does not enable the API.

Send exactly one credential in the header `Authorization: Bearer <API_KEY>`. Cookies, administrator credentials, query-string keys, and CSRF tokens do not authenticate machine requests. Valid Bearer requests do not require Origin or CSRF. No CORS policy is added. The key grants all protected v1 operations; rotate it by changing configuration and restarting Yard. The server retains only a SHA-256 digest and compares fixed-length digests in constant time. Keys are never returned or logged. Auxiliary password recovery and worker commands do not require machine API configuration.

Keep the existing trusted LAN/VPN or HTTPS reverse-proxy deployment boundary. This feature does not change network exposure. Health and info remain public even when protected routes are disabled.

## Endpoints

| Method and path | Authentication | Behavior |
| --- | --- | --- |
| `GET /v1/health` | Public | Service/storage readiness and version. |
| `GET /v1/info` | Public | Identity, version, activation, and capabilities. |
| `GET /v1/status` | Bearer | Docker availability and lightweight counts. |
| `GET /v1/projects` | Bearer | Projects with nested container summaries. |
| `GET /v1/containers/{id}` | Bearer | Container ports, mounts, and networks. |
| `POST /v1/containers/{id}/actions` | Bearer | Start, stop, or restart a container. |
| `POST /v1/projects/{id}/actions` | Bearer | Start, stop, or restart a group. |
| `POST /v1/containers/{id}/pull` | Bearer | Pull the configured image. |
| `POST /v1/projects/{id}/pull` | Bearer | Pull distinct configured group images. |

Only these methods are supported. HEAD and OPTIONS return 405 on known routes. Responses use JSON with `Cache-Control: no-store`. Unknown routes return JSON 404, never the frontend application. Route resolution precedes activation/authentication, then method checking and validation. An unauthenticated incorrect method on a protected route therefore returns an authentication error first. Noncanonical paths return JSON 404 instead of redirects.

Container IDs are full 64-character lowercase hexadecimal IDs. Project IDs are `container:<full ID>` or `compose:<nonempty name>`. Treat IDs as opaque and URL-encode the entire ID as one path segment. Project names cannot contain path separators or control characters; external names are not subject to the managed-create form's 63-character limit. Short IDs and name lookup are not supported.

Actions require `Content-Type: application/json` (an optional charset is accepted), one object at most 4 KiB, and exactly the supported action field:

```json
{"action":"restart"}
```

Only `start`, `stop`, and `restart` are accepted. Unknown fields, duplicate action keys, invalid JSON, missing/invalid actions, and trailing JSON are rejected. Field names are case-sensitive. Pull requests have an empty body and need no Content-Type header.

## Response contracts

DTOs are independent of Docker SDK and internal structs. Fields use camelCase; timestamps are UTC RFC3339; empty collections are `[]`. Clients must ignore additional fields. Removing fields, changing types, or changing existing semantics requires another API version. Application versions and API versions are separate.

### Health and info

```json
{
  "service": "nox-yard", "version": "git-<full SHA>", "apiVersion": "v1",
  "ready": true, "storage": {"available": true}
}
```

Readiness means HTTP is serving and SQLite responds within two seconds. It does not depend on Docker, administrator setup, or machine API activation. Storage failure returns 503 with the same metadata, `ready:false`, `storage.available:false`, `code:"STORAGE_UNAVAILABLE"`, and an informative `message`.

The existing `/healthz` is unchanged: it checks SQLite and returns `{"status":"ok"}` on success. Docker healthchecks, ARM64 smoke verification, and self-update recovery continue to use it. Docker failure does not make either health endpoint unhealthy when storage is available.

Info returns:

```json
{
  "service": "nox-yard", "version": "git-<full SHA>", "apiVersion": "v1",
  "apiEnabled": false,
  "capabilities": [
    "docker-status", "project-inventory", "container-inspection",
    "container-lifecycle", "project-lifecycle",
    "container-image-pull", "project-image-pull"
  ]
}
```

Capabilities describe implemented operations, independent of activation and transient Docker failure. `apiEnabled` reports effective startup configuration. Health, info, and status use the same resolved version.

### Status and projects

```json
{
  "service": "nox-yard", "version": "...", "apiVersion": "v1",
  "docker": {"available": true},
  "inventory": {
    "collectedAt": "2026-10-03T10:00:00Z",
    "projects": 4, "containers": 8, "runningContainers": 6
  }
}
```

Status performs one snapshot/list request with a five-second overall deadline. It adds no Docker Ping/Info, inspect, stats, or shell probes. Counts include stored managed projects without containers. Docker unavailability or timeout returns HTTP 200 with `docker.available:false`, `docker.error:{code,message}`, and `inventory:null`. Unknown inventory is never zero counts or stale success. Storage/internal errors produce HTTP errors instead of being attributed to Docker.

Projects returns `{"collectedAt":"...","projects":[...]}`. Each project has `id`, `name`, `kind`, `state`, `health`, optional `operation`, and `containers`. Each container has `id`, `name`, optional `service`, `image`, `state`, `health`, and optional `operation`.

| Field | Values |
| --- | --- |
| Project kind | `managed-compose`, `external-compose`, `standalone`, `unknown` |
| Project state | `running`, `stopped`, `partial`, `unknown` |
| Container state | `created`, `running`, `paused`, `restarting`, `removing`, `exited`, `dead`, `unknown` |
| Health | `none`, `healthy`, `unhealthy`, `starting`, `unknown` |

Unknown internal values map to `unknown`. Optional operation reflects the shared pending-operation view. No metrics, terminal capabilities, Compose sources, variables, job data, or managed configuration paths are included. Container listing is nested in projects; there is no separate collection endpoint.

### Inspection

The response has `id`, `ports`, `mounts`, and `networks`:

- Port: `containerPort` (such as `80/tcp`), optional `hostIP` and `hostPort`.
- Mount: `type`, `source` (volume name or bind path), `destination`, `readOnly`.
- Network: `name`, optional `ipv4` and `ipv6`.

Environment is completely omitted, including names and masked entries. Inspection always disables reveal and performs no extra inventory call. Raw Docker configuration and labels are not exposed.

### Actions and pulls

```json
{"action":"restart","succeeded":2,"skipped":0,"failed":0,"queued":0,"failures":[]}
```

```json
{"succeeded":1,"failed":0,"images":["example/app:latest"],"failures":[]}
```

Each failure has `target` (container ID or image reference), stable `code`, and sanitized `message`. Actions have a two-minute deadline, including waiting for serialized work. Pulls have a ten-minute deadline and extend only their response's write deadline. Pull updates the image cache without recreating containers.

HTTP 200 means no failures, including skips for targets already in the relevant state. HTTP 202 with `queued>0` means a restart helper was launched, not that it completed. There is no machine job-polling endpoint.

Managed projects with existing containers use the same generic Engine operations as other groups. A stored managed project without containers remains listed, but generic actions/pulls return `TARGET_NOT_FOUND`. These routes do not dispatch to Compose `up`, create jobs, or deploy missing services.

Existing protections apply: Yard stop/pull and protected maintenance workers are blocked. Yard restart uses the existing independent helper. Protected group stop is rejected before mutating other members.

## Stable errors and partial completion

```json
{"code":"TARGET_NOT_FOUND","message":"Target does not exist."}
```

`code` is stable; `message` is informative English text and may evolve. Clients must not parse messages. Underlying Docker error strings and credentials are not returned.

| HTTP | Code | Meaning |
| --- | --- | --- |
| 503 | `API_DISABLED` | Protected routes disabled. |
| 401 | `AUTH_REQUIRED` | Authorization header absent. |
| 401 | `AUTH_INVALID` | Invalid, ambiguous, or duplicate credentials. |
| 400 | `INVALID_TARGET_ID` | Invalid target ID. |
| 400 | `INVALID_PAYLOAD` | Invalid action/body. |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Action Content-Type is not JSON. |
| 413 | `PAYLOAD_TOO_LARGE` | Action body exceeds 4 KiB. |
| 404 | `TARGET_NOT_FOUND` | Target no longer exists in the Engine. |
| 409 | `TARGET_PROTECTED` | Operation blocked for this target. |
| 409 | `OPERATION_CONFLICT` | State changed or maintenance conflicts. |
| 503 | `DOCKER_UNAVAILABLE` | Docker cannot be reached/accessed. |
| 504 | `OPERATION_TIMEOUT` | Deadline exceeded. |
| 405 | `METHOD_NOT_ALLOWED` | Unsupported method; `Allow` gives the method. |
| 404 | `ROUTE_NOT_FOUND` | Unknown route. |
| 503 | `STORAGE_UNAVAILABLE` | Health storage check failed. |
| 502 | `OPERATION_FAILED` | Execution failed or was canceled. |
| 502 | `OPERATION_PARTIAL_FAILURE` | Some work completed and some failed. |
| 500 | `INTERNAL_ERROR` | Unexpected internal/storage failure. |

401 includes `WWW-Authenticate: Bearer`. Operation errors include `result` with counters and failures. Partial completion returns 502/`OPERATION_PARTIAL_FAILURE`; homogeneous total failures use their mapped code/status. Timeout takes precedence and retains any partial result. A nil internal Go error alone does not establish success. No automatic retries or additional rollback guarantees are introduced. Reconcile current state before retrying after timeout, cancellation, or an interrupted restart response.

## Application version and builds

Resolve once at HTTP startup: nonempty trimmed `NOX_YARD_VERSION`, then incorporated `buildVersion`, then `dev`. `buildSHA` remains independent for self-update.

The Dockerfile accepts `BUILD_VERSION` and `BUILD_SHA` separately. CI uses `scripts/build-version.sh` for the exact compiled commit: an exact lightweight/annotated tag, otherwise `git-<full SHA>`. Multiple tags use the first in C-locale lexical order. An ancestor tag is never used. Publication reuses the verified build's version output. Image tags such as `latest` or `sha-*` do not define the application version. Existing publication triggers, gates, architectures, and image tags remain unchanged.

## Examples

Set `YARD_URL` and `YARD_API_KEY` locally without committing them:

```sh
curl "$YARD_URL/v1/health"
curl "$YARD_URL/v1/info"
curl -H "Authorization: Bearer $YARD_API_KEY" "$YARD_URL/v1/status"
curl -H "Authorization: Bearer $YARD_API_KEY" "$YARD_URL/v1/projects"
curl -H "Authorization: Bearer $YARD_API_KEY" "$YARD_URL/v1/containers/$CONTAINER_ID"
curl -X POST -H "Authorization: Bearer $YARD_API_KEY" \
  -H 'Content-Type: application/json' -d '{"action":"restart"}' \
  "$YARD_URL/v1/projects/compose%3Aexample/actions"
curl -X POST -H "Authorization: Bearer $YARD_API_KEY" \
  "$YARD_URL/v1/containers/$CONTAINER_ID/pull"
```

## Initial scope and tests

Initial scope is inventory, inspection, lifecycle, and image pulls. Excluded: removal, Compose deploy/import/editing, settings, self-update mutations, terminal, log streaming, SSE, WebSocket, environment reveal, and CLI pairing/discovery/onboarding. Future capabilities must preserve existing v1 semantics.

Contract tests use HTTP recorders, temporary SQLite, and fake inventory/controllers, without a Docker daemon. Lifecycle tests use fake Docker transports for protection, deduplication, typed failures, and canceled serialization waits. Version tests use temporary Git repositories. Shared source checks, Docker architecture checks, and ARM64 `/healthz` smoke verification remain release gates.
