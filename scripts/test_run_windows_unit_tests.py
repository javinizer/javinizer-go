"""Offline contract tests for the Windows CI package orchestrator."""

import json
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock

import run_windows_unit_tests as runner


PACKAGES = [runner.BATCH, runner.WORKFLOW, "example/rest", "example/no-tests"]


def passed(members):
    return [{"Action": "skip" if p.endswith("no-tests") else "pass", "Package": p}
            for p in members]


class WindowsUnitRunnerTests(unittest.TestCase):
    def test_partition_is_disjoint_and_exhaustive(self):
        groups = runner.partition(PACKAGES)
        self.assertEqual([p for _, members in groups for p in members], PACKAGES)
        for invalid in ([], PACKAGES + [runner.BATCH], PACKAGES[1:], PACKAGES[:2], PACKAGES + [""]):
            with self.subTest(invalid=invalid), self.assertRaises(ValueError):
                runner.partition(invalid)

    def test_cached_and_no_test_package_terminals_are_valid(self):
        events = passed(PACKAGES)
        events[0]["Output"] = "ok (cached)"
        self.assertEqual(runner.validate_events(events, PACKAGES), PACKAGES)

    def test_rejects_incomplete_duplicate_extra_and_failed_results(self):
        cases = [passed(PACKAGES)[:-1], passed(PACKAGES) + passed([runner.BATCH]),
                 passed(PACKAGES) + passed(["extra"]),
                 passed(PACKAGES) + [{"Action": "fail", "Package": runner.BATCH, "Test": "nested/test"}],
                 [{"Action": "fail", "Package": p} for p in PACKAGES],
                 [None], [3], [[]], [{"Action": "pass", "Package": []}],
                 passed(PACKAGES) + [{"Action": "garbage"}],
                 passed(PACKAGES) + [{"Action": "output"}],
                 passed(PACKAGES) + [{"Action": [], "Package": runner.BATCH}],
                 passed(PACKAGES) + [{"Action": "pass", "Package": runner.BATCH, "Test": 0}],
                 passed(PACKAGES) + [{"Action": "build-output", "ImportPath": 2, "Output": "warning"}],
                 passed(PACKAGES) + [{"Action": "build-output", "ImportPath": "dependency"}],
                 passed(PACKAGES) + [{"Action": "build-fail", "ImportPath": "dependency"}]]
        for events in cases:
            with self.subTest(events=events), self.assertRaises(ValueError):
                runner.validate_events(events, PACKAGES)

    def test_compiler_warnings_do_not_count_as_package_results(self):
        for path in ("dependency", ""):
            events = passed(PACKAGES) + [{"Action": "build-output", "ImportPath": path, "Output": "warning\n"}]
            self.assertEqual(runner.validate_events(events, PACKAGES), PACKAGES)

    def test_stale_evidence_is_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FileExistsError), mock.patch.object(runner.subprocess, "run") as execute:
                runner.run(pathlib.Path(directory))
            execute.assert_not_called()

    def exercise(self, defect=None):
        calls = []

        def fake_run(command, **kwargs):
            calls.append(command)
            if command[1] == "list":
                return subprocess.CompletedProcess(command, 1 if defect == "inventory-exit" else 0,
                                                   "\n".join(PACKAGES) + "\n", "")
            members = command[5:]
            self.assertEqual(command[:5], ["go", "test", "-short", "-timeout=10m", "-json"])
            events = passed(members)
            code = 0
            if members == [runner.BATCH]:
                if defect == "launch":
                    raise OSError("simulated launch failure")
                if defect == "json":
                    kwargs["stdout"].write("not JSON\n")
                    return subprocess.CompletedProcess(command, 0)
                if defect == "schema":
                    events = [None]
                if defect == "missing":
                    events = []
                if defect == "duplicate":
                    events += passed(members)
                if defect == "fail":
                    events = [{"Action": "fail", "Package": runner.BATCH}]
                if defect == "exit":
                    code = 1
            for event in events:
                kwargs["stdout"].write(json.dumps(event) + "\n")
            return subprocess.CompletedProcess(command, code)

        with tempfile.TemporaryDirectory() as directory:
            evidence = pathlib.Path(directory) / "evidence"
            with mock.patch.object(runner.subprocess, "run", side_effect=fake_run):
                result = runner.run(evidence)
            summary = json.loads((evidence / "summary.json").read_text())
            if defect == "inventory-exit":
                self.assertEqual(len(calls), 1)
            else:
                self.assertEqual(len(calls), 4, "all three groups must run even after failure")
                self.assertEqual(calls[-1][5:], PACKAGES[2:])
                proof = json.loads((evidence / "partition.json").read_text())
                self.assertTrue(proof["unique"] and proof["disjoint"] and proof["exhaustive"])
                self.assertTrue((evidence / "rest.jsonl").is_file())
                self.assertEqual((evidence / "rest.exit").read_text(), "0\n")
            self.assertEqual(result, int(defect is not None))
            self.assertEqual(bool(summary["errors"]), defect is not None)
            if defect is None:
                self.assertEqual(summary["validated_terminals"], PACKAGES)

    def test_complete_success(self):
        self.exercise()

    def test_failed_groups_continue_and_leave_failure_evidence(self):
        for defect in ("launch", "json", "schema", "missing", "duplicate", "fail", "exit", "inventory-exit"):
            with self.subTest(defect=defect):
                self.exercise(defect)


if __name__ == "__main__":
    unittest.main()
