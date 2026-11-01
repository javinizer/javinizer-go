#!/usr/bin/env python3
"""Admit two existing native Windows contracts, not simulated execution."""
import json
import pathlib
import re
import sys

PACKAGE = "github.com/javinizer/javinizer-go/internal/api/file"
ROOT = "TestAutocompleteDriveRelativePreservesEmptyFragment"
SOURCE = PACKAGE + "/autocomplete.go"
CONTRACTS = {
    "autocomplete": (PACKAGE, ROOT, SOURCE, 113),
    "umask": ("github.com/javinizer/javinizer-go/cmd/javinizer", "TestInitConfig_UmaskValid",
              "github.com/javinizer/javinizer-go/cmd/javinizer/root.go", 231),
}


def verify(profile, log, metadata, expected_head, event_name, declared_head, declared_base="", contract="autocomplete"):
    if contract not in CONTRACTS or metadata.get("contract") != contract:
        raise ValueError("unknown or mismatched contract selector")
    package, root, source, target_line = CONTRACTS[contract]
    if not re.fullmatch(r"[0-9a-f]{40}", expected_head):
        raise ValueError("expected head must be a full commit SHA")
    if metadata.get("checkout_sha") != expected_head or metadata.get("workflow_sha") != expected_head or metadata.get("goos") != "windows":
        raise ValueError("wrong head or non-native Windows evidence")
    if not re.fullmatch(r"[0-9a-f]{40}", declared_head):
        raise ValueError("declared source head must be a full SHA")
    if metadata.get("event_name") != event_name or metadata.get("declared_head_sha") != declared_head:
        raise ValueError("event/source head mismatch")
    if not re.fullmatch(r"[0-9a-f]{40}", metadata.get("tested_tree", "")):
        raise ValueError("missing tested tree")
    parents = metadata.get("raw_parents")
    if not isinstance(parents, list) or any(not re.fullmatch(r"[0-9a-f]{40}", p) for p in parents):
        raise ValueError("malformed raw parents")
    if event_name == "pull_request":
        if (not re.fullmatch(r"[0-9a-f]{40}", declared_base)
                or metadata.get("declared_base_sha") != declared_base
                or len(parents) != 2 or parents != [declared_base, declared_head]):
            raise ValueError("tested merge must have declared PR head as second parent")
    elif expected_head != declared_head:
        raise ValueError("non-PR checkout must equal declared source head")
    if metadata.get("exit") != 0:
        raise ValueError("native test command failed")
    events = [json.loads(line) for line in log.splitlines() if line.strip()]
    terminal = {}
    package_pass = False
    for event in events:
        if event.get("Action") not in {"pass", "skip", "fail"}:
            continue
        if event.get("Package") != package:
            raise ValueError("unexpected package evidence")
        test = event.get("Test")
        if not test:
            if event["Action"] != "pass":
                raise ValueError("package did not pass")
            package_pass = True
        else:
            if test in terminal:
                raise ValueError("duplicate terminal test event")
            terminal[test] = event["Action"]
    required = {root}
    if contract == "autocomplete":
        children = set(terminal) - {root}
        volumes = {name[len(root) + 1:] for name in children
                   if re.fullmatch(re.escape(root) + r"/[A-Za-z]:", name)}
        if len(volumes) != 1:
            raise ValueError("missing native bare-drive subcase")
        volume = volumes.pop()
        required.update({root + "/" + volume, root + "/" + volume + "."})
    if set(terminal) != required or any(value != "pass" for value in terminal.values()) or not package_pass:
        raise ValueError("selected contract root and required subcases must explicitly pass without skips")
    rows = profile.splitlines()
    if not rows or rows[0] != "mode: atomic" or len(rows) < 2:
        raise ValueError("expected nonempty atomic profile")
    target_hit = False
    seen = set()
    for row in rows[1:]:
        match = re.fullmatch(r"(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)", row)
        if not match:
            raise ValueError("malformed coverage row")
        filename, *numbers = match.groups()
        if filename.rsplit("/", 1)[0] != package:
            raise ValueError("profile contains a different package")
        start, startcol, end, endcol, statements, count = map(int, numbers)
        key = (filename, start, startcol, end, endcol)
        if key in seen or min(start, startcol, end, endcol, statements) <= 0 or (end, endcol) < (start, startcol):
            raise ValueError("invalid or duplicate coverage block")
        seen.add(key)
        if filename == source and start <= target_line <= end and count > 0:
            target_hit = True
    if not target_hit:
        raise ValueError(f"{source}:{target_line} not covered")
    return {"checkout_sha": expected_head, "workflow_sha": expected_head,
            "declared_head_sha": declared_head, "declared_base_sha": declared_base, "event_name": event_name,
            "tested_tree": metadata["tested_tree"], "raw_parents": parents,
            "evidence_scope": "executed checkout; pull_request checkout may be a synthetic merge, not the PR source head", "goos": "windows", "package": package, "contract": contract,
            "passed_tests": sorted(required), "source": source, "covered_lines": [target_line]}


def main():
    if len(sys.argv) != 9:
        raise SystemExit("usage: verify_windows_autocomplete_coverage.py PROFILE JSONL METADATA EXPECTED_CHECKOUT EVENT_NAME DECLARED_HEAD DECLARED_BASE CONTRACT")
    profile, log, metadata = [pathlib.Path(name).read_text(encoding="utf-8") for name in sys.argv[1:4]]
    print(json.dumps(verify(profile, log, json.loads(metadata), sys.argv[4], sys.argv[5], sys.argv[6], sys.argv[7], sys.argv[8]), indent=2))


if __name__ == "__main__":
    main()
