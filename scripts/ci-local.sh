#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# Git clients may hide hook stdout. Send both progress and tool diagnostics to stderr.
exec 1>&2

sh "$script_dir/check.sh"
sh "$script_dir/test.sh"
printf '\n[pass] All local pre-push checks passed.\n'
