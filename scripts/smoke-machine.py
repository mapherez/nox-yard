#!/usr/bin/env python3
"""Validate MCP security and network recreation against a disposable Yard.

Requires a prebuilt image and local Linux Docker Engine/Docker Desktop.
Never contacts an installed Yard; every resource has a generated fixture name.
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
import urllib.error
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = "machine-smoke-" + secrets.token_hex(6)
base = root / ".tmp" / name
(base / "data").mkdir(parents=True)
host = str(base).replace("\\", "/")
if len(host) > 2 and host[1] == ":":
    host = "/run/desktop/mnt/host/" + host[0].lower() + "/" + host[3:]

def port():
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]

web_port, mcp_port = port(), port()
origin = "http://127.0.0.1:" + str(web_port)
mcp_origin = "http://127.0.0.1:" + str(mcp_port)
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ""
token = ""

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

def request(url, path, body=None, method=None, bearer=None, browser=False):
    headers = {"Content-Type": "application/json", "Accept": "application/json, text/event-stream"}
    if browser:
        headers.update({"Origin": origin, "X-CSRF-Token": csrf})
    if bearer is not None:
        headers["Authorization"] = "Bearer " + bearer
    req = urllib.request.Request(url + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
    try:
        response = (opener.open if browser else urllib.request.urlopen)(req, timeout=30)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read()
        return response.status, json.loads(raw) if raw else None

def api(path, body=None, method=None):
    status, response = request(origin, path, body, method, browser=True)
    assert 200 <= status < 300, (path, status, response)
    return response

def rpc(method, params, bearer=None):
    status, response = request(mcp_origin, "/mcp", {"jsonrpc": "2.0", "id": 1, "method": method, "params": params}, bearer=bearer)
    assert status == 200 and "result" in response, (status, response)
    return response["result"]

def wait(check, label, timeout=100):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            result = check()
            if result:
                return result
        except Exception as error:
            last = error
        time.sleep(.4)
    raise RuntimeError(label + (": " + str(last) if last else ""))

def current():
    identifiers = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + name,
                         "--filter", "label=com.docker.compose.service=nox-yard").splitlines()
    assert len(identifiers) == 1, "fixture must contain one Yard"
    return json.loads(docker("inspect", identifiers[0]))[0]

def save(**changes):
    settings = api("/api/machine")["settings"]
    settings.update(changes)
    return api("/api/machine", settings, "PUT")["settings"]

def finished(identifier):
    job = api("/api/jobs/" + identifier)
    return job if job["status"] != "running" else None

source = f'''services:
  nox-yard:
    image: {json.dumps(args.image)}
    init: true
    labels: {{nox-yard.acceptance: {name}}}
    environment: {{NOX_DATA_DIR: /data, NOX_LISTEN_ADDR: ':8080', NOX_MCP_LISTEN_ADDR: ':8081'}}
    ports: ['127.0.0.1:{web_port}:8080', '127.0.0.1:{mcp_port}:8081']
    volumes: ['./data:/data', '/var/run/docker.sock:/var/run/docker.sock']
    healthcheck:
      test: ['CMD-SHELL', 'wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1']
      interval: 1s
      timeout: 1s
      retries: 2
    stop_grace_period: 1s
'''
try:
    (base / "compose.yaml").write_text(source, newline="")
    # No .env initially: rollback must also handle newly created configuration.
    compose("up", "-d", "--no-build", "--pull", "never")
    wait(lambda: request(origin, "/healthz")[0] == 200, "Yard startup")
    assert request(mcp_origin, "/mcp", {})[0] == 503
    assert request(origin, "/mcp", {})[0] == 404
    assert request(mcp_origin, "/api/bootstrap")[0] == 404
    api("/api/setup", {"username": "fixture", "password": "fixture-password-machine"})
    csrf = api("/api/bootstrap")["csrfToken"]
    assert not api("/api/machine")["settings"]["mcpEnabled"]
    token = api("/api/machine/token", {"confirm": True})["token"]
    save(mcpEnabled=True, apiEnabled=True)
    assert request(mcp_origin, "/mcp", {})[0] == 401
    tools = rpc("tools/list", {}, token)["tools"]
    assert tools and all(tool["annotations"]["readOnlyHint"] for tool in tools)
    assert not any(tool["name"] in ("yard_container_environment", "yard_container_logs") for tool in tools)
    result = rpc("tools/call", {"name": "yard_project_configuration_read", "arguments": {"id": "compose:" + name, "reveal": True}}, token)
    assert result["isError"] and result["structuredContent"]["code"] == "FORBIDDEN", result
    assert request(origin, "/v1/projects", bearer=token)[0] == 200
    assert request(origin, "/v1/projects/compose:" + name + "/actions", {"action": "stop"}, bearer=token)[0] == 403
    previous = token
    token = api("/api/machine/token", {"confirm": True})["token"]
    assert request(mcp_origin, "/mcp", {}, bearer=previous)[0] == 401
    assert request(origin, "/v1/projects", bearer=previous)[0] == 401
    save(authMode="anonymous")
    assert rpc("tools/list", {})["tools"]
    assert request(origin, "/v1/projects")[0] == 401
    save(authMode="token", write=True, secrets=True)
    assert len(rpc("tools/list", {}, token)["tools"]) == 35
    print("PASS: opt-in, separate routes, shared token rotation, inspection scopes and anonymous isolation", flush=True)

    original = current()["Id"]
    requested_port = port()
    requested_web_port = port()
    desired = api("/api/machine")["settings"]["network"]
    desired["mcp"]["port"] = requested_port
    desired["yard"]["port"] = requested_web_port
    settings = save(network=desired)
    assert api("/api/machine")["networkStatus"]["pending"]
    assert current()["Id"] == original
    accepted = api("/api/machine/apply", {"revision": settings["revision"], "confirm": True})
    origin = "http://127.0.0.1:" + str(requested_web_port)
    result = wait(lambda: finished(accepted["id"]), "network worker", 160)
    assert result["status"] == "succeeded" and result["outcome"] == "applied", result
    mcp_port = requested_port
    mcp_origin = "http://127.0.0.1:" + str(mcp_port)
    assert current()["Id"] != original
    assert not api("/api/machine")["networkStatus"]["pending"]
    assert len(rpc("tools/list", {}, token)["tools"]) == 35
    assert "NOX_MCP_PORT" in (base / ".env").read_text()
    compose("up", "-d", "--no-build", "--pull", "never")
    wait(lambda: request(origin, "/healthz")[0] == 200, "host Compose restart")
    assert not api("/api/machine")["networkStatus"]["pending"]
    assert request(origin, "/v1/projects", bearer=token)[0] == 200
    print("PASS: durable network worker, /healthz and published-port verification, host Compose persistence", flush=True)

    # Fail only the replacement's healthcheck. The recorded old runtime remains
    # healthy and must be restored with its exact previous port bindings.
    file = base / "compose.yaml"
    file.write_text(file.read_text().replace("/healthz || exit 1", "/healthz && exit 1"), newline="")
    desired = api("/api/machine")["settings"]["network"]
    desired["mcp"]["port"] = port()
    settings = save(network=desired)
    accepted = api("/api/machine/apply", {"revision": settings["revision"], "confirm": True})
    result = wait(lambda: finished(accepted["id"]), "failed network recovery", 190)
    assert result["status"] == "failed" and result["rollback"] == "restored", result
    assert api("/api/machine")["networkStatus"]["pending"]
    assert rpc("tools/list", {}, token)["tools"]
    public = json.dumps(api("/api/jobs?target=compose:" + name))
    assert token not in public and previous not in public
    assert len(api("/api/machine/token/history")) == 2
    print("PASS: failed health verification restores runtime/ports, pending settings and secret-free history", flush=True)
finally:
    try:
        for container in docker("ps", "-aq", "--filter", "label=nox-yard.role=operation-worker").splitlines():
            # Yard's observer can remove a completed worker after this listing.
            inspection = subprocess.run(["docker", "inspect", container], capture_output=True, text=True)
            if inspection.returncode:
                if "no such" in inspection.stderr.lower():
                    continue
                raise RuntimeError("Fixture worker inspection failed: " + inspection.stderr)
            info = json.loads(inspection.stdout)[0]
            if any(mount.get("Source") in (str(base / "data"), host + "/data") for mount in info["Mounts"]):
                removal = subprocess.run(["docker", "rm", "-f", container], capture_output=True, text=True)
                if removal.returncode and "no such" not in removal.stderr.lower():
                    raise RuntimeError("Fixture worker cleanup failed: " + removal.stderr)
    finally:
        try:
            compose("down", "--volumes", "--remove-orphans", "--timeout", "1")
        finally:
            resolved = base.resolve()
            assert resolved.is_relative_to((root / ".tmp").resolve()) and resolved.name == name
            shutil.rmtree(resolved, ignore_errors=True)
