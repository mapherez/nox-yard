#!/usr/bin/env python3
"""C3 acceptance using a disposable registry, app images and Linux Go runner.

Never targets user stacks. The URL business-flow test uses an injected source
loader, without publishing fixture files or weakening production URL checks.
"""
import argparse
import json
import pathlib
import secrets
import shutil
import socket
import subprocess

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
name = "managed-smoke-" + secrets.token_hex(6)
base = root / ".tmp" / name
base.mkdir(parents=True)
projects = base / "projects"
projects.mkdir()
images = []
registry = None
reference = ""

def docker(*parts, stream=False):
    result = subprocess.run(["docker", *parts], text=True, capture_output=not stream)
    if result.returncode:
        raise RuntimeError("Fixture Docker command failed: " + " ".join(parts[:3]) + "\n" + (result.stderr or ""))
    return (result.stdout or "").strip()

try:
    # Explicitly select the same port on the Windows forwarder and Linux daemon.
    # Docker Desktop may allocate different ephemeral ports for these surfaces.
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = str(probe.getsockname()[1])
    registry = docker("create", "--name", name + "-registry", "--label", "nox-yard.acceptance=" + name,
        "--publish", "127.0.0.1:" + port + ":5000", "--tmpfs", "/var/lib/registry", "registry:3")
    docker("start", registry)
    info = json.loads(docker("inspect", registry))[0]
    port = info["NetworkSettings"]["Ports"]["5000/tcp"][0]["HostPort"]
    reference = "127.0.0.1:" + port + "/" + name + ":app"
    versions = {}
    for version in ("V1", "V2", "BAD"):
        image = name + ":" + version.lower()
        images.append(image)
        (base / "Dockerfile").write_text("FROM alpine:3.23\nLABEL nox-yard.acceptance=" + name + " fixture.version=" + version +
            "\nHEALTHCHECK --interval=1s --timeout=1s --retries=1 CMD " + ("false" if version == "BAD" else "true") +
            "\nCMD [\"sleep\", \"600\"]\n", newline="")
        docker("build", "-t", image, str(base))
        versions[version] = image
    docker("tag", versions["V1"], reference)
    docker("push", reference)
    runner = name + ":runner"
    images.append(runner)
    (base / "Dockerfile").write_text("FROM " + args.image + " AS tools\nFROM golang:1.26-alpine\n" +
        "COPY --from=tools /usr/bin/docker /usr/bin/docker\n" +
        "COPY --from=tools /usr/libexec/docker/cli-plugins/docker-compose /usr/libexec/docker/cli-plugins/docker-compose\n", newline="")
    docker("build", "-t", runner, str(base))
    host_base = str(projects).replace("\\", "/")
    if len(host_base) > 2 and host_base[1] == ":":
        host_base = "/run/desktop/mnt/host/" + host_base[0].lower() + "/" + host_base[3:]
    env = {"NOX_HELPER_IMAGE": args.image, "NOX_MANAGED_ACCEPTANCE_BASE": host_base,
        "NOX_MANAGED_ACCEPTANCE_NAME": name, "NOX_MANAGED_ACCEPTANCE_IMAGE": reference,
        "NOX_MANAGED_ACCEPTANCE_REGISTRY": registry}
    env.update({"NOX_MANAGED_ACCEPTANCE_" + key: value for key, value in versions.items()})
    command = ["run", "--rm", "--name", name + "-runner", "--label", "nox-yard.acceptance=" + name,
        "--mount", "type=bind,source=" + str(root) + ",target=/src,readonly",
        "--mount", "type=bind,source=" + str(projects) + ",target=" + host_base,
        "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
        "--mount", "type=volume,source=nox-yard-c2-go-cache,target=/go/pkg",
        "--mount", "type=volume,source=nox-yard-c2-build-cache,target=/root/.cache/go-build", "--workdir", "/src"]
    for key, value in env.items():
        command += ["--env", key + "=" + value]
    docker(*command, runner, "go", "test", "-v", "-count=1", "-timeout=15m", "-run", "^TestDeploymentDockerAcceptance$", "./internal/managed", stream=True)
finally:
    # All application identities are generated above; mounts/data are fixture-only.
    for project in (name, name + "-copy"):
        identifiers = docker("ps", "-aq", "--filter", "label=com.docker.compose.project=" + project).splitlines()
        if identifiers:
            docker("rm", "-f", *identifiers)
        networks = docker("network", "ls", "-q", "--filter", "label=com.docker.compose.project=" + project).splitlines()
        if networks:
            docker("network", "rm", *networks)
        subprocess.run(["docker", "volume", "rm", project + "_data"], capture_output=True)
    remaining = docker("ps", "-aq", "--filter", "label=nox-yard.acceptance=" + name).splitlines()
    if remaining:
        docker("rm", "-f", *remaining)
    if reference:
        subprocess.run(["docker", "image", "rm", reference], capture_output=True)
    retained = docker("image", "ls", "--filter", "label=nox-yard.acceptance=" + name, "--format", "{{.Repository}}:{{.Tag}}").splitlines()
    for tag in retained:
        if tag.startswith("nox-yard-rollback/"):
            subprocess.run(["docker", "image", "rm", tag], capture_output=True)
    for image in reversed(images):
        subprocess.run(["docker", "image", "rm", image], capture_output=True)
    assert base.resolve().is_relative_to((root / ".tmp").resolve()) and base.name == name
    shutil.rmtree(base)
