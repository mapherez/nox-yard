#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/ci-output.sh"
repo_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
cd "$repo_root"

# Formatting belongs to the lightweight pre-push hook, not remote functional CI.
run_check 'go vet ./...' go vet ./...
run_check 'Docker Compose configuration' docker compose config --quiet
