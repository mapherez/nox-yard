# Isolated host acceptance

Use this procedure on the Raspberry Pi 5 and a designated Linux amd64 host. Docker Desktop and ARM64 emulation are local preparation evidence; they do not close the physical-host gates in [the completion plan](Completion-Plan.md). Record results in [Progress](Progress.md).

## Prepare a versioned Pi 5 handoff

Prepare a fresh handoff from the exact revision under review. The old `.tmp/c6-pi5/` bundle and `c6-*-20261008` images describe historical preparation and must not be used as evidence for newer source. Rebuild and test matching current/baseline images first, use a new output directory, and retain the manifest with their exact IDs, source commit, dirty state and archive checksum. The packaging script includes tracked/untracked non-ignored source and excludes ignored data, dependencies and local secrets; do not reuse an older archive after code or toolchain changes.

To reproduce the handoff after building/testing the images from the current source, run `python scripts/prepare-host-validation.py --image nox-yard:validation-arm64-20261020 --legacy-image nox-yard:baseline-v1.0.1-arm64 --output .tmp/host-validation-20261020` from a Git checkout. It refuses an existing output directory, includes current tracked/untracked non-ignored source, normalizes text to LF and records whether the source has uncommitted changes. Keep image/source versions aligned before packaging.

Copy that directory to a new directory on the Pi, for example `~/nox-yard-c6-validation`. Prerequisites are a 64-bit Linux OS, Docker with its Compose plugin, Python 3, curl and access to public fixture/build images. The fixture suite builds Linux Go runners and retains Go build/module cache volumes; allow time and disk space for these images on the Pi. Browser checks run on a desktop against the Pi's validation instance.

Run from the transferred directory:

```sh
uname -m
# Expected: aarch64. Confirm the model is Raspberry Pi 5.
tr -d '\000' < /proc/device-tree/model
sha256sum -c SHA256SUMS
docker load -i nox-yard-c6-pi5.tar
python3 -m zipfile -e nox-yard-c6-source.zip .
cd nox-yard-c6-source
python3 scripts/acceptance.py \
  --image nox-yard:validation-arm64-20261020 \
  --legacy-image nox-yard:baseline-v1.0.1-arm64 \
  --report .tmp/pi5-acceptance-results.json
```

Keep the resulting JSON report. It records the host model/kernel, Engine version, exact image IDs and each gate's outcome/duration. `complete` means the isolated runtime gates passed; the manual host checklist below remains separate. The harness refuses images for the wrong Engine architecture and stops on the first failed gate. It runs seven legacy suites; additionally run `python3 scripts/smoke-configuration.py --image CURRENT` and `python3 scripts/smoke-service-recreate.py --image CURRENT` for current file-backed Yard recovery and service scope. Those two suites are not yet aggregated into its report. Repeat an individual script to diagnose a failure, then rerun the suite for a complete report.

The suite creates random fixture identities and deletes only its own recorded containers, networks, volumes, helper images and temporary source directories. It does not select an existing Yard or user stack. It exercises:

- Real v1.0.1 initialization/deployment; schema 6→9; preserved account/session, managed source/variables/env files, history and unchanged application containers/data.
- Interrupted legacy job reconciliation and explicit recovery acknowledgement after inspecting the disposable target; missing host-directory records fail without deploying guessed paths.
- The actual password-reset CLI through a Linux pseudo-terminal, no password echo, refusal of non-interactive input, revocation and persistence.
- Three-part backup/restore of SQLite, original source/env/bind files and representative named-volume data, followed by file-configuration and credential verification.
- Durable jobs/interruption, managed/external/standalone updates and rollback, import/adoption, HTTP/MCP, removal protections and schedules with real independent workers.
- Successful Yard replacement and failed replacement recovery, including an actually committed incompatible SQLite change, restored administrator/session and failed-image suppression.

The self-update fixture redirects only its compiled restore-tag constant to a private local tag. It launches the real independent worker from the previous image and runs the replacement/health/SQLite rollback protocol. Official GHCR discovery/pull and publication are outside this fixture gate; verify that release path separately in C7. The shipped application has no test override for the official registry.

## Persistent validation instance for browser checks

After the isolated suite passes, create a separate instance with its own state, name and port. The following file is included in the source archive:

```sh
mkdir -p .tmp/pi5-ui/data
NOX_VALIDATION_IMAGE=nox-yard:validation-arm64-20261020 \
  docker compose -f scripts/fixtures/host-validation.yaml up -d
```

The default port is 18096 and the project/container are `nox-yard-c6-validation` / `nox-yard-c6-validation-yard`. The Compose file refuses an unset validation image and uses `.tmp/pi5-ui/data` relative to the source root. If this name or port is already occupied, choose a different validation name/port through the documented variables in that file. The regular production Compose file is not involved. Access is intended for the trusted LAN or VPN.

Keep Yard automatic self-update off while using unpublished validation images; the isolated worker fixture tests replacement, and C7 verifies the official registry path. Open `http://PI_ADDRESS:18096` in the desktop browser. Create a disposable administrator, then set the projects directory to the absolute host path of a new fixture-only directory, such as `.../nox-yard-c6-source/.tmp/pi5-ui/projects`. Record:

| Check | Evidence to keep |
| --- | --- |
| First run, concurrent setup, login/logout and restart | One account, persisted state and expected sign-in behavior. The automated auth/store regressions also verify concurrency, rate limit, expiry, Origin/CSRF and secure cookies. |
| Inventory and lifecycle | Running/stopped disposable Compose and standalone targets; health, uptime and metrics; realtime refresh after external changes; unavailable target feedback. |
| Logs and terminal | Logs in the drawer, lazy shell check, working shell and missing `/bin/sh`; close/reopen and keyboard focus. Reset the validation account while both streams are open: the UI returns to sign-in and releases connections. |
| Import | `scripts/fixtures/managed-acceptance.yaml` by paste and upload; review variables/env files, named volume and relative bind before deployment. For real public URL intake, host the same fixture at a public HTTPS URL accessible by Yard and record URL copy/sync/cancel separately; injected fixture URL tests do not prove the network path. |
| Updates/recovery/removal | Review healthy/unchanged and induced rollback outcomes/history from the isolated suite; reload/reopen during a sample job; explicit volume choice, shared resources, stale previews and protected Yard/helpers. |
| Schedules | Default off, explicit opt-in, displayed `Europe/Lisbon` timezone and next check; real daily operation through a local 03:00 boundary. The isolated scheduler test also supplies times inside the Go test for missed/DST/concurrency/failure cases without a production clock override. |
| Responsive/accessibility | 1440, 768, 390 and 320 px; keyboard navigation, Escape, focus return, reduced motion and retry/error states. |
| Docker connectivity | On a dedicated disposable Engine, unavailable Docker and recovery must show an error followed by fresh authoritative inventory. Do not restart/disconnect the host daemon if it would interrupt unrelated user stacks; leave this case pending or use a designated isolated Engine. |
| HTTPS | If the supported deployment uses a proxy, configure the exact `NOX_PUBLIC_URL` through `NOX_VALIDATION_PUBLIC_URL` and confirm `__Host-nox_session`, Secure/HttpOnly/SameSite=Strict, accepted same-origin mutations and rejected other origins. |

To reset only the validation administrator interactively:

```sh
docker exec -it nox-yard-c6-validation-yard nox-yard reset-admin-password
```

To stop this validation instance while retaining its data:

```sh
NOX_VALIDATION_IMAGE=nox-yard:validation-arm64-20261020 \
  docker compose -f scripts/fixtures/host-validation.yaml down
```

Remove any manually created sample stacks using their explicit fixture names. The validation directory remains for review/backup. Do not apply these lifecycle checks to existing deployed projects.

## Native Linux amd64

Use the same source and scripts with native amd64 images. Use freshly tested tags such as `nox-yard:validation-amd64-20261020` and `nox-yard:baseline-v1.0.1-amd64`; transfer them with `docker save` / `docker load`, or rebuild from the same source snapshot and an archived `v1.0.1` tag. Run `scripts/acceptance.py` with those two references and retain a separate host report. A Windows client using Docker Desktop is not this real-host checkpoint.

## Backup and upgrade boundaries

Yard state, original project directories and application storage are separate backup parts. Before a real upgrade, stop admissions, disable automatic schedules/self-update, let workers finish, stop Yard and quiesce application writers. Record image IDs and the exact bind/volume layout. Take a consistent SQLite backup, preserve original source/env/interpolation settings and ownership/permissions, and use each application's supported data backup method. Protect backups as credentials: SQLite and env files contain authentication state and secrets.

Restore into an isolated instance first. Preserve the original host paths, volume identities and directory roots for bind mounts; restore file contents in place. Deleting/recreating a bind-mounted directory while containers exist can leave their mounts pointing at the old directory inode. Do not assume reverting the image downgrades schema 9 to schema 6: reverting to v1.0.1 requires its pre-upgrade database backup and corresponding source/application data. Yard's self-update rollback snapshots SQLite and restores the previous Yard container; it is not an application-data rollback.

Current Docker labels and saved file locations identify the original directory and ordered Compose filenames. Restore/locate missing source files and validate them through the file editors before applying changes. The one-off configuration migration has been retired; installations with DB-only configuration must finish conversion on the preceding release before upgrading. See [Configuration files](Configuration-Files.md). Restoring SQLite also restores historical password hashes and sessions. After a restore, run the interactive reset if those credentials/sessions should no longer be valid.

Keep the image IDs, source archive checksum, host report and completed manual checklist together. C6 remains open until both designated native hosts and the original acceptance cases have evidence; C7 handles CI/publication gates and the formal release.
