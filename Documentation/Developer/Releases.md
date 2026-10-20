# Formal releases

Run from the repository root with no modified, staged or untracked files:

```sh
npm run release
```

The script shows the current version from the root `package.json` and asks for the new version. Enter a SemVer such as `1.3.0` or `1.3.0-rc.1`; the normal release flow starts after that answer. Empty input, EOF or Ctrl+C cancels before making changes. An optional initial `v` is normalized. The root `package.json` stores SemVer without `v`; Git uses an annotated `v<version>` tag. Build metadata (`+...`) is not supported. Explicit arguments remain supported for automation: `npm run release -- X.Y.Z`.

Clean source control does not mean every commit must already be pushed. Clean local commits ahead of the remote are supported: the script adds the version-only release commit and sends the branch history plus its annotated tag in the same atomic push. If the remote contains commits missing locally, fetch/integrate them first; Git will reject a non-fast-forward push. The script never force-pushes. Avoid running VS Code Push/Sync and the release command at the same time.

## One pipeline, publication after CI

Every branch push starts `.github/workflows/ci.yml`, including pushes to feature branches. Tag pushes do not start a separate pipeline. There is no second pull-request trigger for the same branch push.

The pipeline runs in this order:

1. **Source validation** runs the shared Go, release-tooling and frontend checks.
2. **Docker builds and ARM64 smoke test** builds both architectures and tests the ARM64 executable and runtime.
3. **Release after successful CI** calls the reusable `.github/workflows/release.yml` only when the script requested publication for this exact commit. Failure or cancellation of either CI gate prevents this call.

The release workflow has only `workflow_call`; it cannot independently start on a tag push or a manual dispatch. It checks the request identity again, publishes the multi-architecture image, then creates the GitHub Release. It does not repeat source tests. Docker publication imports the architecture-specific BuildKit caches populated by CI to reuse build layers. Publication uses the exact CI SHA and validated release tag, even if the branch advances.

## What the command does

The command requires Node.js from `.nvmrc`, Git, a usable Git identity and permission to push the current branch and tags to `origin`. The remote must support atomic pushes; branch protection still applies. Project validation runs only after the push in CI. An older clone must retire the legacy Yard hook with `sh scripts/install-hooks.sh`; that command preserves custom hooks. No Go, Docker, frontend install or Chrome is needed locally for the release command itself. CI remains the publication gate.

Commit implementation changes first. The command rejects tracked, staged or untracked changes, detached HEAD, an unchanged version, an existing local/remote tag, or an inaccessible origin. It updates only `package.json`, creates `chore: release v<version>` with the `NoX-Yard-Release: v<version>` trailer, creates the annotated tag with message `Release v<version>`, and atomically pushes the branch and tag. It does not explicitly rerun CI or build a validation image locally.

CI accepts the request only when the marker, commit subject, package version, annotated tag, annotation and exact target SHA agree, and the release commit changes only `package.json`. An ordinary version bump, old release commit without the marker, or manually pushed tag does not request publication. These are provenance checks within the repository's existing trusted-push model, not a cryptographic proof of which client command was used.

| Version | GHCR tags | GitHub Release |
| --- | --- | --- |
| `1.2.0` | `v1.2.0`, `latest` | Stable |
| `1.2.0-rc.1` | `v1.2.0-rc.1` only | Prerelease |

Stable publication cannot move `latest` behind a newer published stable version. Installation and Yard self-update continue to use `ghcr.io/mapherez/nox-yard:latest`.

## Duplicate runs and failure recovery

The entire caller workflow uses one repository-wide `nox-yard-pipeline` concurrency group with `cancel-in-progress:true`. A newer push cancels the previous pipeline and replaces an older pending run, across branches and commits. Source checks, Docker builds/smoke and publication share that one lane. Publication starts only after both CI gates succeed for the exact source commit. The reusable workflow defines no second concurrency group, avoiding self-cancellation or overlap with another pipeline. See [GitHub concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency). The pipeline cannot prevent GitHub from creating duplicate event deliveries, but a duplicate publication of the same completed release becomes a successful skip. Each new release records its source SHA in a hidden body marker after the image push completes. Existing drafts, legacy releases without this marker, or mismatching markers remain conflicts and are never overwritten.

A new push can cancel a run during publication too. Cancellation does not undo image tags or a GitHub Release already written. Inspect that state before retrying; matching completed publication is skipped and conflicting metadata remains blocked.

Before a release commit exists, a preparation/commit failure restores `package.json` byte-for-byte and unstages only its change. Once a release commit exists, it and any created tag are preserved on failure. The script prints exact recovery commands; inspect the local state and use those commands instead of rerunning with an existing tag. It never automatically resets commits or deletes tags.

After a successful push, inspect the **CI** run, including its dependent Release job. If CI fails, fix the code and issue a new release request, or rerun CI when the failure was environmental and the commit remains valid. Do not push an old tag expecting an independent Release workflow. If image publication succeeds but GitHub Release creation fails, the image remains; retry the failed job only after inspecting that state. The current request identity and downgrade/conflict guards still apply.

### Reuse an unpublished version after a source correction

Re-running an existing GitHub Actions run uses its original SHA/ref, so it cannot pick up a later source fix. A failed pre-publication attempt does not require consuming a new version number. Reuse is appropriate only after confirming the original pipeline is completed, its publication jobs were skipped, and neither a GitHub Release nor a GHCR image exists for that version. If any publication exists, preserve its tag and use a new version, or recover the matching original publication as described above.

For the failed `v1.3.1` attempt, the preceding published version is `1.3.0`. After checking the conditions above, run from the repository root in PowerShell:

```powershell
# Restore the preceding version metadata before committing the source correction.
npm pkg set version=1.3.0
```

Commit the source corrections, tests, documentation and `package.json` together. Pending local commits are supported; they need not be pushed separately. Then remove the unpublished attempt's tag, guarded against an unexpected remote change, and create the fresh request:

```powershell
$failedTagObject = git rev-parse refs/tags/v1.3.1
if ($LASTEXITCODE -ne 0) { throw 'Cannot identify the failed local tag.' }
git push "--force-with-lease=refs/tags/v1.3.1:$failedTagObject" origin :refs/tags/v1.3.1
if ($LASTEXITCODE -ne 0) { throw 'Remote tag changed or deletion failed; stop and inspect.' }
git tag -d v1.3.1
if ($LASTEXITCODE -ne 0) { throw 'Local tag deletion failed; stop and inspect.' }
npm run release -- 1.3.1
```

This keeps the branch history and source corrections. Only the unshipped attempt's tag is replaced; the normal release script creates a new version-only commit and annotated tag, then atomically pushes the branch/tag to start fresh CI followed by publication. The old failed run still targets the old commit and is not the retry mechanism. A disposable Git integration test covers this recovery path and verifies the new tag contains the corrected source.

## Tooling validation

`node --test scripts/release.test.mjs` uses disposable Git repositories and local bare remotes. It exercises real commits, annotated tags, atomic pushes, failed commits/pushes, request identity, normal-push exclusion, metadata, duplicate publication, conflict/downgrade handling and the documented same-version recovery after an unpublished source failure. Tests never publish to GitHub or GHCR. These tests also run in `scripts/ci-local.sh` and normal CI. Use actionlint to validate the caller/callee workflow syntax and job dependencies.
