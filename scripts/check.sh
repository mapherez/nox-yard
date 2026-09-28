#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/ci-output.sh"
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$repo_root"

check_gofmt() {
  unformatted=$(git ls-files -z -- '*.go' | xargs -0 -r gofmt -l) || return $?
  if [ -n "$unformatted" ]; then
    printf 'Go files need gofmt:\n%s\n' "$unformatted" >&2
    return 1
  fi
}

run_check 'gofmt' check_gofmt
run_check 'go vet ./...' go vet ./...
run_check 'Docker Compose configuration' docker compose config --quiet
