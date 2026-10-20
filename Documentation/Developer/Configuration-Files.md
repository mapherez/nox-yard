# Project configuration files

## Source and drawer

The current host Compose files, `.env` and service `env_file` files are authoritative. Docker's `com.docker.compose.project.working_dir` and ordered `config_files` labels identify the original locations. Yard remembers locations when its own resolved stdin deployment changes that label to `-`. A subsequent terminal deployment with real file labels takes precedence. No extra discovery roots are configured.

The existing environment fields have pencil, save/check and cancel controls. Editing loads the saved value; saving validates Compose and writes the respective dotenv assignment or service environment definition. Unrelated values survive, including duplicate and multiline dotenv definitions. A saved/runtime difference displays **Saved · Recreate required**. Values stay hidden until an explicit reveal/edit action.

The **Compose and environment files** accordion starts closed. It contains a full text editor and **Save** for each original Compose/environment filename. Compose merge order and relative bind bases survive. Invalid YAML written from the terminal remains readable for repair, but cannot be used by Recreate. File errors never silently select an old DB configuration.

Saving changes files only. The activation flow is **edit → Save → Recreate**. Existing containers, images, volumes and application data remain untouched by Save. Each write requires a current fingerprint over directory, ordered Compose names and file bytes. Validation precedes writing; the host helper compares the old bytes again before same-directory atomic renames. Concurrent changes produce a conflict and retain the browser draft. Reload before retrying.

## Uniform actions

| Action | Effect |
| --- | --- |
| Start / Stop | Start/stop existing containers, without applying edited files. |
| Restart | Restart existing containers, without applying edited files. |
| Pull | Read current configuration and download images, without replacing containers. |
| Recreate | Apply current files and local cached images with `--no-build --pull never`; explicitly recreate even when image IDs are unchanged. |

The UI has no Update action. The legacy update APIs and daily automatic jobs remain compatible and pull before applying the current configuration. URL Sync remains an explicitly previewed source refresh. Single-file legacy Sync reads its previous inputs from disk; multi-file projects use the individual file editors.

Recreate in a container's details targets its Compose service and all that service's replicas, for both managed and external projects. The confirmation names the project/service and lists only selected containers. Recreate at project level still targets the whole project. Service operations use `--no-deps`, omit orphan removal and never start/recreate other services or rerun their initializers. Stopped services remain stopped; mixed running/stopped replicas must be stabilized first. Scope, current file contents and runtime identities/state are bound to the confirmation fingerprint. The independent worker, recovery and restart reconciliation retain that scope, and verify that unselected containers and their running states remain unchanged. Project-level reservations still serialize competing operations safely.

A Compose service may declare both `image:` and a development `build:`. Yard uses the named local image and removes build metadata only from its private execution model; original host files remain unchanged. Services without an explicit prebuilt image still fail validation. An unavailable local image fails without building or pulling.

Missing Compose files permit the existing assessed Engine fallback, preserving runtime configuration and any explicitly selected service scope. Recreate still selects local images using the original reference, including images downloaded by Pull. Inaccessible/symlinked files or an incompatible existing configuration produce an error instead of fallback. Build-only services, unresolved file dependencies and files escaping the project directory are refused. Individual files are limited to 1 MiB; reads have an aggregate bound. Files outside this supported contract require host editing/deployment.

File-backed recreation runs in the independent worker. It journals actual old Engine inspections and retains old images before mutation. Native Compose receives the full resolved definition with immutable image IDs, original image references and original relative-bind base. Existing stopped services remain stopped; new services start. Bounded readiness and image verification precede success. Ordinary failure restores the previous runtime from the private inspections without undoing the user's saved source edits. Named/anonymous volumes and bind data are never deleted by recovery. Shared application writes and database migrations are not undone. Interrupted/uncertain workers retain recovery reservations; observers do not replay commands.

## Direct deployment persistence

Deploy/copy/Sync write Compose and environment files within the existing reviewed job. Interpolation overrides become deterministic, escaped `.env` assignments before deployment; adoption saves them as part of its own job. Successful completion atomically commits the terminal job result and location-only project metadata. Private operation payloads retain the source/recovery information needed by workers, while active configuration is read from host files.

Configuration reads occur on opening or explicitly refreshing the drawer; there is no configuration polling. The completed one-off DB-to-file conversion has been removed: no startup migrator, periodic scan, dashboard notice, migration buttons or migration endpoints remain. Historical jobs and compatibility database columns are retained as data. This version assumes that conversion already completed; installations still holding DB-only configuration must complete it on the preceding release before upgrading.

Direct project-file writes inherit the project/base owner. Atomic file edits preserve existing non-root ownership and mode. SQLite/WAL/SHM ownership handling remains part of storage opening. Application data is never recursively chowned.

## Authenticated browser API

All configuration POSTs require the existing session, exact Origin and CSRF token, and use `Cache-Control: no-store`:

- `POST /api/projects/{id}/configuration/read` accepts `{"reveal":false}` by default. Metadata and pending flags are returned, with file contents and variable values redacted. `true` explicitly reveals them.
- `POST /api/projects/{id}/configuration/save` accepts `fingerprint`, `content` and either `path` or `service` + `variable`. It validates, writes and returns the refreshed saved view. Stale fingerprints return 409.

IDs use `compose:NAME`. Contents/values do not enter public job records, previews or error logs. The existing Control API v1 remains unchanged. Internal recovery snapshots remain sensitive native Yard data.

Existing `POST /api/recreate/preview` and `/api/recreate/submit` also accept optional `service` for `operation:recreate` with a Compose project ID. Omission preserves whole-project behavior; submission must use the same service and reviewed fingerprint. Invalid/unknown services fail without widening to the project. MCP's existing recreate tools expose the same optional field through the shared facade.

## Yard itself

The Yard drawer uses the same editors, Pull/Recreate/Restart and daily schedule. Independent recreation stops the web process before making its SQLite rollback snapshot, applies the current host files and verifies health. Failure restores SQLite before restarting the previous runtime. Edited host files are preserved for correction. See [Self-update](Self-Update.md) for Check now, daily scheduling and the no-files fallback.

See [Development](Development.md) for direct-persistence, native-worker, browser and Engine-fallback fixtures.
