import { publicationMarker } from './release-request.mjs';
import { parseVersion } from './release-version.mjs';

// Only the requested version is publishable; Unreleased is never used as a fallback.
export function releaseSummary(markdown, tag) {
  const { version } = parseVersion(tag);
  let collecting = false;
  let fence;
  const lines = [];
  for (const line of markdown.replaceAll('\r\n', '\n').split('\n')) {
    const marker = /^ {0,3}(`{3,}|~{3,})(.*)$/.exec(line);
    if (marker) {
      if (!fence) fence = marker[1];
      else if (marker[1][0] === fence[0] && marker[1].length >= fence.length && !marker[2].trim()) fence = undefined;
    }
    if (!fence && line.startsWith('## ')) {
      if (collecting) break;
      collecting = line.trimEnd() === `## ${version}`;
      continue;
    }
    if (collecting) lines.push(line);
  }
  return lines.join('\n').trim();
}

export function releaseBody({ changelog, tag, sha, owner, repo, ref, digest }) {
  const source = `https://github.com/${owner}/${repo}`;
  const summary = releaseSummary(changelog, tag);
  return [
    publicationMarker(sha),
    '',
    '## What\'s new',
    '',
    summary || `See the [changelog](${source}/blob/${sha}/CHANGELOG.md) and generated changes below.`,
    '',
    '<details>',
    '<summary>Container signature and build provenance</summary>',
    '',
    `Image: \`ghcr.io/${owner}/${repo}@${digest}\``,
    `Source commit: \`${sha}\``,
    `Signer: \`${source}/.github/workflows/release.yml@${ref}\``,
    '',
    'Keyless Cosign signature verified with GitHub Actions OIDC. BuildKit SLSA provenance is included in the signed multi-architecture index.',
    `[Verification instructions](${source}/blob/${sha}/Documentation/Developer/Releases.md#verify-a-release-image)`,
    '',
    '</details>',
  ].join('\n');
}
