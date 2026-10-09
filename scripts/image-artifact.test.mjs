import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checksum, validateManifest, validateIndex, load, publish } from './image-artifact.mjs';
import { image, source } from './release-metadata.mjs';

const expected = { sha: 'a'.repeat(40), version: 'v2.0.0', runId: '10' };
async function fixture(t, version = expected.version) {
  const directory = mkdtempSync(join(tmpdir(), 'nox-images-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const manifests = [], calls = [], references = new Map();
  const identity = { ...expected, version };
  for (const [index, architecture] of ['amd64', 'arm64'].entries()) {
    const root = join(directory, architecture); mkdirSync(root);
    writeFileSync(join(root, `${architecture}.tar`), `fixture archive ${architecture}`);
    const manifest = { schema: 1, ...identity, architecture, imageId: `sha256:${String(index + 1).repeat(64)}`,
      archiveSHA256: await checksum(join(root, `${architecture}.tar`)) };
    writeFileSync(join(root, `${architecture}.json`), JSON.stringify(manifest));
    manifests.push(manifest);
  }
  const index = { manifests: manifests.map((m, i) => ({ platform: { os: 'linux', architecture: m.architecture }, digest: `sha256:${String(i + 3).repeat(64)}` })) };
  const execute = (command, args) => {
    assert.equal(command, 'docker'); calls.push(args);
    if (args[0] === 'load' || args[0] === 'tag' || args[0] === 'pull') return '';
    if (args[0] === 'image') {
      if (args[2] === `${image}:latest`) return JSON.stringify([{ Config: { Labels: { 'org.opencontainers.image.version': 'v1.0.0' } } }]);
      const m = manifests.find(m => args[2] === `nox-yard:ci-${m.architecture}`);
      return JSON.stringify([{ Id: m.imageId, Os: 'linux', Architecture: m.architecture, Config: { Labels: {
        'org.opencontainers.image.revision': identity.sha, 'org.opencontainers.image.version': identity.version,
        'org.opencontainers.image.source': source,
      } } }]);
    }
    if (args[0] === 'push') return `pushed digest: ${index.manifests[args[1].endsWith('amd64') ? 0 : 1].digest}`;
    if (args[0] === 'buildx' && args[2] === 'create') { references.set(args[4], index); return ''; }
    if (args[0] === 'buildx' && args[2] === 'inspect') {
      if (args[4] === '--format') return JSON.stringify({ digest: index.manifests[args[3].endsWith('amd64') ? 0 : 1].digest });
      const child = index.manifests.find(m => args[3] === `${image}@${m.digest}`);
      if (child) return JSON.stringify({ config: { digest: manifests.find(m => m.architecture === child.platform.architecture).imageId } });
      if (!references.has(args[3])) throw new Error('manifest unknown');
      return JSON.stringify(references.get(args[3]));
    }
    throw new Error(`Unexpected Docker operation: ${args}`);
  };
  return { directory, manifests, calls, references, index, identity, execute };
}

test('rejects artifacts for another commit, run, version or architecture', async t => {
  const f = await fixture(t), m = f.manifests[0];
  validateManifest(m, { ...expected, architecture: 'amd64' });
  for (const override of [{ sha: 'b'.repeat(40) }, { runId: '11' }, { version: 'v9.0.0' }, { architecture: 'arm64' }]) {
    assert.throws(() => validateManifest(m, { ...expected, architecture: 'amd64', ...override }), /identity/);
  }
});
test('archive tampering is rejected before Docker load', async t => {
  const f = await fixture(t);
  writeFileSync(join(f.directory, 'amd64', 'amd64.tar'), 'tampered');
  await assert.rejects(load(f.directory, expected, ['amd64'], f.execute), /checksum/);
  assert.equal(f.calls.length, 0);
});
test('publication reuses tested image IDs, creates both platforms and stable latest; never builds', async t => {
  const f = await fixture(t);
  await publish(f.directory, expected, f.execute);
  assert(f.references.has(`${image}:v2.0.0`)); assert(f.references.has(`${image}:latest`));
  assert.equal(f.calls.filter(args => args[0] === 'push').length, 2);
  assert.equal(f.calls.some(args => args.includes('build')), false);
  // A partial publication retry verifies existing version and avoids image pushes.
  f.calls.length = 0;
  await publish(f.directory, expected, f.execute);
  assert.equal(f.calls.filter(args => args[0] === 'push').length, 0);
});
test('prerelease does not pull, publish or modify latest', async t => {
  const f = await fixture(t, 'v2.0.0-rc.1');
  await publish(f.directory, f.identity, f.execute);
  assert.equal(f.calls.some(args => args.some(value => value === `${image}:latest`)), false);
});
test('registry latest guard blocks downgrade after a partial GitHub publication', async t => {
  const f = await fixture(t);
  const execute = (command, args) => args[0] === 'image' && args[2] === `${image}:latest`
    ? JSON.stringify([{ Config: { Labels: { 'org.opencontainers.image.version': 'v3.0.0' } } }]) : f.execute(command, args);
  await assert.rejects(publish(f.directory, expected, execute), /newer stable release/);
  assert.equal(f.references.size, 0);
});
test('existing version with different image config or missing architecture cannot be overwritten', async t => {
  const f = await fixture(t);
  f.references.set(`${image}:v2.0.0`, f.index);
  const execute = (command, args) => args[0] === 'buildx' && args[2] === 'inspect' && args[3].includes('@')
    ? JSON.stringify({ config: { digest: 'sha256:' + '9'.repeat(64) } }) : f.execute(command, args);
  await assert.rejects(publish(f.directory, expected, execute), /refusing to replace/);
  assert.equal(f.calls.some(args => args[0] === 'push'), false);
  assert.throws(() => validateIndex({ manifests: [f.index.manifests[0]] }), /exactly linux/);
});
