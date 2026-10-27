"""Adversarial synthetic reporter fixtures; these do not claim browser execution."""
import base64
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import verify_offline_evidence_browser as verifier


def attachment(name, value, content_type="application/json"):
    raw = json.dumps(value).encode() if content_type == "application/json" else value
    return {"name": name, "contentType": content_type, "body": base64.b64encode(raw).decode()}


def fixture():
    report = {"suites": [], "errors": [], "stats": {"expected": 3, "unexpected": 0, "flaky": 0, "skipped": 0}}
    for file, title, project in sorted(verifier.REQUIRED):
        test = {"projectName": project, "expectedStatus": "passed", "status": "expected", "results": [{"status": "passed", "retry": 0}]}
        report["suites"].append({"title": file, "specs": [{"title": title, "file": file, "ok": True, "tests": [test]}]})
        if project == "chromium":
            contract = {"requests": [{"show_offline_evidence": True}, {"show_offline_evidence": True}, {}], "report": {
                "outcome": "evidence_available", "snapshot_digest": "a" * 64, "observations": [
                    {"source": "r18dev", "source_id": key, "outcome": "evidence_available", "japanese_name": name}
                    for key, name in (("7", "Supplemental evidence name"), ("007", "Supplemental leading-zero name"))]}}
            test["results"][0]["attachments"] = [attachment("real-api-evidence-contract.json", contract),
                attachment("read-only-evidence.png", b"\x89PNG\r\n\x1a\nsynthetic desktop", "image/png"),
                attachment("mobile-evidence.png", b"\x89PNG\r\n\x1a\nsynthetic mobile", "image/png")]
    inventory = copy.deepcopy(report)
    for _, test in verifier.tests(inventory).values():
        test.update(results=[], status="skipped")
    return inventory, report


def primary(report):
    return next(test for title, test in verifier.tests(report).values() if title == verifier.TITLE)


class VerifyTests(unittest.TestCase):
    def test_exact_three_and_inline_proof(self):
        inventory, report = fixture()
        self.assertEqual(verifier.verify(inventory, report), 3)
        proof = verifier.source_proof(report)
        self.assertEqual(proof["source_ids"], ["007", "7"])
        self.assertEqual(len(proof["inline_sha256"]), 3)

    def test_bad_inventory(self):
        for change in (lambda r: primary(r).update(results=[{"status": "passed", "retry": 0}]),
                       lambda r: r["suites"][0]["specs"][0].update(ok=False)):
            inventory, report = fixture()
            change(inventory)
            with self.assertRaises(ValueError):
                verifier.verify(inventory, report)

    def test_collection_failures(self):
        mutations = [lambda r: r.update(suites=[]), lambda r: r["suites"].pop(),
            lambda r: r["suites"].append(copy.deepcopy(r["suites"][0])),
            lambda r: r["suites"][0]["specs"][0].update(title="extra case"),
            lambda r: r["suites"][0]["specs"][0].update(file="wrong.ts"),
            lambda r: primary(r).update(projectName="firefox"),
            lambda r: r["suites"][0]["specs"][0].update(tests=[]),
            lambda r: r["suites"].append({"title": "empty"}),
            lambda r: r["suites"][0].update(title="changed identity"),
            lambda r: r["suites"][0]["specs"].append(copy.deepcopy(r["suites"][0]["specs"][0]))]
        for mutate in mutations:
            with self.subTest(mutation=mutate):
                inventory, report = fixture()
                mutate(report)
                with self.assertRaises(ValueError):
                    verifier.verify(inventory, report)

    def test_outcomes_retries_skips(self):
        for status in ("skipped", "unexpected", "flaky", "", None):
            inventory, report = fixture()
            primary(report)["status"] = status
            with self.assertRaises(ValueError):
                verifier.verify(inventory, report)
        mutations = [lambda t: t.update(expectedStatus="failed"), lambda t: t.update(results=[]),
                     lambda t: t["results"].append(copy.deepcopy(t["results"][0]))]
        mutations += [lambda t, value=v: t["results"][0].update(retry=value) for v in (1, -1, True, None)]
        mutations += [lambda t, value=v: t["results"][0].update(status=value) for v in ("skipped", "failed", "timedOut", "interrupted")]
        mutations += [lambda t, owner=o, kind=k: (t if owner == "test" else t["results"][0]).update(annotations=[{"type": kind}])
                      for o in ("test", "result") for k in ("skip", "fail", "fixme")]
        for mutate in mutations:
            inventory, report = fixture()
            mutate(primary(report))
            with self.assertRaises(ValueError):
                verifier.verify(inventory, report)

    def test_errors_at_every_level_and_stats(self):
        for level in ("top", "suite", "spec", "test", "result", "step"):
            inventory, report = fixture()
            owners = {"top": report, "suite": report["suites"][0], "spec": report["suites"][0]["specs"][0],
                      "test": primary(report), "result": primary(report)["results"][0]}
            if level == "step":
                owners["result"]["steps"] = [{"error": {"message": "failed"}}]
            else:
                owners[level]["errors"] = [{"message": "failed"}]
            with self.assertRaises(ValueError):
                verifier.verify(inventory, report)
        for key in ("expected", "unexpected", "flaky", "skipped"):
            inventory, report = fixture()
            report["stats"][key] += 1
            with self.assertRaises(ValueError):
                verifier.verify(inventory, report)

    def test_inline_attachment_failures(self):
        for name in ("real-api-evidence-contract.json", "read-only-evidence.png", "mobile-evidence.png"):
            for mutation in ("missing", "duplicate", "encoding", "type"):
                _, report = fixture()
                attachments = primary(report)["results"][0]["attachments"]
                target = next(a for a in attachments if a["name"] == name)
                if mutation == "missing": attachments.remove(target)
                if mutation == "duplicate": attachments.append(copy.deepcopy(target))
                if mutation == "encoding": target["body"] = "%invalid"
                if mutation == "type": target["contentType"] = "text/plain"
                with self.assertRaises(ValueError): verifier.source_proof(report)
        _, report = fixture()
        primary(report)["results"][0]["attachments"][1]["body"] = base64.b64encode(b"not PNG").decode()
        with self.assertRaises(ValueError): verifier.source_proof(report)

    def test_contract_failures(self):
        mutations = [lambda c: c.update(requests=[]), lambda c: c["requests"][0].pop("show_offline_evidence"),
            lambda c: c["requests"][1].update(show_offline_evidence=False),
            lambda c: c["requests"][2].update(show_offline_evidence=False),
            lambda c: c["requests"][0].update(artifact_directory="/client"),
            lambda c: c["report"].update(snapshot_digest="short"),
            lambda c: c["report"].update(outcome="unmatched"),
            lambda c: c["report"]["observations"].pop(),
            lambda c: c["report"]["observations"][1].update(source_id="7"),
            lambda c: c["report"]["observations"][0].update(source="dmm"),
            lambda c: c["report"]["observations"][0].update(japanese_name="canonical mutation"),
            lambda c: c["report"]["observations"][0].update(outcome="stale")]
        for mutate in mutations:
            _, report = fixture()
            attachments = primary(report)["results"][0]["attachments"]
            contract = json.loads(base64.b64decode(attachments[0]["body"]))
            mutate(contract)
            attachments[0] = attachment("real-api-evidence-contract.json", contract)
            with self.assertRaises(ValueError): verifier.source_proof(report)


class DirectoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo, self.evidence = self.root / "repo", self.root / "evidence"
        self.repo.mkdir(); self.evidence.mkdir()
        self.git("init", "-q")
        (self.repo / "source.txt").write_text("synthetic source\n")
        self.git("add", "source.txt")
        self.git("-c", "user.name=Synthetic", "-c", "user.email=synthetic@example.invalid", "commit", "-qm", "synthetic")
        self.head = self.git("rev-parse", "HEAD")
        self.populate()

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, text=True).strip()

    def populate(self):
        inventory, report = fixture()
        for name, value in (("inventory.json", inventory), ("results.json", report)):
            (self.evidence / name).write_text(json.dumps(value))
        for name in verifier.FILES[2:]:
            (self.evidence / name).write_text("0\n" if name == "exit.txt" else "")

    def verify(self):
        return verifier.verify_directory(self.evidence, self.head, self.repo)

    def test_actual_cli_canary_and_hashes(self):
        env = {**os.environ, "OFFLINE_EVIDENCE_EXPECTED_SHA": self.head, "PYTHONDONTWRITEBYTECODE": "1"}
        script = Path(verifier.__file__).resolve()
        result = subprocess.run([sys.executable, "-B", str(script), str(self.evidence)], cwd=self.repo, env=env, check=True, capture_output=True, text=True)
        proof = json.loads(result.stdout)
        self.assertEqual(proof["head"], self.head)
        self.assertEqual(proof["tree"], self.git("rev-parse", "HEAD^{tree}"))
        self.assertEqual(set(proof["sha256"]), set(verifier.FILES))
        self.assertNotIn("proof.json", proof["sha256"])
        self.assertEqual(self.verify(), proof)

    def test_required_files_exit_audits(self):
        for name in verifier.FILES:
            (self.evidence / name).unlink()
            with self.assertRaises(ValueError): self.verify()
            self.populate()
        for name, value in (("exit.txt", "1"), ("backend.audit", " "), ("node.audit", "blocked"), ("browser.audit", "blocked")):
            (self.evidence / name).write_text(value)
            with self.assertRaises(ValueError): self.verify()
            self.populate()

    def test_head_and_dirty_tree_remove_stale_success(self):
        self.verify()
        with self.assertRaises(ValueError): verifier.verify_directory(self.evidence, "0" * 40, self.repo)
        self.assertFalse((self.evidence / "proof.json").exists())
        (self.repo / "source.txt").write_text("changed")
        with self.assertRaises(ValueError): self.verify()
        self.git("add", "source.txt")
        with self.assertRaises(ValueError): self.verify()

    def test_auth_symlink_recording_and_budgets(self):
        for name in ("browser-artifacts/.auth/auth-state.json", "browser-artifacts/trace.zip", "browser-artifacts/video.webm", "browser-artifacts/network.har"):
            path = self.evidence / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("secret")
            with self.assertRaises(ValueError): self.verify()
            path.unlink()
            if path.parent.name == ".auth": path.parent.rmdir()
        link = self.evidence / "link"
        link.symlink_to(self.repo / "source.txt")
        with self.assertRaises(ValueError): self.verify()
        link.unlink()
        big = self.evidence / "large"
        with big.open("wb") as stream: stream.truncate(verifier.MAX_FILE_BYTES + 1)
        with self.assertRaises(ValueError): self.verify()

    def test_budgets_precede_json_decode_and_changes_are_rejected(self):
        report = self.evidence / "results.json"
        with report.open("wb") as stream:
            stream.write(b"not JSON")
            stream.truncate(verifier.MAX_FILE_BYTES + 1)
        with self.assertRaisesRegex(ValueError, "archive budget"):
            self.verify()
        self.populate()
        original = verifier.source_proof
        def change_during_verification(value):
            proof = original(value)
            (self.evidence / "browser.log").write_text("changed while verifying")
            return proof
        with mock.patch.object(verifier, "source_proof", side_effect=change_during_verification):
            with self.assertRaisesRegex(ValueError, "changed during verification"):
                self.verify()
        self.assertFalse((self.evidence / "proof.json").exists())

    def test_untracked_source_but_allow_proof_under_repo(self):
        (self.repo / "untracked.py").write_text("pass")
        with self.assertRaises(ValueError): self.verify()
        (self.repo / "untracked.py").unlink()
        new_path = self.repo / "evidence"
        self.evidence.rename(new_path)
        self.evidence = new_path
        self.assertEqual(self.verify()["passed"], 3)


if __name__ == "__main__":
    unittest.main()
