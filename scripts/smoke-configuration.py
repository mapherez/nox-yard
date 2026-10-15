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
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ""

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
    preview = api("/api/recreate/preview", {"id": "compose:" + name, "operation": "recreate"})
    job = api("/api/recreate/submit", {"id": "compose:" + name, "operation": "recreate", "confirm": True,
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
    ports: ['127.0.0.1:{port}:8080']
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
    target = "/api/projects/compose:" + name
    configuration = api(target + "/configuration/read", {"reveal": True})
    assert configuration["available"] and configuration["state"] == "ready"
    assert any(file["path"] == "compose.yaml" for file in configuration["files"])
    hidden = api(target + "/configuration/read", {"reveal": False})
    assert all(not file["content"] for file in hidden["files"])
    first = current()
    api(target + "/configuration/save", {"fingerprint": configuration["fingerprint"], "service": "nox-yard",
                                          "variable": "NOX_FILE_TEST", "content": "edited$private-value"})
    assert current()["Id"] == first["Id"], "Save must not replace containers"
    wait(lambda: current()["State"].get("Health", {}).get("Status") == "healthy", "fixture not healthy")
    schedule = api(target + "/schedule")
    assert schedule["time"] == "03:00" and schedule["eligible"] and not schedule["enabled"]
    api(target + "/schedule", {"enabled": True, "time": "03:15"}, "PUT")
    result = recreate()
    assert result["status"] == "succeeded", result
    second = current()
    assert second["Id"] != first["Id"] and "NOX_FILE_TEST=edited$private-value" in second["Config"]["Env"]
    assert api(target + "/schedule")["time"] == "03:15"
    assert api(target + "/schedule")["enabled"]
    print("PASS: inline env save uses .env; independent Yard Recreate applies it and preserves sessions/daily schedule", flush=True)

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
