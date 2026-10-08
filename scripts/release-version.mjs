import { resolve } from 'node:path';
import { pathToFileURL } from 'node:url';

// SemVer without build metadata. Numeric identifiers must not have leading zeros.
const semver = /^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?$/;

export function parseVersion(input) {
  if (typeof input !== 'string' || input.includes('+')) {
    throw new Error('Build metadata (+...) is not supported for releases.');
  }
  const version = input.startsWith('v') ? input.slice(1) : input;
  const match = semver.exec(version);
  if (!match) throw new Error(`Invalid SemVer version: ${input}`);
  const tag = `v${version}`;
  if (tag.length > 128) throw new Error('Release tag exceeds the Docker tag limit of 128 characters.');
  return { version, tag, prerelease: match[4] !== undefined };
}

export function readVersion(contents) {
  const { version } = JSON.parse(contents.toString('utf8'));
  if (typeof version !== 'string') throw new Error('package.json must contain a version string.');
  const parsed = parseVersion(version);
  if (parsed.version !== version) throw new Error('package.json version must contain SemVer without a v prefix.');
  return parsed.version;
}

export function checkReleaseVersion(tag, contents) {
  const release = parseVersion(tag);
  if (tag !== release.tag) throw new Error('Release tags must start with v.');
  const version = readVersion(contents);
  if (release.version !== version) {
    throw new Error(`Release tag ${tag} does not match package.json version (${version}).`);
  }
  return release;
}

// Serialized publishers must not move the stable channel back to an older version.
export function assertLatestStable(tag, publishedTags) {
  const release = parseVersion(tag);
  if (release.prerelease) return;
  const parts = release.version.split('.').map(BigInt);
  for (const publishedTag of publishedTags) {
    let published;
    try { published = parseVersion(publishedTag); } catch { continue; }
    if (published.prerelease) continue;
    const other = published.version.split('.').map(BigInt);
    for (let i = 0; i < parts.length; i++) {
      if (other[i] > parts[i]) throw new Error(`${tag} would replace a newer stable release (${publishedTag}) on latest.`);
      if (other[i] < parts[i]) break;
    }
  }
}

export function isMain(url) {
  return process.argv[1] && pathToFileURL(resolve(process.argv[1])).href === url;
}
