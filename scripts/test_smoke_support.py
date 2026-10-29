"""Cleanup confinement and optional native-Linux ownership regressions."""
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from smoke_support import cleanup_fixture


class FixtureCase(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(dir=os.environ.get("NOX_CLEANUP_TEST_ROOT"))
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name).resolve()
        (self.root / ".tmp").mkdir()
        self.name = "cleanup-smoke-" + secrets.token_hex(6)
        self.base = self.root / ".tmp" / self.name
        self.base.mkdir()

    def clean(self):
        cleanup_fixture(self.base, self.root, self.name, "unused-test-image")


class FixtureCleanupTests(FixtureCase):
    def test_owned_tree_and_symlink_leave_sibling_untouched(self):
        sibling = self.root / ".tmp" / "other-fixture"
        sibling.mkdir()
        (sibling / "keep").write_text("keep")
        (self.base / "nested").mkdir()
        (self.base / "nested" / "file").write_text("remove")
        (self.base / "linked").symlink_to(sibling, target_is_directory=True)
        self.clean()
        self.assertFalse(self.base.exists())
        self.assertEqual((sibling / "keep").read_text(), "keep")
        self.clean()  # Already removed is safe.

    def test_outside_or_wrong_fixture_is_rejected(self):
        for base, name in ((self.root, self.root.name), (self.base, "other-fixture")):
            with self.assertRaises(ValueError):
                cleanup_fixture(base, self.root, name, "unused-test-image")
        self.assertTrue(self.base.exists())

    def test_symlink_target_is_rejected(self):
        self.base.rmdir()
        self.base.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.clean()
        self.assertTrue(self.root.exists())

    def test_symlink_temporary_root_is_rejected(self):
        self.base.rmdir()
        (self.root / ".tmp").rmdir()
        external = self.root / "external"
        external.mkdir()
        (self.root / ".tmp").symlink_to(external, target_is_directory=True)
        with self.assertRaises(ValueError):
            self.clean()

    def test_helper_failure_is_not_silenced(self):
        with mock.patch("shutil.rmtree", side_effect=PermissionError), mock.patch(
                "subprocess.run", return_value=subprocess.CompletedProcess([], 1, "", "unavailable")):
            with self.assertRaisesRegex(RuntimeError, "cleanup helper"):
                self.clean()
        self.assertTrue(self.base.exists())

    def test_helper_refuses_directory_still_mounted_by_container(self):
        responses = [subprocess.CompletedProcess([], 0, "other-container\n", ""),
                     subprocess.CompletedProcess([], 0, json.dumps([
                         {"Mounts": [{"Source": str(self.base / "protected")}]}]), "")]
        with mock.patch("shutil.rmtree", side_effect=PermissionError), mock.patch(
                "subprocess.run", side_effect=responses):
            with self.assertRaisesRegex(RuntimeError, "Remove containers"):
                self.clean()
        self.assertTrue(self.base.exists())


@unittest.skipUnless(sys.platform == "linux" and os.environ.get("NOX_CLEANUP_TEST_IMAGE"),
                     "set NOX_CLEANUP_TEST_IMAGE for native Linux Docker ownership checks")
class LinuxFixtureCleanupTests(FixtureCase):
    # Exercise real UID ownership independently of the portable unit checks.
    def docker(self, *parts):
        return subprocess.check_output(["docker", *parts], text=True).strip()

    def prepare_root_owned_data(self):
        self.image = os.environ["NOX_CLEANUP_TEST_IMAGE"]
        self.sibling = self.root / ".tmp" / "other-fixture"
        self.sibling.mkdir()
        (self.sibling / "keep").write_text("keep")
        (self.base / "linked").symlink_to(self.sibling, target_is_directory=True)
        self.docker("run", "--rm", "--pull", "never", "--network", "none",
                    "--user", "0:0", "--label", "nox-yard.acceptance=" + self.name,
                    "--mount", "type=bind,source=" + str(self.base) + ",target=/fixture",
                    "--entrypoint", "sh", self.image, "-ec",
                    "mkdir /fixture/protected; echo private > /fixture/protected/file; "
                    "chown 0:0 /fixture/protected; chmod 000 /fixture/protected")
        # The local harness can run as root inside Linux; reproduce GitHub's
        # non-root caller while retaining its Docker-socket group access.
        self.previous_uid = os.geteuid()
        if self.previous_uid == 0:
            for path in (self.root, self.root / ".tmp", self.base):
                os.chown(path, 1001, os.getegid())
                path.chmod(0o755)
            os.seteuid(1001)
        self.addCleanup(os.seteuid, self.previous_uid)

    def clean(self):
        cleanup_fixture(self.base, self.root, self.name,
                        os.environ["NOX_CLEANUP_TEST_IMAGE"])

    def test_nonroot_cleanup_of_actual_root_owned_bind_data(self):
        self.prepare_root_owned_data()
        with self.assertRaises(PermissionError):
            shutil.rmtree(self.base)
        self.clean()
        self.assertFalse(self.base.exists())
        self.assertEqual((self.sibling / "keep").read_text(), "keep")

    def test_actual_mount_blocks_privileged_cleanup(self):
        self.prepare_root_owned_data()
        identifier = self.docker("create", "--pull", "never", "--network", "none",
                                 "--label", "nox-yard.acceptance=" + self.name,
                                 "--mount", "type=bind,source=" + str(self.base) + ",target=/fixture",
                                 "--entrypoint", "sh", self.image, "-c", "true")
        try:
            with self.assertRaisesRegex(RuntimeError, "Remove containers"):
                self.clean()
            self.assertTrue((self.base / "protected").exists())
        finally:
            self.docker("rm", identifier)
            self.clean()
        self.assertEqual((self.sibling / "keep").read_text(), "keep")


if __name__ == "__main__":
    unittest.main()
