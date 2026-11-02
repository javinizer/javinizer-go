#!/usr/bin/env python3
"""Admit existing native Windows contracts, not simulated execution."""
import json
import pathlib
import re
import sys

PACKAGE = "github.com/javinizer/javinizer-go/internal/api/file"
ROOT = "TestAutocompleteDriveRelativePreservesEmptyFragment"
SOURCE = PACKAGE + "/autocomplete.go"
CLI_PACKAGE = "github.com/javinizer/javinizer-go/cmd/javinizer"
CLEANUP_ROOTS = {"TestCleanupStaleWindowsOldExe_RemovesStaleOld", "TestCleanupStaleWindowsOldExe_NoOpWhenAbsent"}
FSUTIL_PACKAGE = "github.com/javinizer/javinizer-go/internal/fsutil"
FSUTIL_ROOTS = {
    "TestIsCrossDeviceError_Windows", "TestReplaceFileWindows_AtomicReplace",
    "TestReplaceFileW13_VirtualFallbackSuccessReplacesDestination",
    "TestPublishNoReplaceW15_OsFsOccupiedDestinationCollides",
    "TestPublishNoReplaceW15_OsFsMissingSourceIsNotACollision",
    "TestDurableStagingBindsHandleAndFlushesMetadata",
    "TestVacateLinkObject_MovesLinkNotTarget",
    "TestVacateLinkObject_OccupiedTerminalCollides", "TestVacateLinkObject_AbsentNameNotExist",
}
FSUTIL_COMMAND = ["go", "test", "-short", "-count=1", "-timeout=10m", "-json",
                  "-covermode=atomic", "-coverprofile=coverage-fsutil-windows.out", "./internal/fsutil"]

CONTRACTS = {
    "fsutil": (FSUTIL_PACKAGE, "TestIsCrossDeviceError_Windows",
               FSUTIL_PACKAGE + "/move_crossdev_windows.go", 13),
    "cleanup": (CLI_PACKAGE, "TestCleanupStaleWindowsOldExe_RemovesStaleOld",
                CLI_PACKAGE + "/cleanup_windows.go", 16),
    "catalog": ("github.com/javinizer/javinizer-go/internal/catalogname", "TestLoadReadOnlyDetachedSnapshot",
                "github.com/javinizer/javinizer-go/internal/catalogname/catalogname.go", 66),
    "autocomplete": (PACKAGE, ROOT, SOURCE, 113),
    "umask": ("github.com/javinizer/javinizer-go/cmd/javinizer", "TestInitConfig_UmaskValid",
              "github.com/javinizer/javinizer-go/cmd/javinizer/root.go", 231),
}


def fsutil_environment(environ):
    """Remove implicit filtering/source overlays and use the native Go target."""
    env = {key: value for key, value in environ.items()
           if key.upper() not in {"GOFLAGS", "GOENV", "GOWORK", "GOOS", "GOARCH"}}
    env.update(GOFLAGS="", GOENV="off", GOWORK="off")
    return env


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
    if contract == "fsutil":
        effective = metadata.get("effective_go_env", {})
        if (metadata.get("command") != FSUTIL_COMMAND
                or metadata.get("input_go_overrides") != {"GOFLAGS": "", "GOENV": "off", "GOWORK": "off"}
                or effective.get("GOFLAGS") != "" or effective.get("GOENV") != ""
                or effective.get("GOWORK") != "off" or effective.get("GOOS") != "windows"
                or effective.get("GOHOSTOS") != "windows"
                or not effective.get("GOARCH") or effective.get("GOARCH") != effective.get("GOHOSTARCH")
                or not re.fullmatch(r"go1\.\d+\.\d+", effective.get("GOVERSION", ""))):
            raise ValueError("complete native short-package invocation requires exact unfiltered command and isolated Go environment")
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
            if package_pass:
                raise ValueError("duplicate terminal package event")
            package_pass = True
        else:
            if test in terminal:
                raise ValueError("duplicate terminal test event")
            terminal[test] = event["Action"]
    required = CLEANUP_ROOTS if contract == "cleanup" else {root}
    target_lines = {16, 17, 20, 21} if contract == "cleanup" else {target_line}
    if contract == "autocomplete":
        children = set(terminal) - {root}
        volumes = {name[len(root) + 1:] for name in children
                   if re.fullmatch(re.escape(root) + r"/[A-Za-z]:", name)}
        if len(volumes) != 1:
            raise ValueError("missing native bare-drive subcase")
        volume = volumes.pop()
        required.update({root + "/" + volume, root + "/" + volume + "."})
    if contract == "fsutil":
        required = FSUTIL_ROOTS
        target_lines = {13, 14, 15}
        if (not package_pass or any(terminal.get(name) != "pass" for name in required)
                or any(value == "fail" for value in terminal.values())
                or any(value != "pass" for name, value in terminal.items()
                       if name.split("/", 1)[0] in required)):
            raise ValueError("required native roots and their observed subcases must pass without skips")
    elif set(terminal) != required or any(value != "pass" for value in terminal.values()) or not package_pass:
        raise ValueError("selected contract root and required subcases must explicitly pass without skips")
    rows = profile.splitlines()
    if not rows or rows[0] != "mode: atomic" or len(rows) < 2:
        raise ValueError("expected nonempty atomic profile")
    covered_lines = set()
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
        if key in seen or min(start, startcol, end, endcol) <= 0 or (end, endcol) < (start, startcol):
            raise ValueError("invalid or duplicate coverage block")
        seen.add(key)
        # Go emits zero-statement counters for empty blocks. Preserve them,
        # but an entry count alone cannot satisfy executable-statement admission.
        if filename == source and statements > 0 and count > 0:
            covered_lines.update(line for line in target_lines if start <= line <= end)
    if covered_lines != target_lines:
        raise ValueError(f"{source}: required lines not covered: {sorted(target_lines - covered_lines)}")
    return {"checkout_sha": expected_head, "workflow_sha": expected_head,
            "declared_head_sha": declared_head, "declared_base_sha": declared_base, "event_name": event_name,
            "tested_tree": metadata["tested_tree"], "raw_parents": parents,
            "evidence_scope": "executed checkout; pull_request checkout may be a synthetic merge, not the PR source head", "goos": "windows", "package": package, "contract": contract,
            "passed_tests": sorted(required), "source": source, "covered_lines": sorted(covered_lines),
            "observed_passed_tests": sorted(name for name, action in terminal.items() if action == "pass"),
            "observed_skipped_tests": sorted(name for name, action in terminal.items() if action == "skip"),
            "invocation_scope": "complete package invocation in -short mode; explicit skips retained" if contract == "fsutil" else "selected roots"}


def main():
    if len(sys.argv) != 9:
        raise SystemExit("usage: verify_windows_autocomplete_coverage.py PROFILE JSONL METADATA EXPECTED_CHECKOUT EVENT_NAME DECLARED_HEAD DECLARED_BASE CONTRACT")
    profile, log, metadata = [pathlib.Path(name).read_text(encoding="utf-8") for name in sys.argv[1:4]]
    print(json.dumps(verify(profile, log, json.loads(metadata), sys.argv[4], sys.argv[5], sys.argv[6], sys.argv[7], sys.argv[8]), indent=2))


if __name__ == "__main__":
    main()
