#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir/.."

# Build the scanner for the host before changing the analyzed target platform.
# Installing into a temporary directory leaves the application module unchanged.
scanner_dir=${NOX_SCANNER_CACHE:-$(mktemp -d)}
mkdir -p "$scanner_dir"
scanner_dir=$(CDPATH= cd -- "$scanner_dir" && pwd)
scanner_bin="$scanner_dir/govulncheck$(go env GOEXE)"
if [ -z "${NOX_SCANNER_CACHE:-}" ]; then
  trap 'rm -f "$scanner_bin"; rmdir "$scanner_dir"' EXIT
fi
scanner_install_dir="$scanner_dir"
if command -v cygpath >/dev/null 2>&1; then
  scanner_install_dir=$(cygpath -w "$scanner_dir")
fi
if [ ! -x "$scanner_bin" ]; then
  GOBIN="$scanner_install_dir" go install golang.org/x/vuln/cmd/govulncheck@v1.8.0
fi
GOOS=linux GOARCH=amd64 "$scanner_bin" ./...
GOOS=linux GOARCH=arm64 "$scanner_bin" ./...
