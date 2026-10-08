#!/usr/bin/env python3
"""C5 daily scheduling through real independent workers and a disposable registry."""
import argparse
import pathlib
import secrets
import shutil
import socket
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = "recreate-smoke-" + secrets.token_hex(6)
base = root / ".tmp" / name
base.mkdir(parents=True)
images = []
references = []

def docker(*parts, stream=False):
    result = subprocess.run(["docker", *parts], text=True, capture_output=not stream)
    if result.returncode:
        raise RuntimeError("Fixture Docker command failed: " + " ".join(parts[:3]) + "\n" + (result.stderr or ""))
    return (result.stdout or "").strip()

try:
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = str(probe.getsockname()[1])
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        app_port = str(probe.getsockname()[1])
    registry = docker("create", "--name", name + "-registry", "--label", "nox-yard.acceptance=" + name,
        "--publish", "127.0.0.1:" + port + ":5000", "--tmpfs", "/var/lib/registry", "registry:3")
    docker("start", registry)
    reference = "127.0.0.1:" + port + "/" + name
    versions = {}
    for version in ("V1", "V2", "BAD", "EXTRA"):
        image = name + ":" + version.lower()
        images.append(image)
        (base / "start").write_text("#!/bin/sh\n" + ("touch /tmp/ready\n" if version != "BAD" else "") + "exec sleep 600\n", newline="")
        (base / "Dockerfile").write_text("FROM alpine:3.23\nLABEL nox-yard.acceptance=" + name + " fixture.version=" + version +
            "\nCOPY start /app/start\nRUN chmod +x /app/start\n" +
            "VOLUME /anonymous\n" + ("VOLUME /extra\n" if version == "EXTRA" else "") +
            "HEALTHCHECK --interval=1s --timeout=1s --retries=1 CMD test -f /tmp/ready\nCMD [\"/app/start\"]\n", newline="")
        docker("build", "-t", image, str(base))
        versions[version] = image
    for tag in ("app", "db", "web", "recovery"):
        ref = reference + ":" + tag
        references.append(ref)
        docker("tag", versions["V1"], ref)
        docker("push", ref)
    docker("volume", "create", "--label", "nox-yard.acceptance=" + name, name + "-state")
    runner = name + ":runner"
    images.append(runner)
    (base / "Dockerfile").write_text("FROM " + args.image + " AS tools\nFROM golang:1.26-alpine\n" +
        "LABEL nox-yard.acceptance=" + name + "\n" +
        "COPY --from=tools /usr/local/bin/nox-yard /usr/local/bin/nox-yard\n" +
        "COPY --from=tools /usr/bin/docker /usr/bin/docker\n" +
        "COPY --from=tools /usr/libexec/docker/cli-plugins/docker-compose /usr/libexec/docker/cli-plugins/docker-compose\n", newline="")
    docker("build", "-t", runner, str(base))
    host_base = str(base).replace("\\", "/")
    if host_base[1:2] == ":":
        host_base = "/run/desktop/mnt/host/" + host_base[0].lower() + "/" + host_base[3:]
    env = {"NOX_DATA_DIR": "/data", "NOX_HELPER_IMAGE": runner, "NOX_RECREATE_BASE": host_base, "NOX_RECREATE_NAME": name, "NOX_RECREATE_IMAGE": reference,
        "NOX_RECREATE_REGISTRY": registry, "NOX_RECREATE_PORT": app_port}
    env.update({"NOX_RECREATE_" + key: value for key, value in versions.items()})
    command = ["run", "--rm", "--name", name + "-runner", "--label", "nox-yard.acceptance=" + name,
        "--mount", "type=volume,source=" + name + "-state,target=/data",
        "--mount", "type=bind,source=" + str(root) + ",target=/src,readonly",
        "--mount", "type=bind,source=" + str(base) + ",target=" + host_base,
        "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
        "--mount", "type=volume,source=nox-yard-c2-go-cache,target=/go/pkg",
        "--mount", "type=volume,source=nox-yard-c2-build-cache,target=/root/.cache/go-build", "--workdir", "/src"]
    for key, value in env.items():
        command += ["--env", key + "=" + value]
    docker(*command, runner, "go", "test", "-v", "-count=1", "-timeout=15m", "-run", "^TestScheduleDockerAcceptance$", "./internal/application", stream=True)
finally:
    identifiers = docker("ps", "-aq", "--filter", "label=nox-yard.acceptance=" + name).splitlines()
    # Image VOLUME declarations create anonymous volumes without labels.
    # Capture only names actually mounted by this disposable fixture.
    anonymous = set()
    recorded = base / "anonymous-volumes"
    if recorded.exists():
        import re
        anonymous.update(value for value in recorded.read_text().splitlines() if re.fullmatch(r"[0-9a-f]{64}", value))
    if identifiers:
        import json
        for item in json.loads(docker("inspect", *identifiers)):
            anonymous.update(m["Name"] for m in item.get("Mounts", []) if m["Type"] == "volume" and len(m.get("Name", "")) == 64)
    if identifiers:
        docker("rm", "-f", *identifiers)
    for volume in anonymous:
        subprocess.run(["docker", "volume", "rm", volume], capture_output=True)
    for project in (name + "-external", name + "-managed"):
        for kind in ("network", "volume"):
            identifiers = docker(kind, "ls", "-q", "--filter", "label=com.docker.compose.project=" + project).splitlines()
            if identifiers:
                docker(kind, "rm", *identifiers)
    for kind in ("network", "volume"):
        identifiers = docker(kind, "ls", "-q", "--filter", "label=nox-yard.acceptance=" + name).splitlines()
        if identifiers:
            docker(kind, "rm", *identifiers)
    retained = docker("image", "ls", "--filter", "label=nox-yard.acceptance=" + name, "--format", "{{.Repository}}:{{.Tag}}").splitlines()
    for tag in retained:
        if tag.startswith("nox-yard-rollback/"):
            subprocess.run(["docker", "image", "rm", tag], capture_output=True)
    for image in references + list(reversed(images)):
        subprocess.run(["docker", "image", "rm", image], capture_output=True)
    assert base.resolve().is_relative_to((root / ".tmp").resolve()) and base.name == name
    shutil.rmtree(base)
