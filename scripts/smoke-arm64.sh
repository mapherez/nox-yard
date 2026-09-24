#!/bin/sh
set -eu

image=${1:?Usage: sh scripts/smoke-arm64.sh IMAGE}
binary=$(mktemp)
container_id=

cleanup() {
  if [ -n "$container_id" ]; then
    docker rm -f "$container_id" >/dev/null 2>&1 || true
  fi
  rm -f "$binary"
}
trap cleanup EXIT

container_id=$(docker create --platform linux/arm64 \
  -p 127.0.0.1:18095:8080 \
  -e NOX_DATA_DIR=/tmp/nox-yard-smoke \
  "$image")
docker cp "$container_id:/usr/local/bin/nox-yard" "$binary"

python3 - "$binary" <<'PY'
import sys

with open(sys.argv[1], "rb") as executable:
    header = executable.read(20)

if (len(header) != 20 or header[:4] != b"\x7fELF" or header[4] != 2
        or header[5] != 1 or int.from_bytes(header[18:20], "little") != 183):
    raise SystemExit("The image does not contain a Linux ARM64 executable")
print("Confirmed Linux ARM64 executable")
PY

docker start "$container_id" >/dev/null
attempt=0
while [ "$attempt" -lt 45 ]; do
  if curl --fail --silent --show-error http://127.0.0.1:18095/healthz 2>/dev/null | grep -q '"status":"ok"'; then
    echo "ARM64 container started and /healthz responded"
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 2
done

docker logs "$container_id" >&2 || true
echo "ARM64 smoke test failed: /healthz did not respond" >&2
exit 1
