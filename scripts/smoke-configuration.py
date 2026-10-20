#!/usr/bin/env python3
"""Exercise file editing and Yard self-recreation using a disposable local Yard.

All resources use a generated fixture identity; never contacts a live Yard.
"""
import argparse
import http.cookiejar
import json
import pathlib
import secrets
import shutil
import socket
import subprocess
import time
import urllib.request
from machine_smoke import enable_mcp

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = "configuration-smoke-" + secrets.token_hex(6)
base = root / ".tmp" / name
base.mkdir(parents=True)
(base / "data").mkdir()
host = str(base).replace("\\", "/")
if len(host) > 2 and host[1] == ":":
    host = "/run/desktop/mnt/host/" + host[0].lower() + "/" + host[3:]

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

with socket.socket() as probe:
    probe.bind(("127.0.0.1", 0))
    port = probe.getsockname()[1]
origin = "http://127.0.0.1:" + str(port)
with socket.socket() as probe:
    probe.bind(("127.0.0.1", 0))
    mcp_port = probe.getsockname()[1]
mcp_origin = "http://127.0.0.1:" + str(mcp_port)
mcp_token = ""
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ""
sequence = 0

def rpc(method, params):
    global sequence
    sequence += 1
    request = urllib.request.Request(mcp_origin + "/mcp", data=json.dumps({
        "jsonrpc": "2.0", "id": sequence, "method": method, "params": params,
    }).encode(), headers={"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "Authorization": "Bearer " + mcp_token,
                          "MCP-Protocol-Version": "2025-11-25"})
    with urllib.request.urlopen(request, timeout=65) as response:
        result = json.load(response)
    assert "result" in result, result
    return result["result"]

def tool(name, arguments, error=None):
    result = rpc("tools/call", {"name": name, "arguments": arguments})
    value = result["structuredContent"]
    if error:
        assert result.get("isError") and value["code"] == error, result
    else:
        assert not result.get("isError"), result
    return value

def api(path, body=None, method=None):
    headers = {"Origin": origin, "Content-Type": "application/json", "X-CSRF-Token": csrf}
    request = urllib.request.Request(origin + path, data=json.dumps(body).encode() if body is not None else None,
                                     headers=headers, method=method)
    with opener.open(request, timeout=30) as response:
        return json.load(response)

def wait(condition, description, timeout=100):
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

def current():
    identifiers = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + name,
                         "--filter", "label=com.docker.compose.service=nox-yard").splitlines()
    if len(identifiers) != 1:
        raise RuntimeError("fixture must contain exactly one Yard")
    return json.loads(docker("inspect", identifiers[0]))[0]

def recreate():
    preview = tool("yard_recreate_preview", {"id": "compose:" + name, "operation": "recreate"})
    job = tool("yard_recreate_submit", {"id": "compose:" + name, "operation": "recreate", "confirm": True,
                                       "fingerprint": preview["fingerprint"]})
    return wait(lambda: (value if (value := api("/api/jobs/" + job["id"]))["status"] != "running" else None),
                "independent self-recreation did not finish", 150)

source = f'''services:
  nox-yard:
    image: {json.dumps(args.image)}
    init: true
    labels:
      nox-yard.acceptance: {name}
    environment:
      NOX_DATA_DIR: /data
      NOX_LISTEN_ADDR: ':8080'
      TZ: Europe/Lisbon
      NOX_FILE_TEST: '${{FILE_TEST}}'
    ports: ['127.0.0.1:{port}:8080', '127.0.0.1:{mcp_port}:8081']
    volumes: ['./data:/data', '/var/run/docker.sock:/var/run/docker.sock']
    healthcheck:
      test: ['CMD-SHELL', 'wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1']
      interval: 1s
      timeout: 1s
      retries: 5
    stop_grace_period: 2s
'''
try:
    (base / "compose.yaml").write_text(source, newline="")
    (base / ".env").write_text("FILE_TEST=initial\n", newline="")
    compose("up", "-d", "--pull", "never")
    wait(lambda: api("/api/bootstrap"), "fixture did not start")
    api("/api/setup", {"username": "fixture", "password": "fixture-password-configuration"})
    csrf = api("/api/bootstrap")["csrfToken"]
    mcp_token = enable_mcp(origin, opener, csrf)
    target = "/api/projects/compose:" + name
    configuration = api(target + "/configuration/read", {"reveal": True})
    assert configuration["available"] and configuration["state"] == "ready"
    assert any(file["path"] == "compose.yaml" for file in configuration["files"])
    hidden = api(target + "/configuration/read", {"reveal": False})
    assert all(not file["content"] for file in hidden["files"])
    rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                       "clientInfo": {"name": "configuration-fixture", "version": "1"}})
    catalog = rpc("tools/list", {})["tools"]
    assert len(catalog) == 35 and not any("migration" in item["name"] for item in catalog)
    configuration_id = {"id": "compose:" + name}
    assert tool("yard_project_configuration_read", configuration_id) == hidden
    assert tool("yard_project_configuration_read", {**configuration_id, "reveal": True}) == configuration
    first = current()
    saved = tool("yard_project_variable_set", {**configuration_id, "fingerprint": configuration["fingerprint"],
                 "service": "nox-yard", "name": "NOX_FILE_TEST", "value": "edited$private-value"})
    assert "edited$private-value" not in json.dumps(saved)
    assert any(value["name"] == "NOX_FILE_TEST" and value["pending"] for value in saved["variables"])
    assert (base / ".env").read_text() != "FILE_TEST=initial\n"
    revealed = tool("yard_project_configuration_read", {**configuration_id, "reveal": True})
    assert any(value["name"] == "NOX_FILE_TEST" and value["value"] == "edited$private-value" for value in revealed["variables"])
    tool("yard_project_variable_set", {**configuration_id, "fingerprint": configuration["fingerprint"],
         "service": "nox-yard", "name": "NOX_FILE_TEST", "value": "stale-value"}, error="OPERATION_CONFLICT")
    assert current()["Id"] == first["Id"], "Save must not replace containers"
    wait(lambda: current()["State"].get("Health", {}).get("Status") == "healthy", "fixture not healthy")
    schedule = api(target + "/schedule")
    assert schedule["time"] == "03:00" and schedule["eligible"] and not schedule["enabled"]
    scheduled = tool("yard_project_schedule_set", {**configuration_id, "enabled": True, "time": "03:15"})
    assert scheduled["time"] == "03:15" and scheduled["enabled"] and scheduled["timezone"] == "Europe/Lisbon"
    assert api(target + "/schedule")["time"] == "03:15"
    result = recreate()
    assert result["status"] == "succeeded", result
    second = current()
    assert second["Id"] != first["Id"] and "NOX_FILE_TEST=edited$private-value" in second["Config"]["Env"]
    assert api(target + "/schedule")["time"] == "03:15"
    assert api(target + "/schedule")["enabled"]
    assert not any(value["pending"] for value in tool("yard_project_configuration_read", configuration_id)["variables"])
    assert "edited$private-value" not in json.dumps(tool("yard_job_history", {"target": "compose:" + name}))
    print("PASS: MCP/browser share host files, redaction, fingerprint conflicts and daily schedule; MCP Yard Recreate applies env without build", flush=True)

    # Full-file MCP editing follows the same validation and save-only path.
    configuration = tool("yard_project_configuration_read", {**configuration_id, "reveal": True})
    modified = source.replace("      TZ: Europe/Lisbon", "      TZ: Europe/Lisbon\n      NOX_MCP_TEST: from-mcp-file")
    saved = tool("yard_project_configuration_save", {**configuration_id, "fingerprint": configuration["fingerprint"],
                 "path": "compose.yaml", "content": modified})
    assert all(not file["content"] for file in saved["files"])
    assert (base / "compose.yaml").read_text() == modified and current()["Id"] == second["Id"]
    result = recreate()
    assert result["status"] == "succeeded", result
    assert "NOX_MCP_TEST=from-mcp-file" in current()["Config"]["Env"]
    print("PASS: MCP full-file save preserves filenames and leaves containers untouched until reviewed Recreate", flush=True)

    # Pull must use this service's current file reference, not its runtime image
    # or another service. The sibling's closed registry port makes scope errors
    # fail immediately. These edits are never applied to the Yard runtime.
    pull_source = modified.replace("image: " + json.dumps(args.image), "image: alpine:3.23")
    pull_source += "  unavailable-sibling:\n    image: 127.0.0.1:1/fixture:never\n"
    before_pull = current()
    try:
        (base / "compose.yaml").write_text(pull_source, newline="")
        pulled = tool("yard_container_pull", {"id": before_pull["Id"]})
        assert pulled["images"] == ["alpine:3.23"] and pulled["succeeded"] == 1, pulled
        assert current()["Id"] == before_pull["Id"] and current()["Image"] == before_pull["Image"]
        failed = rpc("tools/call", {"name": "yard_project_pull", "arguments": configuration_id})
        assert failed.get("isError"), "project Pull must report the unavailable sibling"
        assert current()["Id"] == before_pull["Id"]
    finally:
        (base / "compose.yaml").write_text(modified, newline="")
    print("PASS: container Pull reads the current service image and excludes siblings; project Pull reports failure without changing containers", flush=True)

    # Manual edits must be read afresh, without a DB configuration copy.
    manual = (base / "compose.yaml").read_text().replace("      TZ: Europe/Lisbon", "      TZ: Europe/Lisbon\n      NOX_MANUAL_TEST: from-host-file")
    (base / "compose.yaml").write_text(manual, newline="")
    result = recreate()
    assert result["status"] == "succeeded", result
    assert "NOX_MANUAL_TEST=from-host-file" in current()["Config"]["Env"]
    print("PASS: manual compose.yaml edits are the source of truth after self-recreation", flush=True)

    # A valid Compose with an application-invalid option must restore the old
    # runtime and SQLite snapshot, while preserving the user's edited file.
    invalid = manual.replace("      TZ: Europe/Lisbon", "      TZ: Europe/Lisbon\n      NOX_PUBLIC_URL: 'ftp://invalid-fixture'")
    (base / "compose.yaml").write_text(invalid, newline="")
    result = recreate()
    assert result["status"] == "failed" and result["rollback"] == "restored", result
    assert "NOX_FILE_TEST=edited$private-value" in current()["Config"]["Env"]
    assert not any(value == "NOX_PUBLIC_URL=ftp://invalid-fixture" for value in current()["Config"]["Env"])
    assert (base / "compose.yaml").read_text() == invalid
    assert api("/api/bootstrap")["authenticated"]
    print("PASS: failed self-recreation restores previous runtime/database without overwriting manual source edits", flush=True)
finally:
    identifiers = docker("ps", "-aq", "--filter", "label=nox-yard.acceptance=" + name).splitlines()
    if identifiers:
        docker("rm", "-f", *identifiers)
    # Durable workers use the target project as a resource; only this fixture's
    # worker names/labels are eligible for cleanup, never other Yard jobs.
    workers = docker("ps", "-aq", "--filter", "label=nox-yard.role=operation-worker").splitlines()
    for worker in workers:
        info = json.loads(docker("inspect", worker))[0]
        if any(mount.get("Source", "").replace("\\", "/").endswith(name + "/data") for mount in info.get("Mounts", [])):
            docker("rm", "-f", worker)
    networks = docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + name).splitlines()
    if networks:
        docker("network", "rm", *networks)
    assert base.resolve().is_relative_to((root / ".tmp").resolve()) and base.name == name
    shutil.rmtree(base)
