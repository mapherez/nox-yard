#!/bin/sh
# Resolve metadata for the exact commit being built, never an ancestor's tag.
set -eu

commit=$(git rev-parse --verify "${1:-HEAD}^{commit}")
version=$(git tag --points-at "$commit" | LC_ALL=C sort | sed -n '1p')
if [ -z "$version" ]; then
  version="git-$commit"
fi
printf '%s\n' "$version"
