#!/usr/bin/env python3
"""Negative evidence-admission tests, not production execution evidence."""
import hashlib
import importlib.util
import json
import os
import shutil
import subprocess
from pathlib import Path
import tempfile
import unittest
from unittest import mock

import run_sync_entry_coverage as RUNNER
import sync_entry_auth as AUTH

SPEC = importlib.util.spec_from_file_location("verify_sync_entry_coverage", Path(__file__).with_name("verify_sync_entry_coverage.py"))
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


class EntryEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "counters").mkdir()
        self.commit = "a" * 40
        package = MODULE.PACKAGE
        canaries = ["TestExternalBeforeDNS", "TestDirectDialAndUDPWrites", "TestLoopbackAllowed", "TestHTTPExternalTargetBlockedBeforeProxy", "TestHTTPExternalTargetBlockedBeforeSOCKS", "TestInfrastructureProxyEndpointBlocked", "TestHTTPLoopbackAllowed"]
        data = {"binary": b"unit-test placeholder, not runtime proof", "build.stdout": b"", "build.stderr": b"", "child.stdout": b"", "child.stderr": b"2026/11/01 08:00:00 fixture marker must be a fixed regular file\n", "child.audit": b"", "canary.stdout": ("\n".join(json.dumps({"Action": "pass", "Test": t, "Package": MODULE.GUARD_PACKAGE}) for t in canaries) + "\n").encode(), "canary.stderr": b"", "canary.audit": b"OFFLINE279_BLOCKED\n", "counters/covmeta.fixture": b"unit metadata", "counters/covcounters.fixture": b"unit counters", "coverage-sync-entry.out": ("mode: atomic\n" + package + "/main.go:38.2,38.30 1 1\n" + package + "/main.go:39.3,40.1 1 1\n").encode()}
        self.proof = {"commit": self.commit, "build_exit": 0, "convert_exit": 0, "child_exit": 1, "build_command": ["go", "build", "-p=1", "-cover", "-covermode=atomic", "-coverpkg=" + package, "-o", str(self.root / "binary"), "./cmd/javinizer-sync-e2e"], "child_command": [str(self.root / "binary")], "convert_command": ["go", "tool", "covdata", "textfmt", "-i=" + str(self.root / "counters"), "-o=" + str(self.root / "coverage-sync-entry.out")], "before": {".": {"mode": 0o700, "inode": 1}, "foreign.txt": {"mode": 0o600, "inode": 2, "sha256": "original"}}, "files": {}}
        self.proof["after"] = json.loads(json.dumps(self.proof["before"]))
        data["canary.stdout"] += (json.dumps({"Action": "pass", "Package": MODULE.GUARD_PACKAGE}) + "\n").encode()
        for name, content in data.items():
            self.put(name, content)
        self.proof.update(execution_root=str(self.root), repo="/unit/repo", tree="b" * 40, canary_exit=0)
        self.go = "/unit/go/bin/go"
        for key in ("build_command", "convert_command"):
            self.proof[key][0] = self.go
        toolchain = {"GOVERSION": "go1.27.2", "GOROOT": "/unit/go", "GOCACHE": "/unit/cache", "GOMODCACHE": "/unit/modules", "GOOS": "linux", "GOHOSTOS": "linux", "GOARCH": "amd64", "GOHOSTARCH": "amd64", "CGO_ENABLED": "1", "go_executable": self.go, "go_sha256": "c" * 64, "native_machine": "x86_64"}
        toolchain["discovery_command"] = [self.go, "env", "-json", "GOVERSION", "GOROOT", "GOCACHE", "GOMODCACHE", "GOOS", "GOARCH", "GOHOSTOS", "GOHOSTARCH", "CGO_ENABLED"]
        self.put_json("toolchain.json", toolchain)
        environments = {}
        for stage in ("overlay", "canary", "build", "child", "convert"):
            env = {"PATH": "/unit/go/bin:/usr/bin", "GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GONOPROXY": "none", "GOSUMDB": "off", "GOVCS": "*:off", "GOMAXPROCS": "2", "GODEBUG": "netdns=go"}
            env.update({k: toolchain[k] for k in ("GOROOT", "GOCACHE", "GOMODCACHE", "GOOS", "GOARCH", "CGO_ENABLED")})
            env.update({k: str(self.root / n) for k, n in [("HOME", "home"), ("XDG_CONFIG_HOME", "config"), ("XDG_CACHE_HOME", "cache"), ("XDG_DATA_HOME", "data"), ("TMPDIR", "tmp"), ("GOTMPDIR", "tmp")]})
            env["GOFLAGS"] = "" if stage == "overlay" else "-overlay=" + str(self.root / "overlay/overlay.json")
            if stage != "overlay":
                env["OFFLINE279_AUDIT_LOG"] = str(self.root / ("canary.audit" if stage == "canary" else "child.audit"))
            if stage == "child":
                env.update(GOCOVERDIR=str(self.root / "counters"), JAVINIZER_SYNC_E2E_ROOT=str(self.root / "fixture"))
            environments[stage] = env
            self.put(stage + ".exit", ("1\n" if stage == "child" else "0\n").encode())
        self.put_json("environment.json", environments)
        overlay, original_hashes, replacement_hashes, rows = {}, {}, {}, []
        for name in ("dial.go", "lookup.go", "tcpsock_posix.go", "udpsock_posix.go", "iprawsock_posix.go", "sock_posix.go", "http/transport.go"):
            replacement = "overlay/" + name
            self.put(replacement, ("unit overlay " + name).encode())
            original = "/unit/go/src/net/" + name
            original_hashes[name] = hashlib.sha256(("unit original " + name).encode()).hexdigest()
            replacement_hashes[name] = self.proof["files"][replacement]
            overlay[original] = str(self.root / replacement)
            self.put("stdlib-originals/" + name, ("unit original " + name).encode())
            rows.append({"original_snapshot": "stdlib-originals/" + name, "original": original, "replacement": replacement, "original_sha256": original_hashes[name], "replacement_sha256": replacement_hashes[name]})
        self.put_json("overlay/overlay.json", {"Replace": overlay})
        self.put_json("overlay/manifest.json", {"go_version": "go1.27.2", "source_sha256": original_hashes, "overlay_sha256": replacement_hashes})
        self.put("stdlib-originals/LICENSE", b"unit license attribution placeholder")
        self.put_json("overlay-proof.json", rows)
        commands = [{"stage": "overlay", "cwd": self.proof["repo"], "command": ["python3", "/unit/repo/scripts/build_offline_test_overlay.py", "--output", str(self.root / "overlay")]}, {"stage": "canary", "cwd": "/unit/repo/test/offlineguard", "command": [self.go, "test", "-short", "-p=1", "-count=1", "-timeout=120s", "-json", "./..."]}]
        commands += [{"stage": stage, "cwd": self.proof["repo"], "command": self.proof[stage + "_command"]} for stage in ("build", "child", "convert")]
        self.put_json("commands.json", commands)
        source_files = {}
        for name in ("go.mod", "go.sum", "cmd/javinizer-sync-e2e/main.go", "cmd/javinizer-sync-e2e/fixture.go", "cmd/javinizer-sync-e2e/gate.go", "scripts/build_offline_test_overlay.py", "scripts/run_sync_entry_coverage.py", "scripts/verify_sync_entry_coverage.py", "scripts/sync_entry_auth.py", "test/offlineguard/go.mod", "test/offlineguard/guard_test.go", "test/offlineguard/proxy_test.go"):
            data = ("unit source " + name).encode()
            self.put("source/" + name, data)
            source_files[name] = {"sha256": hashlib.sha256(data).hexdigest(), "git_blob": hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()}
        objects, hierarchy = {}, {}
        for name, row in source_files.items():
            node = hierarchy
            parts = name.split("/")
            for part in parts[:-1]:
                node = node.setdefault(part, {})
            node[parts[-1]] = row["git_blob"]
        def obj(kind, data):
            oid = AUTH.git_hash(kind, data)
            self.put("git-objects/" + oid, data)
            objects[oid] = {"type": kind, "artifact": "git-objects/" + oid}
            return oid
        def make_tree(node):
            raw = b""
            for name, child in sorted(node.items()):
                mode, oid = ("40000", make_tree(child)) if isinstance(child, dict) else ("100644", child)
                raw += mode.encode() + b" " + name.encode() + b"\0" + bytes.fromhex(oid)
            return obj("tree", raw)
        self.proof["tree"] = make_tree(hierarchy)
        self.commit = obj("commit", ("tree " + self.proof["tree"] + "\n\nSynthetic schema fixture, not execution proof.\n").encode())
        self.proof["commit"] = self.commit
        self.put_json("source-proof.json", {"commit": self.commit, "tree": self.proof["tree"], "repo": self.proof["repo"], "files": source_files, "git_objects": objects})
        self.put_json("refusal.json", {"exit": 1, "before": self.proof["before"], "after": self.proof["after"]})

    def put(self, name, data):
        (self.root / name).parent.mkdir(parents=True, exist_ok=True)
        (self.root / name).write_bytes(data)
        self.proof["files"][name] = hashlib.sha256(data).hexdigest()

    def put_json(self, name, obj):
        self.put(name, (json.dumps(obj) + "\n").encode())

    def check(self):
        (self.root / "receipt.json").write_text(json.dumps(self.proof))
        return self.schema_check(self.root)

    def schema_check(self, directory):
        # Synthetic schema tests do not claim real counter execution. Replay
        # trust/error behavior is tested separately; hosted proof uses real tools.
        with mock.patch.object(MODULE, "replay_counters", return_value={"unit_mock": True}):
            return MODULE.verify(directory, self.commit, Path("/trusted/go"), "c" * 64, Path("/trusted/covdata"), "d" * 64)

    def test_rejects_self_consistent_source_not_in_expected_commit(self):
        source = json.loads((self.root / "source-proof.json").read_text())
        name = "cmd/javinizer-sync-e2e/main.go"
        data = b"self-consistent forged source"
        self.put("source/" + name, data)
        source["files"][name] = {"sha256": hashlib.sha256(data).hexdigest(), "git_blob": AUTH.git_hash("blob", data)}
        self.put_json("source-proof.json", source)
        with self.assertRaisesRegex(ValueError, "member"):
            self.check()

    def test_rejects_archive_relabelled_as_external_commit(self):
        self.commit = "f" * 40
        self.proof["commit"] = self.commit
        source = json.loads((self.root / "source-proof.json").read_text())
        source["commit"] = self.commit
        self.put_json("source-proof.json", source)
        with self.assertRaisesRegex(ValueError, "ancestry"):
            self.check()

    def test_rejects_tampered_git_tree_even_with_manifest_rehashed(self):
        source = json.loads((self.root / "source-proof.json").read_text())
        self.put("git-objects/" + source["tree"], b"bad tree")
        with self.assertRaisesRegex(ValueError, "object hash"):
            self.check()

    def test_accepts_consistent_schema(self):
        self.assertEqual(self.check()["main_lines_hit"], [38, 39])

    def test_rejects_wrong_source(self):
        self.proof["commit"] = "b" * 40
        with self.assertRaisesRegex(ValueError, "commit"):
            self.check()

    def test_rejects_wrong_exit(self):
        self.proof["child_exit"] = 0
        with self.assertRaisesRegex(ValueError, "exit"):
            self.check()

    def test_rejects_nonproduction_command(self):
        self.proof["child_command"].append("-test.run=FakeMain")
        with self.assertRaisesRegex(ValueError, "command receipt mismatch"):
            self.check()

    def test_rejects_fixture_mutation(self):
        self.proof["after"]["foreign.txt"]["inode"] = 99
        with self.assertRaisesRegex(ValueError, "refusal receipt mismatch"):
            self.check()

    def test_rejects_wrong_refusal_or_network_or_stdout(self):
        for name, data in [("child.stderr", b"other failure\n"), ("child.audit", b"OFFLINE279_BLOCKED\n"), ("child.stdout", b"ready\n")]:
            with self.subTest(name=name):
                original = (self.root / name).read_bytes()
                self.put(name, data)
                with self.assertRaises(ValueError):
                    self.check()
                self.put(name, original)

    def test_rejects_missing_counters(self):
        (self.root / "counters/covcounters.fixture").unlink()
        self.proof["files"].pop("counters/covcounters.fixture")
        with self.assertRaisesRegex(ValueError, "counter"):
            self.check()

    def test_rejects_unhit_or_malformed_profile(self):
        for content in ["mode: atomic\n" + MODULE.PACKAGE + "/main.go:38.2,40.1 2 0\n", "mode: set\n", "mode: atomic\nbad\n"]:
            self.put("coverage-sync-entry.out", content.encode())
            with self.assertRaises(ValueError):
                self.check()

    def test_rejects_tampered_artifact(self):
        (self.root / "binary").write_bytes(b"changed")
        with self.assertRaisesRegex(ValueError, "hash"):
            self.check()

    def test_rejects_skipped_canary(self):
        self.put("canary.stdout", b'{"Action":"skip","Test":"TestExternalBeforeDNS"}\n')
        with self.assertRaisesRegex(ValueError, "guard"):
            self.check()

    def test_rejects_unsafe_manifest_path(self):
        self.proof["files"]["../outside"] = "0" * 64
        with self.assertRaisesRegex(ValueError, "unsafe"):
            self.check()

    def test_rejects_canary_wrong_package_missing_package_or_duplicate(self):
        original = (self.root / "canary.stdout").read_bytes()
        events = [json.loads(line) for line in original.splitlines()]
        wrong = [{**event, "Package": "wrong/package"} for event in events]
        for mutated in (wrong, events[:-1], events + [events[0]], events + [{"Action": "pass", "Test": "Unexpected", "Package": MODULE.GUARD_PACKAGE}], [{**events[0], "Action": "fail"}, *events[1:]]):
            self.put("canary.stdout", ("\n".join(json.dumps(e) for e in mutated) + "\n").encode())
            with self.assertRaisesRegex(ValueError, "guard"):
                self.check()
        self.put("canary.stdout", original)

    def test_rejects_missing_required_attribution(self):
        for name in ("environment.json", "overlay-proof.json", "overlay/manifest.json", "commands.json", "toolchain.json", "canary.exit", "build.exit", "child.exit", "convert.exit"):
            digest = self.proof["files"].pop(name)
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "required"):
                self.check()
            self.proof["files"][name] = digest

    def test_rejects_ambient_workspace_flags_or_wrong_audit(self):
        original = json.loads((self.root / "environment.json").read_text())
        for key, value in (("GOENV", "/ambient/config"), ("GOWORK", "/ambient/go.work"), ("GOFLAGS", "-overlay=/ambient/replacement.json"), ("OFFLINE279_AUDIT_LOG", "/wrong/audit"), ("GOARCH", "arm64")):
            mutated = json.loads(json.dumps(original))
            mutated["build"][key] = value
            self.put_json("environment.json", mutated)
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "environment"):
                self.check()
        self.put_json("environment.json", original)

    def test_rejects_overlay_hash_or_mapping_mismatch(self):
        rows = json.loads((self.root / "overlay-proof.json").read_text())
        for key, value in (("original_sha256", "0" * 64), ("replacement_sha256", "0" * 64), ("replacement", "outside.go")):
            mutated = json.loads(json.dumps(rows))
            mutated[0][key] = value
            self.put_json("overlay-proof.json", mutated)
            with self.assertRaisesRegex(ValueError, "overlay"):
                self.check()
        self.put("stdlib-originals/LICENSE", b"unit license attribution placeholder")
        self.put_json("overlay-proof.json", rows)

    def test_rejects_exit_file_or_command_mismatch(self):
        self.put("canary.exit", b"1\n")
        with self.assertRaisesRegex(ValueError, "exit"):
            self.check()
        self.put("canary.exit", b"0\n")
        commands = json.loads((self.root / "commands.json").read_text())
        commands[-1]["command"] = [self.go, "version"]
        self.put_json("commands.json", commands)
        source_files = {}
        for name in ("go.mod", "go.sum", "cmd/javinizer-sync-e2e/main.go", "cmd/javinizer-sync-e2e/fixture.go", "cmd/javinizer-sync-e2e/gate.go", "scripts/build_offline_test_overlay.py", "scripts/run_sync_entry_coverage.py", "scripts/verify_sync_entry_coverage.py", "scripts/sync_entry_auth.py", "test/offlineguard/go.mod", "test/offlineguard/guard_test.go", "test/offlineguard/proxy_test.go"):
            data = ("unit source " + name).encode()
            self.put("source/" + name, data)
            source_files[name] = {"sha256": hashlib.sha256(data).hexdigest(), "git_blob": hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()}
        objects, hierarchy = {}, {}
        for name, row in source_files.items():
            node = hierarchy
            parts = name.split("/")
            for part in parts[:-1]:
                node = node.setdefault(part, {})
            node[parts[-1]] = row["git_blob"]
        def obj(kind, data):
            oid = AUTH.git_hash(kind, data)
            self.put("git-objects/" + oid, data)
            objects[oid] = {"type": kind, "artifact": "git-objects/" + oid}
            return oid
        def make_tree(node):
            raw = b""
            for name, child in sorted(node.items()):
                mode, oid = ("40000", make_tree(child)) if isinstance(child, dict) else ("100644", child)
                raw += mode.encode() + b" " + name.encode() + b"\0" + bytes.fromhex(oid)
            return obj("tree", raw)
        self.proof["tree"] = make_tree(hierarchy)
        self.commit = obj("commit", ("tree " + self.proof["tree"] + "\n\nSynthetic schema fixture, not execution proof.\n").encode())
        self.proof["commit"] = self.commit
        self.put_json("source-proof.json", {"commit": self.commit, "tree": self.proof["tree"], "repo": self.proof["repo"], "files": source_files, "git_objects": objects})
        with self.assertRaisesRegex(ValueError, "command"):
            self.check()

    def test_rejects_cross_compile_toolchain(self):
        toolchain = json.loads((self.root / "toolchain.json").read_text())
        toolchain["GOARCH"] = "arm64"
        self.put_json("toolchain.json", toolchain)
        with self.assertRaisesRegex(ValueError, "native Linux"):
            self.check()

    def test_copied_artifact_preserves_original_execution_attribution(self):
        self.check()
        with tempfile.TemporaryDirectory() as other:
            copied = Path(other) / "downloaded-artifact"
            shutil.copytree(self.root, copied)
            # Relocation needs no original source paths; trusted external converter
            # access is explicit and mocked only for this synthetic schema test.
            shutil.rmtree(self.root / "source")
            shutil.rmtree(self.root / "stdlib-originals")
            self.assertEqual(self.schema_check(copied)["main_lines_hit"], [38, 39])

    def test_rejects_mismatched_execution_root(self):
        self.proof["execution_root"] = "/different/execution"
        with self.assertRaisesRegex(ValueError, "environment"):
            self.check()

    def test_rejects_parent_symlink_escape_and_noncanonical_paths(self):
        with tempfile.TemporaryDirectory() as other:
            foreign = Path(other) / "entry"
            foreign.write_bytes(b"foreign")
            (self.root / "escape").symlink_to(other, target_is_directory=True)
            for name in ("escape/entry", "./binary", "overlay//dial.go", "source/../binary", ""):
                self.proof["files"][name] = hashlib.sha256(b"foreign").hexdigest()
                with self.subTest(name=name), self.assertRaisesRegex(ValueError, "unsafe"):
                    self.check()
                del self.proof["files"][name]

    def test_rejects_missing_or_mismatched_original_snapshot(self):
        name = "stdlib-originals/dial.go"
        digest = self.proof["files"].pop(name)
        with self.assertRaisesRegex(ValueError, "original snapshot"):
            self.check()
        self.proof["files"][name] = digest
        self.put(name, b"different original")
        with self.assertRaisesRegex(ValueError, "original snapshot"):
            self.check()

    def test_rejects_source_snapshot_hash_blob_or_root_mismatch(self):
        original = json.loads((self.root / "source-proof.json").read_text())
        for field in ("sha256", "git_blob", "repo"):
            mutated = json.loads(json.dumps(original))
            if field == "repo":
                mutated[field] = "/foreign/repo"
            else:
                mutated["files"]["go.mod"][field] = "0" * (64 if field == "sha256" else 40)
            self.put_json("source-proof.json", mutated)
            with self.subTest(field=field), self.assertRaisesRegex(ValueError, "source"):
                self.check()
        self.put_json("source-proof.json", original)

    def test_hostile_inherited_git_routing_cannot_redirect_attribution(self):
        selected = self.root / "selected-repo"
        foreign = self.root / "foreign-repo"
        home = self.root / "isolated-home"
        home.mkdir()
        clean = {"PATH": os.environ["PATH"], "HOME": str(home)}
        for repo in (selected, foreign):
            subprocess.run(["git", "init", "--quiet", str(repo)], env=clean, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        hostile = {**clean, "GIT_DIR": str(foreign / ".git"), "GIT_WORK_TREE": str(foreign), "GIT_INDEX_FILE": str(foreign / ".git/alternate-index"), "GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.worktree", "GIT_CONFIG_VALUE_0": str(foreign), "GOENV": "/ambient/config", "GOWORK": "/ambient/work", "GOFLAGS": "-overlay=/ambient/source"}
        with mock.patch.dict(os.environ, hostile, clear=True):
            sanitized = RUNNER.initial_environment()
            self.assertFalse(any(key.startswith("GIT_") for key in sanitized))
            self.assertEqual((sanitized["GOENV"], sanitized["GOWORK"], sanitized["GOFLAGS"]), ("off", "off", ""))
            self.assertEqual(RUNNER.git_output(selected, sanitized, "rev-parse", "--show-toplevel").decode().strip(), str(selected))
            (selected / "owned.go").write_text("package main\n")
            self.assertEqual(RUNNER.git_output(selected, sanitized, "ls-files", "--others", "--", "*.go").decode().strip(), "owned.go")


class ToolchainDiagnosticTests(unittest.TestCase):
    def test_receipt_survives_every_rejected_admission_predicate(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            go = root / "go-root/bin/go"
            go.parent.mkdir(parents=True)
            go.write_bytes(b"unit-only executable identity; never executed")
            good = {"GOVERSION": "go1.27.2", "GOROOT": str(go.parent.parent),
                    "GOOS": "linux", "GOHOSTOS": "linux", "GOARCH": "amd64",
                    "GOHOSTARCH": "amd64", "CGO_ENABLED": "1"}
            cases = [("GOVERSION", "go1.27.3"), ("GOOS", "windows"),
                     ("GOHOSTOS", "windows"), ("GOARCH", "arm64"),
                     ("GOHOSTARCH", "arm64"), ("CGO_ENABLED", "0"),
                     ("GOROOT", str(root / "other-go")), ("native_machine", "unknown")]
            for key, value in cases:
                with self.subTest(key=key):
                    observed = dict(good)
                    machine = value if key == "native_machine" else "x86_64"
                    if key != "native_machine":
                        observed[key] = value
                    out = root / key
                    out.mkdir()
                    command = [str(go), "env", "-json"]
                    with mock.patch.object(RUNNER.platform, "machine", return_value=machine):
                        with self.assertRaises(RuntimeError):
                            RUNNER.record_and_validate_toolchain(out, str(go), command, observed)
                    receipt = json.loads((out / "toolchain.json").read_text())
                    self.assertEqual(receipt[key], value)
                    self.assertEqual(receipt["go_sha256"], hashlib.sha256(go.read_bytes()).hexdigest())
                    self.assertEqual(receipt["discovery_command"], command)
                    self.assertEqual(list(out.iterdir()), [out / "toolchain.json"])
            out = root / "accepted"
            out.mkdir()
            with mock.patch.object(RUNNER.platform, "machine", return_value="x86_64"):
                RUNNER.record_and_validate_toolchain(out, str(go), [str(go), "env"], dict(good))
            self.assertEqual(json.loads((out / "toolchain.json").read_text())["GOVERSION"], "go1.27.2")

    def test_reviewed_toolchain_is_scoped_only_to_entry_runner(self):
        workflow = (Path(__file__).resolve().parents[1] / ".github/workflows/test.yml").read_text()
        step = workflow.split("    - name: Verify production sync fixture refusal with real entry coverage", 1)[1].split("    - name:", 1)[0]
        self.assertIn("GOENV=off GOWORK=off GOFLAGS= GOSUMDB=sum.golang.org GOTOOLCHAIN=go1.27.2 go env GOROOT", step)
        self.assertIn('PATH="$reviewed_goroot/bin:$PATH" python3 scripts/run_sync_entry_coverage.py', step)
        self.assertIn('cmp "$RUNNER_TEMP/sync-entry-toolchain.ambient.json" "$RUNNER_TEMP/sync-entry-toolchain.after.json"', step)
        for global_mutation in ("GITHUB_PATH", "GITHUB_ENV", "go env -w", "export PATH", "export GOTOOLCHAIN"):
            self.assertNotIn(global_mutation, step)
        self.assertIn("go-version-file: ${{ env.GO_VERSION_FILE }}", workflow)
        archive = workflow.split("    - name: Archive production sync entry evidence, including first failures", 1)[1].split("    - name:", 1)[0]
        self.assertIn("sync-entry-toolchain.*", archive)
        for suffix in ("goroot", "provision.log", "provision.exit", "ambient.json", "after.json"):
            self.assertIn("sync-entry-toolchain." + suffix, step)
        self.assertIn('test "$provision_exit" -eq 0', step)

    def test_failure_archive_includes_preflight_receipt(self):
        workflow = Path(__file__).resolve().parents[1] / ".github/workflows/test.yml"
        archive = workflow.read_text().split("    - name: Archive production sync entry evidence, including first failures", 1)[1].split("    - name:", 1)[0]
        self.assertIn("if: always()", archive)
        self.assertIn("sync-entry-coverage/*.json", archive)


if __name__ == "__main__":
    unittest.main()
