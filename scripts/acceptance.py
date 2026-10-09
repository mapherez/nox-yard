#!/usr/bin/env python3
"""Run isolated runtime gates on the selected local Docker Engine and save evidence.

Requires Python 3, Docker with Compose, current/legacy Linux images for the
native host architecture, and outbound access to public fixture/build images.
Uses only randomly named disposable fixtures. Does not deploy the normal Yard.
"""
import argparse
import datetime
import json
import pathlib
import platform
import subprocess
import sys
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
parser.add_argument("--legacy-image")
parser.add_argument("--groups", nargs="+", choices=["upgrade", "jobs", "managed", "recreate", "mcp", "schedules", "self-update"],
                    default=["upgrade", "jobs", "managed", "recreate", "mcp", "schedules", "self-update"])
parser.add_argument("--report", default=".tmp/acceptance-results.json")
args = parser.parse_args()
if "upgrade" in args.groups and not args.legacy_image:
    parser.error("--legacy-image is required for the upgrade gate")
root = pathlib.Path(__file__).resolve().parents[1]
report = pathlib.Path(args.report).resolve()
report.parent.mkdir(parents=True, exist_ok=True)

def inspect_image(image):
    result = subprocess.run(["docker", "image", "inspect", image], check=True, text=True, capture_output=True)
    item = json.loads(result.stdout)[0]
    return {"reference": image, "id": item["Id"], "os": item["Os"], "architecture": item["Architecture"]}

images = [inspect_image(args.image)]
if "upgrade" in args.groups:
    images.append(inspect_image(args.legacy_image))
if any(image["architecture"] != images[0]["architecture"] or image["os"] != "linux" for image in images):
    raise SystemExit("Current and legacy images must both target the same Linux architecture")
info = subprocess.run(["docker", "info", "--format", "{{json .}}"], check=True, capture_output=True, text=True)
engine = json.loads(info.stdout)
architecture = {"aarch64": "arm64", "x86_64": "amd64"}.get(engine["Architecture"], engine["Architecture"])
if images[0]["architecture"] != architecture:
    raise SystemExit("Use native images for the selected Docker Engine; ARM64 emulation is a separate smoke gate")
model = pathlib.Path("/proc/device-tree/model")
evidence = {"scope": "isolated-runtime-gates", "manualHostAcceptancePending": True, "startedAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "host": {"system": platform.system(), "machine": platform.machine(), "kernel": platform.release(),
        "model": model.read_text().strip("\x00\n") if model.exists() else None},
    "engine": {"version": engine["ServerVersion"], "os": engine["OSType"], "architecture": engine["Architecture"]},
    "images": images, "selectedGroups": args.groups, "checks": [], "complete": False}
checks = [
    ("upgrade", "v1.0.1 upgrade/password recovery/backup restore", "smoke-upgrade.py", ["--legacy-image", args.legacy_image]),
    ("jobs", "durable jobs and interruption", "smoke-jobs.py", []),
    ("managed", "managed updates and rollback", "smoke-managed.py", []),
    ("recreate", "external/standalone recreation and rollback", "smoke-recreate.py", []),
    ("mcp", "HTTP/MCP, lifecycle, import/adoption and removal", "smoke-mcp.py", []),
    ("schedules", "automatic project scheduling", "smoke-schedules.py", []),
    ("self-update", "Yard self-update worker and failed replacement/SQLite recovery", "smoke-self-update.py", []),
]
try:
    for group, title, script, extra in checks:
        if group not in args.groups:
            continue
        print("Running: " + title, flush=True)
        started = time.monotonic()
        result = subprocess.run([sys.executable, "-X", "utf8", "-u", str(root / "scripts" / script), "--image", args.image, *extra], cwd=root)
        evidence["checks"].append({"name": title, "script": script, "passed": result.returncode == 0,
            "seconds": round(time.monotonic() - started, 2)})
        report.write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
        if result.returncode:
            raise SystemExit(result.returncode)
    evidence["complete"] = True
finally:
    evidence["finishedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    report.write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
print("PASS: isolated runtime gates; evidence saved to " + str(report), flush=True)
