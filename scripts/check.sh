#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

echo "Checking Go formatting..."
unformatted=$(git ls-files -z -- '*.go' | xargs -0 -r gofmt -l)
if [ -n "$unformatted" ]; then
  printf 'Go files need gofmt:\n%s\n' "$unformatted" >&2
  exit 1
fi

echo "Running go vet..."
go vet ./...

echo "Validating Docker Compose..."
docker compose config --quiet
