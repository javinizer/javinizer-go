import json
from pathlib import Path
import shutil
import tempfile
import unittest
from unittest import mock

import stage_sync_entry_toolchain as STAGE


class ToolchainStageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.cache = self.root / "modules"
        self.source = self.cache / "official-go"
        for name, data in {"bin/go": b"unit-only binary", "src/net/dial.go": b"source",
                           "pkg/tool/compiler": b"compiler", "VERSION": b"go1.27.2", "LICENSE": b"license"}.items():
            target = self.source / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(data)
        (self.source / "bin/go").chmod(0o755)
        self.destination = self.root / "owned-go"
        self.receipt = self.root / "copy.json"

    def run_stage(self):
        return STAGE.stage(self.source, self.destination, self.cache, self.receipt)

    def test_whole_copy_preserves_bytes_modes_and_source(self):
        before = STAGE.inventory(self.source)
        result = self.run_stage()
        self.assertEqual(result["status"], "verified")
        self.assertEqual(before, STAGE.inventory(self.source))
        self.assertEqual(before, STAGE.inventory(self.destination))
        self.assertGreater(result["required_free_bytes"], result["source_bytes"] + STAGE.HEADROOM)

    def test_rejects_existing_destination(self):
        self.destination.mkdir()
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_rejects_cache_or_source_destination(self):
        for path in (self.cache / "copy", self.source / "copy"):
            with self.subTest(path=path):
                self.destination = path
                with self.assertRaises(ValueError):
                    self.run_stage()

    def test_rejects_symlink_and_incomplete_source(self):
        (self.source / "extra").symlink_to(self.source / "VERSION")
        with self.assertRaises(ValueError):
            self.run_stage()
        (self.source / "extra").unlink()
        (self.source / "VERSION").unlink()
        with self.assertRaises(ValueError):
            self.run_stage()

    def test_capacity_guard_records_failure_before_copy(self):
        with mock.patch.object(STAGE.shutil, "disk_usage", return_value=shutil._ntuple_diskusage(1, 1, 0)):
            with self.assertRaises(RuntimeError):
                self.run_stage()
        self.assertFalse(self.destination.exists())
        self.assertEqual(json.loads(self.receipt.read_text())["status"], "insufficient-capacity")

    def test_destination_permission_change_never_verifies(self):
        original = shutil.copytree
        def change_mode(*args, **kwargs):
            result = original(*args, **kwargs)
            if Path(args[0]) == self.source:
                (self.destination / "bin/go").chmod(0o600)
            return result
        with mock.patch.object(STAGE.shutil, "copytree", side_effect=change_mode):
            with self.assertRaises(RuntimeError):
                self.run_stage()
        self.assertEqual(json.loads(self.receipt.read_text())["status"], "failed")

    def test_partial_copy_failure_is_preserved_but_not_verified(self):
        def partial(*args, **kwargs):
            self.destination.mkdir()
            (self.destination / "partial").write_bytes(b"incomplete")
            raise OSError("simulated copy failure")
        with mock.patch.object(STAGE.shutil, "copytree", side_effect=partial):
            with self.assertRaises(OSError):
                self.run_stage()
        self.assertEqual(json.loads(self.receipt.read_text())["status"], "failed")
        self.assertTrue((self.destination / "partial").exists())

    def test_source_mutation_and_partial_copy_never_verify(self):
        original = shutil.copytree
        def mutate(*args, **kwargs):
            result = original(*args, **kwargs)
            if Path(args[0]) == self.source:
                (self.source / "VERSION").write_bytes(b"changed")
            return result
        with mock.patch.object(STAGE.shutil, "copytree", side_effect=mutate):
            with self.assertRaises(RuntimeError):
                self.run_stage()
        self.assertEqual(json.loads(self.receipt.read_text())["status"], "failed")
        self.assertTrue(self.destination.exists())


if __name__ == "__main__":
    unittest.main()
