#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

if [ -n "$(git config --get core.hooksPath || true)" ]; then
  echo "A custom core.hooksPath is configured; it was not modified. Remove any Yard validation call there manually. Checks now run in CI." >&2
  exit 1
fi

hooks_dir=$(git rev-parse --git-path hooks)
case "$hooks_dir" in
  /*|[A-Za-z]:/*) ;;
  *) hooks_dir="$repo_root/$hooks_dir" ;;
esac

hook="$hooks_dir/pre-push"
if [ ! -e "$hook" ] && [ ! -L "$hook" ]; then
  echo "No Yard pre-push hook is installed. Pushes use CI validation only."
  exit 0
fi
if [ -L "$hook" ] || [ ! -f "$hook" ]; then
  echo "The existing pre-push hook is not a regular file; it was not modified." >&2
  exit 1
fi

# Compatibility entry point: retire only the exact hook the old installer wrote.
# Never overwrite or remove a user's custom hook. Accept Windows CRLF as well.
legacy_hook=$(cat <<'HOOK'
#!/bin/sh
set -eu
repo_root=$(git rev-parse --show-toplevel)
exec sh "$repo_root/scripts/ci-local.sh"
HOOK
)
if [ "$(tr -d '\r' < "$hook")" != "$legacy_hook" ]; then
  echo "An unrecognized pre-push hook was found at $hook; it was not modified. Remove any Yard validation call there manually." >&2
  exit 1
fi

rm -- "$hook"
echo "Removed the legacy Yard pre-push hook. Checks now run in CI; manual checks remain available with sh scripts/ci-local.sh."
