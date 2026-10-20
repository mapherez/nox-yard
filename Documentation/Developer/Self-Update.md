# Yard self-update

## Daily schedule and Check now

Stable formal [releases](Releases.md) publish the `latest` channel; prereleases and ordinary branch pushes do not replace it. Automatic Yard updates use the same daily project schedule, occurrence admission and drawer controls as other projects. First startup preserves the former automatic on/off state and creates a 03:00 slot in the server timezone. Existing edited daily times survive restarts. The old interval timer no longer runs; the legacy interval settings API remains compatible without starting another timer.

The Yard project drawer contains Compose/environment editing, Pull, Recreate, Restart and daily scheduling in the same positions as other projects. Stop/removal protection remains. **Settings → Yard updates** contains only Status, Last checked, Current build and **Check now**. Check now requests an immediate check and installation independently of daily scheduling or automatic on/off, and permits an explicit retry of a failed version.

The updater reads the public GHCR OCI index and manifest for the running Linux architecture. The manifest config digest is compared with the actual running image ID. An identical ID records the check without pulling or replacing the container.

## Current host configuration

When Compose files are available, installation applies the current host configuration through the independent file-backed worker. Pull reads those files and only downloads images. Recreate applies current files using local images without contacting the registry or building. Explicit Recreate still runs when the image is unchanged. Terminal edits and edits saved in the drawer use the same files. See [Configuration files](Configuration-Files.md).

The worker runs independently of the web container, reserves the Yard target, persists the private old runtime and retains previous images. It stops the old web process before creating a SQLite snapshot. Native Compose applies the prepared immutable image/configuration, and the worker checks startup/readiness and image identity. Successful completion removes retained image references and the temporary SQLite snapshot.

On ordinary startup/health failure, the worker stops the candidate, closes its DB connection, restores the pre-upgrade SQLite file, reopens it and restores the previous runtime. The user's edited host files remain unchanged so they can be corrected. The session and daily settings survive rollback. Changing the Yard data-directory location or replacing its persistent data/socket mounts requires a separate host migration; Recreate refuses those changes instead of starting with an unrelated empty database. Shared application writes are not reverted. Interrupted workers are observed rather than replayed; uncertain results and failed recovery retain reservations and recovery resources for explicit host review.

Scheduled file-backed replacement respects the shared failed immutable-image-set fence. A manual action can retry. The daily updater and Check now keep the existing public-GHCR check contract; they do not fetch the original Compose URL or change an explicitly pinned Compose version reference.

## Runtime fallback when files are absent

The original dedicated self-update protocol remains available if host files cannot be located. It requires the standard single Yard service, public GHCR image reference, writable persistent `/data` and Docker socket mounts, supported network configuration and `/healthz` Docker healthcheck. Self-identification uses the default Docker hostname/ID prefix. Unsupported or inaccessible existing host configuration fails explicitly instead of falling back to stale DB contents.

The fallback verifies the pulled image against the registry identity, runs a worker from the exact current image, retains the old container, snapshots SQLite after stopping the web process and preserves the assessed Engine-visible configuration. Failure restores SQLite and the previous container before reporting failure. A failed digest is suppressed automatically; Check now explicitly retries it. This fallback does not apply unavailable Compose files.

## API and recovery

Authenticated `GET /api/self-update` retains the compatibility fields `automatic`, `intervalMinutes`, `status`, `lastChecked`, `currentBuildSHA` and optional safe error. `automatic` reflects the daily row; `intervalMinutes` no longer drives a timer. The legacy `PUT /api/self-update` synchronizes on/off with the daily row without overwriting the selected time. `POST /api/self-update/check` queues immediate check/install and returns 202. Mutations require exact Origin, session and CSRF. Status values remain `not_checked`, `checking`, `up_to_date`, `updating`, `update_failed`.

Daily scheduling uses the normal authenticated project schedule API. Private jobs and the shared operation history retain ownership, stages and safe outcomes. File-backed jobs use the managed domain and `.configuration-JOB_ID.sqlite`; the absent-files fallback keeps its dedicated self-update job and `.self-update-JOB_ID.sqlite`. Treat snapshots and SQLite as sensitive. Recovery/cleanup errors remain visible in project history. Do not delete retained resources before reviewing an uncertain operation.

Schema changes must remain compatible with a worker running the previous image. The snapshot is taken only after the old web process stops and restored before that process restarts. No permanent updater service is added to Compose.

## References

- [Project schedules](Project-Schedules.md)
- [Durable operations and recovery](Operation-Jobs.md)
- [SQLite VACUUM INTO](https://www.sqlite.org/lang_vacuum.html)
