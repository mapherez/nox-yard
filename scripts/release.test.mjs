import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';
import { commandRunner, release } from './release.mjs';
import { assertLatestStable, checkReleaseVersion, parseVersion } from './release-version.mjs';
import { image, releaseMetadata, source, writeMetadataOutputs } from './release-metadata.mjs';
import { publicationMarker, requestedRelease, shouldPublish } from './release-request.mjs';

const scripts = dirname(fileURLToPath(import.meta.url));
const sha = 'a'.repeat(40);
const created = '2026-10-06T12:00:00.000Z';

function fixture(t, original = '0.0.0\n') {
  const directory = mkdtempSync(join(tmpdir(), 'nox-yard-release-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const root = join(directory, 'yard');
  const remote = join(directory, 'origin.git');
  mkdirSync(join(root, 'scripts'), { recursive: true });
  const realRun = commandRunner(root);
  const git = (...args) => realRun('git', args).trim();
  git('-c', 'init.templateDir=', 'init', '--initial-branch=delivery/topic');
  for (const [key, value] of Object.entries({
    'user.name': 'Release Tests', 'user.email': 'release@example.test',
    'commit.gpgSign': 'false', 'tag.gpgSign': 'false', 'core.autocrlf': 'false',
    'core.hooksPath': join(directory, 'no-hooks'),
  })) git('config', key, value);
  const newline = original.includes('\r\n') ? '\r\n' : '\n';
  writeFileSync(join(root, 'package.json'), JSON.stringify({ name: 'nox-yard', private: true, version: original.trim(), scripts: { release: 'node scripts/release.mjs' } }, null, 2).replaceAll('\n', newline) + newline);
  writeFileSync(join(root, 'go.mod'), 'module github.com/mapherez/nox-yard\n');
  writeFileSync(join(root, 'Dockerfile'), 'FROM scratch\n');
  writeFileSync(join(root, 'other.txt'), 'unchanged\n');
  writeFileSync(join(root, 'scripts', 'ci-local.sh'), '#!/bin/sh\nexit 0\n');
  for (const name of ['release.mjs', 'release-version.mjs', 'release-metadata.mjs', 'release-request.mjs', 'check-release-version.mjs']) {
    copyFileSync(join(scripts, name), join(root, 'scripts', name));
  }
  git('add', '.');
  git('commit', '-m', 'Fixture baseline');
  git('-c', 'init.templateDir=', 'init', '--bare', remote);
  git('remote', 'add', 'origin', remote);
  git('push', 'origin', 'HEAD:refs/heads/delivery/topic');
  const originalHead = git('rev-parse', 'HEAD');
  const calls = [];
  const messages = [];
  const controls = {};
  const run = (command, args) => {
    calls.push({ command, args });
    if (command === 'sh' || command === 'docker') throw new Error('Release preparation must leave validation/builds to CI.');
    if (command === 'git' && args[0] === 'commit') controls.commit?.();
    if (command === 'git' && args[0] === 'tag' && args.includes('-a')) controls.tag?.();
    return realRun(command, args);
  };
  const options = { cwd: root, root, run, log: (message) => messages.push(message) };
  return {
    root, remote, directory, git, calls, controls, messages, options, originalHead,
    release: (version = '1.0.0') => release([version], options),
    remoteGit: (...args) => git('--git-dir', remote, ...args),
    version: () => JSON.parse(readFileSync(join(root, 'package.json'), 'utf8')).version,
    packageContents: () => readFileSync(join(root, 'package.json')),
    assertUnreleased() {
      assert.equal(git('rev-parse', 'HEAD'), originalHead);
      assert.equal(git('tag', '--list'), '');
      assert.equal(git('diff', '--cached', '--name-only'), '');
      assert.equal(git('--git-dir', remote, 'rev-parse', 'refs/heads/delivery/topic'), originalHead);
      assert.equal(git('--git-dir', remote, 'tag', '--list'), '');
    },
  };
}

for (const argv of [[], ['1.0.0', '2.0.0']]) {
  test(`rejects ${argv.length} version arguments`, () => {
    assert.throws(() => release(argv), /exactly one argument/);
  });
}

for (const version of ['1', '1.0', 'vfoo', '01.0.0', '1.02.0', '1.0.03', '1.0.0-01', '1.0.0-rc..1', '1.0.0-', '1.0.0\n', ' 1.0.0']) {
  test(`rejects invalid SemVer ${JSON.stringify(version)}`, () => {
    assert.throws(() => release([version]), /Invalid SemVer/);
    assert.throws(() => checkReleaseVersion(`v${version}`, JSON.stringify({ version: '1.0.0' })), /Invalid SemVer/);
  });
}

test('rejects build metadata explicitly', () => {
  for (const version of ['1.0.0+build.1', 'v1.0.0+build.1']) {
    assert.throws(() => release([version]), /Build metadata/);
  }
});

for (const [kind, prepare] of [
  ['tracked', (f) => writeFileSync(join(f.root, 'other.txt'), 'dirty\n')],
  ['staged', (f) => { writeFileSync(join(f.root, 'other.txt'), 'dirty\n'); f.git('add', 'other.txt'); }],
  ['untracked', (f) => writeFileSync(join(f.root, 'untracked.txt'), 'dirty\n')],
]) {
  test(`rejects a dirty ${kind} file before changing package.json`, (t) => {
    const f = fixture(t);
    prepare(f);
    assert.throws(() => f.release(), /Working tree must be completely clean/);
    assert.equal(f.version(), '0.0.0');
    assert.equal(f.git('rev-parse', 'HEAD'), f.originalHead);
    assert.equal(f.calls.some(({ command }) => command === 'sh' || command === 'docker'), false);
  });
}

test('rejects the wrong repository root', (t) => {
  const f = fixture(t);
  assert.throws(() => release(['1.0.0'], { ...f.options, cwd: join(f.root, 'scripts') }), /repository root/);
  assert.equal(f.version(), '0.0.0');
});

test('rejects a repository with a different module', (t) => {
  const f = fixture(t);
  writeFileSync(join(f.root, 'go.mod'), 'module example.test/other\n');
  f.git('add', 'go.mod'); f.git('commit', '-m', 'Other module');
  assert.throws(() => f.release(), /not the nox-yard repository/);
});

test('rejects detached HEAD', (t) => {
  const f = fixture(t);
  f.git('checkout', '--detach');
  assert.throws(() => f.release(), /detached HEAD/);
  f.assertUnreleased();
});

test('requires usable Git identity', (t) => {
  const f = fixture(t);
  const run = commandRunner(f.root);
  assert.throws(() => release(['1.0.0'], {
    ...f.options, run: (command, args) => run(command, ['-c', 'user.name=', '-c', 'user.email=', ...args]),
  }), /GIT_AUTHOR_IDENT failed/);
  f.assertUnreleased();
});

test('inaccessible origin aborts instead of treating the tag as absent', (t) => {
  const f = fixture(t);
  f.git('remote', 'set-url', 'origin', join(f.directory, 'missing.git'));
  assert.throws(() => f.release(), /ls-remote.*failed/);
  assert.equal(f.version(), '0.0.0');
  f.assertUnreleased();
});

test('rejects package.json equal to the requested version', (t) => {
  const f = fixture(t, '1.0.0');
  assert.throws(() => f.release('v1.0.0'), /package.json version is already 1.0.0/);
  f.assertUnreleased();
});

test('rejects an existing local tag', (t) => {
  const f = fixture(t);
  f.git('tag', 'v1.0.0');
  assert.throws(() => f.release(), /already exists locally/);
  assert.equal(f.version(), '0.0.0');
  assert.equal(f.git('rev-parse', 'HEAD'), f.originalHead);
});

test('rejects a tag that exists only on origin', (t) => {
  const f = fixture(t);
  f.remoteGit('update-ref', 'refs/tags/v1.0.0', f.originalHead);
  assert.equal(f.git('tag', '--list'), '');
  assert.throws(() => f.release(), /already exists on origin/);
  assert.equal(f.version(), '0.0.0');
  assert.equal(f.git('rev-parse', 'HEAD'), f.originalHead);
});

test('commit failure restores package.json byte-for-byte before any release', (t) => {
  const f = fixture(t, '0.0.0\r\n');
  const original = f.packageContents();
  f.controls.commit = () => { throw new Error('Simulated commit failure'); };
  assert.throws(() => f.release(), /Simulated commit failure.*\npackage.json restored/);
  assert.deepEqual(f.packageContents(), original);
  f.assertUnreleased();
  assert.equal(f.git('status', '--porcelain'), '');
});

for (const version of ['v1.0.0', '2.0.0-rc.1', '2.0.0-beta.2']) {
  test(`success for ${version}: only package.json, annotated tag and atomic branch/tag push`, (t) => {
    const f = fixture(t, '0.0.0\r\n');
    const result = f.release(version);
    const normalized = version.replace(/^v/, '');
    const tag = `v${normalized}`;
    assert.equal(result.version, normalized);
    assert.equal(result.branch, 'delivery/topic');
    assert.equal(f.version(), normalized);
    assert(f.packageContents().toString().endsWith('\r\n'));
    assert.equal(f.packageContents().toString().replaceAll('\r\n', '').includes('\n'), false);
    const metadata = JSON.parse(f.packageContents());
    assert.equal(metadata.name, 'nox-yard');
    assert.equal(metadata.private, true);
    assert.deepEqual(metadata.scripts, { release: 'node scripts/release.mjs' });
    assert.equal(f.git('show', '-s', '--format=%s', result.commit), `chore: release ${tag}`);
    assert.equal(f.git('show', '-s', '--format=%B', result.commit), `chore: release ${tag}\n\nNoX-Yard-Release: ${tag}`);
    assert.equal(f.git('diff-tree', '--no-commit-id', '--name-only', '-r', result.commit), 'package.json');
    assert.equal(f.git('rev-parse', `${result.commit}^`), f.originalHead);
    assert.equal(f.git('cat-file', '-t', `refs/tags/${tag}`), 'tag');
    assert.equal(f.git('for-each-ref', '--format=%(contents)', `refs/tags/${tag}`), `Release ${tag}`);
    assert.equal(f.git('rev-parse', `${tag}^{commit}`), result.commit);
    assert.equal(f.remoteGit('rev-parse', `refs/tags/${tag}^{commit}`), result.commit);
    assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), result.commit);
    assert.equal(f.git('status', '--porcelain'), '');
    assert.equal(f.calls.some(({ command }) => command === 'sh' || command === 'docker'), false);
    assert.equal(requestedRelease({ cwd: f.root, revision: result.commit, ref: 'refs/heads/delivery/topic' }), tag);
    assert.deepEqual(f.calls.find(({ command, args }) => command === 'git' && args[0] === 'push').args, [
      'push', '--atomic', 'origin', 'refs/heads/delivery/topic:refs/heads/delivery/topic', `refs/tags/${tag}:refs/tags/${tag}`,
    ]);
    assert(f.messages.at(-1).includes('GitHub Actions'));
  });
}

test('tag creation failure preserves the release commit and prints recovery', (t) => {
  const f = fixture(t);
  f.controls.tag = () => { throw new Error('Tag creation failed'); };
  assert.throws(() => f.release(), (error) => {
    assert.match(error.message, /without a release tag/);
    assert.match(error.message, /git tag -a 'v1.0.0' -m 'Release v1.0.0'/);
    assert.match(error.message, /git 'push' '--atomic' 'origin'/);
    return true;
  });
  assert.notEqual(f.git('rev-parse', 'HEAD'), f.originalHead);
  assert.equal(f.git('tag', '--list'), '');
  assert.equal(f.version(), '1.0.0');
  assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), f.originalHead);
});

test('release includes clean local commits that have not been pushed yet', (t) => {
  const f = fixture(t);
  writeFileSync(join(f.root, 'other.txt'), 'first pending change\n');
  f.git('add', 'other.txt'); f.git('commit', '-m', 'First pending change');
  writeFileSync(join(f.root, 'pending.txt'), 'second pending change\n');
  f.git('add', 'pending.txt'); f.git('commit', '-m', 'Second pending change');
  const source = f.git('rev-parse', 'HEAD');
  assert.equal(f.git('status', '--porcelain'), '');
  assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), f.originalHead);
  const result = f.release();
  assert.equal(f.git('rev-parse', `${result.commit}^`), source);
  assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), result.commit);
  assert.equal(f.remoteGit('show', `${result.commit}:pending.txt`), 'second pending change');
  assert.equal(requestedRelease({cwd:f.root,revision:result.commit,ref:'refs/heads/delivery/topic'}), 'v1.0.0');
});

test('an unpublished failed request can be recreated for the same version after a source fix', (t) => {
  const f = fixture(t);
  const first = f.release('1.0.0');
  const originalTag = f.git('rev-parse', 'refs/tags/v1.0.0');
  // The documented recovery first confirms no GitHub/registry publication.
  // Restore only version metadata alongside the source fix; preserve history.
  const metadata = JSON.parse(f.packageContents());
  metadata.version = '0.0.0';
  writeFileSync(join(f.root, 'package.json'), JSON.stringify(metadata, null, 2) + '\n');
  writeFileSync(join(f.root, 'other.txt'), 'fixed source\n');
  f.git('add', 'package.json', 'other.txt');
  f.git('commit', '-m', 'Fix source before retrying unpublished release');
  const fixed = f.git('rev-parse', 'HEAD');
  f.git('push', `--force-with-lease=refs/tags/v1.0.0:${originalTag}`, 'origin', ':refs/tags/v1.0.0');
  f.git('tag', '-d', 'v1.0.0');
  const second = f.release('1.0.0');
  assert.notEqual(second.commit, first.commit);
  assert.equal(f.git('rev-parse', `${second.commit}^`), fixed);
  assert.equal(f.git('merge-base', first.commit, second.commit), first.commit);
  assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), second.commit);
  assert.equal(f.remoteGit('rev-parse', 'refs/tags/v1.0.0^{commit}'), second.commit);
  assert.equal(f.remoteGit('show', 'v1.0.0:other.txt'), 'fixed source');
  assert.equal(requestedRelease({cwd:f.root,revision:second.commit,ref:'refs/heads/delivery/topic'}), 'v1.0.0');
  assert.throws(() => requestedRelease({cwd:f.root,revision:first.commit,ref:'refs/heads/delivery/topic'}), /does not match its annotated tag/);
});

function hookFixture(t) {
  const f = fixture(t);
  copyFileSync(join(scripts, 'install-hooks.sh'), join(f.root, 'scripts', 'install-hooks.sh'));
  f.git('config', '--unset', 'core.hooksPath');
  const env = {...process.env, GIT_CONFIG_NOSYSTEM:'1', GIT_CONFIG_GLOBAL:join(f.directory, 'empty.gitconfig')};
  const shell = process.platform === 'win32' ? 'C:/Program Files/Git/bin/sh.exe' : 'sh';
  const install = () => {
    const result = spawnSync(shell, [join(f.root, 'scripts', 'install-hooks.sh').replaceAll('\\', '/')], {cwd:f.root,env,encoding:'utf8'});
    assert.ifError(result.error);
    return result;
  };
  const push = () => spawnSync('git', ['push', 'origin', 'HEAD:refs/heads/delivery/topic'], {cwd:f.root,env,encoding:'utf8'});
  const hook = join(f.root, '.git', 'hooks', 'pre-push');
  mkdirSync(dirname(hook), {recursive:true});
  return {...f, install, push, hook};
}

test('hook migration leaves a new clone without a pre-push hook', (t) => {
  const f = hookFixture(t);
  assert.equal(f.install().status, 0);
  assert.throws(() => readFileSync(f.hook), {code:'ENOENT'});
});

for (const newline of ['\n', '\r\n']) {
  test(`hook migration removes only the legacy Yard hook (${JSON.stringify(newline)}) and is idempotent`, (t) => {
    const f = hookFixture(t);
    writeFileSync(f.hook, ['#!/bin/sh', 'set -eu', 'repo_root=$(git rev-parse --show-toplevel)', 'exec sh "$repo_root/scripts/ci-local.sh"', ''].join(newline));
    chmodSync(f.hook, 0o755);
    // A remaining validation call would fail the real disposable push.
    writeFileSync(join(f.root, 'scripts', 'ci-local.sh'), '#!/bin/sh\nexit 97\n');
    assert.equal(f.install().status, 0);
    assert.throws(() => readFileSync(f.hook), {code:'ENOENT'});
    assert.equal(f.install().status, 0);
    f.git('add', '.'); f.git('commit', '-m', 'CI-only validation');
    const pushed = f.push();
    assert.equal(pushed.status, 0, pushed.stderr);
    assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), f.git('rev-parse', 'HEAD'));
  });
}

test('hook migration preserves an unrecognized custom hook', (t) => {
  const f = hookFixture(t);
  const custom = '#!/bin/sh\necho user hook\n';
  writeFileSync(f.hook, custom);
  const result = f.install();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /unrecognized.*not modified/);
  assert.equal(readFileSync(f.hook, 'utf8'), custom);
});

test('hook migration preserves custom core.hooksPath', (t) => {
  const f = hookFixture(t);
  const custom = join(f.directory, 'custom-hooks');
  f.git('config', 'core.hooksPath', custom);
  const result = f.install();
  assert.equal(result.status, 1);
  assert.match(result.stderr, /custom core.hooksPath.*not modified/);
  assert.equal(f.git('config', '--get', 'core.hooksPath'), custom);
});

test('an atomic push rejected for the tag preserves local commit/tag and updates neither remote ref', (t) => {
  const f = fixture(t);
  const hook = join(f.remote, 'hooks', 'pre-receive');
  mkdirSync(dirname(hook), { recursive: true });
  writeFileSync(hook, '#!/bin/sh\nwhile read old new ref; do\n  case "$ref" in refs/tags/*) exit 1 ;; esac\ndone\n');
  chmodSync(hook, 0o755);
  // The bare remote must use its fixture hook, regardless of the host's hooksPath.
  f.remoteGit('config', 'core.hooksPath', join(f.remote, 'hooks'));
  assert.throws(() => f.release(), (error) => {
    assert.match(error.message, /commit .* and annotated tag v1.0.0 remain local/);
    assert.match(error.message, /git 'push' '--atomic' 'origin' 'refs\/heads\/delivery\/topic:refs\/heads\/delivery\/topic' 'refs\/tags\/v1.0.0:refs\/tags\/v1.0.0'/);
    return true;
  });
  const commit = f.git('rev-parse', 'HEAD');
  assert.notEqual(commit, f.originalHead);
  assert.equal(f.git('rev-parse', 'v1.0.0^{commit}'), commit);
  assert.equal(f.git('cat-file', '-t', 'v1.0.0'), 'tag');
  assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), f.originalHead);
  assert.equal(f.remoteGit('tag', '--list'), '');
  assert.equal(f.git('status', '--porcelain'), '');
});

test('stable metadata includes the version and latest, with all OCI labels', () => {
  const metadata = releaseMetadata('v1.0.0', sha, created);
  assert.equal(metadata.prerelease, false);
  assert.deepEqual(metadata.tags, [`${image}:v1.0.0`, `${image}:latest`]);
  assert.deepEqual(metadata.labels, {
    'org.opencontainers.image.version': 'v1.0.0',
    'org.opencontainers.image.revision': sha,
    'org.opencontainers.image.source': source,
    'org.opencontainers.image.created': created,
  });
});

test('prerelease metadata never includes latest and classifies the GitHub prerelease', () => {
  const metadata = releaseMetadata('v1.1.0-rc.1', sha, created);
  assert.equal(metadata.prerelease, true);
  assert.deepEqual(metadata.tags, [`${image}:v1.1.0-rc.1`]);
  assert.equal(metadata.labels['org.opencontainers.image.version'], 'v1.1.0-rc.1');
});

test('metadata validates full SHA, Docker tag length and UTC timestamp', () => {
  assert.throws(() => releaseMetadata('v1.0.0', 'abc', created), /full commit SHA/);
  assert.throws(() => releaseMetadata('v1.0.0', sha, '2026-10-06'), /UTC ISO timestamp/);
  assert.throws(() => parseVersion(`1.0.0-${'a'.repeat(128)}`), /Docker tag limit/);
});

test('GitHub output serialization preserves tags, labels and prerelease classification', (t) => {
  const directory = mkdtempSync(join(tmpdir(), 'nox-yard-metadata-'));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const output = join(directory, 'output');
  writeMetadataOutputs(releaseMetadata('v1.0.0', sha, created), output);
  const text = readFileSync(output, 'utf8');
  const delimiter = text.match(/tags<<([^\n]+)/)[1];
  assert(text.includes(`prerelease=false\ntags<<${delimiter}\n${image}:v1.0.0\n${image}:latest\n${delimiter}\n`));
  assert(text.includes(`labels<<${delimiter}\norg.opencontainers.image.version=v1.0.0\n`));
  assert(text.includes(`org.opencontainers.image.revision=${sha}\n`));
  assert(text.includes(`org.opencontainers.image.source=${source}\n`));
  assert(text.endsWith(`org.opencontainers.image.created=${created}\n${delimiter}\n`));
});

test('workflow validator rejects invalid tags and package.json mismatch using the same parser', () => {
  assert.equal(checkReleaseVersion('v2.0.0-rc.1', JSON.stringify({ version: '2.0.0-rc.1' }) + '\r\n').prerelease, true);
  assert.throws(() => checkReleaseVersion('v1.0.0', JSON.stringify({ version: '1.0.1' })), /does not match package.json/);
  for (const tag of ['1.0.0', 'vfoo', 'v1.0.0+build.1']) {
    assert.throws(() => checkReleaseVersion(tag, JSON.stringify({ version: '1.0.0' })));
  }
  for (const contents of ['invalid JSON', '{}', '{"version":1}', '{"version":"v1.0.0"}', '{"version":"1.0.0\\n"}', '{"version":"1.0.0 "}']) {
    assert.throws(() => checkReleaseVersion('v1.0.0', contents));
  }
});

test('stable publication cannot move latest backwards; prereleases do not affect it', () => {
  assertLatestStable('v1.10.0', ['v1.9.0', 'v2.0.0-rc.1', 'legacy']);
  assertLatestStable('v1.0.0-rc.1', ['v2.0.0']);
  for (const newer of ['v2.0.0', 'v1.11.0', 'v1.10.1']) {
    assert.throws(() => assertLatestStable('v1.10.0', [newer]), /newer stable release/);
  }
});

test('normal pushes, version bumps and manual tags do not request publication', (t) => {
  const f = fixture(t, '1.0.0');
  f.git('tag', '-a', 'v1.0.0', '-m', 'Release v1.0.0');
  for (const [event, ref] of [['push', 'refs/heads/delivery/topic'], ['push', 'refs/tags/v1.0.0'], ['pull_request', 'refs/pull/1/merge']]) {
    assert.equal(requestedRelease({ cwd: f.root, revision: f.originalHead, ref, event }), '');
  }
});

for (const invalid of ['lightweight', 'other commit', 'annotation', 'missing tag', 'version mismatch', 'extra files', 'duplicate marker']) {
  test(`release request refuses ${invalid}`, (t) => {
    const f = fixture(t);
    let { commit } = f.release();
    if (invalid === 'lightweight' || invalid === 'missing tag') {
      f.git('tag', '-d', 'v1.0.0');
      if (invalid === 'lightweight') f.git('tag', 'v1.0.0');
    }
    if (invalid === 'other commit') f.git('tag', '-f', '-a', 'v1.0.0', '-m', 'Release v1.0.0', f.originalHead);
    if (invalid === 'annotation') f.git('tag', '-f', '-a', 'v1.0.0', '-m', 'Manual tag');
    if (invalid === 'version mismatch') {
      writeFileSync(join(f.root, 'package.json'), JSON.stringify({ name: 'nox-yard', version: '1.0.1' }));
      f.git('add', 'package.json'); f.git('commit', '--amend', '--no-edit');
      commit = f.git('rev-parse', 'HEAD');
    }
    if (invalid === 'extra files') {
      writeFileSync(join(f.root, 'other.txt'), 'also changed');
      f.git('add', 'other.txt'); f.git('commit', '--amend', '--no-edit');
      commit = f.git('rev-parse', 'HEAD');
    }
    if (invalid === 'duplicate marker') {
      f.git('commit', '--amend', '-m', 'chore: release v1.0.0', '-m', 'NoX-Yard-Release: v1.0.0\nNoX-Yard-Release: v1.0.0');
      commit = f.git('rev-parse', 'HEAD');
    }
    assert.throws(() => requestedRelease({ cwd: f.root, revision: commit, ref: 'refs/heads/delivery/topic' }));
  });
}

test('publication retries skip only an identical completed release and never overwrite conflicts', () => {
  const published = { tag_name: 'v1.0.0', target_commitish: sha, body: publicationMarker(sha), draft: false, prerelease: false };
  assert.equal(shouldPublish('v1.0.0', sha, []), true);
  assert.equal(shouldPublish('v1.0.0', sha, [published]), false);
  // GitHub may retain a branch name as target_commitish for an existing tag.
  assert.equal(shouldPublish('v1.0.0', sha, [{ ...published, target_commitish: 'master' }]), false);
  // A later release must not turn a retry into a downgrade of latest.
  assert.equal(shouldPublish('v1.0.0', sha, [published, { tag_name: 'v2.0.0' }]), false);
  for (const change of [{ draft: true }, { prerelease: true }, { body: '' }, { body: publicationMarker('b'.repeat(40)) }]) {
    assert.throws(() => shouldPublish('v1.0.0', sha, [{ ...published, ...change }]), /different or unverified/);
  }
  assert.throws(() => shouldPublish('v1.0.0', sha, [{ tag_name: 'v2.0.0' }]), /newer stable release/);
});

test('request command writes only the validated tag to GitHub outputs', (t) => {
  const f = fixture(t);
  const output = join(f.directory, 'github-output');
  const invoke = (revision) => {
    const result = spawnSync(process.execPath, [join(f.root, 'scripts', 'release-request.mjs')], {
      cwd: f.root, encoding: 'utf8',
      env: { ...process.env, GITHUB_SHA: revision, GITHUB_REF: 'refs/heads/delivery/topic', GITHUB_EVENT_NAME: 'push', GITHUB_OUTPUT: output },
    });
    assert.equal(result.status, 0, result.stderr);
  };
  invoke(f.originalHead);
  assert.equal(readFileSync(output, 'utf8'), 'tag=\n');
  const { commit } = f.release();
  invoke(commit);
  assert.equal(readFileSync(output, 'utf8'), 'tag=\ntag=v1.0.0\n');
});

test('real command entrypoints cancel an unanswered prompt and reject mismatched tags without mutations', (t) => {
  const f = fixture(t);
  for (const [script, args, message] of [
    ['release.mjs', [], /Release cancelled/],
    ['check-release-version.mjs', ['v1.0.0'], /does not match package.json/],
    ['release-metadata.mjs', ['vfoo', sha], /Invalid SemVer/],
  ]) {
    const result = spawnSync(process.execPath, [join(f.root, 'scripts', script), ...args], { cwd: f.root, encoding: 'utf8' });
    assert.equal(result.status, 1);
    assert.match(result.stderr, message);
  }
  f.assertUnreleased();
});

test('interactive release shows the package version and pushes the entered version to a local fixture remote', (t) => {
  const f = fixture(t, '1.2.0');
  const result = spawnSync(process.execPath, [join(f.root, 'scripts', 'release.mjs')], {
    cwd: f.root, encoding: 'utf8', input: '  v1.3.0-rc.1  \n', timeout: 15_000,
  });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /Current version: 1\.2\.0/);
  assert.match(result.stdout, /New version:/);
  assert.equal(f.version(), '1.3.0-rc.1');
  assert.equal(f.remoteGit('tag', '--list'), 'v1.3.0-rc.1');
  assert.match(f.git('log', '-1', '--format=%B'), /NoX-Yard-Release: v1\.3\.0-rc\.1/);
  assert.equal(f.git('status', '--porcelain'), '');
});

for (const [input, message] of [['\n', /Release cancelled/], ['', /Release cancelled/], ['invalid\n', /Invalid SemVer/], ['1.2.0\n', /already 1\.2\.0/]]) {
  test(`interactive release rejects ${JSON.stringify(input)} without mutations`, (t) => {
    const f = fixture(t, '1.2.0');
    const result = spawnSync(process.execPath, [join(f.root, 'scripts', 'release.mjs')], {
      cwd: f.root, encoding: 'utf8', input, timeout: 15_000,
    });
    assert.equal(result.status, 1, result.stderr);
    assert.match(result.stdout, /Current version: 1\.2\.0/);
    assert.match(result.stderr, message);
    assert.equal(f.version(), '1.2.0');
    f.assertUnreleased();
  });
}
