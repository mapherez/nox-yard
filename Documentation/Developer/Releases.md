# Formal releases

## Prepare user-facing notes

Maintain [CHANGELOG.md](../../CHANGELOG.md) alongside product changes. Keep pending changes under `## Unreleased`; before a release, move them into an exact version heading such as `## 1.3.4` or `## 1.3.4-rc.1` and commit those notes with the implementation. Use short descriptions of user-visible improvements, fixes, and any action required to upgrade. Do not use commit subjects as the release summary.

The publisher reads the matching section from the exact release commit and puts it first in the GitHub Release. Signature, digest, source SHA, and verification instructions remain available in an expandable section; GitHub's generated changes follow. Missing or empty version notes fall back to a changelog link and generated changes, without blocking publication or publishing Unreleased content. Previously published releases are not rewritten.

The release command still changes only `package.json`; prepare and commit notes **before** invoking it. Adding notes does not add a workflow or change CI/publication sequencing.

## Start a release

Run from the repository root with no modified, staged or untracked files:

```sh
npm run release
```

The script shows the current version from the root `package.json` and asks for the new version. Enter a SemVer such as `1.3.0` or `1.3.0-rc.1`; the normal release flow starts after that answer. Empty input, EOF or Ctrl+C cancels before making changes. An optional initial `v` is normalized. The root `package.json` stores SemVer without `v`; Git uses an annotated `v<version>` tag. Build metadata (`+...`) is not supported. Explicit arguments remain supported for automation: `npm run release -- X.Y.Z`.

Push implementation changes and wait for a successful **CI approved (policy v1)** job before releasing. Documentation/changelog-only commits may follow that approval. The script can still send clean local commits plus the release request atomically, but changes to validation/build inputs without matching CI approval stop publication before any heavy work. Validate those inputs first, or explicitly run CI manually on the branch and rerun the stopped release workflow. If the remote contains commits missing locally, fetch/integrate them first; Git will reject a non-fast-forward push. The script never force-pushes. Avoid running VS Code Push/Sync and the release command at the same time.

## CI approval and publication

A normal push affecting CI inputs runs source validation and the AMD64/ARM64 image builds in parallel. The AMD64 image is exported once and shared with three isolated runtime groups; the ten current-image suites come from `scripts/runtime_suites.py` and each runs exactly once. Native Linux fixture ownership regressions run in group 1. All source, architecture and runtime gates remain mandatory. **CI approved (policy v1)** also checks the reports for complete suite coverage and a common immutable AMD64 image identity, then saves `ci-approval` evidence with the SHA, input fingerprint and image identities.

A script release follows a separate short path:

1. Validate the marked version-only commit and its annotated tag. Reject other changes inside `package.json` too.
2. Find a successful CI run on the same branch and ancestor history with identical inputs (normalizing only the root package version). Check every required job through the GitHub API, including the current policy approval job. A merely green workflow with skipped gates is insufficient. Intervening documentation/changelog commits do not invalidate approval.
3. If equivalent CI is already running, await its result for up to 35 minutes without cancelling or repeating it. With missing, failed, cancelled or mismatched approval, stop **before source tests or Docker builds**. There is no automatic fallback to full CI.
4. Call the reusable publisher, recheck the selected approval, build the final multi-architecture image once with the release commit SHA/version, smoke test the actual published digest on both architectures, sign/verify it, and create the GitHub Release. The tests from the approved CI run are not repeated.

The final release build remains necessary: source CI embeds its own SHA/version, whereas the published binary must identify the version-only release commit. BuildKit imports the CI caches. The updater's image, version/revision fields, signing workflow identity and signature checks remain unchanged.

The CI workflow has an explicit push allowlist, documented in [Development](Development.md#github-ci-and-image-publication). Documentation, changelogs, `.gitignore`, `.dockerignore`, root Compose files and `.env.example` do not automatically start CI. Changing `.dockerignore`, Compose or another retained validation/build input still invalidates an older approval: use the **Run workflow** button on **CI** to validate such a change explicitly before releasing. A manual CI run never publishes; afterwards rerun the stopped release workflow. The search covers the latest 500 CI runs on the same branch. Missing/deleted job evidence requires a fresh manual approval.

The first push introducing this policy runs full CI once. Only approval under this policy can be reused; older green pipelines cannot authorize it. Subsequent valid version-only releases use the short path on their first attempt. Tag pushes and pull requests have no duplicate automatic pipeline. `.github/workflows/release.yml` remains callable only from CI, on the original push event, preserving the updater's certificate identity.

## What the command does

The command requires Node.js from `.nvmrc`, Git, a usable Git identity and permission to push the current branch and tags to `origin`. The remote must support atomic pushes; branch protection still applies. Validation runs on source pushes or explicit manual CI runs; release requests reuse a matching approval. An older clone must retire the legacy Yard hook with `sh scripts/install-hooks.sh`; that command preserves custom hooks. No Go, Docker, frontend install or Chrome is needed locally for the release command itself. CI remains the publication gate.

Commit implementation changes first. The command rejects tracked, staged or untracked changes, detached HEAD, an unchanged version, an existing local/remote tag, or an inaccessible origin. It updates only `package.json`, creates `chore: release v<version>` with the `NoX-Yard-Release: v<version>` trailer, creates the annotated tag with message `Release v<version>`, and atomically pushes the branch and tag. It does not explicitly rerun CI or build a validation image locally.

CI accepts the request only when the marker, commit subject, package version, annotated tag, annotation and exact target SHA agree, and the release commit changes only the version in `package.json`. An ordinary version bump, old release commit without the marker, or manually pushed tag does not request publication. These are provenance checks within the repository's existing trusted-push model, not a cryptographic proof of which client command was used.

| Version | GHCR tags | GitHub Release |
| --- | --- | --- |
| `1.2.0` | `v1.2.0`, `latest` | Stable |
| `1.2.0-rc.1` | `v1.2.0-rc.1` only | Prerelease |

Stable publication cannot move `latest` behind a newer published stable version. Installation and Yard self-update continue to use `ghcr.io/mapherez/nox-yard:latest`.

## Image signing and build provenance

New releases use keyless Cosign signing with GitHub Actions OIDC and the public Sigstore services (Fulcio and Rekor). There is no persistent signing key or extra repository secret to configure. The caller grants `id-token: write` only to the release job; the called publisher also declares that permission. Ordinary CI jobs retain read-only repository access and cannot request an OIDC token.

The publisher signs the exact multi-architecture index digest returned by Buildx, with the release tag and source commit as signed annotations. A stable version tag and `latest` initially point to that same digest, so they do not need separate signatures. Prereleases remain version-tagged only. The signature covers the index, including its references to the platform images and BuildKit provenance attestations. It does not separately sign each platform manifest or the annotated Git tag.

Buildx explicitly generates SLSA provenance with `mode=max`, recording build inputs and instructions for both architectures. This does not claim a particular SLSA assurance level. Build arguments are public in this mode: the current release passes only `BUILD_SHA` and `BUILD_VERSION`; credentials must use BuildKit secret mounts instead of build arguments. A second `cosign attest` statement is unnecessary for this index-contained provenance.

Before creating the GitHub Release, verification requires the GitHub Actions issuer, this repository's reusable `release.yml` identity on the triggering branch, the exact commit SHA, the push trigger and matching signed release/revision annotations. Signature, certificate and transparency-log checks remain enabled. A signing or verification failure blocks GitHub Release creation. The release notes record the immutable image reference, source SHA and signing identity. Existing releases are not signed retroactively.

### Verify a release image

Install Cosign v3 using the [official installation instructions](https://docs.sigstore.dev/cosign/system_config/installation/). Copy the image digest, source commit and signer from the GitHub Release notes. For a release published from `master`, run in Bash:

```sh
IMAGE='ghcr.io/mapherez/nox-yard@sha256:REPLACE_WITH_RELEASE_DIGEST'
RELEASE_TAG='vX.Y.Z'
REVISION='REPLACE_WITH_FULL_SOURCE_COMMIT'
SIGNER='https://github.com/mapherez/nox-yard/.github/workflows/release.yml@refs/heads/master'

cosign verify \
  --certificate-identity "$SIGNER" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-github-workflow-repository mapherez/nox-yard \
  --certificate-github-workflow-sha "$REVISION" \
  --certificate-github-workflow-trigger push \
  -a "release=$RELEASE_TAG" -a "revision=$REVISION" \
  "$IMAGE"
```

Use the exact signer recorded in the release notes if publication used another branch. Do not replace the identity with an unrestricted regular expression or disable certificate, claim or transparency-log verification. Verification must finish successfully before relying on the image. Docker pull/Compose do not enforce Cosign verification themselves. Yard's update installer now enforces the [publisher policy](Self-Update.md#signature-verification): the official repository's `release.yml` on `master`, GitHub Actions issuer, push trigger and matching signed version/source revision. A release signed from another branch can be verified manually but is not accepted by the built-in updater.

With Docker Buildx installed, inspect the provenance attached to that same verified digest:

```sh
docker buildx imagetools inspect "$IMAGE" --format '{{ json .Provenance }}'
```

Provenance inspection alone is not signature verification. The immutable index reference links the inspected attestations to the verified image. See [Sigstore verification](https://docs.sigstore.dev/cosign/verifying/verify/), [BuildKit provenance](https://docs.docker.com/build/metadata/attestations/slsa-provenance/) and [Buildx inspection](https://docs.docker.com/reference/cli/docker/buildx/imagetools/inspect/).

### Choose an update policy in Compose

The main `compose.yaml` keeps `ghcr.io/mapherez/nox-yard:latest` for the normal stable-channel installation and optional automatic updates. To control upgrades manually, change the service's `image` reference in your local Compose file:

```yaml
# Choose a published version; replace this example with your desired release.
image: ghcr.io/mapherez/nox-yard:v1.3.1

# Alternatively, fix the exact verified multi-architecture image.
# Replace the placeholder with the digest recorded in a signed release.
# image: ghcr.io/mapherez/nox-yard@sha256:REPLACE_WITH_RELEASE_DIGEST
```

`latest` can move to a new stable release on a future pull. A version tag keeps the selected version under this repository's no-overwrite publication policy, but registry tags are not inherently immutable. A digest fixes the exact image content. The file-backed Yard updater respects an explicitly pinned Compose reference and does not rewrite it to `latest`; upgrading a pinned installation requires changing that reference. Pinning and signature verification solve different problems, and neither implies the other.

## Duplicate runs and failure recovery

Normal source pushes use one concurrency lane per branch and cancel older source CI on that branch. A marked release request uses a separate SHA lane, so it cannot cancel the source CI whose result it needs. Publishers share `nox-yard-publication` with cancellation disabled; an ordinary source push cannot cancel a running publisher. GitHub still limits pending work in a concurrency group; inspect cancelled queued publications before retrying. See [GitHub concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency).

A duplicate completed publication is skipped. Each new GitHub Release records its source SHA in a hidden body marker after publication, smoke tests, signing and verification complete. Existing drafts, legacy releases without this marker and mismatching markers remain conflicts and are never overwritten. Explicitly cancelling a publisher does not undo registry tags or a GitHub Release already created.

Before a release commit exists, a preparation/commit failure restores `package.json` byte-for-byte and unstages only its change. Once a release commit exists, it and any created tag are preserved on failure. The script prints exact recovery commands; inspect the local state and use those commands instead of rerunning with an existing tag. It never automatically resets commits or deletes tags.

After a successful push, inspect the **CI** run, including its dependent Release job. If approval is missing, validate the source or run CI manually on the branch, then rerun the stopped release workflow. A manual run at the release SHA can grant approval for that retry. If source tests fail, fix and validate the source before preparing a corrected release request. Do not push an old tag expecting an independent Release workflow. If image publication succeeds but signing, verification or GitHub Release creation fails, the image remains and its tags (including `latest` for a stable release) may already have moved. Image publication and GitHub Release creation are not one transaction. Do not treat the image as a completed signed release until verification succeeds. Retry the failed job only after inspecting that state; the current request identity and downgrade/conflict guards still apply. After a successful build, the publisher saves an immutable receipt artifact belonging to that workflow run before smoke tests/signing. A retry within the same workflow run verifies that receipt against the release tag, SHA and run ID, then reuses the recorded registry digest without rebuilding. Missing registry content or an invalid receipt fails rather than choosing another image. If the build was interrupted before its receipt was saved, or the artifact expired (90 days), a retry builds again. Use the digest from the completed release notes.

### Reuse an unpublished version after a source correction

Re-running an existing GitHub Actions run uses its original SHA/ref, so it cannot pick up a later source fix. A failed pre-publication attempt does not require consuming a new version number. Reuse is appropriate only after confirming the original pipeline is completed, its publication jobs were skipped, and neither a GitHub Release nor a GHCR image exists for that version. If any publication exists, preserve its tag and use a new version, or recover the matching original publication as described above.

For the failed `v1.3.1` attempt, the preceding published version is `1.3.0`. After checking the conditions above, run from the repository root in PowerShell:

```powershell
# Restore the preceding version metadata before committing the source correction.
npm pkg set version=1.3.0
```

Commit the source corrections, tests, documentation and `package.json` together. Push and validate corrected implementation commits before requesting publication. Then remove the unpublished attempt's tag, guarded against an unexpected remote change, and create the fresh request:

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
