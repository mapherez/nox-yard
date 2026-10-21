#!/usr/bin/env python3
"""Verify service-only Recreate and recovery in a disposable local Compose project."""
import argparse
import json
import pathlib
import secrets
import shutil
import socket
import subprocess
import time
import urllib.request
from machine_smoke import bootstrap_mcp

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = "service-recreate-" + secrets.token_hex(6)
base = root / ".tmp" / name
(base / "data").mkdir(parents=True)
(base / "state").mkdir()
host = str(base).replace("\\", "/")
if len(host) > 2 and host[1] == ":":
    host = "/run/desktop/mnt/host/" + host[0].lower() + "/" + host[3:]
with socket.socket() as probe:
    probe.bind(("127.0.0.1", 0))
    port = probe.getsockname()[1]
origin = "http://127.0.0.1:" + str(port)
with socket.socket() as probe:
    probe.bind(("127.0.0.1", 0))
    mcp_port = probe.getsockname()[1]
mcp_origin = "http://127.0.0.1:" + str(mcp_port)
mcp_token = ""
sequence = 0

def docker(*parts):
    result = subprocess.run(["docker", *parts], capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError("Fixture Docker command failed: " + " ".join(parts[:3]) + "\n" + result.stderr)
    return result.stdout.strip()

def compose(*parts):
    return docker("run", "--rm", "--label", "nox-yard.acceptance=" + name,
                  "--mount", "type=bind,source=" + str(base) + ",target=" + host,
                  "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
                  "--entrypoint", "docker", args.image, "compose", "--project-directory", host,
                  "-p", name, "-f", host + "/compose.yaml", *parts)

def rpc(method, params):
    global sequence
    sequence += 1
    request = urllib.request.Request(mcp_origin + "/mcp", data=json.dumps({"jsonrpc": "2.0", "id": sequence, "method": method, "params": params}).encode(),
        headers={"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "MCP-Protocol-Version": "2025-11-25", "Authorization": "Bearer " + mcp_token})
    with urllib.request.urlopen(request, timeout=65) as response:
        return json.load(response)["result"]

def tool(name, arguments, error=None):
    result = rpc("tools/call", {"name": name, "arguments": arguments})
    value = result.get("structuredContent")
    if value is None:
        value = json.loads(next(block["text"] for block in result["content"] if block["type"] == "text"))
    if error:
        assert result.get("isError") and value["code"] == error, result
    else:
        assert not result.get("isError"), result
    return value

def wait(condition, description, timeout=160):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            result = condition()
            if result:
                return result
        except Exception as error:
            last = error
        time.sleep(.5)
    raise RuntimeError(description + (": " + str(last) if last else ""))

def runtime(service=None):
    filters = ["--filter", "label=com.docker.compose.project=" + name]
    if service:
        filters += ["--filter", "label=com.docker.compose.service=" + service]
    ids = docker("ps", "-aq", *filters).splitlines()
    return json.loads(docker("inspect", *ids)) if ids else []

def peers():
    return sorted((item["Id"], item["State"]["Running"], item["State"]["StartedAt"], item["RestartCount"], item["Image"])
                  for item in runtime() if item["Config"]["Labels"]["com.docker.compose.service"] != "app")

target = {"id": "compose:" + name, "operation": "recreate", "service": "app"}

def recreate():
    preview = tool("yard_recreate_preview", target)
    assert len(preview["items"]) == 2 and all(item["service"] == "app" for item in preview["items"]), preview
    job = tool("yard_recreate_submit", {**target, "fingerprint": preview["fingerprint"], "confirm": True})
    assert len(job["sourceImages"]) == 2 and all(item["service"] == "app" for item in job["sourceImages"]), job
    return wait(lambda: value if (value := tool("yard_job", {"id": job["id"]}))["status"] != "running" else None, "service job did not finish")

source = f'''services:
  nox-yard:
    image: {json.dumps(args.image)}
    labels: {{nox-yard.acceptance: {name}}}
    environment: {{NOX_DATA_DIR: /data, NOX_LISTEN_ADDR: ':8080'}}
    ports: ['127.0.0.1:{port}:8080', '127.0.0.1:{mcp_port}:8081']
    volumes: ['./data:/data', '/var/run/docker.sock:/var/run/docker.sock']
    healthcheck:
      test: ['CMD-SHELL', 'wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1']
      interval: 1s
      timeout: 1s
      retries: 5
    stop_grace_period: 1s
  db:
    image: {json.dumps(args.image)}
    labels: {{nox-yard.acceptance: {name}}}
    command: [sh, -c, 'exec sleep 600']
    stop_grace_period: 1s
  init:
    image: {json.dumps(args.image)}
    labels: {{nox-yard.acceptance: {name}}}
    command: [sh, -c, 'echo initialized >> /state/init-runs']
    volumes: ['./state:/state']
    depends_on: {{db: {{condition: service_started}}}}
  app:
    image: {json.dumps(args.image)}
    build: ./development-only-missing-context
    pull_policy: build
    labels: {{nox-yard.acceptance: {name}}}
    command: [sh, -c, 'test "$$FAIL" != 1 || exit 42; exec sleep 600']
    environment: {{VALUE: '${{VALUE:-initial}}', FAIL: '${{FAIL:-0}}'}}
    deploy: {{replicas: 2}}
    depends_on: {{init: {{condition: service_completed_successfully}}, db: {{condition: service_started}}}}
    stop_grace_period: 1s
'''
try:
    (base / "compose.yaml").write_text(source, newline="")
    (base / ".env").write_text("VALUE=initial\nFAIL=0\n", newline="")
    compose("up", "-d", "--no-build", "--pull", "never")
    wait(lambda: urllib.request.urlopen(origin + "/healthz", timeout=3).status == 200, "Yard did not start")
    _, _, mcp_token = bootstrap_mcp(origin)
    rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "service-fixture", "version": "1"}})
    initial_peers = peers()
    initial_runs = (base / "state/init-runs").read_text()
    old_ids = {item["Id"] for item in runtime("app")}
    # Make the unrelated database image unavailable in the current file. Scoped
    # recreation must neither inspect/pull it nor recreate that dependency.
    edited = source.replace("  db:\n    image: " + json.dumps(args.image), "  db:\n    image: 127.0.0.1:1/unavailable:never")
    (base / "compose.yaml").write_text(edited, newline="")
    (base / ".env").write_text("VALUE=from-current-file\nFAIL=0\n", newline="")
    result = recreate()
    assert result["status"] == "succeeded", result
    assert {item["Id"] for item in runtime("app")}.isdisjoint(old_ids)
    assert all("VALUE=from-current-file" in item["Config"]["Env"] for item in runtime("app"))
    assert peers() == initial_peers and (base / "state/init-runs").read_text() == initial_runs
    assert (base / "compose.yaml").read_text() == edited and "build:" in edited
    print("PASS: two service replicas use current env/local images with image+build; dependencies, initializer, Yard and source files remain unchanged", flush=True)

    reviewed = tool("yard_recreate_preview", target)
    tool("yard_recreate_submit", {**target, "service": "db", "fingerprint": reviewed["fingerprint"], "confirm": True}, error="OPERATION_CONFLICT")
    tool("yard_recreate_preview", {**target, "service": "missing"}, error="INVALID_PAYLOAD")
    assert peers() == initial_peers
    print("PASS: unknown services and confirmation scope changes fail without widening to the project", flush=True)

    peer = docker("run", "-d", "--label", "nox-yard.acceptance=" + name, "--network", "container:" + runtime("app")[0]["Id"], args.image, "sh", "-c", "exec sleep 600")
    try:
        tool("yard_recreate_preview", target, error="UNSUPPORTED_CONFIGURATION")
    finally:
        docker("rm", "-f", peer)
    print("PASS: a container outside the Compose project sharing the target namespace blocks recreation before mutation", flush=True)

    (base / ".env").write_text("VALUE=failing-candidate\nFAIL=1\n", newline="")
    result = recreate()
    assert result["status"] == "failed" and result["rollback"] == "restored", result
    assert all(item["State"]["Running"] and "VALUE=from-current-file" in item["Config"]["Env"] for item in runtime("app"))
    assert peers() == initial_peers and (base / "state/init-runs").read_text() == initial_runs
    print("PASS: failed replacement restores only selected replicas; dependency IDs/start times and initializer run count are preserved", flush=True)

    (base / ".env").write_text("VALUE=stopped-candidate\nFAIL=0\n", newline="")
    for item in runtime("app"):
        docker("stop", "--time", "1", item["Id"])
    result = recreate()
    assert result["status"] == "succeeded", result
    assert all(not item["State"]["Running"] and "VALUE=stopped-candidate" in item["Config"]["Env"] for item in runtime("app"))
    assert peers() == initial_peers
    print("PASS: a stopped service remains stopped after current-file recreation", flush=True)

    # Missing-file fallback uses the runtime of the selected service, retaining
    # the same scope and its dependencies through the Engine worker.
    for item in runtime("app"):
        docker("start", item["Id"])
    (base / "compose.yaml").rename(base / "compose.saved")
    old_ids = {item["Id"] for item in runtime("app")}
    result = recreate()
    assert result["status"] == "succeeded", result
    wait(lambda: len(items := runtime("app")) == 2 and {item["Id"] for item in items}.isdisjoint(old_ids), "Engine retained-container cleanup did not finish")
    assert peers() == initial_peers
    print("PASS: missing Compose files retain service-only Engine fallback without mutating peers", flush=True)
finally:
    ids = docker("ps", "-aq", "--filter", "label=nox-yard.acceptance=" + name).splitlines()
    if ids:
        docker("rm", "-f", *ids)
    for worker in docker("ps", "-aq", "--filter", "label=nox-yard.role=operation-worker").splitlines():
        info = json.loads(docker("inspect", worker))[0]
        if any(mount.get("Source", "").replace("\\", "/").endswith(name + "/data") for mount in info.get("Mounts", [])):
            docker("rm", "-f", worker)
    networks = docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + name).splitlines()
    if networks:
        docker("network", "rm", *networks)
    assert base.resolve().is_relative_to((root / ".tmp").resolve()) and base.name == name
    shutil.rmtree(base)
