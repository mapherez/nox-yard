#!/usr/bin/env node
import { existsSync, mkdirSync, readFileSync, writeFileSync, chmodSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { run } from './push-check.mjs';
import { isMain } from './release-version.mjs';

export const legacyHook = '#!/bin/sh\nset -eu\nrepo_root=$(git rev-parse --show-toplevel)\nexec sh "$repo_root/scripts/ci-local.sh"\n';
export const pushHook = '#!/bin/sh\nset -eu\nrepo_root=$(git rev-parse --show-toplevel)\nexec node "$repo_root/scripts/push-check.mjs" "$@"\n';

export function installHooks({ cwd = process.cwd(), log = console.log } = {}) {
  const git = (...args) => run('git', args, undefined, cwd).trim();
  let custom = '';
  try { custom = git('config', '--get', 'core.hooksPath'); } catch {}
  if (custom) throw new Error('Custom core.hooksPath: add the push-check.mjs call to your existing hook; it was not replaced.');
  const path = resolve(cwd, git('rev-parse', '--git-path', 'hooks/pre-push'));
  if (existsSync(path)) {
    const contents = readFileSync(path, 'utf8').replaceAll('\r\n', '\n');
    if (contents !== legacyHook && contents !== pushHook) throw new Error(`Custom hook at ${path}; it was not replaced.`);
    if (contents === pushHook) { log('NoX Yard push hook is already current.'); return; }
  }
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, pushHook);
  chmodSync(path, 0o755);
  log(`Installed lightweight NoX Yard push hook: ${path}`);
}

if (isMain(import.meta.url)) {
  try { installHooks(); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
