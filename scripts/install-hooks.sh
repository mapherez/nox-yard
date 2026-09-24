#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

if [ -n "$(git config --get core.hooksPath || true)" ]; then
  echo "A custom core.hooksPath is already configured; install the NoX Yard pre-push call there manually." >&2
  exit 1
fi

hooks_dir=$(git rev-parse --git-path hooks)
case "$hooks_dir" in
  /*|[A-Za-z]:/*) ;;
  *) hooks_dir="$repo_root/$hooks_dir" ;;
esac

mkdir -p "$hooks_dir"
if [ -e "$hooks_dir/pre-push" ]; then
  echo "An existing pre-push hook was found at $hooks_dir/pre-push; it was not replaced." >&2
  exit 1
fi

cat > "$hooks_dir/pre-push" <<'HOOK'
#!/bin/sh
set -eu
repo_root=$(git rev-parse --show-toplevel)
exec sh "$repo_root/scripts/ci-local.sh"
HOOK
chmod +x "$hooks_dir/pre-push"
echo "Installed $hooks_dir/pre-push"
