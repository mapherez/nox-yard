import { appendFileSync } from 'node:fs';
import { commandRunner } from './release.mjs';
import { assertLatestStable, checkReleaseVersion, isMain, parseVersion } from './release-version.mjs';

export const releaseTrailer = (tag) => `NoX-Yard-Release: ${tag}`;
export const publicationMarker = (revision) => `<!-- nox-yard-release:${revision} -->`;

// Stored only as an artifact of this exact workflow run. A retry can finish
// smoke/signing/publication for the same immutable index without rebuilding it.
export function publicationReceipt({ tag, revision, runId, digest }) {
  if (parseVersion(tag).tag !== tag || !/^[a-f0-9]{40}$/.test(revision) ||
      !/^[1-9][0-9]*$/.test(String(runId)) || !/^sha256:[a-f0-9]{64}$/.test(digest)) {
    throw new Error('Invalid release image receipt.');
  }
  return { tag, revision, runId: String(runId), digest };
}

export function verifyPublicationReceipt(value, expected) {
  const receipt = publicationReceipt(value);
  if (receipt.tag !== expected.tag || receipt.revision !== expected.revision || receipt.runId !== String(expected.runId)) {
    throw new Error('Release image receipt does not belong to this request/run.');
  }
  return receipt.digest;
}

// A version bump or a manually pushed tag is not a request to publish. The
// release script creates a marked commit and its matching annotated tag.
export function requestedRelease({ cwd = process.cwd(), revision, ref, event = 'push', run = commandRunner(cwd) }) {
  if (event !== 'push' || !ref?.startsWith('refs/heads/')) return '';
  if (!/^[a-f0-9]{40}$/.test(revision)) throw new Error('A full release commit SHA is required.');
  const git = (...args) => run('git', args).trim();
  const message = git('show', '-s', '--format=%B', revision);
  const trailers = message.split('\n').filter((line) => line.startsWith('NoX-Yard-Release:'));
  if (!trailers.length) return '';
  if (trailers.length !== 1) throw new Error('Ambiguous release request.');
  const tag = trailers[0].slice('NoX-Yard-Release: '.length);
  if (parseVersion(tag).tag !== tag || trailers[0] !== releaseTrailer(tag) || message.split('\n')[0] !== `chore: release ${tag}`) {
    throw new Error('Invalid release request marker.');
  }
  checkReleaseVersion(tag, git('show', `${revision}:package.json`));
  if (git('diff-tree', '--no-commit-id', '--name-only', '-r', revision) !== 'package.json') {
    throw new Error('A release request must change only package.json.');
  }
  if (git('cat-file', '-t', `refs/tags/${tag}`) !== 'tag' ||
      git('rev-parse', `refs/tags/${tag}^{commit}`) !== revision ||
      git('for-each-ref', '--format=%(contents)', `refs/tags/${tag}`) !== `Release ${tag}`) {
    throw new Error('Release request does not match its annotated tag.');
  }
  return tag;
}

// Publication is serialized. A duplicate delivery/retry of the same completed
// publication is a no-op; a conflicting or manually created release is refused.
export function shouldPublish(tag, revision, releases) {
  const release = parseVersion(tag);
  if (!/^[a-f0-9]{40}$/.test(revision)) throw new Error('A full release commit SHA is required.');
  const existing = releases.find((release) => release.tag_name === tag);
  if (existing) {
    if (!existing.draft && existing.prerelease === release.prerelease && existing.body?.includes(publicationMarker(revision))) return false;
    throw new Error(`GitHub Release ${tag} already exists with different or unverified publication metadata.`);
  }
  assertLatestStable(tag, releases.filter((release) => !release.draft && !release.prerelease).map((release) => release.tag_name));
  return true;
}

if (isMain(import.meta.url)) {
  try {
    const tag = requestedRelease({ revision: process.env.GITHUB_SHA, ref: process.env.GITHUB_REF, event: process.env.GITHUB_EVENT_NAME });
    if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `tag=${tag}\n`);
    console.log(tag ? `Release requested: ${tag}; publication requires a matching successful CI approval.` : 'Normal push: CI only.');
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
