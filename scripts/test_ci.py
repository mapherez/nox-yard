"""Offline tests of CI grouping, evidence and shell handoffs. No real Docker."""
import json
import os
import pathlib
import re
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import importlib.util
from runtime_suites import SUITES, CI_GROUPS, ci_suites

ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("ci_evidence", ROOT / "scripts/ci-evidence.py")
ci_evidence = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ci_evidence)


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="yard-ci-evidence-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = pathlib.Path(self.temporary.name)
        self.identity = "sha256:" + "a" * 64
        for arch in ("amd64", "arm64"):
            self.write("image-" + arch + ".json", {"Architecture": arch, "Os": "linux", "Id": self.identity})
        for group, scripts in CI_GROUPS.items():
            self.write("runtime-" + group + ".json", {"group": group, "complete": True,
                "selectedSuites": list(scripts), "checks": [{"script": script, "passed": True} for script in scripts],
                "images": [{"id": self.identity}], "fixtureOwnership": {"passed": True}})

    def write(self, name, value):
        (self.directory / name).write_text(json.dumps(value))

    def mutate(self, name, fn):
        value = json.loads((self.directory / name).read_text())
        fn(value)
        self.write(name, value)

    def test_groups_cover_every_current_suite_once_without_legacy_upgrade(self):
        grouped = [script for group in CI_GROUPS for script in ci_suites(group)]
        self.assertCountEqual(grouped, [script for _, script, legacy in SUITES if not legacy])
        self.assertEqual(len(grouped), len(set(grouped)))

    def test_workflow_matrix_runs_all_inventory_groups(self):
        workflow = (ROOT / ".github/workflows/ci.yml").read_text()
        configured = re.search(r"^        group: \[([^\]]+)\]$", workflow, re.MULTILINE)
        self.assertIsNotNone(configured)
        self.assertEqual(re.findall(r"'([^']+)'", configured.group(1)), list(CI_GROUPS))

    def test_adding_a_suite_without_assigning_it_is_an_error(self):
        with patch("runtime_suites.SUITES", SUITES + [("new", "new.py", False)]):
            with self.assertRaises(ValueError):
                ci_suites("1")

    def test_complete_evidence_is_accepted(self):
        self.assertEqual(len(ci_evidence.verify_evidence(self.directory)["runtime"]), 3)

    def test_rejects_missing_group(self):
        (self.directory / "runtime-2.json").unlink()
        with self.assertRaises(FileNotFoundError):
            ci_evidence.verify_evidence(self.directory)

    def test_rejects_failure_missing_duplicate_and_unexpected_suites(self):
        filename = "runtime-1.json"
        original = json.loads((self.directory / filename).read_text())
        for mutate in [lambda v: v.update(complete=False), lambda v: v["checks"][0].update(passed=False),
                       lambda v: v["checks"].pop(), lambda v: v["checks"].append(v["checks"][0]),
                       lambda v: v["checks"][0].update(script="other.py"), lambda v: v.update(group="2"),
                       lambda v: v.update(selectedSuites=[]), lambda v: v.update(fixtureOwnership={"passed": False})]:
            self.write(filename, original)
            self.mutate(filename, mutate)
            with self.assertRaises(ValueError):
                ci_evidence.verify_evidence(self.directory)

    def test_rejects_different_runtime_image_or_platform(self):
        self.mutate("runtime-3.json", lambda v: v["images"][0].update(id="sha256:" + "b" * 64))
        with self.assertRaisesRegex(ValueError, "different image"):
            ci_evidence.verify_evidence(self.directory)
        self.mutate("image-arm64.json", lambda v: v.update(Architecture="amd64"))
        with self.assertRaisesRegex(ValueError, "platform"):
            ci_evidence.verify_evidence(self.directory)

    def test_invalid_group_combination_fails_before_docker(self):
        for arguments in [["--group", "1"], ["--group", "1", "--skip-upgrade", "--suite", "smoke-jobs.py"]]:
            result = subprocess.run([os.sys.executable, str(ROOT / "scripts/acceptance.py"), "--image", "not-used", *arguments], capture_output=True, text=True)
            self.assertEqual(result.returncode, 2)
            self.assertIn("--group requires", result.stderr)


class ShellHandoffTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="yard-ci-shell-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = pathlib.Path(self.temporary.name)
        self.log = self.directory / "commands.jsonl"
        self.bin = self.directory / "bin"
        self.bin.mkdir()
        self.environment = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.defpath,
                                FAKE_LOG=str(self.log), FAKE_ARCH="arm64")
        self.executable("docker", '''#!/usr/bin/env python3
import json, os, pathlib, sys
args = sys.argv[1:]
with open(os.environ["FAKE_LOG"], "a") as log: log.write(json.dumps(args) + "\\n")
if args[0] == "create":
    print("disposable-smoke-container")
elif args[0] == "cp":
    header = bytearray(20); header[:4] = b"\\x7fELF"; header[4:6] = bytes([2, 1])
    header[18:20] = (62 if os.environ["FAKE_ARCH"] == "amd64" else 183).to_bytes(2, "little")
    pathlib.Path(args[-1]).write_bytes(header)
elif args[0] == "port":
    print("127.0.0.1:4567")
elif args[:2] == ["volume", "create"]:
    print(args[-1])
elif args[0] == "run":
    mounts = [args[i+1] for i, value in enumerate(args[:-1]) if value == "--mount"]
    transfer = next(value for value in mounts if "target=/transfer" in value)
    directory = pathlib.Path(dict(item.split("=", 1) for item in transfer.split(",") if "=" in item)["source"])
    archive = directory / (args[-1] + ".tar")
    assert archive.exists(), "host must create the private archive before root opens it"
    if "-cf" in args[-3]:
        assert archive.stat().st_mode & 0o777 == 0o600
        assert archive.stat().st_uid == os.getuid()
        archive.write_bytes(b"fake-cache-archive")
    else:
        assert "readonly" in transfer
elif args[0] not in ("start", "exec", "rm", "logs"):
    raise SystemExit("Unexpected fake Docker command")
''')
        self.executable("curl", '#!/bin/sh\nprintf \'{"status":"ok"}\\n\'\n')

    def executable(self, name, contents):
        path = self.bin / name
        path.write_text(contents)
        path.chmod(0o755)

    def commands(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def test_cache_save_creates_private_host_owned_archives_then_restores_readonly(self):
        directory = self.directory / "cache"
        for operation in ("restore", "save", "restore"):
            result = subprocess.run(["sh", str(ROOT / "scripts/ci-runtime-cache.sh"), operation, str(directory), "fake-image"],
                                    env=self.environment, capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len([args for args in self.commands() if args[0] == "run"]), 4)
        for entry in ("modules", "build"):
            self.assertEqual((directory / (entry + ".tar")).read_bytes(), b"fake-cache-archive")

    def test_smoke_checks_both_architectures_and_cleans_container_and_binary(self):
        for arch in ("amd64", "arm64"):
            result = subprocess.run(["sh", str(ROOT / "scripts/smoke-arm64.sh"), "fake-image", arch],
                                    env=dict(self.environment, FAKE_ARCH=arch), capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn(arch + " executable", result.stdout)
        commands = self.commands()
        self.assertEqual(len([args for args in commands if args[:2] == ["rm", "-f"]]), 2)
        for args in commands:
            if args[0] == "cp":
                self.assertFalse(pathlib.Path(args[-1]).exists())

    def test_wrong_executable_architecture_fails_and_cleans_up(self):
        result = subprocess.run(["sh", str(ROOT / "scripts/smoke-arm64.sh"), "fake-image", "amd64"],
                                env=self.environment, capture_output=True, text=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not contain a Linux amd64", result.stderr)
        self.assertEqual(self.commands()[-1], ["rm", "-f", "disposable-smoke-container"])


if __name__ == "__main__":
    unittest.main()
