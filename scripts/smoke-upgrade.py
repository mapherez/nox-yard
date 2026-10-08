#!/usr/bin/env python3
"""Isolated v1.0.1 upgrade, password recovery, backup and restore acceptance.

Requires Docker, Python 3 and images for the current source and v1.0.1.
Creates only random, explicitly tracked fixtures; never uses an existing Yard.
Runs natively on a Linux Pi 5/amd64 host or through Linux Docker Desktop.
"""
import argparse
import hashlib
import json
import os
import pathlib
import secrets
import shutil
import subprocess
import time
import urllib.error
import urllib.request
from http.cookiejar import CookieJar

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
parser.add_argument("--legacy-image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = "upgrade-smoke-" + secrets.token_hex(6)
base = root / ".tmp" / name
base.mkdir(parents=True, mode=0o700)
projects = base / "projects"
projects.mkdir()
host_base = str(base.resolve()).replace("\\", "/")
if host_base[1:2] == ":":
    host_base = "/run/desktop/mnt/host/" + host_base[0].lower() + "/" + host_base[3:]
state = name + "-state"
app_volume = name + "-application"
containers = []
volumes = []
workers = set()
tools = name + ":tools"
password = secrets.token_urlsafe(24)
replacement = secrets.token_urlsafe(24)
(base / "passwords.json").write_text(json.dumps([password, replacement]), encoding="utf-8")
os.chmod(base / "passwords.json", 0o600)


def docker(*parts, input=None):
    result = subprocess.run(["docker", *parts], input=input, text=True, capture_output=True)
    if result.returncode:
        raise RuntimeError("Fixture Docker command failed: " + " ".join(parts[:2]) + "\n" + result.stderr)
    return result.stdout.strip()


def until(check, description, seconds=90):
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


def request(path, body=None, method=None, expected=200, authenticated=True):
    headers = {"Origin": url}
    if authenticated:
        headers["X-CSRF-Token"] = csrf
    if body is not None:
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url + path, data=None if body is None else json.dumps(body).encode(), headers=headers, method=method)
    try:
        response = opener.open(req, timeout=15)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        assert response.status == expected, "Unexpected HTTP status for " + path + ": " + str(response.status)
        content = response.read()
        return json.loads(content) if content else None


def helper(code, extra=()):
    return docker("run", "--rm", "--label", "nox-yard.acceptance=" + name,
                  "--mount", "type=volume,source=" + state + ",target=/data",
                  "--mount", "type=bind,source=" + str(base.resolve()) + ",target=/fixture",
                  *extra, tools, "python", "-c", code)


def serve(image):
    global url
    identifier = docker("create", "--name", name + "-yard-" + str(len(containers)),
        "--label", "nox-yard.acceptance=" + name, "--publish", "127.0.0.1::8080",
        "--mount", "type=volume,source=" + state + ",target=/data",
        "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
        "--env", "NOX_DATA_DIR=/data", "--env", "TZ=Europe/Lisbon", image)
    containers.append(identifier)
    docker("start", identifier)
    info = json.loads(docker("inspect", identifier))[0]
    url = "http://127.0.0.1:" + info["NetworkSettings"]["Ports"]["8080/tcp"][0]["HostPort"]
    until(lambda: request("/healthz", authenticated=False), "Yard readiness")
    return identifier


def completed_job(identifier):
    job = request("/api/managed/jobs/" + identifier)
    if job.get("workerID"):
        workers.add(job["workerID"])
    return job if job["status"] != "running" else None


def assert_data():
    ids = docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project=" + name).splitlines()
    assert ids == original_ids, "Upgrade or restore replaced application containers"
    assert docker("exec", ids[0], "cat", "/volume/keep") == "volume-before-upgrade"
    assert docker("exec", ids[0], "cat", "/bind/keep") == "bind-before-upgrade"
    for relative, digest in source_hashes.items():
        assert hashlib.sha256((projects / name / relative).read_bytes()).hexdigest() == digest, "Project source or env file changed"


try:
    for image in (args.image, args.legacy_image):
        info = json.loads(docker("image", "inspect", image))[0]
        assert info["Os"] == "linux", "Acceptance requires Linux images"
    assert json.loads(docker("image", "inspect", args.image))[0]["Architecture"] == json.loads(docker("image", "inspect", args.legacy_image))[0]["Architecture"], "Use the same native architecture for both images"
    (base / "Dockerfile").write_text("FROM " + args.image + " AS yard\nFROM python:3.13-alpine\nCOPY --from=yard /usr/local/bin/nox-yard /usr/local/bin/nox-yard\nENV NOX_DATA_DIR=/data\n", encoding="utf-8")
    docker("build", "-t", tools, str(base))
    docker("volume", "create", "--label", "nox-yard.acceptance=" + name, state)
    volumes.append(state)
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(CookieJar()))
    csrf = ""
    old = serve(args.legacy_image)
    assert request("/api/bootstrap", authenticated=False)["needsSetup"]
    csrf = request("/api/setup", {"username": "owner", "password": password}, expected=201, authenticated=False)["csrfToken"]
    request("/api/managed/settings", {"projectsBase": host_base + "/projects"}, method="PUT")
    (projects / name / "files").mkdir(parents=True)
    (projects / name / "files/keep").write_text("bind-before-upgrade", encoding="utf-8")
    source = ("services:\n  demo:\n    image: ${APP_IMAGE}\n    command: [sleep, '600']\n    stop_grace_period: 1s\n    env_file: env/runtime.env\n    labels:\n      nox-yard.acceptance: " + name + "\n    volumes:\n      - data:/volume\n      - ./files:/bind\nvolumes:\n  data:\n    name: " + app_volume + "\n    labels:\n      nox-yard.acceptance: " + name + "\nnetworks:\n  default:\n    labels:\n      nox-yard.acceptance: " + name + "\n")
    input = {"name": name, "source": {"kind": "paste", "yaml": source}, "variables": {"APP_IMAGE": "alpine:3.23"}, "envFiles": {"env/runtime.env": "TOKEN=private-upgrade-fixture\n"}, "mode": "new"}
    docker("pull", "alpine:3.23")
    preview = request("/api/managed/preview", input)
    input["fingerprint"] = preview["fingerprint"]
    job = request("/api/managed/deploy", input, expected=202)
    assert until(lambda: completed_job(job["id"]), "v1.0.1 deployment")["status"] == "succeeded"
    volumes.append(app_volume)
    original_ids = docker("ps", "-aq", "--no-trunc", "--filter", "label=com.docker.compose.project=" + name).splitlines()
    assert len(original_ids) == 1
    docker("exec", original_ids[0], "sh", "-c", "printf volume-before-upgrade > /volume/keep")
    source_hashes = {p: hashlib.sha256((projects / name / p).read_bytes()).hexdigest() for p in ("compose.yml", "env/runtime.env", "files/keep")}
    docker("stop", old)
    # Add representative interrupted and pre-host-directory legacy records to
    # the actual schema initialized and exercised by the released source.
    helper("import sqlite3,time; db=sqlite3.connect('/data/nox-yard.sqlite'); assert db.execute('PRAGMA user_version').fetchone()[0]==6; now=int(time.time()); db.execute(\"INSERT INTO managed_jobs VALUES (?,?,?,?,?,?,?)\", ('legacy-interrupted','" + name + "','update','running','',now,0)); db.execute(\"INSERT INTO managed_projects(name,source_kind,yaml,variables_json,created_at,updated_at) VALUES(?,?,?,?,?,?)\", ('" + name + "-missing','paste','services: {}','{}',now,now)); db.commit(); db.close()")
    # Stop writers before taking a consistent three-part backup.
    helper("import sqlite3,shutil,pathlib; source=sqlite3.connect('/data/nox-yard.sqlite'); dest=sqlite3.connect('/fixture/backup.sqlite'); source.backup(dest); assert dest.execute('PRAGMA integrity_check').fetchone()[0]=='ok'; dest.close(); source.close(); shutil.copytree('/fixture/projects','/fixture/backup-projects')")
    helper("import shutil; shutil.copytree('/application','/fixture/backup-application')", ("--mount", "type=volume,source=" + app_volume + ",target=/application,readonly"))
    print("PASS: real v1.0.1 setup/deployment, schema 6, consistent SQLite/source/env/application backup", flush=True)
    current = serve(args.image)
    assert request("/api/bootstrap")["authenticated"], "Upgrade lost session"
    assert_data()
    history = until(lambda: next((j for j in request('/api/jobs?target=compose:' + name) if j['id']=='legacy-interrupted' and j['status']=='failed'), None), "interrupted legacy reconciliation")
    assert history["outcome"] == "recovery_required" and history["stage"] == "awaiting_recovery", "Interrupted legacy work was claimed successful"
    # Fixture originals and source hashes were reviewed above; no worker exists.
    request("/api/jobs/legacy-interrupted/recovery", {"confirm": True, "updatedAt": history["updatedAt"]})
    missing = request("/api/managed/projects/" + name + "-missing/operations", {"operation": "update", "removeVolumes": False}, expected=202)
    refused = until(lambda: completed_job(missing["id"]), "missing-directory refusal")
    assert refused["status"] == "failed" and "directory" in refused.get("error", "").lower(), "Legacy missing directory did not fail safely"
    assert not docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + name + "-missing"), "Missing directory created host resources"
    schedule = request("/api/projects/compose:" + name + "/schedule")
    assert not schedule["enabled"], "Upgrade enabled automatic updates"
    updated = request("/api/managed/projects/" + name + "/operations", {"operation": "update", "removeVolumes": False}, expected=202)
    outcome = until(lambda: completed_job(updated["id"]), "upgraded unchanged update")
    assert outcome["status"] == "succeeded" and outcome["outcome"] == "unchanged"
    assert_data()
    print("PASS: schema 6 to 8, account/session/project/history persistence, interrupted-job recovery, missing-directory refusal, default-off and unchanged update", flush=True)
    # CLI must refuse piped credentials without modifying authentication state.
    denied = subprocess.run(["docker", "run", "--rm", "--mount", "type=volume,source=" + state + ",target=/data", args.image, "nox-yard", "reset-admin-password"], text=True, capture_output=True)
    assert denied.returncode != 0 and "interactive terminal" in denied.stderr
    assert request("/api/bootstrap")["authenticated"]
    # Run the actual CLI with a Linux pseudo-terminal; verify no password echo.
    reset = r"""import pty,os,select,subprocess,time,json
secret=json.load(open('/fixture/passwords.json'))[1]
master,slave=pty.openpty()
process=subprocess.Popen(['nox-yard','reset-admin-password'],stdin=slave,stdout=slave,stderr=slave)
os.close(slave); output=b''
try:
 for prompt in (b'New password: ',b'Confirm password: '):
  deadline=time.monotonic()+15
  while prompt not in output:
   assert time.monotonic()<deadline, 'Password prompt missing'
   if select.select([master],[],[],0.2)[0]: output+=os.read(master,4096)
  os.write(master,(secret+'\n').encode())
 assert process.wait(timeout=15)==0, 'Interactive reset failed'
 while select.select([master],[],[],0.1)[0]:
  try: output+=os.read(master,4096)
  except OSError: break
 assert secret.encode() not in output, 'Password was echoed'
 assert b'All sessions have been signed out.' in output, 'Reset not confirmed'
finally:
 if process.poll() is None: process.kill();process.wait()
 os.close(master)
"""
    helper(reset)
    assert not request("/api/bootstrap")["authenticated"]
    request("/api/login", {"username": "owner", "password": password}, expected=401, authenticated=False)
    csrf = request("/api/login", {"username": "owner", "password": replacement}, authenticated=False)["csrfToken"]
    docker("restart", current)
    port = json.loads(docker("inspect", current))[0]["NetworkSettings"]["Ports"]["8080/tcp"][0]["HostPort"]
    url = "http://127.0.0.1:" + port
    until(lambda: request("/healthz"), "restart after password reset")
    assert request("/api/bootstrap")["authenticated"]
    print("PASS: actual interactive CLI reset without echo, non-TTY refusal, session revocation and persisted replacement password", flush=True)
    docker("stop", current)
    # Corrupt only our disposable fixture data, then restore all backup parts.
    helper("import pathlib; pathlib.Path('/application/keep').write_text('after-backup'); pathlib.Path('/fixture/projects/" + name + "/files/keep').write_text('after-backup')", ("--mount", "type=volume,source=" + app_volume + ",target=/application",))
    helper("import pathlib,shutil; [p.unlink() for p in pathlib.Path('/data').glob('nox-yard.sqlite*')]; shutil.copy2('/fixture/backup.sqlite','/data/nox-yard.sqlite'); shutil.copytree('/fixture/backup-projects','/fixture/projects',dirs_exist_ok=True); [p.unlink() for p in pathlib.Path('/application').iterdir()]; [shutil.copy2(p,'/application/'+p.name) for p in pathlib.Path('/fixture/backup-application').iterdir()]", ("--mount", "type=volume,source=" + app_volume + ",target=/application",))
    restored = serve(args.image)
    request("/api/login", {"username": "owner", "password": replacement}, expected=401, authenticated=False)
    csrf = request("/api/login", {"username": "owner", "password": password}, authenticated=False)["csrfToken"]
    assert request("/api/bootstrap")["authenticated"]
    assert_data()
    helper("import sqlite3; db=sqlite3.connect('/data/nox-yard.sqlite'); assert db.execute('PRAGMA user_version').fetchone()[0]==8; assert db.execute('PRAGMA integrity_check').fetchone()[0]=='ok'; db.close()")
    print("PASS: restore of baseline database, source/env files, relative bind and named-volume data; repeat migration and original credentials", flush=True)
finally:
    for identifier in workers:
        subprocess.run(["docker", "rm", "-f", identifier], capture_output=True)
    identifiers = docker("ps", "-aq", "--filter", "label=nox-yard.acceptance=" + name).splitlines()
    if identifiers:
        docker("rm", "-f", *identifiers)
    for kind in ("network", "volume"):
        identifiers = docker(kind, "ls", "-q", "--filter", "label=nox-yard.acceptance=" + name).splitlines()
        if identifiers:
            docker(kind, "rm", *identifiers)
    subprocess.run(["docker", "image", "rm", tools], capture_output=True)
    assert base.resolve().is_relative_to((root / ".tmp").resolve()) and base.name == name
    shutil.rmtree(base)
