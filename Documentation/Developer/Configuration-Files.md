# Project configuration files

## Source and drawer

The current host Compose files, `.env` and service `env_file` files are authoritative. Docker's `com.docker.compose.project.working_dir` and ordered `config_files` labels identify the original locations. Yard remembers locations when its own resolved stdin deployment changes that label to `-`. The ordered list is explicit: later files override earlier files. Yard never auto-selects an override added through the terminal. **Add override** reviews and appends a new file. A subsequent external deployment with new container identities and real file labels can update the observed selection; unchanged labels from the previous deployment cannot undo a saved selection. No extra discovery roots are configured.

The existing environment fields have pencil, save/check and cancel controls. Editing loads the saved value; saving validates Compose and writes the respective dotenv assignment or service environment definition. Unrelated values survive, including duplicate and multiline dotenv definitions. A saved/runtime difference displays **Saved · Recreate required**. Values stay hidden until an explicit reveal/edit action.

The **Compose and environment files** accordion starts closed. It contains a full text editor and **Save** for each original Compose/environment filename. Compose merge order and relative bind bases survive. Invalid YAML written from the terminal remains readable for repair, but cannot be used by Recreate. File errors never silently select an old DB configuration.

Saving changes files only. The activation flow is **edit → Save → Recreate**. Existing containers, images, volumes and application data remain untouched by Save. Each write requires a current fingerprint over directory, ordered Compose names, file bytes, environment/profile selection and context revision. A private operation journal records the before/after files and selection before writing. Success requires both files and SQLite selection to be confirmed; unknown helper/commit outcomes retain the recovery reservation. Validation precedes writing; the host helper compares the old bytes again before same-directory atomic renames. Concurrent changes produce a conflict and retain the browser draft. Reload before retrying.

## Overrides, environment and profiles

**Add override** is the last action in the files accordion. URL, paste and upload reuse the creation inputs. **Read source** resolves the current ordered files plus the candidate through native Compose and reviews the effective YAML and a textual diff. This is sensitive configuration: reveal protections and `no-store` apply. Cancel writes nothing. Confirmation uses the frozen source bytes and both reviewed fingerprints, never another URL fetch.

The first unused name is shown before saving: `compose.override.yaml`, `compose.override.2.yaml`, and so on. Existing files are never overwritten. The original Compose bytes, comments and formatting remain unchanged. Partial overrides are validated against the combined project; existing service images need not be repeated. Missing required interpolation or local files must be supplied before saving. Optional interpolation overrides become escaped `.env` assignments, and local dependency uploads remain separate files. Removing an override from selection requires another combined review and retains its host file.

Profiles can be selected in project configuration and on creation/import. Interpolation-file selection is ordered and persistent; an explicit empty list disables implicit `.env`. Defaults, nested references, empty values, escapes, `env_file` order, long `required`/`format`, include environments and local path handling are resolved by Compose. `config --variables --format json` supplies variable discovery. An unset optional `${VAR}` is allowed. Yard's process environment is not a source of arbitrary project variables. Project name and file selection are explicit `-p`/`-f`; profile/environment selection belongs to the same context. Destination-changing `DOCKER_*`, implicit orphan deletion and experimental remote controls are refused.

When original profile intent is unknown, already deployed services are explicitly selected without activating other services sharing their profile. An explicit profile choice replaces that inference. Verification, schedules and recovery use the same active service set and complete resolved JSON. The reduced inspection model does not define which Compose fields survive execution.

Local `include` (including long syntax), `extends`, `configs`, `secrets` and `label_file` are supported when dependencies remain in the allowed project directory. No remote dependencies are fetched. `develop` is preserved; Yard does not execute development commands. Files are limited to 1 MiB each and 8 MiB total (browser managed POST bodies retain their existing 4 MiB encoded-request bound), with bounded dependency traversal; symlinks and outside-root configuration dependencies remain refused. Native/recovery capability restrictions are evaluated before mutation; a valid Docker feature may still require a manual deployment when Yard cannot recover it safely.

## Uniform actions

| Action | Effect |
| --- | --- |
| Start / Stop | Run the corresponding Compose operation with the active selection and dependencies; no configuration is applied. |
| Restart | Native Compose restart, including selected stopped containers; completed initializers can run again. Saved edits are not applied. |
| Pull | Read current configuration and download images, without replacing containers. |
| Recreate | Apply current files and local cached images with `--no-build --pull never`; explicitly recreate even when image IDs are unchanged. |

The UI has no Update action. The legacy update APIs and daily automatic jobs remain compatible and pull before applying the current configuration. URL Sync remains an explicitly previewed source refresh. Single-file legacy Sync reads its previous inputs from disk; multi-file projects use the individual file editors.

Recreate in a container's details targets its Compose service and all that service's replicas, for both managed and external projects. The confirmation names the project/service and lists only selected containers. Recreate at project level still targets the whole project. Service operations use `--no-deps`, omit orphan removal and never start/recreate other services or rerun their initializers. Stopped services remain stopped; mixed running/stopped replicas must be stabilized first. Scope, current file contents and runtime identities/state are bound to the confirmation fingerprint. The independent worker, recovery and restart reconciliation retain that scope, and verify that unselected containers and their running states remain unchanged. Project-level reservations still serialize competing operations safely.

A Compose service may declare both `image:` and a development `build:`. Yard uses the named local image and removes build metadata only from its private execution model; original host files remain unchanged. Services without an explicit prebuilt image still fail validation. An unavailable local image fails without building or pulling.

When all selected Compose files are missing, sources are unavailable and permit the existing assessed Engine fallback, preserving runtime configuration and any explicitly selected service scope. Recreate still selects local images using the original reference, including images downloaded by Pull. A partially missing selected file list, inaccessible/symlinked files or an incompatible existing configuration produces an error instead of fallback. Missing selected files remain visible for restoration or reviewed removal from selection. Build-only services, unresolved file dependencies and files escaping the project directory are refused. Individual files are limited to 1 MiB; reads have an aggregate bound. Files outside this supported contract require host editing/deployment.

File-backed recreation runs in the independent worker. It journals actual old Engine inspections and retains old images before mutation. Native Compose receives the full resolved definition with immutable image IDs, original image references and original relative-bind base. Existing stopped services remain stopped; new services start. Bounded readiness and image verification precede success. Ordinary failure restores the previous runtime from the private inspections without undoing the user's saved source edits. Named/anonymous volumes and bind data are never deleted by recovery. Shared application writes and database migrations are not undone. Interrupted/uncertain workers retain recovery reservations; observers do not replay commands.

## Direct deployment persistence

Deploy/copy/Sync write Compose and environment files within the existing reviewed job. Interpolation overrides become deterministic, escaped `.env` assignments before deployment; adoption saves them as part of its own job. Successful completion atomically commits the terminal job result and location-only project metadata. Private operation payloads retain the source/recovery information needed by workers, while active configuration is read from host files.

Configuration reads occur on opening or explicitly refreshing the drawer; there is no configuration polling. The completed one-off DB-to-file conversion has been removed: no startup migrator, periodic scan, dashboard notice, migration buttons or migration endpoints remain. Historical jobs and compatibility database columns are retained as data. Conversion was last available in v1.3.1 and removed in v1.3.2. Installations still holding DB-only configuration must complete and verify conversion on v1.3.1 before upgrading further. An already-converted installation does not need it again. See [Host validation](Host-Validation.md) for the remaining upgrade acceptance boundary.

Direct project-file writes inherit the project/base owner. Atomic file edits preserve existing non-root ownership and mode. SQLite/WAL/SHM ownership handling remains part of storage opening. Application data is never recursively chowned.

## Authenticated browser API

All configuration POSTs require the existing session, exact Origin and CSRF token, and use `Cache-Control: no-store`:

- `POST /api/projects/{id}/configuration/read` accepts `{"reveal":false}` by default. Metadata and pending flags are returned, with file contents and variable values redacted. `true` explicitly reveals them.
- `POST /api/projects/{id}/configuration/save` accepts `fingerprint` and a file edit (`path`, `content`), variable edit (`service`, `variable`, `content`) or context edit (`profiles`, `interpolationFiles`). It validates, writes and returns the refreshed saved view. Stale fingerprints return 409.

- `POST /api/projects/{id}/configuration/override-preview` accepts `action: add|remove`, the current `fingerprint`, optional `source`/`path`, interpolation `variables` and uploaded `files`. It returns frozen source, chosen path, ordered files, before/after effective YAML and `candidateFingerprint`.
- `POST /api/projects/{id}/configuration/override-apply` accepts those same reviewed request fields and both fingerprints. It returns the saved configuration view; adapters redact contents unless explicitly revealing them.

IDs use `compose:NAME`. Contents/values do not enter public job records, previews or error logs. The existing Control API v1 remains unchanged. Internal recovery snapshots remain sensitive native Yard data.

Existing `POST /api/recreate/preview` and `/api/recreate/submit` also accept optional `service` for `operation:recreate` with a Compose project ID. Omission preserves whole-project behavior; submission must use the same service and reviewed fingerprint. Invalid/unknown services fail without widening to the project. MCP's existing recreate tools expose the same optional field through the shared facade.

## Yard itself

The Yard drawer uses the same editors, Pull/Recreate/Restart and daily schedule. Independent recreation stops the web process before making its SQLite rollback snapshot, applies the current host files and verifies health. Failure restores SQLite before restarting the previous runtime. Edited host files are preserved for correction. See [Self-update](Self-Update.md) for Check now, daily scheduling and the no-files fallback.

See [Development](Development.md) for direct-persistence, native-worker, browser and Engine-fallback fixtures.
