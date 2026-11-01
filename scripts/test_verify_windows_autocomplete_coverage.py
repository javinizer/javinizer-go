"""Verifier unit fixtures are synthetic; they do not claim Windows execution."""
import copy
import json
import unittest
import pathlib
import subprocess
import sys
import tempfile

from verify_windows_autocomplete_coverage import PACKAGE, ROOT, SOURCE, verify


class AdmissionTests(unittest.TestCase):
    def setUp(self):
        self.head = "a" * 40
        self.metadata = {"checkout_sha": self.head, "workflow_sha": self.head, "goos": "windows", "exit": 0, "contract": "autocomplete", "event_name": "push", "declared_head_sha": self.head,
                         "tested_tree": "c" * 40, "raw_parents": ["d" * 40]}
        self.events = [{"Action": "pass", "Package": PACKAGE, "Test": name}
                       for name in [ROOT, ROOT + "/C:", ROOT + "/C:."]]
        self.events.append({"Action": "pass", "Package": PACKAGE})
        self.profile = "mode: atomic\n" + SOURCE + ":113.4,113.17 1 1\n"

    def check(self, profile=None, events=None, metadata=None):
        return verify(self.profile if profile is None else profile,
                      "\n".join(map(json.dumps, self.events if events is None else events)),
                      self.metadata if metadata is None else metadata, self.head, "push", self.head)

    def test_valid(self):
        self.assertEqual([113], self.check()["covered_lines"])

    def test_missing_or_skipped_nodes(self):
        for index in range(4):
            with self.subTest(missing=index), self.assertRaises(ValueError):
                self.check(events=self.events[:index] + self.events[index + 1:])
            events = copy.deepcopy(self.events)
            events[index]["Action"] = "skip"
            with self.subTest(skipped=index), self.assertRaises(ValueError):
                self.check(events=events)

    def test_wrong_provenance(self):
        for key, value in [("checkout_sha", "b" * 40), ("workflow_sha", "b" * 40), ("goos", "linux"), ("exit", 1)]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.check(metadata={**self.metadata, key: value})
        events = copy.deepcopy(self.events)
        events[0]["Package"] = PACKAGE + "wrong"
        with self.assertRaises(ValueError):
            self.check(events=events)

    def test_merge_provenance(self):
        source = "b" * 40
        meta = {**self.metadata, "event_name": "pull_request", "declared_head_sha": source,
                "declared_base_sha": "d" * 40, "raw_parents": ["d" * 40, source]}
        log = "\n".join(map(json.dumps, self.events))
        self.assertEqual(source, verify(self.profile, log, meta, self.head, "pull_request", source, "d" * 40)["declared_head_sha"])
        for bad in [{**meta, "raw_parents": [source, "d" * 40]},
                    {**meta, "declared_head_sha": self.head},
                    {**meta, "event_name": "push"},
                    {**meta, "declared_base_sha": "e" * 40}]:
            with self.subTest(metadata=bad), self.assertRaises(ValueError):
                verify(self.profile, log, bad, self.head, "pull_request", source, "d" * 40)

    def test_utf8_cli_roundtrip(self):
        with tempfile.TemporaryDirectory() as temp:
            root = pathlib.Path(temp) / "日本語"
            root.mkdir()
            profile, log, meta = [root / name for name in ["coverage.out", "tests.jsonl", "meta.json"]]
            profile.write_text(self.profile, encoding="utf-8")
            events = self.events + [{"Action": "output", "Package": PACKAGE, "Output": "日本語 fixture"}]
            log.write_text("\n".join(json.dumps(e, ensure_ascii=False) for e in events), encoding="utf-8")
            meta.write_text(json.dumps(self.metadata), encoding="utf-8")
            result = subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name("verify_windows_autocomplete_coverage.py")),
                                     str(profile), str(log), str(meta), self.head, "push", self.head, "", "autocomplete"],
                                    capture_output=True, text=True, encoding="utf-8")
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertEqual([113], json.loads(result.stdout)["covered_lines"])

    def test_umask_contract_and_mixed_evidence(self):
        package = "github.com/javinizer/javinizer-go/cmd/javinizer"
        root = "TestInitConfig_UmaskValid"
        profile = "mode: atomic\n" + package + "/root.go:231.5,231.83 1 1\n"
        events = [{"Action": "pass", "Package": package, "Test": root},
                  {"Action": "pass", "Package": package}]
        metadata = {**self.metadata, "contract": "umask"}
        def check(p=profile, es=None, meta=None, contract="umask"):
            return verify(p, "\n".join(map(json.dumps, events if es is None else es)),
                          metadata if meta is None else meta, self.head, "push", self.head, "", contract)
        self.assertEqual([231], check()["covered_lines"])
        for index in range(2):
            with self.subTest(missing=index), self.assertRaises(ValueError):
                check(es=events[:index] + events[index + 1:])
            skipped = copy.deepcopy(events)
            skipped[index]["Action"] = "skip"
            with self.subTest(skip=index), self.assertRaises(ValueError):
                check(es=skipped)
        for badprofile in [self.profile, profile + self.profile.splitlines()[1] + "\n", profile.replace("1 1", "1 0"), "mode: atomic\ngarbage\n"]:
            with self.subTest(profile=badprofile), self.assertRaises(ValueError):
                check(p=badprofile)
        for bad in [self.events, events + self.events,
                    [{**events[0], "Test": ROOT}, events[1]],
                    [{**events[0], "Package": PACKAGE}, events[1]]]:
            with self.subTest(events=bad), self.assertRaises(ValueError):
                check(es=bad)
        for selector in ["autocomplete", "unknown"]:
            with self.subTest(selector=selector), self.assertRaises(ValueError):
                check(contract=selector)
        with self.assertRaises(ValueError):
            check(meta=self.metadata)

    def test_invalid_profiles(self):
        for profile in ["", "mode: set\n", "mode: atomic\ngarbage\n",
                        self.profile.replace("1 1", "1 0"),
                        self.profile.replace("113", "112"),
                        self.profile.replace(SOURCE, SOURCE + "wrong"),
                        self.profile.replace(" 1 1", " 0 1"),
                        self.profile + self.profile.splitlines()[1] + "\n"]:
            with self.subTest(profile=profile), self.assertRaises(ValueError):
                self.check(profile=profile)

    def test_malformed_json_and_duplicate_events(self):
        with self.assertRaises(ValueError):
            verify(self.profile, "not json", self.metadata, self.head, "push", self.head)
        with self.assertRaises(ValueError):
            self.check(events=self.events + [self.events[0]])


if __name__ == "__main__":
    unittest.main()
