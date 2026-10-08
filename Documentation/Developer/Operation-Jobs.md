# Durable operations and recovery

Managed Compose, Engine lifecycle/pull/removal, Yard restart and self-update share SQLite operation records and resource reservations. Browser, MCP and Control API mutations acquire the same reservations. A project operation reserves its Compose identity and current container IDs; a container action also reserves its Compose project. Yard operations reserve `yard:self`. These reservations coordinate Yard requests; they cannot prevent external Docker commands.

## Persisted contract

Schema version 7 preserves earlier managed and self-update records without assuming that running work failed. Jobs keep `running`, `succeeded` and `failed` status, with additive target/domain, stage, outcome, worker identity, source/target image identities, execution timestamps, deadline, rollback and cleanup fields. Legacy records can have incomplete identities/timestamps. A successfully restored rollback still has failed status. Managed automatic rollback and unchanged-image detection are C3 work.

Source definitions, environment files, variables, resource keys and ownership tokens remain private. Public history excludes the execution payload and token; migrated managed errors and new operation errors use safe messages. SQLite contains sensitive inputs and must remain private. The dedicated self-update status retains its existing diagnostic contract.

Browser routes require a session: `GET /api/jobs?target=compose:NAME` (or `container:FULL_ID`) returns the latest 30 operations, and `GET /api/jobs/{id}` returns one record. Project history includes container operations that reserved that project. The existing `/api/managed/jobs/{id}` and `yard_compose_job` remain compatible lookups. MCP also exposes `yard_job`, `yard_job_history` and `yard_job_recovery_acknowledge`. Control API v1 keeps its synchronous response shapes and discovers no new routes; its actions are recorded in contextual history.

## Worker lifetime and observation

Managed operations and Yard restart run in separate temporary containers from the exact running Yard image. They mount only the writable persistent state directory at `/data` and Docker socket, use no network or published ports, and carry job/ownership labels. State may use a bind or named-volume mount. Workers claim execution once before mutation and save progress/results themselves. Accepted work survives browser disconnection or web-container stop/restart. A host `go run` process cannot launch these workers: use the development Compose backend for managed mutations and self-restart.

The web observer resumes observation after startup. A live worker or child Compose helper retains ownership. When an exited worker had reached verification/commit, the observer can verify current Docker state and complete metadata without repeating Compose. If verification cannot prove completion, the job becomes `failed` with `outcome: recovery_required`; its reservations remain held. Interrupted synchronous Engine requests also require review when their completion is unknown. No interruption automatically launches another worker or repeats a mutation.

Managed metadata finalization, terminal status and reservation release share one SQLite transaction with an ownership/status guard. A late observer or worker cannot save/delete metadata after a newer operation has acquired the project. This protects job finalization; transactional host-file staging and rollback remain C3 work.

General workers are hidden/protected from inventory actions and removed only after a durable result and worker/helper exit. `cleanupError` reports cleanup trouble separately from operation success/failure. Self-update retains its independent SQLite snapshot/rollback protocol and automatically removed helper; its outcome, stages and ownership are mirrored into general history.

## Recovery review

1. Read the contextual Operations section or job record. Identify the target, stage, worker and source/target images.
2. Inspect the target on the Docker host, its current health/container state, host Compose/env files and retained helper/rollback resources. For self-update, inspect the dedicated status and SQLite snapshot described in [Self-Update](Self-Update.md). Restore or clean up resources as appropriate before admitting another operation.
3. After that review, select the host-review checkbox and **Acknowledge recovery**. The browser sends `POST /api/jobs/{id}/recovery` with `confirm:true` and the reviewed `updatedAt`; session, Origin and CSRF protection apply. MCP accepts the same confirmation/revision.

Acknowledgement fails while a worker/helper is active or the reviewed record changed. It releases reservations and records `recovery_acknowledged`; it does not change containers/files, restore data or convert failure into success. Late worker writes cannot overwrite a terminal result. Reloading/reopening the drawer reads the persisted history; unavailable history keeps mutation controls blocked and offers Retry.

Back up SQLite consistently together with host project files and application volumes/binds. Coordinate backup/restore with active workers as well as the web process. Restoring SQLite alone cannot undo Docker operations or application data writes; inspect and reconcile the restored installation before admitting mutations.

## Isolated acceptance

`scripts/smoke-jobs.py --image IMAGE` uses a derived fixture image to gate Compose commands, random project/container/volume identities and temporary loopback ports. It tests web interruption during pull/replacement/verification, cross-adapter conflicts, vanished workers, verified completion without replay, live child-helper ownership, reviewed recovery and secret-free history. Cleanup is restricted to fixture IDs/directories. `scripts/smoke-history.cjs` exercises browser reload, keyboard review, recovery failures/retry/focus and responsive layouts through the Playwright CLI. See [Development](Development.md) for commands and [Progress](Progress.md) for completed evidence and host limits.
