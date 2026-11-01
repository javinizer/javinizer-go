"""Synthetic converter-control tests; never treated as native Go execution proof."""
import hashlib
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

import sync_entry_auth as AUTH


class CounterReplayTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.archive = self.root / "archive"
        (self.archive / "counters").mkdir(parents=True)
        self.profile = b"mode: atomic\npackage/main.go:1.1,1.2 1 1\n"
        (self.archive / "coverage-sync-entry.out").write_bytes(self.profile)
        self.go = self.root / "trusted/bin/go"
        self.covdata = self.root / "trusted/pkg/tool/linux_amd64/covdata"
        for path in (self.go, self.covdata):
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(b"unit placeholder, never executed")
            path.chmod(0o700)
        self.digest = hashlib.sha256(self.go.read_bytes()).hexdigest()

    def replay(self):
        return AUTH.replay_counters(self.archive, self.go, self.digest, self.covdata, self.digest)

    def fake_run(self, command, **kwargs):
        self.assertEqual(kwargs["env"]["GOTOOLCHAIN"], "local")
        self.assertNotIn("GOROOT", kwargs["env"])
        self.assertEqual(kwargs["env"]["GOFLAGS"], "")
        self.assertNotIn(str(self.archive), str(kwargs["cwd"]))
        if command[1] == "env":
            return subprocess.CompletedProcess(command, 0, b"go1.27.2\n", b"")
        if command[1] == "version":
            return subprocess.CompletedProcess(command, 0, (str(self.covdata) + ": go1.27.2\n\tpath\tcmd/covdata\n").encode(), b"")
        self.assertEqual(command[0], str(self.covdata))
        Path(command[-1][3:]).write_bytes(self.profile)
        return subprocess.CompletedProcess(command, 0, b"", b"")

    def test_replays_to_owned_scratch_and_compares_without_overwriting(self):
        with mock.patch.object(AUTH.subprocess, "run", side_effect=self.fake_run):
            self.assertEqual(self.replay()["exit"], 0)
        self.assertEqual((self.archive / "coverage-sync-entry.out").read_bytes(), self.profile)

    def test_rejects_changed_text_counts(self):
        (self.archive / "coverage-sync-entry.out").write_bytes(self.profile.replace(b"1 1", b"1 2"))
        with mock.patch.object(AUTH.subprocess, "run", side_effect=self.fake_run):
            with self.assertRaisesRegex(ValueError, "disagree"):
                self.replay()

    def test_rejects_invalid_raw_conversion(self):
        def fail(command, **kwargs):
            if command[1] in {"env", "version"}:
                return self.fake_run(command, **kwargs)
            return subprocess.CompletedProcess(command, 1, b"", b"invalid metadata")
        with mock.patch.object(AUTH.subprocess, "run", side_effect=fail):
            with self.assertRaisesRegex(ValueError, "conversion failed"):
                self.replay()

    def test_rejects_untrusted_hash_archive_tool_and_wrong_version(self):
        with mock.patch.object(AUTH.subprocess, "run") as run:
            self.digest = "0" * 64
            with self.assertRaisesRegex(ValueError, "hash"):
                self.replay()
            run.assert_not_called()
        self.digest = hashlib.sha256(self.go.read_bytes()).hexdigest()
        with mock.patch.object(AUTH.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, b"go1.27.3\n", b"")):
            with self.assertRaisesRegex(ValueError, "reviewed"):
                self.replay()
        self.go = self.archive / "binary"
        self.go.write_bytes(b"unit placeholder, never executed")
        self.go.chmod(0o700)
        with self.assertRaisesRegex(ValueError, "outside"):
            self.replay()

    def test_pins_covdata_binary_separately(self):
        self.covdata.write_bytes(b"changed converter")
        with mock.patch.object(AUTH.subprocess, "run") as run:
            with self.assertRaisesRegex(ValueError, "covdata hash"):
                self.replay()
            run.assert_not_called()


if __name__ == "__main__":
    unittest.main()
