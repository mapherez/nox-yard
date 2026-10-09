#!/usr/bin/env node
// Validate Git objects, not the possibly dirty checkout. No dependency installs.
import { spawnSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { readVersion, isMain } from './release-version.mjs';

export function run(command, args, input, cwd = process.cwd()) {
  const result = spawnSync(command, args, { cwd, input, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 });
  if (result.error || result.status !== 0) {
    throw new Error(`${command} ${args.join(' ')}: ${result.error?.message || result.stderr || result.stdout || `exit ${result.status}`}`);
  }
  return result.stdout;
}

export function checkRange(base, head, { execute = run, log = console.error } = {}) {
  const git = (...args) => execute('git', args);
  const commit = git('rev-parse', '--verify', `${head}^{commit}`).trim();
  let previous;
  try { if (base && !/^0+$/.test(base)) previous = git('rev-parse', '--verify', `${base}^{commit}`).trim(); } catch {}
  if (!previous && (!base || /^0+$/.test(base))) {
    for (const ref of git('for-each-ref', '--format=%(refname)', 'refs/remotes').trim().split('\n').filter(Boolean)) {
      try {
        const candidate = git('merge-base', commit, ref).trim();
        if (!previous || git('merge-base', previous, candidate).trim() === previous) previous = candidate;
      } catch {}
    }
  }
  previous ||= execute('git', ['hash-object', '-t', 'tree', '--stdin'], '').trim();
  git('diff', '--check', previous, commit, '--');
  const files = git('diff', '--name-only', '--diff-filter=ACMR', '-z', previous, commit, '--').split('\0').filter(Boolean);
  for (const file of files) {
    const contents = git('show', `${commit}:${file}`);
    if (file.endsWith('.go')) {
      if (execute('gofmt', [], contents) !== contents) throw new Error(`${file} needs gofmt (pushed commit ${commit}).`);
    }
    if (/\.(?:mjs|cjs|js)$/.test(file)) {
      execute(process.execPath, ['--input-type=' + (file.endsWith('.cjs') ? 'commonjs' : 'module'), '--check'], contents);
    }
    if (/(?:^|\/)(?:package(?:-lock)?\.json)$/.test(file)) JSON.parse(contents);
  }
  if (files.includes('package.json')) readVersion(git('show', `${commit}:package.json`));
  if (files.some(file => ['web/package.json', 'web/package-lock.json'].includes(file))) {
    const manifest = JSON.parse(git('show', `${commit}:web/package.json`));
    const lock = JSON.parse(git('show', `${commit}:web/package-lock.json`));
    for (const key of ['dependencies', 'devDependencies', 'optionalDependencies']) {
      const ordered = value => Object.entries(value || {}).sort(([a], [b]) => a.localeCompare(b));
      if (JSON.stringify(ordered(manifest[key])) !== JSON.stringify(ordered(lock.packages?.['']?.[key]))) {
        throw new Error(`web/package-lock.json is out of sync with package.json (${key}).`);
      }
    }
  }
  log(`[pass] Push checks: ${files.length} changed files (${commit.slice(0, 12)}).`);
}

export function checkPush(input, { execute = run, env = process.env, log = console.error } = {}) {
  const updates = input.trim().split('\n').filter(Boolean).map(line => {
    const [ref, head, remoteRef, base] = line.trim().split(/\s+/);
    if (!ref || !remoteRef || !/^[a-f0-9]{40,64}$/.test(head || '') || !/^[a-f0-9]{40,64}$/.test(base || '')) {
      throw new Error('Invalid pre-push ref input.');
    }
    return { ref, head, base };
  }).filter(({ head }) => !/^0+$/.test(head));
  const commits = new Map();
  // Prefer branch ranges over new tags pointing to that same commit.
  for (const update of updates.sort((a, b) => Number(b.ref.startsWith('refs/heads/')) - Number(a.ref.startsWith('refs/heads/')))) {
    const commit = execute('git', ['rev-parse', '--verify', `${update.head}^{commit}`]).trim();
    if (!commits.has(commit)) commits.set(commit, update);
  }
  for (const [commit, { base }] of commits) {
    const tree = execute('git', ['rev-parse', `${commit}^{tree}`]).trim();
    if (env.NOX_PUSH_CHECKED_TREE === tree && env.NOX_PUSH_CHECKED_BASE === base) {
      log(`[pass] Push checks already completed for release tree ${tree.slice(0, 12)}.`);
      continue;
    }
    checkRange(base, commit, { execute, log });
  }
}

if (isMain(import.meta.url)) {
  try {
    if (process.argv[2] === '--release') {
      if (process.argv.length !== 5) throw new Error('Usage: push-check.mjs --release <remote-base> <head>');
      checkRange(process.argv[3], process.argv[4]);
      readVersion(readFileSync('package.json'));
      console.error('[pass] Pending release package.json.');
    } else checkPush(readFileSync(0, 'utf8'));
  } catch (error) { console.error(`[FAIL] ${error.message}`); process.exitCode = 1; }
}
