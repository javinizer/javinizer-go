"""Verifier unit fixtures are synthetic; they do not claim Windows execution."""
import copy
import json
import unittest
import pathlib
import subprocess
import sys
import tempfile

from verify_windows_autocomplete_coverage import PACKAGE, ROOT, SOURCE, verify, FSUTIL_PACKAGE, FSUTIL_ROOTS, FSUTIL_COMMAND, fsutil_environment


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
        for bad in [events + [events[-1]], self.events, events + self.events,
                    [{**events[0], "Test": ROOT}, events[1]],
                    [{**events[0], "Package": PACKAGE}, events[1]]]:
            with self.subTest(events=bad), self.assertRaises(ValueError):
                check(es=bad)
        for selector in ["autocomplete", "unknown"]:
            with self.subTest(selector=selector), self.assertRaises(ValueError):
                check(contract=selector)
        with self.assertRaises(ValueError):
            check(meta=self.metadata)

    def test_cleanup_and_catalog_contracts(self):
        cases = [
            ("cleanup", "github.com/javinizer/javinizer-go/cmd/javinizer", "cleanup_windows.go",
             ["TestCleanupStaleWindowsOldExe_RemovesStaleOld", "TestCleanupStaleWindowsOldExe_NoOpWhenAbsent"],
             [16, 17, 20, 21]),
            ("catalog", "github.com/javinizer/javinizer-go/internal/catalogname", "catalogname.go",
             ["TestLoadReadOnlyDetachedSnapshot"], [66]),
        ]
        for contract, package, filename, roots, lines in cases:
            source = package + "/" + filename
            # Synthetic admission fixtures only, never native execution evidence.
            rows = [f"{source}:{line}.1,{line}.2 1 1" for line in lines]
            profile = "mode: atomic\n" + "\n".join(rows) + "\n"
            events = [{"Action": "pass", "Package": package, "Test": root} for root in roots]
            events.append({"Action": "pass", "Package": package})
            metadata = {**self.metadata, "contract": contract}

            def check(p=profile, es=events, meta=metadata, selector=contract):
                return verify(p, "\n".join(map(json.dumps, es)), meta,
                              self.head, "push", self.head, "", selector)

            with self.subTest(contract=contract):
                self.assertEqual(lines, check()["covered_lines"])
                self.assertEqual(sorted(roots), check()["passed_tests"])
                for index in range(len(events)):
                    with self.subTest(missing=index), self.assertRaises(ValueError):
                        check(es=events[:index] + events[index + 1:])
                    for action in ["skip", "fail"]:
                        bad = copy.deepcopy(events)
                        bad[index]["Action"] = action
                        with self.subTest(index=index, action=action), self.assertRaises(ValueError):
                            check(es=bad)
                for index, row in enumerate(rows):
                    for replacement in [None, row[:-1] + "0"]:
                        badrows = rows.copy()
                        badrows[index:index + 1] = [] if replacement is None else [replacement]
                        with self.subTest(target=lines[index], replacement=replacement), self.assertRaises(ValueError):
                            check(p="mode: atomic\n" + "\n".join(badrows) + "\n")
                for bad in [events + [events[-1]], events + [events[0]], events + self.events,
                            [{**events[0], "Test": roots[0] + "/unexpected"}] + events[1:],
                            [{**events[0], "Package": package + "wrong"}] + events[1:]]:
                    with self.assertRaises(ValueError):
                        check(es=bad)
                for bad in [self.profile, profile + self.profile.splitlines()[1] + "\n",
                            profile.replace(source, source + "wrong"),
                            profile + rows[0] + "\n", profile.replace("mode: atomic", "mode: set")]:
                    with self.assertRaises(ValueError):
                        check(p=bad)
                for key, value in [("contract", "umask"), ("checkout_sha", "b" * 40),
                                   ("workflow_sha", "b" * 40), ("goos", "linux"), ("exit", 1),
                                   ("tested_tree", ""), ("declared_head_sha", "b" * 40)]:
                    with self.subTest(key=key), self.assertRaises(ValueError):
                        check(meta={**metadata, key: value})
                with self.assertRaises(ValueError):
                    check(selector="umask")
                # A PR receipt binds its checkout to the declared base/head order.
                head, base = "b" * 40, "d" * 40
                meta = {**metadata, "event_name": "pull_request", "declared_head_sha": head,
                        "declared_base_sha": base, "raw_parents": [base, head]}
                log = "\n".join(map(json.dumps, events))
                self.assertEqual(lines, verify(profile, log, meta, self.head,
                                              "pull_request", head, base, contract)["covered_lines"])
                with self.assertRaises(ValueError):
                    verify(profile, log, {**meta, "raw_parents": [head, base]}, self.head,
                           "pull_request", head, base, contract)

    def test_invalid_profiles(self):
        for profile in ["", "mode: set\n", "mode: atomic\ngarbage\n",
                        self.profile.replace("1 1", "1 0"),
                        self.profile.replace("113", "112"),
                        self.profile.replace(SOURCE, SOURCE + "wrong"),
                        self.profile.replace(" 1 1", " 0 1"),
                        self.profile + self.profile.splitlines()[1] + "\n"]:
            with self.subTest(profile=profile), self.assertRaises(ValueError):
                self.check(profile=profile)

    def test_duplicate_package_pass(self):
        with self.assertRaisesRegex(ValueError, "duplicate terminal package event"):
            self.check(events=self.events + [self.events[-1]])

    def test_malformed_json_and_duplicate_events(self):
        with self.assertRaises(ValueError):
            verify(self.profile, "not json", self.metadata, self.head, "push", self.head)
        with self.assertRaises(ValueError):
            self.check(events=self.events + [self.events[0]])


class FsutilShortPackageAdmissionTests(unittest.TestCase):
    """Synthetic receipts validate admission only, never claim native execution."""
    def setUp(self):
        self.head = "a" * 40
        self.env = {"GOFLAGS": "", "GOENV": "", "GOWORK": "off", "GOOS": "windows",
                    "GOHOSTOS": "windows", "GOARCH": "amd64", "GOHOSTARCH": "amd64", "GOVERSION": "go1.27.3"}
        self.meta = {"checkout_sha": self.head, "workflow_sha": self.head, "goos": "windows", "exit": 0,
                     "contract": "fsutil", "event_name": "push", "declared_head_sha": self.head,
                     "tested_tree": "c" * 40, "raw_parents": ["d" * 40],
                     "command": FSUTIL_COMMAND.copy(), "effective_go_env": self.env,
                     "input_go_overrides": {"GOFLAGS": "", "GOENV": "off", "GOWORK": "off"}}
        self.events = [{"Action": "pass", "Package": FSUTIL_PACKAGE, "Test": name} for name in sorted(FSUTIL_ROOTS)]
        self.events += [{"Action": "skip", "Package": FSUTIL_PACKAGE, "Test": "TestPOSIXOnly"},
                        {"Action": "pass", "Package": FSUTIL_PACKAGE}]
        self.profile = "mode: atomic\n" + FSUTIL_PACKAGE + "/move_crossdev_windows.go:13.2,15.54 1 3\n"

    def check(self, events=None, meta=None, profile=None):
        return verify(self.profile if profile is None else profile,
                      "\n".join(map(json.dumps, self.events if events is None else events)),
                      self.meta if meta is None else meta, self.head, "push", self.head, "", "fsutil")

    def test_complete_short_invocation_retains_explicit_skips_without_fixed_inventory(self):
        result = self.check()
        self.assertEqual(["TestPOSIXOnly"], result["observed_skipped_tests"])
        extra = {"Action": "pass", "Package": FSUTIL_PACKAGE, "Test": "TestFutureValidContract"}
        self.assertIn(extra["Test"], self.check(events=self.events[:-1] + [extra, self.events[-1]])["observed_passed_tests"])

    def test_environment_neutralizes_saved_or_ambient_filters_before_discovery(self):
        hostile = {"GOFLAGS": "-run=Nothing -skip=.* -overlay=other.json", "goenv": "host-settings",
                   "GOWORK": "foreign.work", "GOOS": "linux", "goarch": "arm64", "GOCACHE": "owned", "PATH": "tools"}
        clean = fsutil_environment(hostile)
        self.assertEqual({"GOFLAGS": "", "GOENV": "off", "GOWORK": "off", "GOCACHE": "owned", "PATH": "tools"}, clean)
        self.assertEqual("linux", hostile["GOOS"])

    def test_rejects_filtered_or_wrong_effective_command(self):
        for command in [FSUTIL_COMMAND + ["-run", ".*"], FSUTIL_COMMAND + ["-skip=Never"],
                        FSUTIL_COMMAND[:-1] + ["./internal/history"], [x for x in FSUTIL_COMMAND if x != "-short"],
                        [x.replace("-count=1", "-count=2") for x in FSUTIL_COMMAND]]:
            with self.subTest(command=command), self.assertRaises(ValueError):
                self.check(meta={**self.meta, "command": command})
        for key, value in [("GOFLAGS", "-run=Nothing"), ("GOENV", "saved"), ("GOWORK", "foreign"),
                           ("GOHOSTOS", "linux"), ("GOARCH", "arm64"), ("GOVERSION", "")]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.check(meta={**self.meta, "effective_go_env": {**self.env, key: value}})
        with self.assertRaises(ValueError):
            self.check(meta={**self.meta, "effective_go_env": {}})

    def test_disabled_saved_settings_input_differs_from_go_env_output(self):
        # cmd/go cfg.EnvFile returns an empty filename for GOENV=off;
        # envcmd reports that filename, not the literal input override.
        self.check()
        for effective in ["off", "/host/go/env"]:
            with self.subTest(effective=effective), self.assertRaises(ValueError):
                self.check(meta={**self.meta, "effective_go_env": {**self.env, "GOENV": effective}})
        for overrides in [None, {}, {"GOFLAGS": "", "GOENV": "", "GOWORK": "off"},
                          {"GOFLAGS": "-run=Nothing", "GOENV": "off", "GOWORK": "off"}]:
            with self.subTest(overrides=overrides), self.assertRaises(ValueError):
                self.check(meta={**self.meta, "input_go_overrides": overrides})

    def test_required_roots_and_subcases_cannot_skip_or_fail(self):
        for root in FSUTIL_ROOTS:
            index = next(i for i, e in enumerate(self.events) if e.get("Test") == root)
            for action in ["skip", "fail"]:
                events = copy.deepcopy(self.events); events[index]["Action"] = action
                with self.subTest(root=root, action=action), self.assertRaises(ValueError):
                    self.check(events=events)
            with self.subTest(missing=root), self.assertRaises(ValueError):
                self.check(events=self.events[:index] + self.events[index+1:])
        for action in ["skip", "fail"]:
            child = {"Package": FSUTIL_PACKAGE, "Test": "TestIsCrossDeviceError_Windows/nil_error", "Action": action}
            with self.subTest(child=action), self.assertRaises(ValueError):
                self.check(events=self.events[:-1] + [child, self.events[-1]])
        failed = {"Package": FSUTIL_PACKAGE, "Test": "TestUnselectedFailure", "Action": "fail"}
        with self.assertRaises(ValueError):
            self.check(events=self.events[:-1] + [failed, self.events[-1]])

    def test_requires_single_native_package_completion_and_real_counters(self):
        for events in [self.events[:-1], self.events + [self.events[-1]],
                       self.events + [self.events[0]], [{**self.events[0], "Package": "wrong"}] + self.events[1:]]:
            with self.subTest(events=events), self.assertRaises(ValueError):
                self.check(events=events)
        for profile in [self.profile.replace("1 3", "1 0"), self.profile.replace("13.2", "14.2"),
                        self.profile.replace("mode: atomic", "mode: set"), self.profile + self.profile.splitlines()[1] + "\n"]:
            with self.subTest(profile=profile), self.assertRaises(ValueError):
                self.check(profile=profile)
        for key, value in [("exit", 1), ("checkout_sha", "b" * 40), ("goos", "linux")]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.check(meta={**self.meta, key: value})


if __name__ == "__main__":
    unittest.main()
