#!/usr/bin/env python3
"""Require real-browser happy-path evidence without changing coverage policy."""

import json
import pathlib
import re
import sys


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit("usage: verify_dmm_browser_coverage.py PROFILE TEST_LOG")
    profile = pathlib.Path(sys.argv[1])
    log = pathlib.Path(sys.argv[2]).read_text()
    test = "TestNativeBrowserScrapeURLCookieAndRenderingContract"
    if f"--- PASS: {test} " not in log or f"--- SKIP: {test} " in log:
        raise SystemExit("native browser contract did not explicitly PASS")
    rows = profile.read_text().splitlines()
    if not rows or rows[0] != "mode: atomic":
        raise SystemExit("expected nonempty atomic coverage profile")
    target = "github.com/javinizer/javinizer-go/internal/scraper/dmm/browser.go"
    expected = {165, 166, 181, 182, 183, 184, 187, 188, 189, 192}
    covered = set()
    for row in rows[1:]:
        match = re.fullmatch(r"(.+):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)", row)
        if match is None:
            raise SystemExit(f"invalid coverage row: {row!r}")
        filename, start, end, statements, count = match.groups()
        if filename == target and int(statements) > 0 and int(count) > 0:
            covered.update(range(int(start), int(end) + 1))
    missed = expected - covered
    if missed:
        raise SystemExit(f"native browser executable targets not hit: {sorted(missed)}")
    print(json.dumps({"test": test, "file": target, "native_lines_hit": sorted(expected)}))


if __name__ == "__main__":
    main()
