import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { approvedJobs, approvalJob, assertVersionOnly, findApproval, inputFingerprint, planCI, requiredJobs, triggersCI } from './ci-policy.mjs';
import { commandRunner } from './release.mjs';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const workflow = readFileSync(join(root, '.github/workflows/ci.yml'), 'utf8');
const publisher = readFileSync(join(root, '.github/workflows/release.yml'), 'utf8');
const heavy = ['Source validation', 'Image (amd64)', 'Image (arm64)', 'Runtime (1)', 'Runtime (2)', 'Runtime (3)'];

function fixture(t) {
  const cwd = mkdtempSync(join(tmpdir(), 'yard-ci-policy-'));
  t.after(() => rmSync(cwd, { recursive: true, force: true }));
  const run = commandRunner(cwd);
  const git = (...args) => run('git', args).trim();
  git('-c', 'init.templateDir=', 'init', '--initial-branch=master');
  for (const [key, value] of Object.entries({ 'user.name': 'CI tests', 'user.email': 'ci@example.test',
    'commit.gpgSign': 'false', 'tag.gpgSign': 'false', 'core.hooksPath': join(cwd, 'no-hooks'), 'core.autocrlf': 'false' })) git('config', key, value);
  const write = (path, text) => { mkdirSync(dirname(join(cwd, path)), { recursive: true }); writeFileSync(join(cwd, path), text); };
  write('package.json', JSON.stringify({ name: 'nox-yard', version: '1.0.0', scripts: { release: 'node scripts/release.mjs' } }));
  write('internal/example.go', 'package example\n');
  write('.github/workflows/ci.yml', workflow);
  const commit = (subject = 'source', ...messages) => { git('add', '.'); git('commit', '-m', subject, ...messages.flatMap((message) => ['-m', message])); return git('rev-parse', 'HEAD'); };
  const source = commit();
  const release = (extra = {}) => {
    const metadata = JSON.parse(readFileSync(join(cwd, 'package.json')));
    write('package.json', JSON.stringify({ ...metadata, version: '1.0.1', ...extra }));
    const revision = commit('chore: release v1.0.1', 'NoX-Yard-Release: v1.0.1');
    git('tag', '-a', 'v1.0.1', '-m', 'Release v1.0.1');
    return revision;
  };
  return { cwd, run, git, write, commit, source, release };
}

function successfulJobs() { return requiredJobs.map((name) => ({ name, status: 'completed', conclusion: 'success' })); }

function api(f, revisions = [f.source]) {
  const runs = revisions.map((head_sha, i) => ({ id: i + 10, head_sha, head_branch: 'master',
    head_repository: { full_name: 'owner/yard' }, event: 'push', path: '.github/workflows/ci.yml',
    status: 'completed', conclusion: 'success' }));
  const calls = [];
  const jobs = new Map(runs.map((run) => [run.id, successfulJobs()]));
  const github = {
    rest: { actions: {
      listWorkflowRuns: async (args) => { calls.push(['runs', args]); return { data: { workflow_runs: args.page === 1 ? runs : [] } }; },
      getWorkflowRun: async (args) => { calls.push(['run', args]); return { data: runs.find((run) => String(run.id) === String(args.run_id)) }; },
      listJobsForWorkflowRun: Symbol('jobs'),
    } },
    paginate: async (method, args) => { assert.equal(method, github.rest.actions.listJobsForWorkflowRun); assert.equal(args.filter, 'latest'); calls.push(['jobs', args]); return jobs.get(Number(args.run_id)); },
  };
  return { github, runs, jobs, calls,
    find: (revision, options = {}) => findApproval({ github, owner: 'owner', repo: 'yard', branch: 'master', revision, runId: 999, run: f.run, timeoutMs: 0, ...options }) };
}

function coreFixture() {
  const outputs = {};
  const messages = [];
  const summary = { addHeading() { return this; }, addRaw() { return this; }, async write() {} };
  return { outputs, core: { setOutput: (key, value) => { outputs[key] = value; }, notice: (message) => messages.push(message), summary } };
}

// Exercise the actual paths from ci.yml, including ordered exclusions. The
// supported globs here are exactly the *, ** patterns used in that filter.
function workflowTriggers(path) {
  const section = workflow.split('    paths:\n')[1].split('  workflow_dispatch:')[0];
  const patterns = [...section.matchAll(/^      - '([^']+)'$/gm)].map((match) => match[1]);
  let included = false;
  for (const pattern of patterns) {
    const negative = pattern.startsWith('!');
    const value = negative ? pattern.slice(1) : pattern;
    let regex = '';
    for (let i = 0; i < value.length; i++) {
      if (value.slice(i, i + 3) === '**/') { regex += '(?:.*/)?'; i += 2; }
      else if (value.slice(i, i + 2) === '**') { regex += '.*'; i++; }
      else if (value[i] === '*') regex += '[^/]*';
      else regex += value[i].replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    }
    if (new RegExp(`^${regex}$`).test(path)) included = !negative;
  }
  return included;
}

for (const [expected, paths] of [
  [true, ['cmd/nox-yard/main.go', 'internal/store/testdata/input.txt', 'go.mod', 'go.sum', '.nvmrc', 'Dockerfile', 'package.json', '.gitattributes',
    'web/src/App.tsx', 'web/public/favicon.svg', 'web/index.html', 'web/package.json', 'web/package-lock.json', 'web/.npmrc',
    'web/vite.config.ts', 'web/tsconfig.json', 'web/tsconfig.node.json', 'scripts/fixtures/managed-acceptance.yaml', 'scripts/ci-policy.mjs',
    '.github/workflows/ci.yml', '.github/workflows/release.yml', 'third_party/cosign/LICENSE']],
  [false, ['Documentation/Developer/Releases.md', 'Documentation/guide.yaml', 'README.md', 'CHANGELOG.md', '.gitignore', '.dockerignore',
    'compose.yaml', 'compose.dev.yaml', 'examples/compose.yaml', '.env.example', 'web/src/.gitkeep', 'scripts/README.md',
    'internal/README.txt', 'web/public/CHANGELOG', 'third_party/cosign/README.md', 'LICENSE', '.editorconfig', 'deploy/.gitkeep']],
]) {
  for (const path of paths) test(`push filter ${expected ? 'includes' : 'excludes'} ${path}`, () => {
    assert.equal(triggersCI(path), expected);
    assert.equal(workflowTriggers(path), expected, 'native workflow and full-diff policy must agree');
  });
}

test('approval fingerprint survives docs/notes changes and only the release version bump', (t) => {
  const f = fixture(t);
  f.write('Documentation/new.md', 'new docs'); f.write('CHANGELOG.md', 'notes'); f.write('.gitignore', 'local data');
  const docs = f.commit('docs');
  const revision = f.release();
  assert.equal(inputFingerprint(f.source, { run: f.run }), inputFingerprint(docs, { run: f.run }));
  assert.equal(inputFingerprint(f.source, { run: f.run }), inputFingerprint(revision, { run: f.run }));
  assertVersionOnly(revision, { run: f.run });
});

for (const path of ['internal/example.go', 'web/src/main.tsx', 'go.sum', '.dockerignore', '.gitattributes', 'compose.yaml', '.env.example', '.github/workflows/ci.yml', 'third_party/cosign/LICENSE']) {
  test(`approval fingerprint covers ${path}, even when it does not trigger CI`, (t) => {
    const f = fixture(t); f.write(path, 'changed'); const revision = f.commit();
    assert.notEqual(inputFingerprint(f.source, { run: f.run }), inputFingerprint(revision, { run: f.run }));
  });
}

test('release marker cannot hide script/dependency changes inside package.json', (t) => {
  const f = fixture(t); const revision = f.release({ scripts: { release: 'malicious command' } });
  assert.throws(() => assertVersionOnly(revision, { run: f.run }), /only the package.json version/);
  assert.notEqual(inputFingerprint(f.source, { run: f.run }), inputFingerprint(revision, { run: f.run }));
});

test('approval covers executable modes, deleted files and renames', (t) => {
  const f = fixture(t);
  f.git('update-index', '--chmod=+x', 'internal/example.go'); f.git('commit', '-m', 'mode');
  assert.notEqual(inputFingerprint(f.source, { run: f.run }), inputFingerprint(f.git('rev-parse', 'HEAD'), { run: f.run }));
  f.git('mv', 'internal/example.go', 'internal/renamed.go'); const renamed = f.commit('rename');
  assert.notEqual(inputFingerprint(f.source, { run: f.run }), inputFingerprint(renamed, { run: f.run }));
  f.git('rm', 'internal/renamed.go'); const removed = f.commit('delete');
  assert.notEqual(inputFingerprint(renamed, { run: f.run }), inputFingerprint(removed, { run: f.run }));
});

test('first version-only release reuses approval across intervening documentation commits', async (t) => {
  const f = fixture(t); const mock = api(f);
  f.write('CHANGELOG.md', '1.0.1 notes'); f.commit('notes'); const revision = f.release();
  const found = await mock.find(revision);
  assert.equal(found.revision, f.source); assert.equal(found.runId, 10);
});

for (const name of requiredJobs) for (const conclusion of ['failure', 'cancelled', 'skipped', null]) {
  test(`reuse refuses ${name} with conclusion ${conclusion}`, () => {
    const jobs = successfulJobs(); jobs.find((job) => job.name === name).conclusion = conclusion;
    assert.equal(approvedJobs(jobs), false);
  });
}

test('missing, duplicate, pending and old-policy jobs cannot grant approval', () => {
  assert.equal(approvedJobs(successfulJobs().slice(1)), false);
  assert.equal(approvedJobs([...successfulJobs(), successfulJobs()[0]]), false);
  const jobs = successfulJobs(); jobs[0].status = 'in_progress'; assert.equal(approvedJobs(jobs), false);
  assert.equal(approvedJobs(successfulJobs().map((job) => ({ ...job, name: job.name.replace('policy v1', 'policy v0') }))), false);
});

for (const change of [{ conclusion: 'failure' }, { conclusion: 'cancelled' }, { event: 'pull_request' }, { head_branch: 'other' },
  { head_repository: { full_name: 'fork/yard' } }, { path: '.github/workflows/other.yml' }, { id: 999 }]) {
  test(`reuse rejects unrelated or unsuccessful run ${JSON.stringify(change)}`, async (t) => {
    const f = fixture(t); const mock = api(f); Object.assign(mock.runs[0], change);
    await assert.rejects(mock.find(f.release()), /stopped before tests\/builds/);
  });
}

test('non-ancestor CI with the same tree cannot grant approval', async (t) => {
  const f = fixture(t); f.git('checkout', '--orphan', 'unrelated'); f.git('commit', '-m', 'unrelated root');
  const unrelated = f.git('rev-parse', 'HEAD'); f.git('checkout', 'master');
  const mock = api(f, [unrelated]);
  assert.equal(inputFingerprint(unrelated, { run: f.run }), inputFingerprint(f.source, { run: f.run }));
  await assert.rejects(mock.find(f.release()), /stopped before tests\/builds/);
});

test('a green workflow with skipped/missing runtime jobs is rejected', async (t) => {
  const f = fixture(t); const mock = api(f); mock.jobs.set(10, [{ name: approvalJob, status: 'completed', conclusion: 'success' }]);
  await assert.rejects(mock.find(f.release()), /stopped before tests\/builds/);
});

test('changed inputs fail immediately; no test/build process or fallback is started', async (t) => {
  const f = fixture(t); const mock = api(f); f.write('.dockerignore', '*'); f.commit('ignored build input');
  let slept = false;
  await assert.rejects(mock.find(f.release(), { sleep: async () => { slept = true; } }), /Full CI is never started automatically/);
  assert.equal(slept, false); assert.equal(mock.calls.some(([kind]) => kind === 'jobs'), false);
});

test('in-flight equivalent CI is awaited once, without being cancelled or rerun', async (t) => {
  const f = fixture(t); const mock = api(f); mock.runs[0].status = 'in_progress'; mock.runs[0].conclusion = null;
  let time = 0; let sleeps = 0;
  const result = await mock.find(f.release(), { now: () => time, timeoutMs: 20000, notice() {}, sleep: async (ms) => {
    sleeps++; time += ms; mock.runs[0].status = 'completed'; mock.runs[0].conclusion = 'success';
  } });
  assert.equal(result.runId, 10); assert.equal(sleeps, 1);
});

test('pending CI failure and timeout both stop without scheduling full CI', async (t) => {
  const f = fixture(t); const mock = api(f); const revision = f.release();
  for (const fail of [false, true]) {
    mock.runs[0].status = 'queued'; mock.runs[0].conclusion = null; let time = 0;
    await assert.rejects(mock.find(revision, { now: () => time, timeoutMs: 15, notice() {}, sleep: async (ms) => {
      time += ms; if (fail) { mock.runs[0].status = 'completed'; mock.runs[0].conclusion = 'failure'; }
    } }), /stopped before tests\/builds/);
  }
});

test('GitHub API errors are not approvals and do not select full CI', async (t) => {
  const f = fixture(t); const mock = api(f);
  mock.github.rest.actions.listWorkflowRuns = async () => { throw new Error('403 API denied'); };
  await assert.rejects(mock.find(f.release()), /403 API denied/);
});

test('publisher rechecks the pinned approval, independent of newer equivalent runs', async (t) => {
  const f = fixture(t); const mock = api(f, [f.source, f.source]);
  const found = await mock.find(f.release(), { approvalRunId: '11' });
  assert.equal(found.runId, 11); assert.equal(mock.calls.some(([kind]) => kind === 'runs'), false);
});

test('approval search paginates past the first 100 workflow runs', async (t) => {
  const f = fixture(t); const mock = api(f);
  const approved = { ...mock.runs[0] };
  mock.github.rest.actions.listWorkflowRuns = async (args) => {
    mock.calls.push(['runs', args]);
    return { data: { workflow_runs: args.page === 1
      ? Array.from({ length: 100 }, (_, i) => ({ ...approved, id: 1000 + i, head_branch: 'other' })) : [approved] } };
  };
  assert.equal((await mock.find(f.release())).runId, approved.id);
  assert.deepEqual(mock.calls.filter(([kind]) => kind === 'runs').map(([, args]) => args.page), [1, 2]);
});

test('explicit manual validation on the release SHA can unblock its retry', async (t) => {
  const f = fixture(t); const revision = f.release(); const mock = api(f, [revision]); mock.runs[0].event = 'workflow_dispatch';
  assert.equal((await mock.find(revision)).revision, revision);
});

function context(f, sha, extra = {}) {
  return { sha, ref: 'refs/heads/master', eventName: 'push', runId: 999, repo: { owner: 'owner', repo: 'yard' }, payload: { before: f.source }, ...extra };
}

test('release plan selects only publication after verifying approval', async (t) => {
  const f = fixture(t); const mock = api(f); const c = coreFixture();
  await planCI({ github: mock.github, context: context(f, f.release()), core: c.core, run: f.run });
  assert.equal(c.outputs.mode, 'release'); assert.equal(c.outputs.tag, 'v1.0.1'); assert.equal(c.outputs.approved_run, '10');
});

test('missing release approval leaves no mode capable of scheduling heavy jobs', async (t) => {
  const f = fixture(t); const mock = api(f, []); const c = coreFixture();
  await assert.rejects(planCI({ github: mock.github, context: context(f, f.release()), core: c.core, run: f.run }), /No successful CI approval/);
  assert.equal(c.outputs.mode, undefined);
});

test('a marked release that changes package scripts cannot schedule any heavy job', async (t) => {
  const f = fixture(t); const mock = api(f); const c = coreFixture();
  await assert.rejects(planCI({ github: mock.github, context: context(f, f.release({ scripts: { release: 'changed' } })), core: c.core, run: f.run }), /only the package.json version/);
  assert.equal(c.outputs.mode, undefined);
  assert.equal(mock.calls.length, 0);
});

test('manual dispatch at a marked release commit validates without publishing', async (t) => {
  const f = fixture(t); const c = coreFixture();
  await planCI({ context: context(f, f.release(), { eventName: 'workflow_dispatch' }), core: c.core, run: f.run });
  assert.equal(c.outputs.mode, 'full'); assert.equal(c.outputs.tag, '');
});

test('all changes in a multi-commit push are considered, including code before a docs-only HEAD', async (t) => {
  const f = fixture(t); f.write('internal/example.go', 'changed'); f.commit('code'); f.write('README.md', 'docs'); const sha = f.commit('docs');
  const c = coreFixture(); await planCI({ context: context(f, sha), core: c.core, run: f.run }); assert.equal(c.outputs.mode, 'full');
});

test('docs-only push fallback skips heavy jobs, but manual dispatch validates explicitly', async (t) => {
  const f = fixture(t); f.write('README.md', 'docs'); const sha = f.commit('docs');
  for (const eventName of ['push', 'workflow_dispatch']) {
    const c = coreFixture(); await planCI({ context: context(f, sha, { eventName }), core: c.core, run: f.run });
    assert.equal(c.outputs.mode, eventName === 'push' ? 'skip' : 'full');
  }
});

test('ordinary package version change is validation, never an implicit release', async (t) => {
  const f = fixture(t); f.write('package.json', '{"name":"nox-yard","version":"1.0.1"}'); const sha = f.commit('version');
  const c = coreFixture(); await planCI({ context: context(f, sha), core: c.core, run: f.run });
  assert.equal(c.outputs.mode, 'full'); assert.equal(c.outputs.tag, '');
});

test('new branch validates its complete tree, not only its last commit', async (t) => {
  const f = fixture(t); f.write('README.md', 'docs'); const sha = f.commit('docs');
  const c = coreFixture(); await planCI({ context: context(f, sha, { payload: { before: '0'.repeat(40) } }), core: c.core, run: f.run });
  assert.equal(c.outputs.mode, 'full');
});

test('workflow heavy jobs require full mode; release does not depend on skipped jobs', () => {
  for (const id of ['validate', 'image', 'runtime']) {
    const job = workflow.split(`\n  ${id}:\n`)[1].split(/\n  [a-z]+:\n/)[0];
    assert.match(job, /if: needs\.plan\.outputs\.mode == 'full'/);
  }
  const release = workflow.split('\n  release:\n')[1];
  assert.match(release, /needs: plan\n/); assert.match(release, /if: needs\.plan\.outputs\.mode == 'release'/);
  assert.match(workflow, /name: CI approved \(policy v1\)/);
  for (const name of heavy.slice(0, 1)) assert.ok(workflow.includes(`name: ${name}`));
  assert.match(publisher, /group: nox-yard-publication\n      cancel-in-progress: false/);
  assert.match(publisher, /--certificate-github-workflow-trigger push/);
  assert.match(publisher, /BUILD_SHA=\$\{\{ github.sha \}\}/);
  assert.match(publisher, /BUILD_VERSION=\$\{\{ inputs.tag \}\}/);
  assert.equal((publisher.match(/uses: docker\/build-push-action/g) || []).length, 1);
  const build = publisher.split('      - name: Build and publish release image\n')[1].split('      - name:')[0];
  assert.match(build, /steps\.receipt\.outputs\.id == ''/);
  for (const name of ['Smoke test the final release images without rebuilding', 'Sign the published image digest',
    'Verify the release signature and workflow identity', 'Create GitHub Release with user-facing notes']) {
    const step = publisher.split(`      - name: ${name}\n`)[1].split('      - name:')[0];
    assert.match(step, /IMAGE_DIGEST: \$\{\{ steps.image.outputs.digest \}\}/);
  }
});

test('changed shell entry points parse without starting Docker', () => {
  for (const path of ['scripts/ci-runtime-cache.sh', 'scripts/smoke-arm64.sh', 'scripts/test.sh']) {
    const result = spawnSync('sh', ['-n', join(root, path)], { encoding: 'utf8' });
    assert.equal(result.status, 0, result.stderr);
  }
});
