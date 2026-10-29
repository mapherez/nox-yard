import { createHash } from 'node:crypto';
import { isDeepStrictEqual } from 'node:util';
import { commandRunner } from './release.mjs';

export const approvalJob = 'CI approved (policy v1)';
export const requiredJobs = [
  'Source validation', 'Image (amd64)', 'Image (arm64)',
  'Runtime (1)', 'Runtime (2)', 'Runtime (3)', approvalJob,
];

// Keep the push filter in ci.yml in sync. Files ignored here can never make a
// runtime/build approval reusable for changed inputs (see inputFingerprint).
export function triggersCI(path) {
  if (/(^|\/)(?:README[^/]*|CHANGELOG[^/]*)$/i.test(path) ||
      /\.(?:md|mdx|rst)$/i.test(path) || /(^|\/)\.gitkeep$/.test(path)) return false;
  return /^(?:cmd\/|internal\/|scripts\/|web\/(?:src\/|public\/|package(?:-lock)?\.json$|\.npmrc$|index\.html$|vite\.config\.[^/]+$|tsconfig[^/]*\.json$)|\.github\/workflows\/)/.test(path) ||
    ['go.mod', 'go.sum', '.nvmrc', 'Dockerfile', 'package.json', 'third_party/cosign/LICENSE', '.gitattributes'].includes(path);
}

function ignoredForApproval(path) {
  return /^(?:Documentation\/|docs\/)/.test(path) ||
    /\.(?:md|mdx|rst)$/i.test(path) ||
    /(^|\/)(?:README[^/]*|CHANGELOG[^/]*|\.gitignore|\.gitkeep)$/.test(path) ||
    path === '.editorconfig';
}

// Commit trees, not a dirty checkout or GitHub's truncated changed-file list.
// Include .dockerignore, Compose and other non-triggering inputs conservatively.
// Only package.json's version is normalized; scripts/dependencies stay covered.
export function inputFingerprint(revision, { cwd = process.cwd(), run = commandRunner(cwd) } = {}) {
  if (!/^[a-f0-9]{40}$/.test(revision)) throw new Error('A full CI revision is required.');
  const git = (...args) => run('git', args);
  const hash = createHash('sha256');
  for (const record of git('ls-tree', '-rz', '--full-tree', revision).split('\0').filter(Boolean)) {
    const separator = record.indexOf('\t');
    const [mode, type, object] = record.slice(0, separator).split(' ');
    const path = record.slice(separator + 1);
    if (ignoredForApproval(path)) continue;
    let identity = object;
    if (path === 'package.json') {
      const metadata = JSON.parse(git('show', `${revision}:package.json`));
      delete metadata.version;
      identity = JSON.stringify(metadata);
    }
    hash.update(JSON.stringify([path, mode, type, identity]) + '\n');
  }
  return hash.digest('hex');
}

export function assertVersionOnly(revision, { cwd = process.cwd(), run = commandRunner(cwd) } = {}) {
  const git = (...args) => run('git', args).trim();
  const parents = git('show', '-s', '--format=%P', revision).split(' ').filter(Boolean);
  if (parents.length !== 1) throw new Error('A release request must have exactly one parent.');
  const before = JSON.parse(git('show', `${parents[0]}:package.json`));
  const after = JSON.parse(git('show', `${revision}:package.json`));
  if (before.version === after.version) throw new Error('A release request must change the version.');
  delete before.version;
  delete after.version;
  if (!isDeepStrictEqual(before, after)) throw new Error('Release reuse permits only the package.json version to change.');
}

export function approvedJobs(jobs) {
  return requiredJobs.every((name) => {
    const matches = jobs.filter((job) => job.name === name);
    return matches.length === 1 && matches[0].status === 'completed' && matches[0].conclusion === 'success';
  });
}

// Injected API/clock keep tests offline. No failure here can select full CI.
export async function findApproval({ github, owner, repo, branch, revision, runId, approvalRunId,
  cwd = process.cwd(), run = commandRunner(cwd),
  now = Date.now, sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
  timeoutMs = 35 * 60 * 1000, notice = console.log,
}) {
  const fingerprint = inputFingerprint(revision, { run });
  const candidates = [];
  // Bounded API search; older evidence can be refreshed with a manual CI run.
  for (let page = 1; page <= (approvalRunId ? 1 : 5); page++) {
    const data = approvalRunId
      ? { workflow_runs: [(await github.rest.actions.getWorkflowRun({ owner, repo, run_id: approvalRunId })).data] }
      : (await github.rest.actions.listWorkflowRuns({ owner, repo, workflow_id: 'ci.yml', branch, per_page: 100, page })).data;
    for (const candidate of data.workflow_runs) {
      if (String(candidate.id) === String(runId) || candidate.head_branch !== branch ||
          candidate.head_repository?.full_name !== `${owner}/${repo}` ||
          !['push', 'workflow_dispatch'].includes(candidate.event) ||
          candidate.path !== '.github/workflows/ci.yml' || !/^[a-f0-9]{40}$/.test(candidate.head_sha)) continue;
      try { run('git', ['merge-base', '--is-ancestor', candidate.head_sha, revision]); }
      catch { continue; }
      if (inputFingerprint(candidate.head_sha, { run }) === fingerprint) candidates.push(candidate);
    }
    if (data.workflow_runs.length < 100) break;
  }
  const deadline = now() + timeoutMs;
  let waiting = false;
  while (true) {
    const pending = [];
    for (const candidate of candidates) {
      const { data: current } = await github.rest.actions.getWorkflowRun({ owner, repo, run_id: candidate.id });
      if (current.status !== 'completed') { pending.push(candidate); continue; }
      if (current.conclusion !== 'success') continue;
      const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRun,
        { owner, repo, run_id: candidate.id, filter: 'latest', per_page: 100 });
      if (approvedJobs(jobs)) return { revision: candidate.head_sha, runId: candidate.id, fingerprint };
    }
    if (!pending.length || now() >= deadline) break;
    if (!waiting) notice('Matching CI is still running. Waiting for its approval; no tests or builds are being repeated.');
    waiting = true;
    // Recheck only the candidates whose CI is still running.
    candidates.splice(0, candidates.length, ...pending);
    await sleep(Math.min(15000, Math.max(0, deadline - now())));
  }
  throw new Error('No successful CI approval with identical inputs was found. Release stopped before tests/builds. Push and validate the source first, or run CI manually on this branch, then rerun this release workflow. Full CI is never started automatically by a release request.');
}

export async function planCI({ github, context, core, run = commandRunner(process.cwd()) }) {
  const { requestedRelease } = await import('./release-request.mjs');
  const tag = requestedRelease({ revision: context.sha, ref: context.ref, event: context.eventName, run });
  core.setOutput('tag', tag);
  if (tag) {
    assertVersionOnly(context.sha, { run });
    const approval = await findApproval({ github, ...context.repo, branch: context.ref.slice('refs/heads/'.length),
      revision: context.sha, runId: context.runId, run, notice: (message) => core.notice(message) });
    core.setOutput('approved_sha', approval.revision);
    core.setOutput('approved_run', String(approval.runId));
    core.setOutput('mode', 'release');
    await core.summary.addHeading('Release reuses approved CI')
      .addRaw(`Approved source: ${approval.revision}\n\nCI run: ${approval.runId}\n\nInput fingerprint: ${approval.fingerprint}\n\nNo source tests or CI image builds will run for this release request.`).write();
    return;
  }
  // Native path filters skip ordinary documentation-only pushes. Recheck the
  // complete diff for GitHub's >1,000-commit/path-filter fallback as well.
  let full = context.eventName === 'workflow_dispatch';
  if (!full) {
    const before = context.payload.before;
    const files = !before || /^0+$/.test(before)
      ? run('git', ['ls-tree', '-rz', '--name-only', context.sha])
      : run('git', ['diff', '--name-only', '--no-renames', '-z', before, context.sha]);
    full = files.split('\0').some(triggersCI);
  }
  core.setOutput('mode', full ? 'full' : 'skip');
  core.notice(full ? 'Full CI: source, both images and all runtime groups are required.' : 'No CI inputs changed; heavy jobs skipped.');
}
