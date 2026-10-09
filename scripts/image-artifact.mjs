#!/usr/bin/env node
import { createHash } from 'node:crypto';
import { createReadStream, readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { run } from './push-check.mjs';
import { image, source } from './release-metadata.mjs';
import { assertLatestStable, isMain } from './release-version.mjs';

const architectures = ['amd64', 'arm64'];
export async function checksum(path) {
  const hash = createHash('sha256');
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest('hex');
}
export function validateManifest(manifest, { sha, version, runId, architecture }) {
  if (!/^[a-f0-9]{40,64}$/.test(sha || '') || !/^\d+$/.test(String(runId)) || !version ||
      manifest.schema !== 1 || manifest.sha !== sha || manifest.version !== version ||
      String(manifest.runId) !== String(runId) || manifest.architecture !== architecture ||
      !architectures.includes(architecture) || !/^sha256:[a-f0-9]{64}$/.test(manifest.imageId) ||
      !/^[a-f0-9]{64}$/.test(manifest.archiveSHA256)) throw new Error('Image artifact identity does not match this validated commit/run/version/architecture.');
  return manifest;
}
export function validateImage(inspected, manifest) {
  const labels = inspected.Config?.Labels || {};
  if (inspected.Id !== manifest.imageId || inspected.Os !== 'linux' || inspected.Architecture !== manifest.architecture ||
      labels['org.opencontainers.image.revision'] !== manifest.sha || labels['org.opencontainers.image.version'] !== manifest.version ||
      labels['org.opencontainers.image.source'] !== source) throw new Error('Loaded image does not match its tested artifact metadata/OCI labels.');
}
export function validateIndex(index) {
  if (!Array.isArray(index.manifests) || index.manifests.length !== 2 ||
      architectures.some(architecture => index.manifests.filter(item => item.platform?.os === 'linux' && item.platform.architecture === architecture).length !== 1)) {
    throw new Error('Published index must contain exactly linux/amd64 and linux/arm64.');
  }
}
export async function capture(directory, architecture, expected, execute = run) {
  const tag = `nox-yard:ci-${architecture}`;
  const inspected = JSON.parse(execute('docker', ['image', 'inspect', tag]))[0];
  const manifest = { schema: 1, ...expected, architecture, imageId: inspected.Id,
    archiveSHA256: await checksum(join(directory, `${architecture}.tar`)) };
  validateManifest(manifest, { ...expected, architecture });
  validateImage(inspected, manifest);
  writeFileSync(join(directory, `${architecture}.json`), JSON.stringify(manifest, null, 2) + '\n');
}
function find(root, name) {
  const matches = [];
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) matches.push(...find(path, name));
    else if (entry.name === name) matches.push(path);
  }
  return matches;
}
export async function load(directory, expected, selected = architectures, execute = run) {
  const manifests = [];
  for (const architecture of selected) {
    const paths = find(directory, `${architecture}.json`);
    if (paths.length !== 1) throw new Error(`Expected exactly one tested ${architecture} manifest.`);
    const manifest = validateManifest(JSON.parse(readFileSync(paths[0], 'utf8')), { ...expected, architecture });
    const archive = join(dirname(paths[0]), `${architecture}.tar`);
    if (await checksum(archive) !== manifest.archiveSHA256) throw new Error(`${architecture} archive checksum mismatch.`);
    execute('docker', ['load', '--input', archive]);
    validateImage(JSON.parse(execute('docker', ['image', 'inspect', `nox-yard:ci-${architecture}`]))[0], manifest);
    manifests.push(manifest);
  }
  return manifests;
}
const missingManifest = error => /manifest unknown|manifest_unknown|not found|404/i.test(error.message);
export function inspectIndex(reference, execute = run) {
  try { return JSON.parse(execute('docker', ['buildx', 'imagetools', 'inspect', reference, '--raw'])); }
  catch (error) { if (missingManifest(error)) return null; throw error; }
}
export function verifyPublished(reference, manifests, execute = run) {
  const index = inspectIndex(reference, execute);
  if (!index) throw new Error(`${reference} has not been published.`);
  validateIndex(index);
  for (const manifest of manifests) {
    const child = index.manifests.find(item => item.platform.architecture === manifest.architecture);
    const remote = JSON.parse(execute('docker', ['buildx', 'imagetools', 'inspect', `${image}@${child.digest}`, '--raw']));
    if (remote.config?.digest !== manifest.imageId) throw new Error(`${reference} differs from the tested ${manifest.architecture} image; refusing to replace it.`);
  }
  return index;
}
export async function publish(directory, expected, execute = run) {
  const manifests = await load(directory, expected, architectures, execute);
  const reference = `${image}:${expected.version}`;
  const stable = !expected.version.includes('-');
  if (stable) {
    // Also guard the registry channel after a previous partial GitHub publication.
    try {
      execute('docker', ['pull', '--platform', 'linux/amd64', `${image}:latest`]);
      const labels = JSON.parse(execute('docker', ['image', 'inspect', `${image}:latest`]))[0].Config?.Labels || {};
      const latest = labels['org.opencontainers.image.version'];
      if (latest) assertLatestStable(expected.version, [latest]);
    } catch (error) { if (!missingManifest(error)) throw error; }
  }
  if (inspectIndex(reference, execute)) verifyPublished(reference, manifests, execute);
  else {
    const digests = [];
    for (const manifest of manifests) {
      const tag = `${image}:build-${expected.sha}-${manifest.architecture}`;
      execute('docker', ['tag', `nox-yard:ci-${manifest.architecture}`, tag]);
      execute('docker', ['push', tag]);
      const descriptor = JSON.parse(execute('docker', ['buildx', 'imagetools', 'inspect', tag, '--format', '{{json .Manifest}}']));
      const digest = descriptor.digest;
      if (!/^sha256:[a-f0-9]{64}$/.test(digest || '')) throw new Error(`Cannot identify pushed ${manifest.architecture} digest.`);
      digests.push(`${image}@${digest}`);
    }
    execute('docker', ['buildx', 'imagetools', 'create', '--tag', reference, ...digests]);
  }
  const official = verifyPublished(reference, manifests, execute);
  if (stable) {
    execute('docker', ['buildx', 'imagetools', 'create', '--tag', `${image}:latest`, reference]);
    const latest = verifyPublished(`${image}:latest`, manifests, execute);
    const entries = index => index.manifests.map(item => `${item.platform.architecture}:${item.digest}`).sort();
    if (JSON.stringify(entries(official)) !== JSON.stringify(entries(latest))) throw new Error('latest does not match the stable release.');
  }
  console.log(`Verified ${reference}: linux/amd64 and linux/arm64 match tested image IDs.`);
}

if (isMain(import.meta.url)) {
  try {
    const [command, directory, architecture] = process.argv.slice(2);
    const expected = { sha: process.env.GITHUB_SHA, version: process.env.BUILD_VERSION, runId: process.env.GITHUB_RUN_ID };
    if (command === 'capture') await capture(directory, architecture, expected);
    else if (command === 'load') await load(directory, expected, architecture ? [architecture] : architectures);
    else if (command === 'publish') await publish(directory, expected);
    else throw new Error('Usage: image-artifact.mjs <capture|load|publish> <directory> [architecture]');
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
