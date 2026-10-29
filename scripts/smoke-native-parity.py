#!/usr/bin/env python3
"""Compare native Compose and Yard on isolated, disposable projects.

No registry pulls, host pruning or existing workloads are used. The supplied
Yard image must already exist. Every created resource is recorded and removed.
"""
from smoke_support import trace_docker, cleanup_fixture
from collections import Counter
import argparse
import datetime
import difflib
import json
import pathlib
import secrets
import socket
import subprocess
import time
import urllib.request
from machine_smoke import bootstrap_mcp

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = "native-parity-" + secrets.token_hex(6)
base = root / ".tmp" / name
base.mkdir(parents=True)
containers, projects, volumes, networks = [], [], [], []
manifest = {"fixture": name, "containers": containers, "projects": projects,
            "volumes": volumes, "networks": networks, "images": [], "checks": []}
report = root / ".tmp" / (name + "-results.json")


def record():
    report.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")


@trace_docker(name, base)
def docker(*parts):
    result = subprocess.run(["docker", *parts], capture_output=True, text=True)
    if result.returncode:
        raise RuntimeError("Fixture Docker command failed: " + " ".join(parts[:3]) + "\n" + result.stderr)
    return result.stdout.strip()


def port():
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        return probe.getsockname()[1]


def wait(condition, label, timeout=150):
    deadline = time.monotonic() + timeout
    last = None
    while time.monotonic() < deadline:
        try:
            value = condition()
            if value:
                return value
        except Exception as error:
            last = error
        time.sleep(.3)
    raise RuntimeError(label + ": " + str(last))


def compose(project, *operation, files=None, profiles=()):
    directory = str(base / project)
    command = ["run", "--rm", "--network", "none", "--label", "nox-yard.acceptance=" + name,
               "--mount", "type=bind,source=" + str(base) + ",target=" + str(base),
               "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
               "--entrypoint", "docker", args.image, "compose", "-p", project, "--project-directory", directory]
    for file in files or ["compose.yaml"]:
        command += ["-f", directory + "/" + file]
    for profile in profiles:
        command += ["--profile", profile]
    return docker(*command, *operation)


def runtime(project):
    ids = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).splitlines()
    return json.loads(docker("inspect", *ids)) if ids else []


def semantic(project):
    result = {}
    for item in runtime(project):
        service = item["Config"]["Labels"]["com.docker.compose.service"]
        if service == "orphan":
            continue
        config, host = item["Config"], item["HostConfig"]
        mounts = sorted((m["Type"], m["Destination"], m["RW"],
                         m.get("Name", "").replace(project, "PROJECT") if m["Type"] == "volume" else
                         m.get("Source", "").removeprefix("/host_mnt").replace(str(base / project), "PROJECT")) for m in item["Mounts"])
        result.setdefault(service, []).append({"image": item["Image"], "running": item["State"]["Running"],
            "exit": item["State"]["ExitCode"], "environment": sorted(config["Env"]), "command": config["Cmd"],
            "entrypoint": config["Entrypoint"], "mounts": mounts, "restart": host["RestartPolicy"],
            "healthcheck": config.get("Healthcheck"), "security": host.get("SecurityOpt"),
            "memory": host["Memory"], "cpus": host["NanoCpus"]})
    return result


def assert_semantics(left, right):
    a, b = semantic(left), semantic(right)
    if a != b:
        raise AssertionError("\n".join(difflib.unified_diff(json.dumps(b,sort_keys=True,indent=2).splitlines(),json.dumps(a,sort_keys=True,indent=2).splitlines(),fromfile="native",tofile="Yard")))


web_port, mcp_port = port(), port()
origin = "http://127.0.0.1:" + str(web_port)
mcp_origin = "http://127.0.0.1:" + str(mcp_port)
token, sequence = "", 0


def tool(tool_name, arguments, error=False):
    global sequence
    sequence += 1
    request = urllib.request.Request(mcp_origin + "/mcp", data=json.dumps({"jsonrpc": "2.0", "id": sequence,
        "method": "tools/call", "params": {"name": tool_name, "arguments": arguments}}).encode(),
        headers={"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
                 "Authorization": "Bearer " + token, "MCP-Protocol-Version": "2025-11-25"})
    with urllib.request.urlopen(request, timeout=65) as response:
        rpc = json.load(response)
    assert "result" in rpc, rpc
    result = rpc["result"]
    value = result.get("structuredContent")
    if value is None:
        value = json.loads(next(block["text"] for block in result["content"] if block["type"] == "text"))
    assert bool(result.get("isError")) == error, result
    return value


def passed(title):
    manifest["checks"].append(title)
    record()
    print("PASS: " + title, flush=True)


try:
    data = base / "yard-data"
    data.mkdir()
    volumes.append(name + "-data")
    yard = docker("create", "--name", name + "-yard", "--label", "nox-yard.acceptance=" + name,
        "-p", "127.0.0.1:" + str(web_port) + ":8080", "-p", "127.0.0.1:" + str(mcp_port) + ":8081",
        "--mount", "type=volume,source=" + name + "-data,target=/data",
        "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock", args.image)
    containers.append(yard)
    record()
    docker("start", yard)
    wait(lambda: urllib.request.urlopen(origin + "/healthz", timeout=3).status == 200, "fixture Yard startup")
    opener, setup, token = bootstrap_mcp(origin)
    target = name + "-yard-workload"
    native = name + "-native-workload"
    source = """services:
  db:
    image: IMAGE
    command: [sh, -c, 'touch /tmp/ready; exec sleep 600']
    healthcheck: {test: [CMD, test, -f, /tmp/ready], interval: 1s, timeout: 1s, retries: 5}
    stop_grace_period: 1s
  init:
    image: IMAGE
    command: [sh, -c, 'echo run >> /state/runs']
    volumes: ['state:/state']
    labels: {nox-yard.lifecycle: oneshot}
    depends_on: {db: {condition: service_healthy}}
  app:
    image: IMAGE
    command: [sleep, '600']
    environment: {VALUE: '${VALUE:-initial}', OPTIONAL: '${OPTIONAL}'}
    depends_on: {db: {condition: service_healthy}, init: {condition: service_completed_successfully}}
    volumes: ['state:/state', './bind:/bind']
    configs: [{source: settings, target: /settings.txt}]
    secrets: [token]
    mem_limit: 64m
    cpus: 0.25
    security_opt: ['no-new-privileges:true']
    stop_grace_period: 1s
  debug:
    image: IMAGE
    command: [sleep, '600']
    profiles: [debug]
    stop_grace_period: 1s
  companion:
    image: IMAGE
    command: [sleep, '600']
    profiles: [debug]
    stop_grace_period: 1s
volumes: {state: {}}
configs: {settings: {file: settings.txt}}
secrets: {token: {file: token.txt}}
""".replace("IMAGE", json.dumps(args.image))
    for project in (target, native):
        projects.append(project)
        directory = base / project
        (directory / "bind").mkdir(parents=True)
        (directory / "bind" / "keep").write_text("fixture-bind-data",encoding="utf-8")
        (directory / "settings.txt").write_text("fixture-config",encoding="utf-8")
        (directory / "token.txt").write_text("fixture-secret",encoding="utf-8")
        (directory / "compose.yaml").write_text(source, encoding="utf-8")
        (directory / ".env").write_text("VALUE=initial\n", encoding="utf-8")
        volumes.append(project + "_state")
        networks.append(project + "_default")
        record()
        compose(project, "up", "-d", "--no-build", "--pull", "never")
    assert_semantics(target,native)
    passed("initial effective state agrees with native Compose")

    orphan = docker("run", "-d", "--name", name + "-orphan", "--label", "nox-yard.acceptance=" + name,
        "--label", "com.docker.compose.project=" + target, "--label", "com.docker.compose.service=orphan",
        args.image, "sleep", "600")
    containers.append(orphan)
    # Conventional filenames are deliberately not discovered after registration.
    (base / target / "compose.override.yaml").write_text("services: {app: {environment: {UNSELECTED: ignored}}}\n", encoding="utf-8")
    view = tool("yard_project_configuration_read", {"id": "compose:" + target, "reveal": True})
    assert view["composeFiles"] == ["compose.yaml"]
    ids = {item["Id"] for item in runtime(target)}
    override = "services:\n  app:\n    environment: {VALUE: '${OVERRIDE_VALUE:?required}', ADDED: kept}\n    env_file: override.env\n"
    preview = tool("yard_project_override_preview", {"id": "compose:" + target, "action": "add",
        "source": {"kind": "paste", "yaml": override}, "variables":{"OVERRIDE_VALUE":"overridden"}, "files":{"override.env":"EXTRA=from-upload\n"}, "fingerprint": view["fingerprint"]})
    assert preview["path"] == "compose.override.2.yaml" and "overridden" in preview["after"]
    assert {item["Id"] for item in runtime(target)} == ids
    assert not (base / target / preview["path"]).exists()
    applied = tool("yard_project_override_apply", {"id": "compose:" + target, **{key: preview[key]
        for key in ("action", "path", "source", "fingerprint", "candidateFingerprint","variables","files") if key in preview}})
    assert applied["composeFiles"] == ["compose.yaml", "compose.override.2.yaml"]
    assert (base / target / "compose.yaml").read_text() == source
    assert {item["Id"] for item in runtime(target)} == ids
    reread = tool("yard_project_configuration_read", {"id": "compose:" + target})
    assert reread["composeFiles"] == applied["composeFiles"], "old runtime labels discarded the new override"
    (base / native / "compose.override.2.yaml").write_text(override, encoding="utf-8")
    (base / native / "override.env").write_text("EXTRA=from-upload\n",encoding="utf-8")
    (base / native / ".env").write_text("VALUE=initial\nOVERRIDE_VALUE=overridden\n",encoding="utf-8")
    files = ["compose.yaml", "compose.override.2.yaml"]
    passed("override preview/save is explicit, collision-safe and preserves original files and runtime")

    def recreate():
        request = {"id": "compose:" + target, "operation": "recreate"}
        reviewed = tool("yard_recreate_preview", request)
        job = tool("yard_recreate_submit", {**request, "fingerprint": reviewed["fingerprint"], "confirm": True})
        result = wait(lambda: value if (value := tool("yard_job", {"id": job["id"]}))["status"] != "running" else None, "recreate job")
        assert result["status"] == "succeeded", result

    recreate()
    compose(native, "up", "--no-start", "--force-recreate", "--no-build", "--pull", "never", files=files)
    compose(native, "up", "-d", "--no-recreate", "--no-build", "--pull", "never", "app", "db", files=files)
    assert_semantics(target,native)
    for item in runtime(target)+runtime(native):
        if item["Config"]["Labels"]["com.docker.compose.service"]=="app":assert docker("exec",item["Id"],"cat","/bind/keep")=="fixture-bind-data"
    assert json.loads(docker("inspect", orphan))[0]["State"]["Running"]
    passed("project Recreate matches native configured behavior and retains an unrelated orphan")

    def lifecycle_events(project,since,until):
        rows=docker("events","--since",since,"--until",until,"--filter","type=container","--filter","label=com.docker.compose.project="+project,"--format","{{json .}}")
        return [(item["Action"],item["Actor"]["Attributes"].get("com.docker.compose.service")) for item in map(json.loads,rows.splitlines()) if item["Action"] in ("start","stop")]
    for action in ("stop", "restart", "stop", "start"):
        since=format(time.time(),".9f")
        result = tool("yard_project_action", {"id": "compose:" + target, "action": action})
        assert not result["failed"], result
        compose(native, action, files=files)
        assert_semantics(target,native)
        until=format(time.time()+.1,".9f")
        yard_events=lifecycle_events(target,since,until);native_events=lifecycle_events(native,since,until)
        assert Counter(yard_events)==Counter(native_events),(action,yard_events,native_events)
        # Native Compose restart dispatches Engine restarts concurrently; event
        # order is not a startup-condition guarantee. Compare its multiplicity
        # and resulting state above. Start/Stop retain their dependency order.
        for phase in (() if action=="restart" else ("start","stop")):
            def dependency_order(events):
                sequence=[service for event,service in events if event==phase]
                if "db" not in sequence or "app" not in sequence:return None
                return max(i for i,v in enumerate(sequence) if v=="db")<min(i for i,v in enumerate(sequence) if v=="app")
            assert dependency_order(yard_events)==dependency_order(native_events),(action,phase,yard_events,native_events)
        assert json.loads(docker("inspect", orphan))[0]["State"]["Running"]
    passed("dependency-sensitive Stop/Restart/Start and completed initializers agree with native Compose")

    for project in (target, native):
        compose(project, "up", "-d", "--no-build", "--pull", "never", "debug", profiles=["debug"], files=files)
    recreate()
    assert "debug" in semantic(target) and "companion" not in semantic(target)
    view = tool("yard_project_configuration_read", {"id": "compose:" + target})
    tool("yard_project_configuration_save", {"id": "compose:" + target, "fingerprint": view["fingerprint"],
        "profiles": ["debug"], "path": "", "content": ""})
    recreate()
    assert semantic(target)["debug"][0]["running"] and semantic(target)["companion"][0]["running"]
    passed("deployed profiled service is preserved without activating companions; explicit profiles activate them")

    override_path=base/target/files[1]
    retained=override_path.with_suffix(".retained")
    override_path.rename(retained)
    try:
        before_ids={item["Id"] for item in runtime(target)}
        tool("yard_recreate_preview",{"id":"compose:"+target,"operation":"recreate"},error=True)
        broken=tool("yard_project_configuration_read",{"id":"compose:"+target})
        assert any(file["path"]==files[1] and file.get("missing") for file in broken["files"])
        assert before_ids=={item["Id"] for item in runtime(target)}
    finally:
        retained.rename(override_path)
    passed("partially missing explicit file lists fail without runtime fallback and remain repairable")
    view = tool("yard_project_configuration_read", {"id": "compose:" + target})
    remove = tool("yard_project_override_preview", {"id": "compose:" + target, "action": "remove", "path": files[1],
        "source": {"kind": "paste", "yaml": ""}, "fingerprint": view["fingerprint"]})
    (base / target / ".env").write_text("VALUE=external-edit\n", encoding="utf-8")
    tool("yard_project_override_apply", {"id": "compose:" + target, **{key: remove[key]
        for key in ("action", "path", "source", "fingerprint", "candidateFingerprint")}}, error=True)
    view = tool("yard_project_configuration_read", {"id": "compose:" + target})
    remove = tool("yard_project_override_preview", {"id": "compose:" + target, "action": "remove", "path": files[1],
        "source": {"kind": "paste", "yaml": ""}, "fingerprint": view["fingerprint"]})
    tool("yard_project_override_apply", {"id": "compose:" + target, **{key: remove[key]
        for key in ("action", "path", "source", "fingerprint", "candidateFingerprint")}})
    assert (base / target / files[1]).exists()
    passed("stale override confirmations fail; removing selection retains the host source file")

    log_id = docker("create", "--name", name + "-logs", "--label", "nox-yard.acceptance=" + name,
        "--log-driver", "local", "--log-opt", "max-size=1m", "--log-opt", "max-file=2", args.image,
        "sh", "-c", "i=0; while [ $i -lt 1205 ]; do echo record-$i; i=$((i+1)); done; printf '\\n'; awk 'BEGIN {for(i=0;i<40000;i++)printf \"á\";printf \"\\n\"}'")
    containers.append(log_id)
    record()
    docker("start", "-a", log_id)
    page = tool("yard_container_log_history", {"id": log_id})
    end = page["until"]
    content = "".join(line["text"] + ("\n" if line["lineEnd"] else "") for line in page["lines"])
    while page.get("next"):
        page = tool("yard_container_log_history", {"id": log_id, "cursor": page["next"]})
        content += "".join(line["text"] + ("\n" if line["lineEnd"] else "") for line in page["lines"])
    native_logs = subprocess.run(["docker", "logs", "--timestamps", "--until", end, log_id], capture_output=True, text=True, check=True).stdout
    assert content == native_logs, "history altered line boundaries or lost records"
    assert not docker("exec",yard,"sh","-c","find /data -type f -name '*.txt'")
    passed("retained Docker history matches native logs, including empty/long Unicode lines, without Yard files")

    rotation_id=docker("create","--name",name+"-rotation","--label","nox-yard.acceptance="+name,"--log-driver","local","--log-opt","max-size=20k","--log-opt","max-file=2",args.image,"sh","-c","awk 'BEGIN {for(i=0;i<10000;i++)printf \"rotation-row-%d %080d\\n\",i,i}'")
    containers.append(rotation_id);record();docker("start","-a",rotation_id)
    rotated=tool("yard_container_log_history",{"id":rotation_id});rotation_end=rotated["until"]
    rotation_content="".join(line["text"]+("\n" if line["lineEnd"] else "") for line in rotated["lines"])
    while rotated.get("next"):
        rotated=tool("yard_container_log_history",{"id":rotation_id,"cursor":rotated["next"]})
        rotation_content+="".join(line["text"]+("\n" if line["lineEnd"] else "") for line in rotated["lines"])
    retained=subprocess.run(["docker","logs","--timestamps","--until",rotation_end,rotation_id],capture_output=True,text=True,check=True).stdout
    assert rotation_content==retained and "rotation-row-9999 " in retained and "rotation-row-0 " not in retained
    passed("rotated history exposes exactly the records retained by the Docker driver")

    for tty in (False,True):
        stream_id=docker("create",*( ["-t"] if tty else []),"--name",name+("-tty" if tty else "-streams"),"--label","nox-yard.acceptance="+name,args.image,"sh","-c","printf 'out\\n\\n'; printf 'err\\n' >&2")
        containers.append(stream_id);record();docker("start","-a",stream_id)
        streamed=tool("yard_container_log_history",{"id":stream_id})
        assert {line["stream"] for line in streamed["lines"]}==({"combined"} if tty else {"stdout","stderr"}),streamed
        assert any(line["lineEnd"] and line["text"].split(" ",1)[-1]=="" for line in streamed["lines"])
    none_id=docker("create","--name",name+"-no-read","--label","nox-yard.acceptance="+name,"--log-driver","none",args.image,"echo","unavailable")
    containers.append(none_id);record();docker("start",none_id)
    unavailable=tool("yard_container_log_history",{"id":none_id},error=True)
    assert "unavailable" in str(unavailable).lower() or "read" in str(unavailable).lower(),unavailable
    passed("stdout/stderr, TTY combined streams, empty lines and unavailable log driver are explicit")

    # Old Compose ownership labels cannot override a new external declaration.
    saved_source=(base/target/"compose.yaml").read_text()
    external_source=saved_source.replace("volumes: {state: {}}", "volumes: {state: {external: true, name: "+target+"_state}}")+"\nnetworks: {default: {external: true, name: "+target+"_default}}\n"
    (base/target/"compose.yaml").write_text(external_source,encoding="utf-8")
    external_plan=tool("yard_project_remove_preview",{"id":"compose:"+target,"removeVolumes":True})
    assert all(item["action"]=="keep" for item in external_plan["items"] if item["kind"] in ("volume","network")),external_plan
    (base/target/"compose.yaml").write_text(saved_source,encoding="utf-8")
    passed("external declarations protect old project-labelled volumes and networks")

    plan = tool("yard_project_remove_preview", {"id": "compose:" + target})
    assert all(item["action"] == "keep" for item in plan["items"] if item["kind"] in ("volume", "image"))
    result = tool("yard_project_remove", {"id": "compose:" + target, "confirm": True, "fingerprint": plan["fingerprint"]})
    assert not any(item["status"] == "failed" for item in result["items"]), result
    assert docker("volume", "inspect", target + "_state") and docker("image", "inspect", args.image)
    passed("project removal retains volumes and images unless explicitly selected")
    image_name=name+":exclusive"
    manifest["images"].append(image_name)
    image_dir=base/"exclusive-image";image_dir.mkdir()
    (image_dir/"Dockerfile").write_text("FROM "+args.image+"\nLABEL nox-yard.acceptance="+name+"\n",encoding="utf-8")
    record();docker("build","-t",image_name,str(image_dir))
    image_container=docker("create","--name",name+"-exclusive","--label","nox-yard.acceptance="+name,image_name,"sleep","600")
    containers.append(image_container);record()
    ordinary=tool("yard_container_remove_preview",{"id":image_container})
    explicit=tool("yard_container_remove_preview",{"id":image_container,"removeImages":True})
    assert ordinary["fingerprint"]!=explicit["fingerprint"]
    assert any(item["kind"]=="image" and item["action"]=="remove" for item in explicit["items"])
    tool("yard_container_remove",{"id":image_container,"fingerprint":ordinary["fingerprint"],"removeImages":True,"confirm":True},error=True)
    deleted=tool("yard_container_remove",{"id":image_container,"fingerprint":explicit["fingerprint"],"removeImages":True,"confirm":True})
    assert any(item["kind"]=="image" and item["status"]=="removed" for item in deleted["items"]),deleted
    passed("image deletion is opt-in, fingerprint-bound and limited to an unused exclusive image")
    manifest["complete"] = True
finally:
    # Clean helpers/workers only if mounted on this exact disposable fixture.
    workers = docker("ps", "-aq", "--filter", "label=nox-yard.role=operation-worker").splitlines()
    for identifier in workers:
        info = subprocess.run(["docker", "inspect", identifier], capture_output=True, text=True)
        if info.returncode == 0 and any(mount.get("Source", "").startswith(str(base) + "/") or mount.get("Name") in volumes for mount in json.loads(info.stdout)[0]["Mounts"]):
            subprocess.run(["docker", "rm", "-f", identifier], capture_output=True)
    for project in projects:
        ids = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).splitlines()
        if ids:
            docker("rm", "-f", *ids)
    for identifier in reversed(containers):
        subprocess.run(["docker", "rm", "-f", identifier], capture_output=True)
    for network in networks:
        subprocess.run(["docker", "network", "rm", network], capture_output=True)
    for volume in volumes:
        subprocess.run(["docker", "volume", "rm", volume], capture_output=True)
    for image in manifest["images"]:
        subprocess.run(["docker","image","rm",image],capture_output=True)
    residue = []
    for kind, command in (("containers", ["ps", "-aq"]), ("networks", ["network", "ls", "-q"]), ("volumes", ["volume", "ls", "-q"])):
        for identifier in docker(*command).splitlines():
            if identifier.startswith(name) or (kind == "containers" and identifier in containers):
                residue.append(kind + ":" + identifier)
    assert not residue, residue
    manifest["cleanup"] = "verified"
    manifest["finishedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    record()
    cleanup_fixture(base, root, name, args.image)
