# NoX Yard self-update

## Scope and data flow

Self-update is off by default. When enabled, the service checks `ghcr.io/mapherez/nox-yard:latest` immediately and then at most once every six hours. It requests the public GHCR OCI index and the manifest for the running Linux architecture. The manifest's config digest is the Docker image ID. If that ID matches the running container, no image is pulled or container changed.

When the IDs differ, the service persists an `updating` job in SQLite, pulls `latest` through the Docker Engine API, and verifies the pulled image ID against the manifest. It then creates a temporary worker container from the **exact current image ID**. The worker has only the persistent `/data` mount and Docker socket; it has no published ports or network. Docker automatically removes the worker when it exits.

The worker validates the existing Compose container and its `/healthz` Docker healthcheck, renames the old container as a rollback reserve, stops it, and creates a SQLite snapshot. It creates the replacement with the original name, mounts, port bindings, restart policy, Compose labels, and network settings, using the verified new image ID. It waits for Docker to report the `/healthz` healthcheck as healthy. Only then does it persist success, remove the old container, and remove the database snapshot. If removal fails after the new instance is healthy, the worker logs the cleanup error and leaves the stopped old container for manual removal.

If creation, startup, or health fails, the worker removes the replacement, restores the SQLite snapshot and local `latest` tag, gives the old container its original name, restarts it, and records the error. The failed platform-manifest digest is not attempted again automatically. Turning automatic updates off and on explicitly retries it. Errors from a failed rollback are appended to the job error and require host intervention.

## Requirements and boundaries

- The package must be publicly readable on GHCR. Private-package credentials are not stored by NoX Yard.
- The app must run as the standard Docker Compose service sourced from `ghcr.io/mapherez/nox-yard:latest`, with writable bind mounts for `/data` and `/var/run/docker.sock`, exactly one Docker network, and a `/healthz` healthcheck. A self-updated container uses the verified image ID internally. The updater refuses to modify an installation that does not satisfy the other preconditions.
- Self-identification uses Docker's default container hostname (the container ID prefix). The replacement lets Docker assign a fresh hostname.
- The replacement preserves the current Engine-visible container configuration. Changes to `compose.yaml` on the host still require a later `docker compose up -d` to take effect.
- New image defaults for environment variables or commands do not automatically replace the existing Compose container settings. Keep image changes compatible with the running service configuration, and use a normal Compose redeploy when that configuration must change.
- The worker and web process share SQLite through WAL mode. The worker's snapshot is taken after the old web process stops. Rollback restores the previous database file before the old container restarts.
- A helper killed by Docker or the host during the narrow replacement window cannot run rollback code. The stopped old container and SQLite snapshot are intentionally retained for manual recovery in this exceptional case. Normal operation errors follow automatic rollback.

## API and UI

Authenticated `GET /api/self-update` returns `automatic`, `status`, `lastChecked`, optional `currentBuildSHA`, and an error when applicable. `PUT /api/self-update` accepts `{"automatic":true|false}` and requires the same Origin and session CSRF checks as other mutations. Status values are `not_checked`, `up_to_date`, `updating`, and `update_failed`. The dashboard polls the status while visible. The build SHA is injected by GitHub Actions at image build time; local builds may omit it.

SQLite schema version 2 adds `self_update_settings` and `self_update_jobs`. Jobs record the old/target image IDs, platform-manifest digest, outcome, timestamps, and failure. This is the canonical record across web-service restarts; no updater service is added to Compose.

## Maintenance

Keep the healthcheck and persistent mount contract stable when changing the Dockerfile or Compose file. Future database migrations should remain backward compatible during an update attempt; the SQLite snapshot protects a failed replacement. Do not add Docker CLI or Compose to the runtime image for this mechanism.

For a failed attempt, inspect the dashboard error and the latest row in `self_update_jobs`. After fixing an environmental issue, switch Automatic updates off and on to retry the same image. If a helper is unexpectedly terminated during replacement, inspect Docker for the original `*-rollback-<job-id>` container and the `.self-update-<job-id>.sqlite` file under the mounted data directory before attempting manual recovery.

## References

- [Docker Registry manifest API](https://docs.docker.com/reference/api/registry/latest/operations/HeadImageManifest/)
- [SQLite `VACUUM INTO` snapshot semantics](https://www.sqlite.org/lang_vacuum.html)
