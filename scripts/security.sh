#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir/.."

# Build the scanner for the host before changing the analyzed target platform.
# Installing into a temporary directory leaves the application module unchanged.
scanner_dir=$(mktemp -d)
scanner_bin="$scanner_dir/govulncheck$(go env GOEXE)"
trap 'rm -f "$scanner_bin"; rmdir "$scanner_dir"' EXIT
scanner_install_dir="$scanner_dir"
if command -v cygpath >/dev/null 2>&1; then
  scanner_install_dir=$(cygpath -w "$scanner_dir")
fi
GOBIN="$scanner_install_dir" go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
GOOS=linux GOARCH=amd64 "$scanner_bin" ./...
GOOS=linux GOARCH=arm64 "$scanner_bin" ./...
