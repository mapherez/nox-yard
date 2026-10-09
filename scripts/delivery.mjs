// Pure delivery policy plus injectable Git/GitHub orchestration. No Docker here.
import { spawnSync } from 'node:child_process';
import { parseVersion, readVersion } from './release-version.mjs';

export const gates = ['upgrade', 'jobs', 'managed', 'recreate', 'mcp', 'schedules', 'self-update'];
export function git(...args) {
  const result = spawnSync('git', args, { encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });
  if (result.error || result.status !== 0) throw new Error(result.error?.message || result.stderr || `git ${args} failed`);
  return args.includes('-z') ? result.stdout : result.stdout.trim();
}
export function ancestor(base, head, execute = git) {
  try { execute('merge-base', '--is-ancestor', base, head); return true; } catch { return false; }
}
export function candidateTag(sha, execute = git) {
  const tag = `v${readVersion(execute('show', `${sha}:package.json`))}`;
  try { return execute('rev-parse', '--verify', `refs/tags/${tag}^{commit}`) === sha ? tag : ''; }
  catch { return ''; }
}
export const docsOnly = files => files.length > 0 && files.every(file => /^(Documentation\/|README\.md$|LICENSE(?:\.md)?$)/.test(file));

export function selectAcceptance(files, { baseline = true, full = false } = {}) {
  const selected = new Set();
  const reasons = [];
  const add = (groups, reason) => { groups.forEach(group => selected.add(group)); reasons.push(reason); };
  if (full || !baseline) add(gates, full ? 'Explicit full acceptance requested.' : 'No published stable ancestor baseline; running all gates.');
  else for (const file of files) {
    if (/^(web\/|Documentation\/|README\.md$|LICENSE(?:\.md)?$)/.test(file) || /^scripts\/smoke-.*\.cjs$/.test(file)) continue;
    // Narrow subsystem files take precedence over their shared parent directories.
    if (/^internal\/schedule\//.test(file) || /^internal\/(?:application|httpapi|store)\/schedules(?:[_.]|\.go$)/.test(file) ||
        /^internal\/(?:managed|recreate)\/automatic(?:[_.]|\.go$)/.test(file)) add(['schedules'], `${file}: scheduling`);
    else if (/^internal\/selfupdate\//.test(file) || /^internal\/store\/self_update/.test(file)) add(['self-update'], `${file}: self-update`);
    else if (/^internal\/managed\//.test(file) && !/\/(?:worker|manager|readiness)\.go$/.test(file)) add(['managed', 'mcp', 'schedules'], `${file}: managed projects`);
    else if (/^internal\/recreate\//.test(file) || /^internal\/(?:application|httpapi)\/recreate/.test(file)) add(['recreate', 'mcp', 'schedules'], `${file}: recreation`);
    else if (/^internal\/(?:inventory|lifecycle|mcpapi)\//.test(file)) add(['mcp'], `${file}: inventory/lifecycle/MCP`);
    else if (/^scripts\/smoke-(upgrade|jobs|managed|recreate|mcp|schedules|self-update)\.py$/.test(file)) {
      add([file.match(/smoke-(.+)\.py$/)[1]], `${file}: acceptance fixture`);
    } else add(gates, `${file}: shared, delivery-policy, runtime or unclassified change`);
  }
  return { groups: gates.filter(group => selected.has(group)), reasons: reasons.length ? reasons : ['No affected Docker acceptance groups.'] };
}

export function stableBaseline(releases, sha, execute = git) {
  const candidates = [];
  for (const release of releases) {
    if (release.draft || release.prerelease) continue;
    let parsed, commit;
    try {
      parsed = parseVersion(release.tag_name);
      if (parsed.prerelease || parsed.tag !== release.tag_name) continue;
      commit = execute('rev-parse', '--verify', `refs/tags/${parsed.tag}^{commit}`);
    } catch { continue; }
    if (commit !== sha && ancestor(commit, sha, execute)) candidates.push({ tag: parsed.tag, sha: commit, parts: parsed.version.split('.').map(BigInt) });
  }
  candidates.sort((a, b) => {
    for (let i = 0; i < 3; i++) if (a.parts[i] !== b.parts[i]) return a.parts[i] > b.parts[i] ? -1 : 1;
    return 0;
  });
  return candidates[0] || null;
}

export async function classify({ github, context, execute = git, full = false }) {
  const { sha, eventName, ref, payload, repo } = context;
  const releaseTag = eventName !== 'pull_request' && ref === 'refs/heads/master' ? candidateTag(sha, execute) : '';
  let files = [], base;
  if (eventName === 'pull_request') base = execute('merge-base', payload.pull_request.base.sha, sha);
  else if (eventName === 'push' && payload.before && !/^0+$/.test(payload.before)) base = payload.before;
  if (base) files = execute('diff', '--name-only', base, sha, '--no-renames', '-z', '--').split('\0').filter(Boolean);
  let baseline = null, acceptance = { groups: [], reasons: [] };
  if (releaseTag) {
    const releases = await github.paginate(github.rest.repos.listReleases, { ...repo, per_page: 100 });
    baseline = stableBaseline(releases, sha, execute);
    files = baseline ? execute('diff', '--name-only', baseline.sha, sha, '--no-renames', '-z', '--').split('\0').filter(Boolean) : [];
    // A release-version-only edit carries no runtime changes of its own.
    if (baseline && files.includes('package.json')) {
      const old = JSON.parse(execute('show', `${baseline.sha}:package.json`));
      const now = JSON.parse(execute('show', `${sha}:package.json`));
      delete old.version; delete now.version;
      if (JSON.stringify(old) === JSON.stringify(now)) files = files.filter(file => file !== 'package.json');
    }
    acceptance = selectAcceptance(files, { baseline: Boolean(baseline), full });
  }
  return { releaseTag, version: releaseTag || `git-${sha}`, docsOnly: !releaseTag && docsOnly(files), baseline, ...acceptance };
}

export async function cancelSuperseded({ github, context, execute = git, log = console.log }) {
  if (context.eventName !== 'push' && context.eventName !== 'workflow_dispatch') return [];
  if (context.ref !== 'refs/heads/master' || !candidateTag(context.sha, execute)) return [];
  // Include the old branch CI during migration; never target the old release workflow.
  const runs = [];
  for (const workflow_id of ['pipeline.yml', 'ci.yml']) {
    runs.push(...await github.paginate(github.rest.actions.listWorkflowRuns, {
      ...context.repo, workflow_id, branch: 'master', per_page: 100,
    }));
  }
  const cancelled = [];
  const visited = new Set();
  for (const run of runs) {
    if (visited.has(run.id)) continue;
    visited.add(run.id);
    if (run.id === context.runId || !['queued', 'in_progress', 'waiting', 'pending', 'requested'].includes(run.status) ||
        !['push', 'workflow_dispatch'].includes(run.event) || run.head_branch !== 'master' ||
        run.head_sha === context.sha || !ancestor(run.head_sha, context.sha, execute)) continue;
    // Preserve release runs even if their tag has since disappeared.
    try { if (candidateTag(run.head_sha, execute)) continue; }
    catch { log(`Preserving run ${run.id}: release identity could not be determined.`); continue; }
    const jobs = await github.paginate(github.rest.actions.listJobsForWorkflowRun, { ...context.repo, run_id: run.id, per_page: 100 });
    if (jobs.some(job => /^Release\s*\//.test(job.name) && job.conclusion !== 'skipped')) continue;
    try { await github.rest.actions.cancelWorkflowRun({ ...context.repo, run_id: run.id }); }
    catch (error) { if (error.status === 409) continue; throw error; } // Finished while we inspected it.
    log(`Cancelled superseded normal CI ${run.id} (${run.head_sha}).`);
    cancelled.push(run.id);
  }
  return cancelled;
}

export function imageArtifacts(artifacts, runId) {
  return ['amd64', 'arm64'].map(architecture => {
    const prefix = `image-${runId}-${architecture}-`;
    const matches = artifacts.filter(item => !item.expired && item.name.startsWith(prefix) && /^\d+$/.test(item.name.slice(prefix.length)));
    matches.sort((a, b) => Number(b.name.slice(prefix.length)) - Number(a.name.slice(prefix.length)));
    if (!matches.length) throw new Error(`Missing tested ${architecture} image artifact.`);
    const { id, name } = matches[0];
    return { id, name, architecture };
  });
}
