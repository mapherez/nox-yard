#!/usr/bin/env python3
"""Exercise embedded MCP against disposable local Docker fixtures.

Requires a local Linux Docker Engine (or Docker Desktop) and an already built
Yard image. Publishes only ephemeral loopback ports. Never targets existing
containers, projects, volumes or settings; cleanup uses recorded fixture IDs.
"""
import argparse
import json
import os
import queue
import secrets
import shutil
import subprocess
import threading
import time
import urllib.error
import urllib.request
from http.cookiejar import CookieJar
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
parser.add_argument("--arm64-image")
parser.add_argument("--docker-command", default="docker")
parser.add_argument("--docker-config")
parser.add_argument("--docker-host")
args = parser.parse_args()
command = [args.docker_command]
if args.docker_config:
    command += ["--config", args.docker_config]
if args.docker_host:
    command += ["--host", args.docker_host]
root = Path(__file__).resolve().parent.parent
suffix = secrets.token_hex(5)
project = "mcp-smoke-" + suffix
base = root / ".tmp" / ("mcp-smoke-" + suffix)
base.mkdir(parents=True, exist_ok=False)
containers = []
volumes = []
managed_names = []
events_response = None
key = secrets.token_hex(32)
sequence = 0

def docker(*arguments):
    result = subprocess.run(command + list(arguments), check=True, text=True,
                            capture_output=True)
    return result.stdout.strip()

def create(*arguments):
    identifier = docker("create", "--label", "nox-yard.mcp-smoke=" + suffix,
                        *arguments)
    containers.append(identifier)
    return identifier

def until(check, description, seconds=60):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            value = check()
            if value:
                return value
        except (urllib.error.URLError, ConnectionError, TimeoutError):
            pass
        time.sleep(0.2)
    raise AssertionError("Timed out: " + description)

def request(url, path, body=None, opener=None, headers=None):
    data = None if body is None else json.dumps(body).encode()
    headers = dict(headers or {})
    if body is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url + path, data=data, headers=headers)
    try:
        response = (opener.open if opener else urllib.request.urlopen)(req, timeout=65)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None

def rpc(url, method, params):
    global sequence
    sequence += 1
    status, response = request(url, "/mcp", {
        "jsonrpc": "2.0", "id": sequence, "method": method, "params": params,
    }, headers={"Accept": "application/json, text/event-stream",
                "MCP-Protocol-Version": "2025-11-25"})
    assert status == 200 and "result" in response, (status, response)
    return response["result"]

def tool(url, name, arguments=None, error=None):
    result = rpc(url, "tools/call", {"name": name, "arguments": arguments or {}})
    value = result["structuredContent"]
    if error:
        assert result.get("isError") and value["code"] == error, result
    else:
        assert not result.get("isError"), result
    return value

def ready(url):
    return request(url, "/healthz")[0] == 200

def address(identifier):
    info = json.loads(docker("inspect", identifier))[0]
    port = info["NetworkSettings"]["Ports"]["8080/tcp"][0]["HostPort"]
    return "http://127.0.0.1:" + port

def serve(image, platform="linux/amd64", socket=False):
    volume = "mcp-smoke-data-" + suffix + "-" + platform.split("/")[-1]
    docker("volume", "create", "--label", "nox-yard.mcp-smoke=" + suffix, volume)
    volumes.append(volume)
    options = ["--platform", platform, "--publish", "127.0.0.1::8080",
               "--mount", "type=volume,source=" + volume + ",target=/data",
               "--env", "NOX_DATA_DIR=/data", "--env", "NOX_YARD_API_ENABLED=true",
               "--env", "NOX_YARD_API_KEY=" + key,
               "--health-cmd", "wget -q -O /dev/null http://127.0.0.1:8080/healthz",
               "--health-interval", "2s", "--health-timeout", "2s",
               "--health-start-period", "1s"]
    if socket:
        options += ["--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock"]
    identifier = create(*options, image)
    docker("start", identifier)
    url = address(identifier)
    until(lambda: ready(url), platform + " health")
    rpc(url, "initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                           "clientInfo": {"name": "yard-smoke", "version": "1"}})
    assert len(rpc(url, "tools/list", {})["tools"]) == 25
    assert tool(url, "yard_health")["ready"]
    return identifier, url

def await_job(url, job):
    value = until(lambda: completed_job(url, job["id"]), "Compose job " + job["id"])
    assert value["status"] == "succeeded", value
    return value

def completed_job(url, identifier):
    job = tool(url, "yard_compose_job", {"id": identifier})
    return job if job["status"] != "running" else None

try:
    yard, url = serve(args.image, socket=True)
    assert request(url, "/api/projects")[0] == 401
    assert request(url, "/v1/projects")[0] == 401
    assert request(url, "/v1/status", headers={"Authorization": "Bearer " + key})[0] == 200
    assert request(url, "/mcp/unknown")[0] == 404
    docker("pull", "alpine:3.23")
    # Keep this shared image referenced during removal tests, so the removal
    # manager cannot delete an unrelated host image-cache entry.
    create("--name", project + "-image-keeper", "alpine:3.23", "true")
    fixture = create("--name", project + "-fixture", "--env", "MCP_TEST_VALUE=fixture",
                     "alpine:3.23", "sh", "-c",
                     "i=1; while [ $i -le 25 ]; do echo log-$i; i=$((i+1)); done; exec sleep 600")
    assert tool(url, "yard_container_action", {"id": fixture, "action": "start"})["succeeded"] == 1
    until(lambda: len(tool(url, "yard_container_logs", {"id": fixture})["lines"]) == 20,
          "log snapshot")
    lines = tool(url, "yard_container_logs", {"id": fixture})["lines"]
    assert lines[0]["text"].endswith("log-6") and lines[-1]["text"].endswith("log-25"), lines
    normal = tool(url, "yard_container_inspect", {"id": fixture})
    assert all("value" not in item for item in normal["environment"])
    revealed = tool(url, "yard_container_environment", {"id": fixture})
    assert any(item == {"name": "MCP_TEST_VALUE", "value": "fixture"}
               for item in revealed["environment"])
    assert tool(url, "yard_container_pull", {"id": fixture})["succeeded"] == 1
    for action in ("stop", "start", "restart"):
        assert tool(url, "yard_container_action", {"id": fixture, "action": action})["succeeded"] == 1
    tool(url, "yard_container_action", {"id": yard, "action": "stop"}, "TARGET_PROTECTED")
    print("PASS: anonymous MCP, existing auth, lifecycle, logs, inspection, pull and self-protection", flush=True)

    browser = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
    status, setup = request(url, "/api/setup", {"username": "smoke",
        "password": secrets.token_hex(24)}, opener=browser, headers={"Origin": url})
    assert status == 201, setup
    events_response = browser.open(url + "/api/projects/events", timeout=10)
    events = queue.Queue()
    def receive_events():
        try:
            while True:
                line = events_response.readline()
                if not line:
                    break
                if line.startswith(b"data: "):
                    events.put(json.loads(line[6:]))
        except (OSError, ValueError):
            pass
    threading.Thread(target=receive_events, daemon=True).start()
    assert events.get(timeout=5)["inventory"]
    tool(url, "yard_container_action", {"id": fixture, "action": "restart"})
    assert events.get(timeout=5)["inventory"]

    settings = tool(url, "yard_projects_settings_set", {"projectsBase": str(base)})
    assert tool(url, "yard_projects_settings_get")["projectsBase"] == settings["projectsBase"]
    source = {"kind": "paste", "yaml": "services:\n  demo:\n    image: alpine:3.23\n    command: [sleep, '600']\n    stop_grace_period: 1s\n"}
    assert tool(url, "yard_compose_source", source)["yaml"] == source["yaml"]
    deploy = {"name": project, "source": source, "variables": {}, "envFiles": {}, "mode": "new"}
    preview = tool(url, "yard_compose_preview", deploy)
    deploy["fingerprint"] = preview["fingerprint"]
    managed_names.append(project)
    await_job(url, tool(url, "yard_compose_submit", deploy))
    for operation in ("stop", "start", "restart", "pull", "update"):
        await_job(url, tool(url, "yard_compose_operation", {
            "name": project, "operation": operation, "removeVolumes": False}))
    assert any(item["id"] == "compose:" + project and item["kind"] == "managed-compose"
               for item in tool(url, "yard_projects")["projects"])
    await_job(url, tool(url, "yard_compose_operation", {
        "name": project, "operation": "remove", "removeVolumes": False}))
    managed_names.remove(project)
    print("PASS: browser SSE receives MCP events; Compose preview, jobs, lifecycle, update and removal", flush=True)

    preview = tool(url, "yard_container_remove_preview", {"id": fixture})
    tool(url, "yard_container_remove", {"id": fixture, "confirm": False,
        "fingerprint": preview["fingerprint"]}, "INVALID_PAYLOAD")
    tool(url, "yard_container_remove", {"id": fixture, "confirm": True,
        "fingerprint": "0" * 64}, "OPERATION_CONFLICT")
    result = tool(url, "yard_container_remove", {"id": fixture, "confirm": True,
        "fingerprint": preview["fingerprint"]})
    assert any(item["kind"] == "container" and item["status"] == "removed" for item in result["items"])
    containers.remove(fixture)

    before = json.loads(docker("inspect", yard))[0]["State"]["StartedAt"]
    result = tool(url, "yard_container_action", {"id": yard, "action": "restart"})
    assert result["queued"] == 1 and result["succeeded"] == 0
    def restarted():
        info = json.loads(docker("inspect", yard))[0]
        return info["State"]["StartedAt"] != before and info["State"]["Health"]["Status"] == "healthy"
    until(restarted, "Yard self-restart helper", seconds=90)
    # Docker can assign a new ephemeral host port when the container restarts.
    url = address(yard)
    until(lambda: ready(url), "Yard recovery")
    assert request(url, "/api/bootstrap", opener=browser)[1]["authenticated"]
    assert tool(url, "yard_projects_settings_get")["projectsBase"] == settings["projectsBase"]
    assert len(rpc(url, "tools/list", {})["tools"]) == 25
    print("PASS: fingerprint protection, removal, queued self-restart, persisted session/settings and MCP recovery", flush=True)
    if args.arm64_image:
        serve(args.arm64_image, platform="linux/arm64")
        print("PASS: ARM64 runtime health and anonymous MCP tool discovery", flush=True)
finally:
    if events_response:
        events_response.close()
    # Remove only containers belonging to this randomly named Compose fixture.
    for name in managed_names:
        found = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + name)
        if found:
            docker("rm", "-f", *found.splitlines())
        networks = docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + name)
        if networks:
            docker("network", "rm", *networks.splitlines())
    for identifier in reversed(containers):
        subprocess.run(command + ["rm", "-f", identifier], capture_output=True)
    for name in volumes:
        subprocess.run(command + ["volume", "rm", name], capture_output=True)
    # This directory was created by this run and is verified inside repo .tmp.
    resolved = base.resolve()
    assert resolved.is_relative_to((root / ".tmp").resolve()) and resolved.name == "mcp-smoke-" + suffix
    shutil.rmtree(resolved)
