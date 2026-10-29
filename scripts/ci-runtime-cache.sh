#!/bin/sh
# Transfer only the two Go caches used by disposable fixture runners. Archives
# are host-created so root containers cannot leave unreadable cache artifacts.
set -eu
operation=${1:?restore or save}
directory=${2:?cache directory}
image=${3:?current CI image}
case "$operation" in restore|save) ;; *) exit 2 ;; esac
mkdir -p "$directory"
directory=$(CDPATH= cd -- "$directory" && pwd)
for entry in modules build; do
  case "$entry" in
    modules) volume=nox-yard-c2-go-cache ;;
    build) volume=nox-yard-c2-build-cache ;;
  esac
  docker volume create "$volume" >/dev/null
  if [ "$operation" = restore ]; then
    if [ ! -f "$directory/$entry.tar" ]; then continue; fi
    docker run --rm --network none --entrypoint sh \
      --mount "type=volume,source=$volume,target=/cache" \
      --mount "type=bind,source=$directory,target=/transfer,readonly" \
      "$image" -ec 'tar -xf "/transfer/$1.tar" -C /cache' sh "$entry"
  else
    (umask 077; : > "$directory/$entry.tar")
    docker run --rm --network none --entrypoint sh \
      --mount "type=volume,source=$volume,target=/cache,readonly" \
      --mount "type=bind,source=$directory,target=/transfer" \
      "$image" -ec 'tar -cf "/transfer/$1.tar" -C /cache .' sh "$entry"
  fi
done
