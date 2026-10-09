import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkRange, checkPush, run } from './push-check.mjs';
import { installHooks, legacyHook, pushHook } from './install-hooks.mjs';
import { classify } from './delivery.mjs';

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'nox-push-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const calls = [];
  const execute = (command, args, input) => {
    calls.push({ command, args });
    assert([process.execPath, 'git', 'gofmt'].includes(command), `Unexpected local dependency: ${command}`);
    return run(command, args, input, root);
  };
  const git = (...args) => execute('git', args).trim();
  git('-c', 'init.templateDir=', 'init', '--initial-branch=master');
  for (const [key, value] of Object.entries({ 'user.name': 'Push tests', 'user.email': 'push@example.test',
    'commit.gpgSign': 'false', 'core.autocrlf': 'false', 'core.hooksPath': join(root, 'no-hooks') })) git('config', key, value);
  writeFileSync(join(root, 'package.json'), '{"name":"fixture","version":"1.0.0"}\n');
  git('add', '.'); git('commit', '-m', 'baseline');
  const base = git('rev-parse', 'HEAD');
  const commit = (files) => {
    for (const [path, contents] of Object.entries(files)) {
      mkdirSync(join(root, path, '..'), { recursive: true });
      writeFileSync(join(root, path), contents);
    }
    git('add', '.'); git('commit', '-m', 'change'); return git('rev-parse', 'HEAD');
  };
  return { root, execute, calls, git, base, commit, options: { execute, log() {} } };
}

test('checks pushed snapshots, independent of dirty checkout; only Git/Node/gofmt', t => {
  const f = fixture(t);
  const head = f.commit({ 'a.go': 'package fixture\n\nvar A = 1\n', 'check.mjs': 'export const valid = true;\n' });
  writeFileSync(join(f.root, 'a.go'), 'broken local edit\n');
  checkRange(f.base, head, f.options);
  assert(f.calls.some(({ command }) => command === 'gofmt'));
  assert(f.calls.some(({ command }) => command === process.execPath));
});
test('rejects malformed pushed Go, JavaScript, manifest and whitespace', t => {
  for (const [path, contents, message] of [
    ['bad.go', 'package fixture\nvar B=1\n', /gofmt/],
    ['bad.mjs', 'const = ;\n', /SyntaxError/],
    ['package.json', '{broken}\n', /JSON/],
    ['bad.txt', 'trailing whitespace \n', /trailing whitespace/],
  ]) {
    const f = fixture(t);
    const head = f.commit({ [path]: contents });
    assert.throws(() => checkRange(f.base, head, f.options), message);
  }
});
test('checks frontend package/lock agreement without npm or dependencies', t => {
  const f = fixture(t);
  const head = f.commit({ 'web/package.json': '{"dependencies":{"x":"1.0.0"}}\n',
    'web/package-lock.json': '{"packages":{"":{"dependencies":{"x":"2.0.0"}}}}\n' });
  assert.throws(() => checkRange(f.base, head, f.options), /out of sync/);
});
test('branch plus annotated tag checks the commit only once; ref deletions are ignored', t => {
  const f = fixture(t), messages = [];
  const head = f.commit({ 'check.mjs': 'export const value = 1;\n' });
  f.git('-c', 'tag.gpgSign=false', 'tag', '-a', 'v1.0.1', '-m', 'Release');
  const tag = f.git('rev-parse', 'v1.0.1'), zero = '0'.repeat(40);
  checkPush(`refs/tags/v1.0.1 ${tag} refs/tags/v1.0.1 ${zero}\nrefs/heads/master ${head} refs/heads/master ${f.base}\nrefs/heads/deleted ${zero} refs/heads/deleted ${f.base}\n`,
    { ...f.options, env: {}, log: message => messages.push(message) });
  assert.equal(messages.length, 1);
  assert.equal(f.calls.filter(({ command }) => command === process.execPath).length, 1);
});
test('new refs with no remote baseline validate the full committed tree', t => {
  const f = fixture(t);
  const head = f.commit({ 'bad.go': 'package fixture\nvar B=1\n' });
  assert.throws(() => checkRange('0'.repeat(40), head, f.options), /gofmt/);
});
test('release hook deduplication requires exact tree and remote base', t => {
  const f = fixture(t);
  const head = f.commit({ 'bad.go': 'package fixture\nvar B=1\n' });
  const input = `refs/heads/master ${head} refs/heads/master ${f.base}\n`;
  const env = { NOX_PUSH_CHECKED_TREE: f.git('rev-parse', `${head}^{tree}`), NOX_PUSH_CHECKED_BASE: f.base };
  checkPush(input, { ...f.options, env });
  assert.throws(() => checkPush(input, { ...f.options, env: { ...env, NOX_PUSH_CHECKED_BASE: '0'.repeat(40) } }), /gofmt/);
});
test('installer migrates only the owned hook and preserves custom hooks/config', t => {
  const f = fixture(t);
  f.git('config', '--unset', 'core.hooksPath');
  const hook = join(f.root, '.git', 'hooks', 'pre-push');
  mkdirSync(join(f.root, '.git', 'hooks'), { recursive: true });
  writeFileSync(hook, legacyHook);
  installHooks({ cwd: f.root, log() {} });
  assert.equal(readFileSync(hook, 'utf8'), pushHook);
  installHooks({ cwd: f.root, log() {} });
  writeFileSync(hook, '#!/bin/sh\ncustom-hook\n');
  assert.throws(() => installHooks({ cwd: f.root }), /not replaced/);
  assert.equal(readFileSync(hook, 'utf8'), '#!/bin/sh\ncustom-hook\n');
  f.git('config', 'core.hooksPath', 'custom');
  assert.throws(() => installHooks({ cwd: f.root }), /core.hooksPath/);
});

test('moving backend code into documentation cannot skip CI as docs-only', async t => {
  const f = fixture(t);
  const base = f.commit({ 'backend.go': 'package fixture\n' });
  mkdirSync(join(f.root, 'Documentation'));
  f.git('mv', 'backend.go', 'Documentation/backend.go');
  f.git('commit', '-m', 'move source');
  const sha = f.git('rev-parse', 'HEAD');
  const policy = await classify({ context: { sha, ref: 'refs/heads/master', eventName: 'push', payload: { before: base } },
    execute: (...args) => { const output = f.execute('git', args); return args.includes('-z') ? output : output.trim(); } });
  assert.equal(policy.docsOnly, false);
});
