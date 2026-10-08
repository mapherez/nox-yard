#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/ci-output.sh"
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$repo_root"

run_check 'go test ./...' go test ./...
run_check 'Release tooling tests' node --test scripts/release.test.mjs

if [ ! -d web/node_modules ]; then
  printf '[FAIL] Frontend dependencies are missing. Run "npm ci --prefix web" first.\n' >&2
  exit 1
fi

cd web
run_check 'Frontend typecheck and build' npm run build

run_check 'Frontend state regressions' npm test
