import { spawnSync } from 'node:child_process';
import { readFileSync, realpathSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { isMain, parseVersion, readVersion } from './release-version.mjs';

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..');

export function commandRunner(cwd) {
  return (command, args, { inherit = false, env = process.env } = {}) => {
    const result = spawnSync(command, args, {
      cwd, env, encoding: 'utf8', stdio: inherit ? 'inherit' : 'pipe',
      maxBuffer: 16 * 1024 * 1024,
    });
    if (result.error || result.status !== 0) {
      const detail = result.error?.message || result.stderr?.trim() || `exit ${result.status}, signal ${result.signal}`;
      throw new Error(`${command} ${args.join(' ')} failed: ${detail}`);
    }
    return result.stdout || '';
  };
}

const quote = (value) => `'${value.replaceAll("'", "'\\''")}'`;

function changedFiles(status) {
  const records = status.split('\0').filter(Boolean);
  const files = [];
  for (let i = 0; i < records.length; i++) {
    const code = records[i].slice(0, 2);
    const path = records[i].slice(3);
    files.push({ code, path });
    if (/[RC]/.test(code)) files.push({ code, path: records[++i] });
  }
  return files;
}

export function release(argv, {
  cwd = process.cwd(), root = repositoryRoot, run = commandRunner(cwd), log = console.log,
} = {}) {
  if (argv.length !== 1) throw new Error('Usage: npm run release -- <version> (exactly one argument).');
  const { version, tag } = parseVersion(argv[0]);
  const git = (...args) => run('git', args).trim();
  const status = () => changedFiles(run('git', ['status', '--porcelain=v1', '-z', '--untracked-files=all']));
  if (realpathSync(cwd) !== realpathSync(root) || realpathSync(git('rev-parse', '--show-toplevel')) !== realpathSync(root)) {
    throw new Error('Run the release command from the NoX Yard repository root.');
  }
  if (JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).name !== 'nox-yard' ||
      !/^module github\.com\/mapherez\/nox-yard\r?$/m.test(readFileSync(join(root, 'go.mod'), 'utf8'))) {
    throw new Error('This is not the NoX Yard repository.');
  }
  const dirty = status();
  if (dirty.length) throw new Error(`Working tree must be completely clean: ${dirty.map(({ path }) => path).join(', ')}`);
  // symbolic-ref fails for detached HEAD; never infer the branch from a default name.
  let branch;
  try { branch = git('symbolic-ref', '--quiet', '--short', 'HEAD'); }
  catch { throw new Error('Cannot release from detached HEAD. Check out a branch first.'); }
  if (branch !== 'master') throw new Error('Official releases are only allowed from master.');
  git('-c', 'user.useConfigOnly=true', 'var', 'GIT_AUTHOR_IDENT');
  git('-c', 'user.useConfigOnly=true', 'var', 'GIT_COMMITTER_IDENT');
  const originalHead = git('rev-parse', '--verify', 'HEAD^{commit}');
  git('ls-files', '--error-unmatch', '--', 'package.json');
  const versionPath = join(root, 'package.json');
  const original = readFileSync(versionPath);
  if (readVersion(original) === version) throw new Error(`package.json version is already ${version}.`);
  if (git('tag', '--list', tag)) throw new Error(`Tag ${tag} already exists locally.`);
  // A failed lookup throws. An inaccessible origin is never treated as an absent tag.
  const remoteTags = git('ls-remote', '--tags', 'origin', `refs/tags/${tag}`, `refs/tags/${tag}^{}`);
  if (remoteTags) throw new Error(`Tag ${tag} already exists on origin.`);
  const remoteBranch = git('ls-remote', '--heads', 'origin', 'refs/heads/master');
  const remoteBase = remoteBranch ? remoteBranch.split(/\s+/)[0] : '0'.repeat(originalHead.length);

  const branchRef = `refs/heads/${branch}`;
  const tagRef = `refs/tags/${tag}`;
  const pushArgs = ['push', '--atomic', 'origin', `${branchRef}:${branchRef}`, `${tagRef}:${tagRef}`];
  const pushCommand = `git ${pushArgs.map(quote).join(' ')}`;
  let commit;
  let commitAttempted = false;
  let staged = false;
  let tagCreated = false;
  const metadata = JSON.parse(original.toString('utf8'));
  metadata.version = version;
  const newline = original.includes(Buffer.from('\r\n')) ? '\r\n' : '\n';
  const updated = Buffer.from(JSON.stringify(metadata, null, 2).replaceAll('\n', newline) +
    (original.toString('utf8').endsWith('\n') ? newline : ''));
  try {
    writeFileSync(versionPath, updated);
    log(`Running lightweight push checks for ${tag} (no Docker or builds)...`);
    run(process.execPath, ['scripts/push-check.mjs', '--release', remoteBase, originalHead], { inherit: true });
    const unexpected = status().filter(({ code, path }) => path !== 'package.json' || code !== ' M');
    if (unexpected.length) {
      throw new Error(`Checks changed unexpected files: ${unexpected.map(({ code, path }) => `${code} ${path}`).join(', ')}`);
    }
    if (!readFileSync(versionPath).equals(updated)) throw new Error('Checks changed package.json unexpectedly.');
    if (git('rev-parse', 'HEAD') !== originalHead || git('symbolic-ref', '--short', 'HEAD') !== branch) {
      throw new Error('HEAD or the current branch changed during validation.');
    }
    staged = true;
    git('add', '--', 'package.json');
    if (git('diff', '--cached', '--name-only') !== 'package.json') throw new Error('Release index must contain only package.json.');
    commitAttempted = true;
    git('commit', '-m', `chore: release ${tag}`);
    commit = git('rev-parse', 'HEAD');
    if (git('rev-parse', `${commit}^`) !== originalHead ||
        git('diff-tree', '--no-commit-id', '--name-only', '-r', commit) !== 'package.json' ||
        readVersion(git('show', `${commit}:package.json`)) !== version || status().length) {
      throw new Error('Release commit or working tree changed unexpectedly. Inspect the local commit before continuing.');
    }
    git('tag', '-a', tag, '-m', `Release ${tag}`, commit);
    tagCreated = true;
    if (git('rev-parse', `${tag}^{commit}`) !== commit) throw new Error('Release tag does not point to the release commit.');
    log(`Pushing ${branch} and ${tag} atomically...`);
    run('git', pushArgs, { inherit: true, env: { ...process.env,
      NOX_PUSH_CHECKED_TREE: git('rev-parse', `${commit}^{tree}`), NOX_PUSH_CHECKED_BASE: remoteBase,
    } });
  } catch (error) {
    // If Git created a commit before reporting an error, preserve that commit too.
    const currentHead = git('rev-parse', 'HEAD');
    if (!commit && commitAttempted && currentHead !== originalHead) commit = currentHead;
    if (!commit) {
      writeFileSync(versionPath, original);
      if (staged) git('restore', '--staged', '--', 'package.json');
      throw new Error(`${error.message}\npackage.json restored byte-for-byte. No release commit, tag or push was created.`);
    }
    const recovery = tagCreated
      ? pushCommand
      : `git tag -a ${quote(tag)} -m ${quote(`Release ${tag}`)} ${quote(commit)}\n${pushCommand}`;
    throw new Error(`${error.message}\nRelease commit ${commit}${tagCreated ? ` and annotated tag ${tag}` : ' (without a release tag)'} remain local. No automatic reset was performed.\nInspect the local state, then recover in a POSIX shell (Git Bash on Windows):\n${recovery}`);
  }
  log(`Pushed ${tag}. GitHub Actions will validate, publish the Docker image and create the GitHub Release.`);
  return { version, tag, branch, commit };
}

if (isMain(import.meta.url)) {
  try { release(process.argv.slice(2)); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
