#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

echo "Running Go tests..."
go test ./...

if [ ! -d web/node_modules ]; then
  echo "Frontend dependencies are missing. Run 'cd web && npm ci' first." >&2
  exit 1
fi

cd web
echo "Type-checking and building the frontend..."
npm run build

# These are no-ops until the corresponding package.json scripts are added.
npm run --if-present test
npm run --if-present lint
npm run --if-present stylelint
