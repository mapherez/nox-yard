"""Disposable real-Docker C2 acceptance; never operates on user stacks."""
import argparse
import json
import pathlib
import secrets
import shutil
import subprocess
import time
import urllib.error
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument("--image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
suffix = secrets.token_hex(6)
name = "jobs-smoke-" + suffix
base = root / ".tmp" / name
base.mkdir(parents=True)
project = name + "-app"
directory = base / "projects" / project
directory.mkdir(parents=True)
volume = name + "-data"
image = name + ":fixture"
yard = None
workers = set()
job_ids = set()
url = ""

def docker(*parts):
    completed = subprocess.run(["docker", *parts], capture_output=True, text=True)
    if completed.returncode:
        raise RuntimeError("Docker fixture command failed: " + " ".join(parts[:3]) + "\n" + completed.stderr)
    return completed.stdout.strip()

def until(check, label, seconds=120):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        try:
            value = check()
            if value:
                return value
        except (urllib.error.URLError, ConnectionError, TimeoutError):
            pass
        time.sleep(0.3)
    raise AssertionError("Timed out: " + label)

def request(path, payload=None):
    data = json.dumps(payload).encode() if payload is not None else None
    req = urllib.request.Request(url + path, data=data, headers={"Content-Type": "application/json", "Accept": "application/json, text/event-stream", "MCP-Protocol-Version": "2025-11-25"})
    with urllib.request.urlopen(req, timeout=10) as response:
        return json.load(response)

def tool(method, payload=None, error=None):
    result = request("/mcp", {"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": method, "arguments": payload or {}}})["result"]
    value = result.get("structuredContent")
    if value is None:
        value = json.loads(next(block["text"] for block in result["content"] if block["type"] == "text"))
    if error:
        assert result.get("isError") and value["code"] == error, value
    else:
        assert not result.get("isError"), value
    return value

def address():
    global url
    info = json.loads(docker("inspect", yard))[0]
    url = "http://127.0.0.1:" + info["NetworkSettings"]["Ports"]["8080/tcp"][0]["HostPort"]
    until(lambda: request("/healthz"), "Yard health")

def job(identifier):
    job_ids.add(identifier)
    value = tool("yard_job", {"id": identifier})
    if value.get("workerID"):
        workers.add(value["workerID"])
    return value

def submit(operation):
    accepted = tool("yard_compose_operation", {"name": project, "operation": operation, "removeVolumes": False})
    job_ids.add(accepted["id"])
    return accepted

def finished(identifier, expected="succeeded"):
    current = until(lambda: (value if (value := job(identifier))["status"] != "running" else None), "job result")
    assert current["status"] == expected, current
    return current

def ids():
    return docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project=" + project).splitlines()

def interrupt_web(identifier):
    before = job(identifier)
    docker("kill", yard)
    docker("start", yard)
    address()
    after = job(identifier)
    assert after["status"] == "running" and after["workerID"] == before["workerID"], after
    tool("yard_compose_operation", {"name": project, "operation": "restart", "removeVolumes": False}, "OPERATION_CONFLICT")
    tool("yard_project_action", {"id": "compose:" + project, "action": "restart"}, "OPERATION_CONFLICT")
    if ids():
        tool("yard_container_action", {"id": ids()[0], "action": "restart"}, "OPERATION_CONFLICT")

try:
    # Gate Compose only in a derived fixture image; production has no test hooks.
    (base / "docker-wrapper").write_text("""#!/bin/sh
operation=''
for argument do case "$argument" in pull|up) operation="$argument";; esac; done
gate="$PWD/.hold-$operation"
if [ "$operation" = pull ] && [ -f "$gate" ]; then
  touch "$PWD/.entered-pull"
  while [ -f "$gate" ]; do sleep 0.1; done
fi
/usr/bin/docker "$@"
result=$?
if [ "$operation" = up ] && [ -f "$gate" ]; then
  touch "$PWD/.entered-up"
  while [ -f "$gate" ]; do sleep 0.1; done
fi
exit "$result"
""", newline="")
    (base / "Dockerfile").write_text("FROM " + args.image + "\nCOPY docker-wrapper /usr/local/bin/docker\nRUN chmod 755 /usr/local/bin/docker\n", newline="")
    docker("build", "-t", image, str(base))
    docker("volume", "create", "--label", "nox-yard.jobs-smoke=" + suffix, volume)
    yard = docker("create", "--name", name, "--label", "nox-yard.jobs-smoke=" + suffix,
        "--publish", "127.0.0.1::8080", "--mount", "type=volume,source=" + volume + ",target=/data",
        "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
        "--env", "NOX_DATA_DIR=/data", image)
    docker("start", yard)
    address()
    tool("yard_projects_settings_set", {"projectsBase": str(base / "projects").replace("\\", "/")})
    (directory / "data").mkdir()
    (directory / "data" / "ready").touch()
    yaml = """services:
  web:
    image: alpine:3.23
    command: [sh, -c, 'exec sleep 600']
    environment: {SECRET_FIXTURE_VALUE: should-not-appear-in-job-history}
    volumes: ['./data:/bind']
    healthcheck:
      test: [CMD-SHELL, 'test -f /bind/ready']
      interval: 1s
      timeout: 1s
      retries: 120
"""
    initial = {"name": project, "mode": "new", "source": {"kind": "paste", "yaml": yaml}, "variables": {}, "envFiles": {}}
    initial["fingerprint"] = tool("yard_compose_preview", initial)["fingerprint"]
    (directory / ".hold-pull").touch()
    accepted = tool("yard_compose_submit", initial)
    identifier = accepted["id"]
    job_ids.add(identifier)
    until(lambda: (directory / ".entered-pull").exists(), "pull gate")
    assert job(identifier)["stage"] == "pulling"
    interrupt_web(identifier)
    (directory / ".hold-pull").unlink()
    result = finished(identifier)
    assert result.get("targetImages") and len(ids()) == 1
    print("PASS: web interruption during pull retains the worker, ownership and final result", flush=True)

    (directory / ".hold-up").touch()
    accepted = submit("update")
    until(lambda: (directory / ".entered-up").exists(), "replacement gate")
    replaced = ids()
    assert job(accepted["id"])["stage"] == "replacing"
    interrupt_web(accepted["id"])
    (directory / ".hold-up").unlink()
    result = finished(accepted["id"])
    assert ids() == replaced and len(replaced) == 1 and result.get("sourceImages") and result.get("targetImages")
    print("PASS: web interruption after replacement does not launch a duplicate replacement", flush=True)

    (directory / "data" / "ready").unlink()
    accepted = submit("update")
    until(lambda: job(accepted["id"])["stage"] == "verifying", "verification gate")
    replaced = ids()
    interrupt_web(accepted["id"])
    (directory / "data" / "ready").touch()
    finished(accepted["id"])
    assert ids() == replaced
    print("PASS: verification continues across web restart and reports a verified outcome", flush=True)

    (directory / "data" / "ready").unlink()
    accepted = submit("update")
    until(lambda: job(accepted["id"])["stage"] == "verifying", "lost worker verification")
    current = job(accepted["id"])
    replaced = ids()
    docker("kill", current["workerID"])
    result = finished(accepted["id"], "failed")
    assert result["outcome"] == "recovery_required" and ids() == replaced
    tool("yard_compose_operation", {"name": project, "operation": "update", "removeVolumes": False}, "OPERATION_CONFLICT")
    tool("yard_container_action", {"id": replaced[0], "action": "restart"}, "OPERATION_CONFLICT")
    tool("yard_job_recovery_acknowledge", {"id": result["id"], "updatedAt": result["updatedAt"], "confirm": False}, "INVALID_PAYLOAD")
    tool("yard_job_recovery_acknowledge", {"id": result["id"], "updatedAt": result["updatedAt"] - 1, "confirm": True}, "OPERATION_CONFLICT")
    acknowledged = tool("yard_job_recovery_acknowledge", {"id": result["id"], "updatedAt": result["updatedAt"], "confirm": True})
    assert acknowledged["outcome"] == "recovery_acknowledged" and ids() == replaced
    (directory / "data" / "ready").touch()
    finished(submit("restart")["id"])
    print("PASS: vanished worker retains ownership; reviewed recovery releases it without replay or secret exposure", flush=True)

    accepted = submit("update")
    until(lambda: job(accepted["id"])["stage"] == "verifying", "completed command before lost worker")
    current = job(accepted["id"])
    replaced = ids()
    docker("kill", current["workerID"])
    result = finished(accepted["id"])
    assert result["outcome"] == "reconciled" and ids() == replaced, result
    print("PASS: lost worker after a healthy replacement is verified without repeating Compose", flush=True)

    (directory / ".entered-pull").unlink()
    (directory / ".hold-pull").touch()
    accepted = submit("update")
    until(lambda: (directory / ".entered-pull").exists(), "child helper gate")
    current = job(accepted["id"])
    docker("kill", current["workerID"])
    time.sleep(2)
    assert job(accepted["id"])["status"] == "running"
    tool("yard_project_action", {"id": "compose:" + project, "action": "restart"}, "OPERATION_CONFLICT")
    (directory / ".hold-pull").unlink()
    result = finished(accepted["id"], "failed")
    assert result["outcome"] == "recovery_required" and ids() == replaced
    tool("yard_job_recovery_acknowledge", {"id": result["id"], "updatedAt": result["updatedAt"], "confirm": True})
    history = tool("yard_job_history", {"target": "compose:" + project})["jobs"]
    assert len(history) == 7 and all("payload" not in item and "owner" not in item for item in history)
    assert "should-not-appear-in-job-history" not in json.dumps(history)
    print("PASS: a live child helper retains ownership after its parent exits; no replacement is replayed", flush=True)
finally:
    # Cleanup is restricted to recorded IDs and this random fixture directory.
    for identifier in workers:
        subprocess.run(["docker", "rm", "-f", identifier], capture_output=True)
    remaining = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project)
    if remaining:
        docker("rm", "-f", *remaining.splitlines())
    for identifier in job_ids:
        helpers = docker("ps", "-aq", "--filter", "label=nox-yard.job=" + identifier)
        if helpers:
            docker("rm", "-f", *helpers.splitlines())
    networks = docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + project)
    if networks:
        docker("network", "rm", *networks.splitlines())
    if yard:
        subprocess.run(["docker", "rm", "-f", yard], capture_output=True)
    subprocess.run(["docker", "volume", "rm", volume], capture_output=True)
    subprocess.run(["docker", "image", "rm", image], capture_output=True)
    assert base.resolve().is_relative_to((root / ".tmp").resolve()) and base.name == name
    shutil.rmtree(base)
