# Formal releases

Run from the repository root with a completely clean working tree:

```sh
npm run release -- X.Y.Z
```

Prereleases use the same command, for example `npm run release -- 1.2.0-rc.1`. An optional initial `v` is normalized. The root `package.json` stores SemVer without `v`; Git uses an annotated `v<version>` tag. Build metadata (`+...`) is not supported.

## One pipeline, publication after CI

Every branch push starts `.github/workflows/ci.yml`, including pushes to feature branches. Tag pushes do not start a separate pipeline. There is no second pull-request trigger for the same branch push.

The pipeline runs in this order:

1. **Source validation** runs the shared Go, release-tooling and frontend checks.
2. **Docker builds and ARM64 smoke test** builds both architectures and tests the ARM64 executable and runtime.
3. **Release after successful CI** calls the reusable `.github/workflows/release.yml` only when the script requested publication for this exact commit. Failure or cancellation of either CI gate prevents this call.

The release workflow has only `workflow_call`; it cannot independently start on a tag push or a manual dispatch. It checks the request identity again, publishes the multi-architecture image, then creates the GitHub Release. It does not repeat source tests. Docker publication imports the architecture-specific BuildKit caches populated by CI to reuse build layers. Publication uses the exact CI SHA and validated release tag, even if the branch advances.

## What the command does

The command requires Node.js from `.nvmrc`, Git, a usable Git identity and permission to push the current branch and tags to `origin`. The remote must support atomic pushes; branch protection still applies. An installed local pre-push hook may require the other project toolchains and run local checks once. CI remains the publication gate.

Commit implementation changes first. The command rejects tracked, staged or untracked changes, detached HEAD, an unchanged version, an existing local/remote tag, or an inaccessible origin. It updates only `package.json`, creates `chore: release v<version>` with the `NoX-Yard-Release: v<version>` trailer, creates the annotated tag with message `Release v<version>`, and atomically pushes the branch and tag. It does not explicitly rerun CI or build a validation image locally.

CI accepts the request only when the marker, commit subject, package version, annotated tag, annotation and exact target SHA agree, and the release commit changes only `package.json`. An ordinary version bump, old release commit without the marker, or manually pushed tag does not request publication. These are provenance checks within the repository's existing trusted-push model, not a cryptographic proof of which client command was used.

| Version | GHCR tags | GitHub Release |
| --- | --- | --- |
| `1.2.0` | `v1.2.0`, `latest` | Stable |
| `1.2.0-rc.1` | `v1.2.0-rc.1` only | Prerelease |

Stable publication cannot move `latest` behind a newer published stable version. Installation and Yard self-update continue to use `ghcr.io/mapherez/nox-yard:latest`.

## Duplicate runs and failure recovery

Source/build job concurrency is scoped to branch and SHA: duplicate deliveries may cancel redundant validation, while different pushed commits retain their checks. Publication is serialized globally and is never canceled by validation concurrency. The pipeline cannot prevent GitHub from creating duplicate event deliveries, but a duplicate publication of the same completed release becomes a successful skip. Each new release records its source SHA in a hidden body marker after the image push completes. Existing drafts, legacy releases without this marker, or mismatching markers remain conflicts and are never overwritten.

Before a release commit exists, a preparation/commit failure restores `package.json` byte-for-byte and unstages only its change. Once a release commit exists, it and any created tag are preserved on failure. The script prints exact recovery commands; inspect the local state and use those commands instead of rerunning with an existing tag. It never automatically resets commits or deletes tags.

After a successful push, inspect the **CI** run, including its dependent Release job. If CI fails, fix the code and issue a new release request, or rerun CI when the failure was environmental and the commit remains valid. Do not push an old tag expecting an independent Release workflow. If image publication succeeds but GitHub Release creation fails, the image remains; retry the failed job only after inspecting that state. The current request identity and downgrade/conflict guards still apply.

## Tooling validation

`node --test scripts/release.test.mjs` uses disposable Git repositories and local bare remotes. It exercises real commits, annotated tags, atomic pushes, failed commits/pushes, request identity, normal-push exclusion, metadata, duplicate publication and conflict/downgrade handling. Tests never publish to GitHub or GHCR. These tests also run in `scripts/ci-local.sh` and normal CI. Use actionlint to validate the caller/callee workflow syntax and job dependencies.
