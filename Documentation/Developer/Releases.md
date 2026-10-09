# Formal releases

Run from a completely clean repository root on `master`:

```sh
npm run release -- 1.2.0
```

Git, Node.js 24 and Go 1.26.6 are the only local prerequisites. No Docker Desktop, Buildx, Compose, frontend `npm ci` or browser is needed. Install the lightweight hook with `npm run hooks:install`; custom hooks are preserved. The release preflight still validates when the hook is absent.

The version accepts an optional `v` prefix and SemVer prereleases, but not build metadata (`+...`). Root `package.json` is the version source; there is no VERSION file. The command rejects other branches, detached HEAD, any tracked/staged/untracked changes, missing Git identity, unchanged version and existing local/remote tags. Inaccessible remotes abort.

## Local preparation and remote order

The command changes only package.json, runs the same lightweight checks as push, commits the version and creates an annotated `v<version>` tag. The branch and tag are pushed atomically. Its pre-push hook recognizes the exact tree/base already checked by this process, avoiding duplicate validation without bypassing custom hooks.

Commit implementation changes and then run this command directly when publishing. An extra normal push first would start CI that the release subsequently cancels.

Only `pipeline.yml` receives push/PR events. It detects a release when the package version's annotated tag points exactly at the pushed master SHA. Tags do not launch a second workflow. `release.yml` calls `ci.yml`, waits for all common checks/builds, runs selected acceptance groups, then publishes. Ordinary pushes/PRs run that same reusable CI without publication. A manual tag-only push does not initiate publication: use the release command. Workflow dispatch on master supports `full_acceptance`; it publishes only when the selected master commit is a release candidate. Recover historical candidates by rerunning their original pipeline, not dispatching a newer master commit.

A release cancels active/pending normal CI for ancestor master commits, including the legacy branch CI during migration. It preserves other releases, current/newer/unrelated runs and completed runs. Only the trusted master cancellation job receives `actions: write`; PRs never cancel or publish. PR cancellation remains enabled.

## Build once and select acceptance

CI runs Go vet/tests, delivery-tooling tests, vulnerability analysis for both Linux targets, production frontend build and four browser fixtures, Compose validation, and AMD64/ARM64 executable/health checks. Frontend and Go jobs run in parallel. Production binaries and frontend assets are compiled once and packaged with the shared runtime Dockerfile target. The publisher consumes tested image archives, validates their SHA/version/run/architecture/checksum/image IDs and OCI labels, and assembles the registry manifest without builds.

Acceptance compares the complete candidate diff with the highest published stable SemVer ancestor, not the version-only release commit. A root package change affecting only version is ignored for runtime selection. Groups are combined:

| Changed area | Docker acceptance |
| --- | --- |
| Frontend/docs | None |
| Schedule-specific files | Schedules |
| Managed projects | Managed, MCP, schedules |
| Recreation | Recreate, MCP, schedules |
| Self-update-specific files | Self-update |
| Inventory/lifecycle/MCP | MCP |
| Shared workers/jobs, auth/schema, dependencies, runtime, delivery policy or unknown files | All seven |

Core managed manager/worker/readiness changes conservatively select all because they cross these subsystem boundaries. Changes to acceptance fixtures run their corresponding group. Missing baseline or explicit full selection also runs all. The policy summary records baseline, groups and reasons. Isolated Linux runners execute groups in parallel and retain timing/result reports for 14 days. Fixture scripts run with sudo on these disposable runners so they can clean root-owned private state/journals; this does not affect local push/release prerequisites. Historical v1.0.1 source/image is built with its own revision-keyed cache only for the upgrade gate. Go module/build caches are shared with fixture runner volumes; production images are not rebuilt for acceptance. Some fixtures deliberately compile modified/test binaries to exercise failure injection, separately from the shipped image.

An equivalent explicitly requested host check is:

```sh
python scripts/acceptance.py --image CURRENT --groups schedules --report .tmp/schedules.json
# Full suite also needs the historical image:
python scripts/acceptance.py --image CURRENT --legacy-image LEGACY
```

These automated gates do not replace native Pi 5 and manual host acceptance.

## Publication and recovery

| Version | Official image tags | GitHub |
| --- | --- | --- |
| Stable | `v<version>` and `latest` | Release |
| Prerelease | `v<version>` only | Prerelease |

Technical `build-<SHA>-amd64/arm64` tags hold uploaded platform images; official manifests reference their immutable digests. OCI labels contain version, full release commit SHA, source and creation time. The two official platform image IDs must match the tested artifacts. Publication is serialized with `queue: max`; pending releases are retained. Both GitHub releases and the registry latest label guard against moving latest backwards, including partial publication failures. Existing version tags with different images are refused; matching images/releases permit safe completion of a partial retry. Registry verification happens before GitHub Release creation.

Before a release commit exists, failure restores package.json byte-for-byte and unstages only the command's change. Unexpected check changes remain available for inspection. After the commit, failures preserve local commit/tag and print recovery commands. There is no automatic reset, deletion, tag replacement or force push.

On GitHub, use **Re-run failed jobs** to reuse successful CI jobs and tested architecture artifacts, including artifacts from earlier attempts of the same run. Release image/backend/frontend artifacts expire after seven days (normal CI keeps intermediate binaries/assets for one day and does not upload image archives); expired artifacts require rerunning the validation/build jobs. Retrying just publication performs no build or test suite. If image publication succeeded but GitHub Release creation failed, a retry verifies the existing images and completes the missing release. Rerunning all jobs can change image IDs through newer base-image/dependency layers; it cannot overwrite an already published version.

## Rollout and evidence

Branch protection should require **Pipeline result**, replacing the previous source/Docker checks; repository settings are not modified by these scripts. Inspect the first real GitHub run's source/build/acceptance timings and registry results. Locally passing delivery tests and YAML validation do not establish remote runner or GHCR success. No real commit, push, release or host deployment is implied by this refactor.
