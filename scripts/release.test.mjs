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
  writeFileSync(join(root, 'VERSION'), original);
  writeFileSync(join(root, 'package.json'), JSON.stringify({ name: 'nox-yard', private: true, scripts: { release: 'node scripts/release.mjs' } }));
  writeFileSync(join(root, 'go.mod'), 'module github.com/mapherez/nox-yard\n');
  writeFileSync(join(root, 'Dockerfile'), 'FROM scratch\n');
  writeFileSync(join(root, 'other.txt'), 'unchanged\n');
  writeFileSync(join(root, 'scripts', 'ci-local.sh'), '#!/bin/sh\nexit 0\n');
  for (const name of ['release.mjs', 'release-version.mjs', 'release-metadata.mjs', 'check-release-version.mjs']) {
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
    if (command === 'sh') { controls.check?.(); return ''; }
    if (command === 'docker') { controls.build?.(); return ''; }
    if (command === 'git' && args[0] === 'commit') controls.commit?.();
    if (command === 'git' && args[0] === 'tag' && args.includes('-a')) controls.tag?.();
    return realRun(command, args);
  };
  const options = { cwd: root, root, run, log: (message) => messages.push(message) };
  return {
    root, remote, directory, git, calls, controls, messages, options, originalHead,
    release: (version = '1.0.0') => release([version], options),
    remoteGit: (...args) => git('--git-dir', remote, ...args),
    version: () => readFileSync(join(root, 'VERSION')),
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
    assert.throws(() => checkReleaseVersion(`v${version}`, '1.0.0\n'), /Invalid SemVer/);
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
  test(`rejects a dirty ${kind} file before changing VERSION`, (t) => {
    const f = fixture(t);
    prepare(f);
    assert.throws(() => f.release(), /Working tree must be completely clean/);
    assert.equal(f.version().toString(), '0.0.0\n');
    assert.equal(f.git('rev-parse', 'HEAD'), f.originalHead);
    assert.equal(f.calls.some(({ command }) => command === 'sh' || command === 'docker'), false);
  });
}

test('rejects the wrong repository root', (t) => {
  const f = fixture(t);
  assert.throws(() => release(['1.0.0'], { ...f.options, cwd: join(f.root, 'scripts') }), /repository root/);
  assert.equal(f.version().toString(), '0.0.0\n');
});

test('rejects a repository with a different module', (t) => {
  const f = fixture(t);
  writeFileSync(join(f.root, 'go.mod'), 'module example.test/other\n');
  f.git('add', 'go.mod'); f.git('commit', '-m', 'Other module');
  assert.throws(() => f.release(), /not the NoX Yard repository/);
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
  assert.equal(f.version().toString(), '0.0.0\n');
  f.assertUnreleased();
});

test('rejects VERSION equal to the requested version', (t) => {
  const f = fixture(t, '1.0.0\n');
  assert.throws(() => f.release('v1.0.0'), /VERSION is already 1.0.0/);
  f.assertUnreleased();
});

test('rejects an existing local tag', (t) => {
  const f = fixture(t);
  f.git('tag', 'v1.0.0');
  assert.throws(() => f.release(), /already exists locally/);
  assert.equal(f.version().toString(), '0.0.0\n');
  assert.equal(f.git('rev-parse', 'HEAD'), f.originalHead);
});

test('rejects a tag that exists only on origin', (t) => {
  const f = fixture(t);
  f.remoteGit('update-ref', 'refs/tags/v1.0.0', f.originalHead);
  assert.equal(f.git('tag', '--list'), '');
  assert.throws(() => f.release(), /already exists on origin/);
  assert.equal(f.version().toString(), '0.0.0\n');
  assert.equal(f.git('rev-parse', 'HEAD'), f.originalHead);
});

for (const failure of ['check', 'build', 'commit']) {
  test(`${failure} failure restores VERSION byte-for-byte before any release`, (t) => {
    const f = fixture(t, '0.0.0\r\n');
    const original = f.version();
    f.controls[failure] = () => { throw new Error(`Simulated ${failure} failure`); };
    assert.throws(() => f.release(), new RegExp(`Simulated ${failure} failure.*\\nVERSION restored`));
    assert.deepEqual(f.version(), original);
    f.assertUnreleased();
    assert.equal(f.git('status', '--porcelain'), '');
  });
}

for (const staged of [false, true]) {
  test(`unexpected ${staged ? 'staged' : 'working tree'} check changes abort and remain available for inspection`, (t) => {
    const f = fixture(t);
    f.controls.check = () => {
      writeFileSync(join(f.root, 'other.txt'), 'check changed this\n');
      if (staged) f.git('add', 'other.txt');
    };
    assert.throws(() => f.release(), /Checks changed unexpected files: .*other.txt/);
    assert.equal(f.version().toString(), '0.0.0\n');
    assert.equal(readFileSync(join(f.root, 'other.txt'), 'utf8'), 'check changed this\n');
    assert.equal(f.git('rev-parse', 'HEAD'), f.originalHead);
    assert.equal(f.git('tag', '--list'), '');
    assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), f.originalHead);
    assert.equal(f.git('diff', '--cached', '--name-only'), staged ? 'other.txt' : '');
  });
}

test('unexpected changes to VERSION during checks also abort', (t) => {
  const f = fixture(t);
  f.controls.build = () => writeFileSync(join(f.root, 'VERSION'), '7.0.0\n');
  assert.throws(() => f.release(), /Checks changed VERSION unexpectedly/);
  assert.equal(f.version().toString(), '0.0.0\n');
  f.assertUnreleased();
});

for (const version of ['v1.0.0', '2.0.0-rc.1', '2.0.0-beta.2']) {
  test(`success for ${version}: only VERSION, annotated tag and atomic branch/tag push`, (t) => {
    const f = fixture(t, '0.0.0\r\n');
    const result = f.release(version);
    const normalized = version.replace(/^v/, '');
    const tag = `v${normalized}`;
    assert.equal(result.version, normalized);
    assert.equal(result.branch, 'delivery/topic');
    assert.equal(f.version().toString(), `${normalized}\r\n`);
    assert.equal(f.git('show', '-s', '--format=%s', result.commit), `chore: release ${tag}`);
    assert.equal(f.git('diff-tree', '--no-commit-id', '--name-only', '-r', result.commit), 'VERSION');
    assert.equal(f.git('rev-parse', `${result.commit}^`), f.originalHead);
    assert.equal(f.git('cat-file', '-t', `refs/tags/${tag}`), 'tag');
    assert.equal(f.git('for-each-ref', '--format=%(contents)', `refs/tags/${tag}`), `Release ${tag}`);
    assert.equal(f.git('rev-parse', `${tag}^{commit}`), result.commit);
    assert.equal(f.remoteGit('rev-parse', `refs/tags/${tag}^{commit}`), result.commit);
    assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), result.commit);
    assert.equal(f.git('status', '--porcelain'), '');
    assert.deepEqual(f.calls.find(({ command }) => command === 'sh').args, ['scripts/ci-local.sh']);
    assert.deepEqual(f.calls.find(({ command }) => command === 'docker').args, [
      'buildx', 'build', '--platform', 'linux/amd64', '--load',
      '--build-arg', `BUILD_SHA=${f.originalHead}`, '--build-arg', `BUILD_VERSION=${tag}`,
      '-t', 'nox-yard:release-check', '.',
    ]);
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
  assert.equal(f.version().toString(), '1.0.0\n');
  assert.equal(f.remoteGit('rev-parse', 'refs/heads/delivery/topic'), f.originalHead);
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

test('workflow validator rejects invalid tags and VERSION mismatch using the same parser', () => {
  assert.equal(checkReleaseVersion('v2.0.0-rc.1', '2.0.0-rc.1\r\n').prerelease, true);
  assert.throws(() => checkReleaseVersion('v1.0.0', '1.0.1\n'), /does not match VERSION/);
  for (const tag of ['1.0.0', 'vfoo', 'v1.0.0+build.1']) {
    assert.throws(() => checkReleaseVersion(tag, '1.0.0\n'));
  }
  for (const contents of ['v1.0.0\n', '1.0.0\n\n', '1.0.0 \n']) {
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

test('real command entrypoints reject missing arguments and a mismatched tag without mutations', (t) => {
  const f = fixture(t);
  for (const [script, args, message] of [
    ['release.mjs', [], /exactly one argument/],
    ['check-release-version.mjs', ['v1.0.0'], /does not match VERSION/],
    ['release-metadata.mjs', ['vfoo', sha], /Invalid SemVer/],
  ]) {
    const result = spawnSync(process.execPath, [join(f.root, 'scripts', script), ...args], { cwd: f.root, encoding: 'utf8' });
    assert.equal(result.status, 1);
    assert.match(result.stderr, message);
  }
  f.assertUnreleased();
});
