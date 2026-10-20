#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# Keep progress and tool diagnostics together in CI and manual local runs.
exec 1>&2

sh "$script_dir/check.sh"
sh "$script_dir/test.sh"
printf '\n[pass] All source checks passed.\n'
