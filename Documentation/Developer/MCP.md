# Embedded MCP

Yard exposes an opt-in Streamable HTTP MCP endpoint at `http://127.0.0.1:8096/mcp` in the default Compose installation. It shares the Go process but uses a separate internal listener on port 8081. Development clients use `http://127.0.0.1:8081/mcp`. The UI/Control API listener no longer serves `/mcp`, and the MCP listener exposes no UI/browser/Control API routes.

MCP starts disabled. Administrator Settings enable it with either a Yard Bearer token (the default authentication mode) or explicitly selected anonymous access. Only one local token is active across MCP and Control API. Rotation/revocation is transactional and checked on each HTTP request; cookies, query parameters and browser credentials never authenticate MCP. The UI displays a new random 256-bit token once; SQLite stores its SHA-256 digest and the latest ten token events. An invalid supplied token never downgrades to anonymous access. When a request includes Origin, it must match `NOX_PUBLIC_URL`, or the direct request origin when that setting is empty. There is no additional CORS policy. Static local Bearer tokens do not implement the MCP OAuth authorization/discovery flow.

Browser authentication and `/api/*` retain session/Origin/CSRF protection. `/v1/*` uses the shared local token with its own enable switch and remains authenticated even in anonymous MCP mode. `/healthz` keeps its SQLite readiness contract. Unknown MCP paths return JSON errors. MCP exposes neither a stdio service nor the NoX desktop bridge. The independent [Suite adapter](Suite-Integration.md) is available on the local Unix channel and accepts verified Backend delegations, not the direct Yard token, browser cookie or caller-supplied identity headers.

## Enforced permissions

Settings select inspection, optional writes, and optional sensitive reads. The MCP catalog omits write tools without write permission and omits environment/log tools without sensitive-read permission. Every execution independently checks its request policy before reaching the facade; `yard_project_configuration_read` additionally requires sensitive-read permission for `reveal:true`. Reads/previews inspect state without deploying or changing project configuration. Pulls, lifecycle actions, saves, schedules, deployment/adoption, removal, recovery acknowledgement and self-update mutations require writes. These checks supplement the existing operation confirmations, fingerprints and resource reservations.

Both bindings default to host loopback. Settings persist desired network configuration separately from Docker's observed published ports. Apply now creates a reserved durable network job before writing Compose/`.env`; a separate worker uses the current image and `up -d --force-recreate --no-deps --no-build --pull never nox-yard`, verifies health and published bindings, and restores prior files/runtime on ordinary failure. Unknown worker outcomes retain reservations for reviewed recovery. Ambiguous multi-file port definitions are refused before writing. Permissions, token changes and the MCP enable switch apply without recreation; published-port changes need recreation. Automatic recreation is opt-in. The sidebar shows local MCP server availability, not gateway/client connection status.

## Shared implementation

`internal/application` is a lightweight orchestration facade. HTTP and MCP share the same inventory, lifecycle, managed Compose, self-update and storage instances. MCP does not call `/v1` over HTTP. Docker operations, source validation, locks, protected-target rules, confirmation fingerprints, jobs and persistence remain implemented by the existing managers.

The executable creates one `inventory.Notifier`. The facade, browser SSE, Compose manager and durable-job observer use that same instance. Persisted running jobs restore pending inventory states after restart; independent workers save their results and the observer notifies clients. All mutation adapters share SQLite resource reservations.

Facade operations accept the adapter's context, introduce no deadlines and do not replace it with Background/WithoutCancel. Existing synchronous HTTP deadlines remain in the HTTP adapters; existing manager deadlines and asynchronous job contexts remain unchanged. Methods without a context API check cancellation before invoking their manager; an already accepted asynchronous operation is not retroactively canceled.

## Tools

The same 38 `yard_*` tools remain canonical. Yard uses the published NoX MCP
`v0.7.3` module. Conventional `tools/list` contains names, descriptions, schemas
and annotations, with no `_meta.cli` or human menu descriptors. The direct endpoint
does not enable NoX discovery. On the Suite path, `nox/tools/definitions` retains
canonical `_meta.cli` paths and separate rich `cli` descriptors for human clients.
Gateway translates their references without changing tool semantics. Paths are
app-local; the product hierarchy is provided by Gateway/CLI, not by renaming tools.

IDs must be copied from inventory: containers use full 64-character lowercase hexadecimal IDs; projects use `container:<id>` or `compose:<name>`. Configuration tools require `compose:<name>`, including external projects and Yard itself. Legacy managed Compose operations take the saved project name instead.

| MCP tool | CLI (`_meta.cli`) | Inputs and behavior |
| --- | --- | --- |
| `yard_health` | `health` | No inputs. Storage readiness and version. |
| `yard_status` | `status` | No inputs. Docker availability and inventory counts. |
| `yard_projects` | `project list` | No inputs. Projects with nested containers and pending states. |
| `yard_metrics` | `metrics` | No inputs. Cached metrics. |
| `yard_container_inspect` | `container inspect` | `id`. Ports, mounts, networks and environment names, without values. |
| `yard_container_environment` | `container env` | `id`. Explicitly returns potentially sensitive environment values. |
| `yard_container_logs` | `container logs` | `id`. Last 20 decoded records, without following the stream. |
| `yard_container_log_history` | `container log history` | `id`, optional RFC3339 `since`, `until`, opaque `cursor`. Sensitive read. At most 1,000 events / 4 MiB; next cursor preserves repeated timestamps. Reads Docker retention only. |
| `yard_container_action` | `container action` | `id`, `action`: `start`, `stop`, `restart`. Shared Engine lifecycle. |
| `yard_project_action` | `project action` | `id`, `action`: `start`, `stop`, `restart`. Native Compose with accessible source and active selection; assessed Engine fallback for absent source. Restart includes stopped services and can rerun initializers. |
| `yard_container_pull` | `container pull` | `id`. For a Compose member, pull only its service image using current host files; otherwise use runtime configuration. No container changes or build. |
| `yard_project_pull` | `project pull` | `id`. Pull current host-file images, or runtime images when files cannot be located. Invalid/inaccessible configuration fails without stale fallback. No container changes or build. |
| `yard_project_configuration_read` | `project configuration read` | `id`, optional `reveal` (default false). Current host Compose/env file paths, fingerprint, service variables and pending flags. File contents and values require explicit `reveal:true`. |
| `yard_project_configuration_save` | `project configuration save` | `id`, `fingerprint`, and file (`path`, `content`) or context (`profiles`, `interpolationFiles`) edits. Validate and save. Does not change containers. Returns metadata with contents/values hidden. |
| `yard_project_override_preview` | `project override preview` | `id`, `action: add|remove`, `fingerprint`, optional `source`, `path`, `variables`, `files`. Sensitive read; returns frozen input and effective before/after diff data with candidate fingerprint. |
| `yard_project_override_apply` | `project override apply` | Same reviewed request and `candidateFingerprint`. Sensitive read and write permissions. Journaled file/selection save; no runtime changes. |
| `yard_project_variable_set` | `project variable set` | `id`, `fingerprint`, `service`, `name`, `value` (empty allowed). Edit the existing variable definition in its source file, preserving other values. Save only; returns hidden values and pending flags. |
| `yard_project_schedule_get` | `project schedule get` | `id`. Daily schedule, server timezone, eligibility and last/next result, including Yard. Initially 03:00; ordinary projects default off and Yard preserves its prior enabled state. |
| `yard_project_schedule_set` | `project schedule set` | `id`, required `enabled` boolean, optional `time` (`HH:MM`). Set daily updates, including Yard; omitted time preserves its saved value. |
| `yard_recreate_preview` | `recreate preview` | `id`, `operation`: `recreate` or legacy `update`. Optional `service` selects all replicas of one Compose service for Recreate, leaving dependencies untouched; omission retains project scope. Returns a secret-safe fingerprint bound to that scope. Recreate uses current host files and local images without pull/build. Read-only. |
| `yard_recreate_submit` | `recreate submit` | Same fields, including the same optional `service`, plus `confirm:true`, `fingerprint`. Independent replacement job, per-container results and recovery with the reviewed scope. Recreate applies saved changes even when images are unchanged. Legacy update pulls and compares images. |
| `yard_container_remove_preview` | `container remove preview` | `id`, optional `removeVolumes` and `removeImages` (both default false). Current removal plan and choice fingerprint. |
| `yard_project_remove_preview` | `project remove preview` | `id`, optional `removeVolumes` and `removeImages` (both default false). Current removal plan and choice fingerprint; supports empty saved managed projects. |
| `yard_container_remove` | `container remove` | `id`, `confirm:true`, `fingerprint`, optional `removeVolumes` and `removeImages` (both default false). Revalidate and execute selected exclusive-resource rules. |
| `yard_project_remove` | `project remove` | Same fields. Runtime-only removal; use Compose operation to delete managed metadata. |
| `yard_compose_source` | `compose source` | Existing source fields: `kind`, optional `url`, `filename`, `yaml`. Inspect source and required variables/env files. |
| `yard_compose_preview` | `compose preview` | Managed request: `name`, `source`, `variables`, `envFiles`, `mode`, optional `profiles` and `interpolationFiles`, optional `fingerprint` and adoption-only `projectDir`. Modes: `new`, `copy`, `sync`, `adopt`. Returns preview/fingerprint and inferred `adoptionDir` when available. |
| `yard_compose_submit` | `compose submit` | Same managed request. Submission requires the current preview fingerprint and returns a job. Adoption resolves/rechecks the original directory and runtime/files before saving ownership; it does not recreate containers. |
| `yard_compose_operation` | `compose operation` | `name`, `operation`, optional booleans `removeVolumes` and `removeImages` (both default false), optional removal `fingerprint` from `yard_project_remove_preview`. Operations: `start`, `stop`, `restart`, `pull`, `recreate`, legacy `update`, `remove`. Returns a job. Removal without a fingerprint retains the input shape but is assessed at submission and rechecked by the worker; shared volumes remain protected. |
| `yard_compose_job` | `compose job` | `id`. Persisted job status. |
| `yard_job` | `job get` | `id`. Typed operation status, stages, images, recovery and cleanup fields. |
| `yard_job_history` | `job history` | `target`: `compose:NAME` or `container:FULL_ID`. Latest 30 contextual operations. |
| `yard_job_recovery_acknowledge` | `job recovery acknowledge` | `id`, `updatedAt`, `confirm:true`. Release an uncertain reservation after host review; no Docker mutation. Reject active workers/helpers and stale reviews. |
| `yard_projects_settings_get` | `settings projects get` | No inputs. Read the configured host projects base directory. |
| `yard_projects_settings_set` | `settings projects set` | `projectsBase`. Existing host-directory checks apply. |
| `yard_self_update_status` | `update status` | No inputs. Current update settings/state. |
| `yard_self_update_settings` | `update settings` | Compatibility inputs `automatic`, `intervalMinutes` (5, 15, 30, 60 or 360). Synchronizes daily on/off, preserving the chosen time; the interval no longer drives a timer and saving does not trigger a check. Prefer `yard_project_schedule_set` for Yard's daily time. |
| `yard_self_update_check_and_update` | `update now` | No inputs. Queue a manual check which can install an update and restart Yard **even when automatic updates are disabled**. Observe progress with `yard_self_update_status`. |

Required fields and output shapes are available through `tools/list`. Normal results use structured content plus a JSON text representation. Errors use `isError: true`, JSON text content and no success `structuredContent`; operation errors retain counters and partial results under `details.result`. Accepted jobs and queued restart helpers are not reported as completed operations.

The configuration flow is **read → save file or set variable → preview Recreate → submit Recreate**. Read the fingerprint before each edit; a terminal edit or another save invalidates it. Conflicts require rereading and reviewing the current contents, never a blind retry. File paths are relative to the returned project directory; ordered Compose names and nested env-file paths remain unchanged. Writes share browser validation, resource reservations and secret-safe job history. Saving never pulls, builds or recreates. Start/Stop/Restart operate existing containers; Restart does not apply new configuration. Read saved configuration through the new tools; `yard_container_environment` still inspects runtime values.

Revealed Compose can contain credentials just like `.env`, so default configuration reads and both write responses hide all file contents and variable values. Request `reveal:true` only when those contents are needed. The existing file validation, fingerprint and operation-reservation guards still apply. The completed one-off configuration migration has been retired; browser and MCP use current host files.

All tools explicitly declare ReadOnly, Destructive and Idempotent through NoX MCP. Reads/previews, including explicit environment reads, are `(true,false,true)`. Engine actions, pulls, removals, Compose submissions/operations and self-update mutations are conservatively `(false,true,false)`. Setting the projects directory or a project schedule is `(false,true,true)`. Multi-mode tool annotations cover all accepted modes.

## Limits and continuous features

Yard configures a **60-second timeout** and 32 concurrent requests. Direct MCP has an 8 MiB payload limit; Suite uses the shared 2 MiB frame bound and its request-budget controls. Existing Compose source limits still apply. Neither the server's 130-second WriteTimeout nor the NoX MCP library's default timeout is changed.

The complete catalog contains 38 tools; clients see the subset allowed by their active permission profile. Managed Compose, external update/recreate and Yard restart use independent durable workers; self-update retains its dedicated worker. Query job/history/status tools rather than treating an accepted response as completion. Pulls and configuration reads/writes remain synchronous and share the MCP timeout. Interrupted Engine responses persist recovery-required outcomes and hold resource reservations until reviewed. There are no automatic mutation retries. See [Durable operations](Operation-Jobs.md) for the additive fields and recovery procedure, and [External updates](External-Updates.md) for configuration limits and removal compatibility. Omitted volume choice now retains volumes, correcting the former Engine default.

Container log snapshots share Docker opening and stdout/stderr/TTY decoding with browser SSE. The browser retains its 200-record initial tail and continuous stream. Long lines retain the existing 16 KiB segmentation, and snapshots keep at most 20 decoded records.

Interactive terminal and continuous log/event subscriptions remain in their native interfaces. Dashboard entry uses NoX Login; the retained password-recovery command is only for compatible older-image rollback, not current login. MCP does not provide an alternate command-execution implementation.

## Human menus through NoX discovery

Every canonical tool has a version `1.0` descriptor in the Suite NoX definitions.
The descriptor supplies an EN-US label, ordering and grouping on the existing
paths: health/status/metrics, projects, containers, Compose, recreation, jobs,
settings and Yard updates. Normal AI tool discovery/execution is independent.

- Project targets and job-history targets select opaque IDs from `yard_projects`.
  Duplicate names remain separate IDs; no names are converted into IDs.
- Container IDs stay explicit because `yard_projects` nests containers inside
  projects and the current generic selector consumes a flat array. Managed-project
  names, service names and action/mode strings also retain explicit inputs; no
  wildcard projection, guessed enum or additional tool is introduced.
- Configuration save selects a returned file path. Source YAML/file contents use
  multiline input; maps/arrays use schema-driven JSON. Variables, env-file maps,
  override file maps and individual variable values use masked input and are
  hidden in the client review. Authorized source/override results can still be
  sensitive; metadata does not change their existing read permissions.
- Removal, recreation and deployment call their existing read-only preview before
  confirmation. The current fingerprint is bound into the unchanged mutation;
  removal/recreation also bind the existing `confirm` flag after a matching typed
  target confirmation. Resource-removal choices and service scope are forwarded.
- File/variable edits and override workflows first capture configuration metadata,
  then collect the edits. Their final write keeps that earlier fingerprint rather
  than silently rebinding changed files. Override application uses the preview's
  frozen source, destination and candidate fingerprint. Containers are not changed
  by a configuration save.
- Recovery acknowledgment reads the existing job revision and requires explicit
  host review and typed confirmation. Self-update warns that it can restart Yard
  even when automatic updates are disabled. Accepted jobs are not shown as finished.

A missing/unauthorized reference, empty required choice, failed preview, stale
fingerprint or canceled confirmation must stop the flow. Descriptors grant no
permissions, bypass no native checks and never cause automatic mutation retries.
The CLI validates required `dynamic-options`, `preview`, `confirmation` and
`workflow` features. These are existing NoX descriptor features, not new methods.

The generic configuration-save menu is a file-edit flow; context-only
profiles/interpolation edits remain available through the existing tool's explicit
API arguments. The generic Compose operation is a compatibility operation with
explicit inputs; use the preview-backed recreation/removal menus for guided flows.
No UI metadata is added to ordinary `tools/list` to support these interactions.

## Updating the library

Consume a published release through Go modules, not a local checkout replacement:

```sh
go get github.com/mapherez/nox-mcp@v0.7.3
go mod tidy
```

Commit the resolved dependency/checksum changes and run the repository checks.
Updates must preserve ordinary tool contracts and validate canonical definitions,
authorized references and real client interactions. The pre-menu tool-contract
hash fixture covers the existing names, schemas, descriptions and annotations.

## Validation

MCP tests use the official Go SDK over real HTTP with temporary SQLite and shared fake dependencies. They cover discovery, schemas/annotations, anonymous MCP versus existing HTTP authentication, origin validation, explicit environment reveal, partial failures, protected/stale removal, manual self-update, shared browser SSE/pending states and asynchronous Compose notification lifetimes. Facade tests cover context propagation, cancellation before dispatch and blocked log-reader cancellation. Existing manager tests continue to cover Docker transport, concurrency and operation rules.

Configuration protocol tests also cover explicit file/value reveal, metadata-only save responses, empty variable values, stale fingerprints, shared active-job reservations, secret-free history and shared manager errors. Facade pull regressions cover service scope, current-file references and fallback only for missing files. `scripts/smoke-configuration.py --image <local-image>` validates MCP/browser parity against a disposable real Yard: Compose/env editing, conflict detection, pending flags, daily schedule, independent self-recreation and failed-start recovery. It never connects to a deployed Yard.

The isolated Docker smoke test creates disposable containers, Compose projects and data volumes, publishes temporary loopback ports, and cleans up its own fixtures. It exercises real lifecycle operations, inspection/environment/logs, pulls, removal fingerprints, browser SSE, Compose jobs, healthy/one-shot verification, unhealthy deploy/update failures, named-volume/bind preservation, original-directory adoption with nested env files and stale runtime/file previews, persisted settings/session and queued Yard self-restart. An optional ARM64 image also checks runtime readiness and MCP discovery under emulation:

```sh
docker buildx build --platform linux/amd64 --load -t nox-yard:mcp-smoke-amd64 .
docker buildx build --platform linux/arm64 --load -t nox-yard:mcp-smoke-arm64 .
python scripts/smoke-mcp.py --image nox-yard:mcp-smoke-amd64 --arm64-image nox-yard:mcp-smoke-arm64
```

The script accepts `--docker-command`, `--docker-config` and `--docker-host` for Docker Desktop installations. It does not install a remote Yard update or publish/deploy the built images.
