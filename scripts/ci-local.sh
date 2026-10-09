#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# Explicit full source validation, never called by the pre-push hook or release.
exec 1>&2

sh "$script_dir/check.sh"
sh "$script_dir/test.sh"
printf '\n[pass] All explicit source checks passed.\n'
