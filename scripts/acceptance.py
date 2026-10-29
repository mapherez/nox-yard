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
from runtime_suites import SUITES

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--image", required=True)
parser.add_argument("--legacy-image")
parser.add_argument("--skip-upgrade", action="store_true", help="Run the current-image runtime gates only; legacy upgrade evidence remains separate")
parser.add_argument("--report", default=".tmp/acceptance-results.json")
parser.add_argument("--keep-going", action="store_true", help="Record all selected suite results before returning failure")
parser.add_argument("--suite", action="append", choices=[suite[1] for suite in SUITES], help="Run only explicitly selected suites")
args = parser.parse_args()
root = pathlib.Path(__file__).resolve().parents[1]
report = pathlib.Path(args.report).resolve()
report.parent.mkdir(parents=True, exist_ok=True)

def inspect_image(image):
    result = subprocess.run(["docker", "image", "inspect", image], check=True, text=True, capture_output=True)
    item = json.loads(result.stdout)[0]
    return {"reference": image, "id": item["Id"], "os": item["Os"], "architecture": item["Architecture"]}

if not args.skip_upgrade and not args.legacy_image: parser.error("--legacy-image is required unless --skip-upgrade is explicit")
images = [inspect_image(image) for image in (args.image, args.legacy_image) if image]
if len({image["architecture"] for image in images}) > 1 or any(image["os"] != "linux" for image in images):
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
    "images": images, "checks": [], "complete": False}
checks = [(title,script,["--legacy-image",args.legacy_image] if legacy else []) for title,script,legacy in SUITES if not (legacy and args.skip_upgrade) and (not args.suite or script in args.suite)]
evidence["legacyUpgradeIncluded"] = any(script=="smoke-upgrade.py" for _,script,_ in checks)
evidence["selectedSuites"] = [script for _,script,_ in checks]

try:
    for title, script, extra in checks:
        print("Running: " + title, flush=True)
        started = time.monotonic()
        result = subprocess.run([sys.executable, "-X", "utf8", "-u", str(root / "scripts" / script), "--image", args.image, *extra], cwd=root)
        evidence["checks"].append({"name": title, "script": script, "passed": result.returncode == 0,
            "seconds": round(time.monotonic() - started, 2)})
        report.write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
        if result.returncode and not args.keep_going:
            raise SystemExit(result.returncode)
    evidence["complete"] = all(check["passed"] for check in evidence["checks"])
finally:
    evidence["finishedAt"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    report.write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
if not evidence["complete"]:
    raise SystemExit("FAIL: one or more isolated runtime gates; evidence saved to " + str(report))
print("PASS: isolated runtime gates; evidence saved to " + str(report), flush=True)
