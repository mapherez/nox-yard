# Project automatic image updates

Project schedules are disabled by default and independent of Yard self-update. In the project's Details drawer, select **Enable automatic updates for this project**, choose **Daily check time**, and select **Save automatic updates**. Enabling requires a supported healthy running project; unsupported or stopped projects must be reviewed through the manual update path. Back up application data before enabling.

## Time and missed checks

Checks run daily at **the time selected for each project, in the server timezone**. The default is 03:00; existing schedules retain that time on migration. Times use HH:MM, from 00:00 to 23:59, and persist across restarts. A Compose project uses one schedule for all its containers; standalone containers have independent schedules. Compose maps `.env`'s `NOX_TIMEZONE` to the backend's `TZ`; both installation and development default to `Etc/UTC`. Use an IANA name such as `Europe/Lisbon`; an invalid name prevents backend startup. Direct local runs use `TZ`, defaulting to UTC. The drawer and adapters show the effective timezone, next due check and last result; browser time does not define the schedule. DST uses civil dates rather than an elapsed 24 hours. A nonexistent spring time moves forward by the DST gap; a repeated autumn time uses its first occurrence and runs at most once for that civil date. Timezone changes apply after server restart.

The server checks due schedules at startup and every minute after its previous check. The selected time is the due time, not a promise of simultaneous replacement. SQLite schema 9 keeps project settings and unique `(schedule key, civil date)` occurrences. Occurrence consumption, existing update job creation and resource reservations commit together. Restart and clock movement cannot repeat a recorded civil date. Missed days coalesce into one check for the latest passed configured slot per project, rather than replaying every missed day. Enabling starts at the next future slot; changing the time recalculates the next future slot, while unchanged saves preserve the current next occurrence. Changing the time does not repeat a civil date already consumed.

## Execution and failures

Automatic checks assess the same supported runtime/source contracts as [managed updates](Managed-Updates.md) and [external updates](External-Updates.md). They pull and compare immutable image identities, preserve containers when unchanged, and use independent workers, verification and bounded rollback when changed. They do not fetch/synchronize source URLs, change Compose definitions, or start stopped projects. Declared successful one-shot dependencies may be rerun by a changed deployment when its long-running services are running. Yard/helpers always use their dedicated management path.

Only one automatic worker is admitted globally, including pending cleanup/recovery. A tick considers at most four due schedules and gives each assessment a 30-second context; update workers retain their existing deadlines. Other due projects remain deferred with a persisted reason. Manual actions keep their normal reservations. Busy, absent, stopped, unhealthy, unsupported or changed targets receive a persisted skipped occurrence with a safe reason and no mutation locks. Docker unavailability also skips without starting work.

A worker stores its immutable service/platform candidate set privately before replacement. The last known failed set is suppressed on future automatic checks, including after restart or disable/re-enable; pulls still discover new images. A different set can proceed. An explicit manual image update bypasses this suppression, and a verified/unchanged successful update clears the failed set. Reviewed recovery retains the failure fence and does not report success. Rollback stays failed with `rolled_back`; an uncertain result stays reserved with `recovery_required` and needs [host review](Operation-Jobs.md).

Disabling is checked before pull and immediately before replacement; it cannot undo a replacement already in progress. Successful removal disables the matching schedule. Standalone schedules follow Yard's guarded replacement lineage/new target ID. An out-of-band removal or recreation may leave an absent target scheduled; its check skips rather than matching a new container by name. Disable or explicitly opt in the new target after review.

Rollback restores the supported container/configuration state, not application writes or schema migrations. Named volumes/binds survive, while writable layers/tmpfs are not copied. External Docker commands remain outside Yard's reservations.

## Adapters and history

- `GET /api/projects/{id}/schedule` requires a browser session.
- `PUT /api/projects/{id}/schedule` requires session, exact Origin, CSRF and `{ "enabled": true|false, "time": "06:45" }`; the flag is required, and omitting `time` preserves the saved time.
- MCP tools `yard_project_schedule_get` and `yard_project_schedule_set` expose the same setting/status, with required `id` and set's `enabled` flag, plus optional `time` in HH:MM. Existing MCP access rules apply.
- Control API v1 is unchanged. Yard self-update settings are separate.

IDs are `compose:NAME` or `container:FULL_ID`. Status includes `targetID`, `enabled`, `time`, `timezone`, `eligible`, optional availability/protection `reason`, and persisted `nextAt`, `lastAt`, `lastOutcome`, `lastReason`, `lastJobID`. Times are Unix seconds. For an admitted check, `lastAt` identifies its due slot; job execution timestamps record when work actually ran. A deferral records the time of its first check. Eligibility indicates availability/protection; enabling also performs full runtime/source assessment. Missing rows read disabled without creating a schedule.

Contextual history retains unchanged, verified, skipped, rollback and recovery results. Public jobs add `scheduledFor`; occurrence keys, candidate/failed fingerprints and execution payloads remain private. A skipped check is a terminal `schedule`-domain update job and does not acquire mutation reservations. Global deferral remains due and is shown in schedule status without creating a duplicate job. See [Development](Development.md) for isolated Docker/browser validation and [Progress](Progress.md) for evidence and remaining real-host checkpoints.
