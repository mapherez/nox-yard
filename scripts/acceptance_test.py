"""Exercise gate selection/reporting with subprocesses mocked; never starts Docker."""
import contextlib
import io
import json
import pathlib
import runpy
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


class AcceptanceRunnerTests(unittest.TestCase):
    def run_gate(self, group, failure=0):
        calls = []
        with tempfile.TemporaryDirectory(prefix="nox-acceptance-test-") as directory:
            report = pathlib.Path(directory) / "report.json"

            def execute(command, **kwargs):
                calls.append(command)
                if command[:3] == ["docker", "image", "inspect"]:
                    output = json.dumps([{"Id": "sha256:fixture", "Os": "linux", "Architecture": "amd64"}])
                elif command[:2] == ["docker", "info"]:
                    output = json.dumps({"Architecture": "x86_64", "ServerVersion": "test", "OSType": "linux"})
                else:
                    return subprocess.CompletedProcess(command, failure)
                return subprocess.CompletedProcess(command, 0, stdout=output)

            argv = ["acceptance.py", "--image", "current", "--groups", group, "--report", str(report)]
            if group == "upgrade":
                argv += ["--legacy-image", "legacy"]
            with patch.object(sys, "argv", argv), patch("subprocess.run", side_effect=execute), \
                    patch("platform.system", return_value="Linux"), patch("platform.machine", return_value="x86_64"), \
                    patch("platform.release", return_value="test"), contextlib.redirect_stdout(io.StringIO()):
                try:
                    runpy.run_path(str(pathlib.Path(__file__).with_name("acceptance.py")), run_name="__main__")
                except SystemExit as error:
                    self.assertEqual(error.code, failure)
            evidence = json.loads(report.read_text())
        return calls, evidence

    def test_only_selected_group_runs_without_legacy_image(self):
        calls, evidence = self.run_gate("schedules")
        children = [c for c in calls if c[0] != "docker"]
        self.assertEqual(len(children), 1)
        self.assertEqual(pathlib.Path(children[0][4]).name, "smoke-schedules.py")
        self.assertEqual(evidence["selectedGroups"], ["schedules"])
        self.assertTrue(evidence["complete"])
        self.assertEqual(len(evidence["images"]), 1)

    def test_upgrade_inspects_and_passes_legacy_only_when_needed(self):
        calls, evidence = self.run_gate("upgrade")
        child = next(c for c in calls if c[0] != "docker")
        self.assertEqual(child[-2:], ["--legacy-image", "legacy"])
        self.assertEqual(len(evidence["images"]), 2)

    def test_failure_retains_report_and_does_not_claim_completion(self):
        _, evidence = self.run_gate("jobs", failure=7)
        self.assertFalse(evidence["complete"])
        self.assertFalse(evidence["checks"][0]["passed"])
        self.assertIn("seconds", evidence["checks"][0])


if __name__ == "__main__":
    unittest.main()
