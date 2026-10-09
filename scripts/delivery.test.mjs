import assert from 'node:assert/strict';
import test from 'node:test';
import { selectAcceptance, gates, docsOnly, classify, cancelSuperseded, stableBaseline, imageArtifacts } from './delivery.mjs';

const sha = 'a'.repeat(40), old = 'b'.repeat(40), released = 'c'.repeat(40);
const context = { sha, ref: 'refs/heads/master', eventName: 'push', runId: 10,
  repo: { owner: 'owner', repo: 'repo' }, payload: { before: old } };
function fakeGit({ tags = { 'v2.0.0': sha }, changes = ['web/src/a.tsx'], previous = ['internal/application/schedules.go'] } = {}) {
  return (...args) => {
    if (args[0] === 'show') return JSON.stringify({ name: 'nox-yard', version: args[1].startsWith(sha) ? '2.0.0' : '1.0.0' });
    if (args[0] === 'rev-parse') {
      const tag = args.at(-1).match(/refs\/tags\/(.+)\^\{commit\}/)?.[1];
      if (!tags[tag]) throw new Error('No tag');
      return tags[tag];
    }
    if (args[0] === 'merge-base') { if (args[2] === 'unrelated') throw new Error('Not an ancestor'); return old; }
    if (args[0] === 'diff') return (args[2] === old ? previous : changes).join('\0');
    throw new Error(`Unexpected git ${args}`);
  };
}
test('acceptance policy selects subsystem unions, skips frontend, defaults unknown/shared to full', () => {
  assert.deepEqual(selectAcceptance(['web/src/a.tsx', 'README.md']).groups, []);
  assert.deepEqual(selectAcceptance(['internal/application/schedules.go', 'internal/store/schedules_test.go']).groups, ['schedules']);
  assert.deepEqual(selectAcceptance(['internal/managed/deployment.go']).groups, ['managed', 'mcp', 'schedules']);
  assert.deepEqual(selectAcceptance(['internal/recreate/worker.go', 'internal/selfupdate/worker.go']).groups, ['recreate', 'mcp', 'schedules', 'self-update']);
  assert.deepEqual(selectAcceptance(['internal/inventory/uptime.go']).groups, ['mcp']);
  for (const file of ['go.mod', 'internal/store/store.go', 'internal/jobs/workers.go', '.github/workflows/ci.yml', 'new-module.go']) {
    assert.deepEqual(selectAcceptance([file]).groups, gates);
  }
  assert.deepEqual(selectAcceptance([], { baseline: false }).groups, gates);
  assert.deepEqual(selectAcceptance(['README.md'], { full: true }).groups, gates);
  assert.deepEqual(selectAcceptance(['scripts/smoke-upgrade.py']).groups, ['upgrade']);
});
test('published stable baseline excludes prereleases, drafts and the current candidate', () => {
  const releases = [{ tag_name: 'v2.0.0' }, { tag_name: 'v1.0.0' }, { tag_name: 'v1.5.0', draft: true }, { tag_name: 'v1.9.0-rc.1', prerelease: true }];
  assert.equal(stableBaseline(releases, sha, fakeGit({ tags: { 'v2.0.0': sha, 'v1.0.0': old } })).sha, old);
});
test('release selection compares all changes since published stable ancestor, ignoring only root version bump', async () => {
  const execute = fakeGit({ tags: { 'v2.0.0': sha, 'v1.0.0': old }, previous: ['package.json', 'internal/application/schedules.go'] });
  const github = { rest: { repos: { listReleases: 'releases' } }, paginate: async () => [{ tag_name: 'v1.0.0' }] };
  const policy = await classify({ github, context, execute });
  assert.equal(policy.releaseTag, 'v2.0.0');
  assert.equal(policy.baseline.sha, old);
  assert.deepEqual(policy.groups, ['schedules']);
});
test('normal push and PR cannot request publication; documentation detection remains narrow', async () => {
  const execute = fakeGit({ tags: {}, previous: ['README.md'] });
  const normal = await classify({ context, execute });
  assert.equal(normal.releaseTag, ''); assert.equal(normal.docsOnly, true);
  const pr = await classify({ context: { ...context, eventName: 'pull_request', payload: { pull_request: { base: { sha: old } } } }, execute: fakeGit() });
  assert.equal(pr.releaseTag, '');
  assert.equal(docsOnly(['README.md', '.github/workflows/ci.yml']), false);
  assert.equal(docsOnly([]), false);
});
test('release cancels only active ancestor normal runs; preserves release, unrelated, current and completed runs', async () => {
  const runs = [
    { id: 1, status: 'in_progress', head_sha: old },
    { id: 2, status: 'queued', head_sha: released },
    { id: 3, status: 'in_progress', head_sha: 'unrelated' },
    { id: 4, status: 'completed', head_sha: old },
    { id: 5, status: 'queued', head_sha: old },
    { id: 10, status: 'in_progress', head_sha: sha },
  ].map(run => ({ event: 'push', head_branch: 'master', ...run }));
  const cancelled = [];
  const github = { rest: { actions: { listWorkflowRuns: 'runs', listJobsForWorkflowRun: 'jobs',
    cancelWorkflowRun: async ({ run_id }) => cancelled.push(run_id) } },
    paginate: async (method, options) => method === 'runs' ? runs : options.run_id === 5 ? [{ name: 'Release / CI / Go', conclusion: null }] : [] };
  const execute = (...args) => {
    if (args[0] === 'show') return JSON.stringify({ version: args[1].startsWith(released) ? '1.1.0' : args[1].startsWith(sha) ? '2.0.0' : '1.0.0' });
    return fakeGit({ tags: { 'v2.0.0': sha, 'v1.1.0': released } })(...args);
  };
  await cancelSuperseded({ github, context, execute, log() {} });
  assert.deepEqual(cancelled, [1]);
  await cancelSuperseded({ github, context: { ...context, eventName: 'pull_request' }, execute, log() {} });
  await cancelSuperseded({ github, context, execute: fakeGit({ tags: {} }), log() {} });
  assert.deepEqual(cancelled, [1]);
});
test('failed-job reruns reuse successful architecture artifacts from earlier attempts', () => {
  const artifacts = [
    { id: 1, name: 'image-10-amd64-1' }, { id: 2, name: 'image-10-arm64-2' },
    { id: 3, name: 'image-9-amd64-10' }, { id: 4, name: 'image-10-amd64-3', expired: true },
  ];
  assert.deepEqual(imageArtifacts(artifacts, 10).map(a => a.id), [1, 2]);
  assert.throws(() => imageArtifacts(artifacts.slice(0, 1), 10), /Missing tested arm64/);
});
