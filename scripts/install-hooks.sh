#!/bin/sh
# Compatibility entrypoint; installation itself is portable Node.js.
set -eu
repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"
exec node "$repo_root/scripts/install-hooks.mjs"
