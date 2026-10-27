#!/usr/bin/env python3
"""Fail closed on incomplete or stale genuine-source, read-only browser proof."""
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from verify_offline_sync_browser import tests

TITLE = "real R18 dump evidence is explicitly requested, read-only, preserved on failure and cleared by opt-out"
REQUIRED = {
    ("global-setup.ts", "authenticate against real backend and save storage state", "setup"),
    ("regressions/offline-evidence.spec.ts", TITLE, "chromium"),
    ("global-teardown.ts", "full-stack teardown", "teardown"),
}
FILES = ("inventory.json", "results.json", "browser.log", "exit.txt", "backend.audit", "node.audit", "browser.audit")
MAX_FILE_BYTES = 256 * 1024 * 1024
MAX_TOTAL_BYTES = 512 * 1024 * 1024
MAX_FILES = 2048


def _errors(value):
    if isinstance(value, dict):
        if value.get("error") or value.get("errors"):
            raise ValueError("browser report contains errors")
        for child in value.values():
            _errors(child)
    elif isinstance(value, list):
        for child in value:
            _errors(child)


def collect(report):
    if not isinstance(report, dict) or not isinstance(report.get("suites"), list):
        raise ValueError("invalid browser report")
    _errors(report)

    def walk(suites):
        for suite in suites:
            if not isinstance(suite, dict):
                raise ValueError("invalid suite")
            specs, children = suite.get("specs", []), suite.get("suites", [])
            if not isinstance(specs, list) or not isinstance(children, list) or not (specs or children):
                raise ValueError("empty or invalid suite")
            for spec in specs:
                if not isinstance(spec, dict) or spec.get("ok") is False or len(spec.get("tests", [])) != 1:
                    raise ValueError("empty, duplicate or unsuccessful spec")
            walk(children)

    walk(report["suites"])
    collected = tests(report)
    triples = {(key[0], title, test.get("projectName")) for key, (title, test) in collected.items()}
    if len(collected) != 3 or triples != REQUIRED:
        raise ValueError("required exact browser cases differ")
    for _, test in collected.values():
        if test.get("expectedStatus") != "passed":
            raise ValueError("case not expected to pass")
    return collected


def verify(inventory, report):
    collected, finished = collect(inventory), collect(report)
    if collected.keys() != finished.keys():
        raise ValueError("browser results differ from collected identities")
    for _, test in collected.values():
        if test.get("results") != []:
            raise ValueError("inventory is not an unexecuted --list report")
    for _, test in finished.values():
        outcomes = test.get("results", [])
        if test.get("status") != "expected" or len(outcomes) != 1:
            raise ValueError("unexpected, skipped or flaky browser case")
        result = outcomes[0]
        if result.get("status") != "passed" or type(result.get("retry")) is not int or result["retry"] != 0:
            raise ValueError("case did not pass exactly once without retries")
        for owner in (test, result):
            if any(a.get("type") in ("skip", "fixme", "fail") for a in owner.get("annotations", [])):
                raise ValueError("case has skip/failure annotations")
    if "stats" in report:
        for key, value in {"expected": 3, "unexpected": 0, "flaky": 0, "skipped": 0}.items():
            if type(report["stats"].get(key)) is not int or report["stats"][key] != value:
                raise ValueError("browser stats are incomplete or unsuccessful")
    return len(finished)


def source_proof(report):
    test = next(test for title, test in collect(report).values() if title == TITLE)
    attachments = test["results"][0].get("attachments", [])

    def body(name, content_type, limit):
        matching = [a for a in attachments if a.get("name") == name]
        if len(matching) != 1 or matching[0].get("contentType") != content_type:
            raise ValueError("missing/duplicate proof attachment: " + name)
        encoded = matching[0].get("body")
        if not isinstance(encoded, str) or len(encoded) > 4 * ((limit + 2) // 3):
            raise ValueError("missing or oversized inline proof: " + name)
        try:
            data = base64.b64decode(encoded, validate=True)
        except ValueError as error:
            raise ValueError("invalid inline proof encoding") from error
        if len(data) > limit:
            raise ValueError("oversized proof attachment")
        return data

    raw = body("real-api-evidence-contract.json", "application/json", 256 * 1024)
    contract = json.loads(raw)
    requests = contract.get("requests")
    if not isinstance(requests, list) or len(requests) != 3 or not all(isinstance(r, dict) for r in requests):
        raise ValueError("missing actual opt-in/failure/opt-out requests")
    if requests[0].get("show_offline_evidence") is not True or requests[1].get("show_offline_evidence") is not True or "show_offline_evidence" in requests[2]:
        raise ValueError("explicit opt-in/opt-out request contract differs")
    if any("artifact_directory" in r for r in requests):
        raise ValueError("client cannot select evidence artifact path")
    evidence = contract.get("report", {})
    digest = evidence.get("snapshot_digest", "")
    if evidence.get("outcome") != "evidence_available" or not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError("missing pinned source report")
    observations = evidence.get("observations", [])
    expected = {"7": "Supplemental evidence name", "007": "Supplemental leading-zero name"}
    if len(observations) != 2 or {o.get("source_id") for o in observations} != set(expected):
        raise ValueError("opaque source IDs differ")
    for observation in observations:
        if observation.get("source") != "r18dev" or observation.get("outcome") != "evidence_available" or observation.get("japanese_name") != expected[observation["source_id"]]:
            raise ValueError("same-source evidence differs")
    hashes = {"real-api-evidence-contract.json": hashlib.sha256(raw).hexdigest()}
    for name in ("read-only-evidence.png", "mobile-evidence.png"):
        png = body(name, "image/png", 16 * 1024 * 1024)
        if not png.startswith(b"\x89PNG\r\n\x1a\n") or len(png) <= 8:
            raise ValueError("invalid screenshot proof")
        hashes[name] = hashlib.sha256(png).hexdigest()
    return {"snapshot_digest": digest, "source_ids": sorted(expected), "inline_sha256": hashes}


def evidence_hashes(directory):
    hashes, total = {}, 0
    for path in sorted(directory.rglob("*")):
        relative = path.relative_to(directory)
        if path.is_symlink():
            raise ValueError("symlink in evidence")
        if any(part == ".auth" or "auth-state" in part for part in relative.parts):
            raise ValueError("browser authentication state in evidence")
        if "browser-artifacts" in relative.parts and path.suffix.lower() in (".zip", ".har", ".webm", ".mp4"):
            raise ValueError("browser recording can contain authentication state")
        if path.is_dir():
            continue
        if not path.is_file():
            raise ValueError("special file in evidence")
        if relative.as_posix() == "proof.json":
            continue
        size = path.stat().st_size
        total += size
        if size > MAX_FILE_BYTES or total > MAX_TOTAL_BYTES or len(hashes) >= MAX_FILES:
            raise ValueError("evidence exceeds bounded archive budget")
        digest = hashlib.sha256()
        with path.open("rb") as stream:
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(block)
        hashes[relative.as_posix()] = digest.hexdigest()
    return hashes


def verify_directory(directory, expected, repo=None):
    directory = Path(directory).resolve()
    proof_path = directory / "proof.json"
    if proof_path.is_symlink():
        raise ValueError("proof must not be a symlink")
    proof_path.unlink(missing_ok=True)  # a failed rerun cannot leave stale success
    if not re.fullmatch(r"[0-9a-f]{40}(?:[0-9a-f]{24})?", expected):
        raise ValueError("expected head must be an exact commit ID")

    def git(*args):
        return subprocess.check_output(["git", *args], cwd=repo, text=True).strip()

    head = git("rev-parse", "HEAD")
    if head != expected:
        raise ValueError("browser proof is not for the requested head")
    tree = git("rev-parse", "HEAD^{tree}")
    root = Path(git("rev-parse", "--show-toplevel")).resolve()
    if subprocess.run(["git", "diff", "--quiet", "HEAD", "--"], cwd=root).returncode:
        raise ValueError("source worktree/index differs from requested head")
    for name in git("ls-files", "--others", "--exclude-standard", "-z").split("\0"):
        if name and not (root / name).resolve().is_relative_to(directory):
            raise ValueError("untracked source file outside proof directory")
    for name in FILES:
        if not (directory / name).is_file() or (directory / name).is_symlink():
            raise ValueError("missing regular evidence file: " + name)
    hashes = evidence_hashes(directory)  # enforce budgets before decoding any report
    if (directory / "exit.txt").read_text().strip() != "0":
        raise ValueError("browser command did not exit successfully")
    for name in ("backend.audit", "node.audit", "browser.audit"):
        if (directory / name).read_bytes():
            raise ValueError("fixture attempted external network: " + name)
    inventory = json.loads((directory / "inventory.json").read_text())
    report = json.loads((directory / "results.json").read_text())
    count = verify(inventory, report)
    source = source_proof(report)
    if evidence_hashes(directory) != hashes:
        raise ValueError("evidence changed during verification")
    proof = {"head": head, "tree": tree, "platform": sys.platform, "passed": count,
             "skipped": 0, "retries": 0, "unexpected": 0, "external_network_events": 0,
             "required": [list(case) for case in sorted(REQUIRED)], "source_evidence": source,
             "sha256": hashes}
    proof_path.write_text(json.dumps(proof, indent=2) + "\n")
    return proof


def main():
    print(json.dumps(verify_directory(sys.argv[1], os.environ["OFFLINE_EVIDENCE_EXPECTED_SHA"])))


if __name__ == "__main__":
    main()
