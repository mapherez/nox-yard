#!/usr/bin/env python3
"""Verify parallel CI evidence without running Docker or application tests."""
import argparse
import json
import pathlib
import re
from runtime_suites import CI_GROUPS, ci_suites


def verify_evidence(directory):
    directory = pathlib.Path(directory)
    identities = {arch: json.loads((directory / ("image-" + arch + ".json")).read_text())
                  for arch in ("amd64", "arm64")}
    for arch, image in identities.items():
        if image["Architecture"] != arch or image["Os"] != "linux":
            raise ValueError("Unexpected image platform: " + arch)
        if not re.fullmatch(r"sha256:[a-f0-9]{64}", image["Id"]):
            raise ValueError("Missing immutable image identity")
    reports = []
    for group in CI_GROUPS:
        report = json.loads((directory / ("runtime-" + group + ".json")).read_text())
        expected = set(ci_suites(group))
        checks = report["checks"]
        if (report.get("group") != group or report.get("complete") is not True or
                sorted(report["selectedSuites"]) != sorted(expected) or len(checks) != len(expected) or
                {check["script"] for check in checks} != expected or
                any(check["passed"] is not True for check in checks)):
            raise ValueError("Incomplete runtime group: " + group)
        if len(report["images"]) != 1 or report["images"][0]["id"] != identities["amd64"]["Id"]:
            raise ValueError("Runtime group used a different image: " + group)
        if group == "1" and report.get("fixtureOwnership", {}).get("passed") is not True:
            raise ValueError("Linux fixture ownership gate did not pass")
        reports.append(report)
    return {"images": identities, "runtime": reports}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory")
    args = parser.parse_args()
    print(json.dumps(verify_evidence(args.directory), indent=2))
