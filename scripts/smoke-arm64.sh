#!/bin/sh
set -eu

image=${1:?Usage: sh scripts/smoke-arm64.sh IMAGE}
architecture=${2:-arm64}
case "$architecture" in
  arm64) machine=183 ;;
  amd64) machine=62 ;;
  *) echo "Unsupported architecture: $architecture" >&2; exit 1 ;;
esac
binary=$(mktemp)
container_id=

cleanup() {
  if [ -n "$container_id" ]; then
    docker rm -f "$container_id" >/dev/null 2>&1 || true
  fi
  rm -f "$binary"
}
trap cleanup EXIT

container_id=$(docker create --platform "linux/$architecture" \
  -p 127.0.0.1::8080 \
  -e NOX_DATA_DIR=/tmp/nox-yard-smoke \
  "$image")
docker cp "$container_id:/usr/local/bin/nox-yard" "$binary"

if command -v python3 >/dev/null 2>&1; then
  python_command=python3
else
  python_command=python
fi
"$python_command" - "$binary" "$machine" "$architecture" <<'PY'
import sys

with open(sys.argv[1], "rb") as executable:
    header = executable.read(20)

if (len(header) != 20 or header[:4] != b"\x7fELF" or header[4] != 2
        or header[5] != 1 or int.from_bytes(header[18:20], "little") != int(sys.argv[2])):
    raise SystemExit("The image does not contain the expected Linux " + sys.argv[3] + " executable")
print("Confirmed Linux " + sys.argv[3] + " executable")
PY

docker start "$container_id" >/dev/null
address=$(docker port "$container_id" 8080/tcp)
attempt=0
while [ "$attempt" -lt 45 ]; do
  if curl --fail --silent --show-error "http://$address/healthz" 2>/dev/null | grep -q '"status":"ok"'; then
    echo "$architecture container started and /healthz responded"
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 2
done

docker logs "$container_id" >&2 || true
echo "$architecture smoke test failed: /healthz did not respond" >&2
exit 1
