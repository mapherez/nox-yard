# Project configuration files

## Source and drawer

The current host Compose files, `.env` and service `env_file` files are authoritative. Docker's `com.docker.compose.project.working_dir` and ordered `config_files` labels identify the original locations. Yard remembers locations when its own resolved stdin deployment changes that label to `-`. A subsequent terminal deployment with real file labels takes precedence. No extra discovery roots are configured.

The existing environment fields have pencil, save/check and cancel controls. Editing loads the saved value; saving validates Compose and writes the respective dotenv assignment or service environment definition. Unrelated values survive, including duplicate and multiline dotenv definitions. A saved/runtime difference displays **Saved · Recreate required**. Values stay hidden until an explicit reveal/edit action.

The **Compose and environment files** accordion starts closed. It contains a full text editor and **Save** for each original Compose/environment filename. Compose merge order and relative bind bases survive. Invalid YAML written from the terminal remains readable for repair, but cannot be used by Recreate. File errors never silently select an old DB configuration.

Saving changes files only. The activation flow is **edit → Save → Recreate**. Existing containers, images, volumes and application data remain untouched by Save. Each write requires a current fingerprint over directory, ordered Compose names and file bytes. Validation precedes writing; the host helper compares the old bytes again before same-directory atomic renames. Concurrent changes produce a conflict and retain the browser draft. Reload before retrying. A crash between multiple migration file writes is resumable; the migration does not become Ready before complete read-back verification.

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

## Automatic migration

The web process starts migration before serving project mutations. Workers opening SQLite do not start another migrator. Managed projects receive one of:

- **Migrating:** queued or exporting; file edits, Recreate and automatic replacement are blocked.
- **Ready:** files were materialized, validated and read back successfully, and their verified recovery snapshot was persisted before the active DB contents were cleared.
- **Needs attention:** export or validation failed, including conflicting manual files. Existing files and legacy configuration remain recoverable. The project offers **Retry migration** and **Use current files**; the latter explicitly chooses conflicting host definitions after validation, while exporting missing DB-only values without overwriting local assignments.

Startup resumes pending exports and recovers legacy missing directories/original filenames from Docker labels. Equivalent multi-file sources migrate automatically with their original merge order; different resolved configurations require explicit conflict resolution. Migration waits for active jobs; cancellation does not turn a pending project into a permanent failure. Other projects, inventory and logs remain available. A temporary global progress/completion notice and the per-project state replace any permanent migration CTA. Do not estimate readiness by waiting a fixed number of minutes: use the reported state.

DB interpolation variables are exported into `.env`, including values not previously present on disk. Effective environment and published ports are compared against the legacy resolution before export completion. Existing different files are never silently overwritten. A guarded transaction requires the exact old record, matching verified export fingerprint and a completed private recovery job before blanking `managed_projects.yaml`, `variables_json` and `env_files_json`. The compatibility columns remain; active configuration no longer depends on them. Locations, accounts, preferences, history and native private job snapshots remain in SQLite.

Only configuration files and directories created for them are repaired/written. Ownership follows the project/host base owner; later atomic replacements preserve existing non-root ownership and mode. The Yard data directory and its exact SQLite/WAL/SHM files receive the host owner, including when root workers reopen the DB. Application data is never recursively chowned.

## Authenticated browser API

All configuration POSTs require the existing session, exact Origin and CSRF token, and use `Cache-Control: no-store`:

- `POST /api/projects/{id}/configuration/read` accepts `{"reveal":false}` by default. Metadata and pending flags are returned, with file contents and variable values redacted. `true` explicitly reveals them.
- `POST /api/projects/{id}/configuration/save` accepts `fingerprint`, `content` and either `path` or `service` + `variable`. It validates, writes and returns the refreshed saved view. Stale fingerprints return 409.
- `POST /api/projects/{id}/configuration/retry-migration` accepts `{"useCurrentFiles":false}`; `true` explicitly resolves in favor of the existing host files.
- Authenticated `GET /api/configuration-migration` exposes per-project migration metadata only.

IDs use `compose:NAME`. Contents/values do not enter public job records, previews or error logs. The existing Control API v1 remains unchanged. Internal recovery snapshots remain sensitive native Yard data.

Existing `POST /api/recreate/preview` and `/api/recreate/submit` also accept optional `service` for `operation:recreate` with a Compose project ID. Omission preserves whole-project behavior; submission must use the same service and reviewed fingerprint. Invalid/unknown services fail without widening to the project. MCP's existing recreate tools expose the same optional field through the shared facade.

## Yard itself

The Yard drawer uses the same editors, Pull/Recreate/Restart and daily schedule. Independent recreation stops the web process before making its SQLite rollback snapshot, applies the current host files and verifies health. Failure restores SQLite before restarting the previous runtime. Edited host files are preserved for correction. See [Self-update](Self-Update.md) for Check now, daily scheduling and the no-files fallback.

See [Development](Development.md) for disposable file-migration, native-worker, browser and Engine-fallback fixtures.
