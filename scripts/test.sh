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

# These are no-ops until the corresponding package.json scripts are added.
run_check 'Optional frontend tests' npm run --if-present test
run_check 'Optional frontend lint' npm run --if-present lint
run_check 'Optional frontend stylelint' npm run --if-present stylelint
