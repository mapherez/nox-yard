# Formal releases

Run from the repository root with a completely clean working tree:

```sh
npm run release -- 1.0.0
```

Prereleases use the same command, for example `npm run release -- 1.1.0-rc.1`. An optional initial `v` is normalized. The version must be valid SemVer; build metadata (`+...`) is not supported. `VERSION` stores the version without `v`, and Git uses an annotated `v<version>` tag. The initial `0.0.0` is a baseline, not a published release.

## Prerequisites

Install Go from `go.mod`, Node.js from `.nvmrc`, Git, a POSIX shell, and Docker with Compose and Buildx. Run `npm ci --prefix web` first. On Windows, use Git Bash with these tools on PATH. Git author and committer identities must be usable, and `origin` must be accessible with permission to push the current branch and tags. The remote must support atomic pushes; branch protection still applies.

Commit the implementation and any other pending work before running the release command. The command rejects tracked, staged, or untracked changes, detached HEAD, an unchanged version, and a tag that already exists locally or on `origin`. A failed remote lookup aborts the release.

## What the command does

The command updates only `VERSION`, runs `sh scripts/ci-local.sh`, and builds `nox-yard:release-check` locally for `linux/amd64`. That validation build receives the full current commit SHA and the requested `v<version>` through the existing Docker build arguments; it is never pushed.

After validation, the command refuses unexpected file changes, commits only `VERSION` as `chore: release v<version>`, creates the annotated tag with message `Release v<version>`, and atomically pushes the current branch and tag to `origin`. It detects the branch dynamically.

The tag push starts `.github/workflows/release.yml`. On a clean runner it checks tag/VERSION agreement, refuses an existing GitHub Release, installs the project toolchains and frontend dependencies, and repeats the shared checks. It then builds and publishes `linux/amd64` and `linux/arm64`, followed by a GitHub Release with generated notes. The release binary receives the release tag and full release commit SHA. OCI labels record version, revision, source, and creation time.

| Version | GHCR tags | GitHub Release |
| --- | --- | --- |
| `1.0.0` | `v1.0.0`, `latest` | Stable |
| `1.1.0-rc.1` | `v1.1.0-rc.1` only | Prerelease |

Only stable releases update `latest`. Publication is serialized, and an older stable version cannot replace a newer published stable release. Normal installation and self-update continue to use `ghcr.io/mapherez/nox-yard:latest`. Ordinary pushes to `master` and pull requests validate source and Docker builds without publishing official images. `scripts/build-version.sh` remains available for normal builds.

## Failure recovery

Before a release commit exists, any validation, build, or commit failure restores `VERSION` byte-for-byte and unstages the command's VERSION change. No release tag or push is made. Unexpected files changed by checks are reported and retained for inspection.

If tag creation fails after the release commit, the commit is preserved locally. If the atomic push fails, both the release commit and annotated tag are preserved locally. There is no automatic reset or deletion. The error prints the exact commands to recover in a POSIX shell, including the branch/tag push. Resolve the reported problem, inspect the local state, and use those commands rather than rerunning the release command with an existing tag.

Watch the GitHub Actions release run after a successful push. Docker publication and GitHub Release creation are separate remote operations: if image publication succeeds but GitHub Release creation fails, the published image remains. Fix the failure and rerun the failed workflow; an existing GitHub Release is always rejected rather than edited silently.

## Tooling validation

`node --test scripts/release.test.mjs` exercises the production validation and metadata functions with disposable Git repositories and local bare remotes. Git operations, commits, annotated tags, and atomic pushes are real inside these fixtures; CI and Docker failure paths use an injected command runner. Tests never contact GitHub or GHCR and remove their fixtures on completion. These tests also run in `scripts/ci-local.sh` and normal CI.
