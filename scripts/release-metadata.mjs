import { appendFileSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { isMain, parseVersion } from './release-version.mjs';

export const image = 'ghcr.io/mapherez/nox-yard';
export const source = 'https://github.com/mapherez/nox-yard';

export function releaseMetadata(tag, revision, created = new Date().toISOString()) {
  const release = parseVersion(tag);
  if (release.tag !== tag) throw new Error('Release tags must start with v.');
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(revision)) throw new Error('A full commit SHA is required.');
  if (new Date(created).toISOString() !== created) throw new Error('created must be a UTC ISO timestamp.');
  return {
    ...release,
    tags: [`${image}:${tag}`, ...(!release.prerelease ? [`${image}:latest`] : [])],
    labels: {
      'org.opencontainers.image.version': tag,
      'org.opencontainers.image.revision': revision,
      'org.opencontainers.image.source': source,
      'org.opencontainers.image.created': created,
    },
  };
}

export function writeMetadataOutputs(metadata, output) {
  const delimiter = `release_${randomUUID()}`;
  appendFileSync(output, `prerelease=${metadata.prerelease}\ntags<<${delimiter}\n${metadata.tags.join('\n')}\n${delimiter}\nlabels<<${delimiter}\n${Object.entries(metadata.labels).map(([key, value]) => `${key}=${value}`).join('\n')}\n${delimiter}\n`);
}

if (isMain(import.meta.url)) {
  try {
    if (process.argv.length !== 4) throw new Error('Usage: node scripts/release-metadata.mjs <tag> <full-sha>');
    const metadata = releaseMetadata(process.argv[2], process.argv[3]);
    if (process.env.GITHUB_OUTPUT) writeMetadataOutputs(metadata, process.env.GITHUB_OUTPUT);
    else console.log(JSON.stringify(metadata, null, 2));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
