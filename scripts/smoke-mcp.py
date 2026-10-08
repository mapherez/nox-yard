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
workers = set()
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
    assert len(rpc(url, "tools/list", {})["tools"]) == 30
    assert tool(url, "yard_health")["ready"]
    return identifier, url

def await_job(url, job, expected="succeeded"):
    value = until(lambda: completed_job(url, job["id"]), "Compose job " + job["id"], seconds=150)
    if value.get("workerID"):
        workers.add(value["workerID"])
    assert value["status"] == expected, value
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

    # C4 uses the same independent worker lifetime as managed operations.
    external = {"id": "container:" + fixture, "operation": "update"}
    external["fingerprint"] = tool(url, "yard_recreate_preview", external)["fingerprint"]
    external["confirm"] = True
    accepted = tool(url, "yard_recreate_submit", external)
    def engine_result(identifier):
        current = tool(url, "yard_job", {"id": identifier})
        return current if current["status"] != "running" else None
    unchanged = until(lambda: engine_result(accepted["id"]), "unchanged external update")
    workers.add(unchanged["workerID"])
    assert unchanged["outcome"] == "unchanged" and unchanged["targetImages"][0]["containerID"] == fixture, unchanged
    external = {"id": "container:" + fixture, "operation": "recreate"}
    external["fingerprint"] = tool(url, "yard_recreate_preview", external)["fingerprint"]
    external["confirm"] = True
    accepted = tool(url, "yard_recreate_submit", external)
    workers.add(accepted["workerID"])
    def candidate():
        current = tool(url, "yard_job", {"id": accepted["id"]})
        return next((item["containerID"] for item in current.get("targetImages", [])
                     if item.get("containerID") and item["containerID"] != fixture), None)
    replacement = until(candidate, "replacement identity reservation")
    tool(url, "yard_container_action", {"id": replacement, "action": "stop"}, "OPERATION_CONFLICT")
    docker("restart", yard)
    url = address(yard)
    until(lambda: ready(url), "Yard restart during Engine replacement")
    final = until(lambda: engine_result(accepted["id"]), "external worker after web restart")
    assert final["status"] == "succeeded" and final["targetID"] == "container:" + replacement, final
    assert final["targetImages"][0]["previousContainerID"] == fixture, final
    containers.remove(fixture)
    containers.append(replacement)
    fixture = replacement
    history = tool(url, "yard_job_history", {"target": "container:" + fixture})["jobs"]
    assert any(item["id"] == final["id"] for item in history)
    assert any(item == {"name": "MCP_TEST_VALUE", "value": "fixture"} for item in tool(url, "yard_container_environment", {"id": fixture})["environment"])
    print("PASS: C4 unchanged/recreate worker, new-target conflict, web restart, replacement identity/history and environment", flush=True)

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
        except (OSError, ValueError, AttributeError):
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
    pinned_ids = docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project=" + project)
    assert tool(url, "yard_project_pull", {"id": "compose:" + project})["succeeded"] == 1
    assert tool(url, "yard_container_pull", {"id": pinned_ids})["succeeded"] == 1
    assert docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project=" + project) == pinned_ids
    assert any(item["id"] == "compose:" + project and item["kind"] == "managed-compose"
               for item in tool(url, "yard_projects")["projects"])
    await_job(url, tool(url, "yard_compose_operation", {
        "name": project, "operation": "remove", "removeVolumes": False}))
    managed_names.remove(project)
    print("PASS: browser SSE receives MCP events; Compose preview, jobs, lifecycle, update and removal", flush=True)

    # C1: Compose's exit code is not a readiness result. Test healthchecks,
    # declared one-shot completion, named-volume and relative-bind preservation.
    acceptance = (root / "scripts/fixtures/managed-acceptance.yaml").read_text(encoding="utf-8")
    healthy_name = project + "-healthy"
    healthy = {"name": healthy_name, "source": {"kind": "paste", "yaml": acceptance},
               "variables": {}, "envFiles": {}, "mode": "new"}
    preview = tool(url, "yard_compose_preview", healthy)
    healthy["fingerprint"] = preview["fingerprint"]
    managed_names.append(healthy_name)
    volumes.append(healthy_name + "_sample-data")
    await_job(url, tool(url, "yard_compose_submit", healthy))
    health_id = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + healthy_name,
                       "--filter", "label=com.docker.compose.service=healthy")
    docker("exec", health_id, "sh", "-c", "echo fixture > /data/keep; echo fixture > /bind/keep")
    for operation in ("restart", "update"):
        await_job(url, tool(url, "yard_compose_operation", {
            "name": healthy_name, "operation": operation, "removeVolumes": False}))
        health_id = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + healthy_name,
                           "--filter", "label=com.docker.compose.service=healthy")
        assert docker("exec", health_id, "cat", "/data/keep", "/bind/keep") == "fixture\nfixture"

    unhealthy_name = project + "-unhealthy"
    unhealthy_yaml = ("services:\n  demo:\n    image: alpine:3.23\n    command: [sleep, '600']\n"
                      "    stop_grace_period: 1s\n    healthcheck:\n      test: [CMD, 'false']\n"
                      "      interval: 1s\n      timeout: 1s\n      retries: 1\n")
    unhealthy = {"name": unhealthy_name, "source": {"kind": "paste", "yaml": unhealthy_yaml},
                 "variables": {}, "envFiles": {}, "mode": "new"}
    unhealthy["fingerprint"] = tool(url, "yard_compose_preview", unhealthy)["fingerprint"]
    managed_names.append(unhealthy_name)
    failed = await_job(url, tool(url, "yard_compose_submit", unhealthy), "failed")
    assert "unhealthy" in failed["error"], failed
    original_ids = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + unhealthy_name)
    unchanged = await_job(url, tool(url, "yard_compose_operation", {
        "name": unhealthy_name, "operation": "update", "removeVolumes": False}))
    assert unchanged["outcome"] == "unchanged", unchanged
    assert docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + unhealthy_name) == original_ids
    print("PASS: healthy/one-shot deploy, unhealthy initial failure, unchanged image identity and data preservation", flush=True)

    # Adopt a fixture created by the host Compose CLI at its original directory.
    # The helper shares that exact path; no existing user stack is involved.
    adoption_name = project + "-adopt"
    adoption_local = base / "original"
    adoption_local.mkdir()
    (adoption_local / "data").mkdir()
    (adoption_local / "env").mkdir()
    (adoption_local / "data/keep").write_text("original-data", encoding="utf-8", newline="")
    env_text = "TOKEN=private-adoption-fixture\n"
    (adoption_local / "env/runtime.env").write_text(env_text, encoding="utf-8", newline="")
    adoption_yaml = ("services:\n  demo:\n    image: alpine:3.23\n    command: [sleep, '600']\n"
                     "    stop_grace_period: 1s\n    env_file: env/runtime.env\n"
                     "    volumes:\n      - ${BIND_PATH:-./data}:/bind\n")
    (adoption_local / "original.yml").write_text(adoption_yaml, encoding="utf-8", newline="")
    host_dir = settings["projectsBase"] + "/original"
    managed_names.append(adoption_name)
    docker("run", "--rm", "--label", "nox-yard.role=managed-helper",
           "--mount", "type=bind,source=" + host_dir + ",target=" + host_dir,
           "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
           "--workdir", host_dir, "--entrypoint", "docker", args.image,
           "compose", "-p", adoption_name, "-f", "original.yml", "up", "-d")
    until(lambda: any(item["id"] == "compose:" + adoption_name for item in
                     tool(url, "yard_projects")["projects"]), "external fixture inventory")
    adoption = {"name": adoption_name, "source": {"kind": "paste", "yaml": adoption_yaml},
                "variables": {}, "envFiles": {"env/runtime.env": env_text}, "mode": "adopt"}
    # An unprovided host .env could redirect binds on the next operation.
    (adoption_local / ".env").write_text("BIND_PATH=./different-data\n", encoding="utf-8", newline="")
    tool(url, "yard_compose_preview", adoption, "OPERATION_CONFLICT")
    (adoption_local / ".env").unlink()
    preview = tool(url, "yard_compose_preview", adoption)
    assert preview["projectDir"] == host_dir and preview["adoptionDir"] == host_dir, preview
    assert not preview["changes"], preview
    assert "private-adoption-fixture" not in json.dumps(preview)
    identifier = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + adoption_name)
    adoption["fingerprint"] = preview["fingerprint"]
    # A new matching file still changes the reviewed presence identity.
    (adoption_local / "compose.yml").write_text(adoption_yaml, encoding="utf-8", newline="")
    tool(url, "yard_compose_submit", adoption, "OPERATION_CONFLICT")
    (adoption_local / "compose.yml").write_text("services: {}\n", encoding="utf-8", newline="")
    tool(url, "yard_compose_preview", adoption, "OPERATION_CONFLICT")
    (adoption_local / "compose.yml").unlink()
    preview = tool(url, "yard_compose_preview", adoption)
    adoption["fingerprint"] = preview["fingerprint"]
    # An additional service invalidates the target before ownership is saved.
    extra = create("--label", "com.docker.compose.project=" + adoption_name,
                   "--label", "com.docker.compose.service=extra", "alpine:3.23", "true")
    tool(url, "yard_compose_submit", adoption, "OPERATION_CONFLICT")
    docker("rm", extra)
    containers.remove(extra)
    preview = tool(url, "yard_compose_preview", adoption)
    adoption["fingerprint"] = preview["fingerprint"]
    await_job(url, tool(url, "yard_compose_submit", adoption))
    assert docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + adoption_name) == identifier
    assert (adoption_local / "compose.yml").read_text(encoding="utf-8") == adoption_yaml
    assert (adoption_local / "env/runtime.env").read_text(encoding="utf-8") == env_text
    for operation in ("restart", "update"):
        await_job(url, tool(url, "yard_compose_operation", {
            "name": adoption_name, "operation": operation, "removeVolumes": False}))
        identifier = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + adoption_name)
        info = json.loads(docker("inspect", identifier))[0]
        assert any(mount["Source"] == host_dir + "/data" and mount["Destination"] == "/bind"
                   for mount in info["Mounts"]), info["Mounts"]
        assert docker("exec", identifier, "cat", "/bind/keep") == "original-data"
    print("PASS: original-directory adoption, masked comparison, stale files/targets and relative binds after restart/update", flush=True)

    # Missing Compose working-directory metadata requires a verified fallback.
    fallback_name = project + "-fallback"
    fallback_local = base / "fallback"
    (fallback_local / "data").mkdir(parents=True)
    (fallback_local / "env").mkdir()
    (fallback_local / "data/keep").write_text("fallback-data", encoding="utf-8", newline="")
    (fallback_local / "env/runtime.env").write_text(env_text, encoding="utf-8", newline="")
    fallback_dir = settings["projectsBase"] + "/fallback"
    managed_names.append(fallback_name)
    docker("network", "create", "--label", "com.docker.compose.project=" + fallback_name,
           "--label", "com.docker.compose.network=default",
           fallback_name + "_default")
    fallback_id = create("--name", fallback_name + "-demo", "--stop-timeout", "1",
        "--label", "com.docker.compose.project=" + fallback_name,
        "--label", "com.docker.compose.service=demo", "--env", "TOKEN=private-adoption-fixture",
        "--label", "com.docker.compose.container-number=1",
        "--label", "com.docker.compose.oneoff=False",
        "--label", "com.docker.compose.config-hash=fixture-original",
        "--network", fallback_name + "_default",
        "--mount", "type=bind,source=" + fallback_dir + "/data,target=/bind",
        "alpine:3.23", "sleep", "600")
    docker("start", fallback_id)
    until(lambda: any(item["id"] == "compose:" + fallback_name for item in
                     tool(url, "yard_projects")["projects"]), "fallback fixture inventory")
    fallback_yaml = adoption_yaml + "    labels:\n      nox-yard.mcp-smoke: '" + suffix + "'\n"
    fallback = dict(adoption, name=fallback_name, source={"kind": "paste", "yaml": fallback_yaml})
    fallback.pop("fingerprint", None)
    tool(url, "yard_compose_preview", fallback, "INVALID_PAYLOAD")
    fallback["projectDir"] = host_dir
    tool(url, "yard_compose_preview", fallback, "INVALID_PAYLOAD")
    assert not (adoption_local / "data/keep").read_text(encoding="utf-8") == "fallback-data"
    fallback["projectDir"] = fallback_dir
    preview = tool(url, "yard_compose_preview", fallback)
    assert preview["projectDir"] == fallback_dir and not preview["changes"], preview
    fallback["fingerprint"] = preview["fingerprint"]
    await_job(url, tool(url, "yard_compose_submit", fallback))
    assert docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project=" + fallback_name) == fallback_id
    await_job(url, tool(url, "yard_compose_operation", {
        "name": fallback_name, "operation": "update", "removeVolumes": False}))
    fallback_id = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + fallback_name)
    assert docker("exec", fallback_id, "cat", "/bind/keep") == "fallback-data"
    print("PASS: missing/wrong adoption directory rejected; explicit fallback preserves the original bind", flush=True)

    # C4: managed removal uses the same preview and protects shared volumes
    # even when deletion is explicitly selected. No host source is removed.
    shared_volume = healthy_name + "_sample-data"
    keeper = create("--mount", "type=volume,source="+shared_volume+",target=/data", "alpine:3.23", "sleep", "600")
    unselected = tool(url, "yard_project_remove_preview", {"id": "compose:"+healthy_name})
    assert all(item["action"] == "keep" for item in unselected["items"] if item["kind"] == "volume")
    selected = tool(url, "yard_project_remove_preview", {"id": "compose:"+healthy_name, "removeVolumes": True})
    assert selected["fingerprint"] != unselected["fingerprint"]
    assert any(item["kind"] == "volume" and item["id"] == shared_volume and item["action"] == "keep" for item in selected["items"])
    tool(url, "yard_compose_operation", {"name": healthy_name, "operation": "remove", "removeVolumes": True,
        "fingerprint": unselected["fingerprint"]}, "OPERATION_CONFLICT")
    await_job(url, tool(url, "yard_compose_operation", {"name": healthy_name, "operation": "remove", "removeVolumes": True,
        "fingerprint": selected["fingerprint"]}))
    docker("start", keeper)
    assert docker("exec", keeper, "cat", "/data/keep") == "fixture"
    assert (base / healthy_name / "compose.yml").is_file()
    assert not any(p["id"] == "compose:"+healthy_name for p in tool(url, "yard_projects")["projects"])
    print("PASS: C4 removal defaults, changed-choice rejection, shared-volume protection, retained source and managed metadata cleanup", flush=True)

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
    assert len(rpc(url, "tools/list", {})["tools"]) == 30
    def restart_job():
        history = tool(url, "yard_job_history", {"target": "container:" + yard})["jobs"]
        return next((job for job in history if job["operation"] == "restart" and job["status"] != "running"), None)
    durable = until(restart_job, "persisted self-restart result")
    assert durable["status"] == "succeeded" and durable["outcome"] == "verified", durable
    assert durable["workerID"] and durable["startedAt"] and durable["completedAt"] and durable["targetImages"], durable
    workers.add(durable["workerID"])
    assert tool(url, "yard_job", {"id": durable["id"]})["id"] == durable["id"]
    print("PASS: fingerprint protection, removal, queued self-restart, persisted session/settings and MCP recovery", flush=True)
    if args.arm64_image:
        serve(args.arm64_image, platform="linux/arm64")
        print("PASS: ARM64 runtime health and anonymous MCP tool discovery", flush=True)
finally:
    if events_response:
        events_response.close()
    for identifier in workers:
        subprocess.run(command + ["rm", "-f", identifier], capture_output=True)
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
