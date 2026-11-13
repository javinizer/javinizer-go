#!/usr/bin/env python3
"""Require real-browser rendering and cancellation evidence without changing coverage policy."""

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
    cancellation_tests = [
        "TestNativeBrowserCancellationAfterNavigation",
        "TestNativeBrowserCancellationAfterNavigation/fetch",
        "TestNativeBrowserCancellationAfterNavigation/scrape_url",
    ]
    for required in [test, *cancellation_tests]:
        if f"--- PASS: {required} " not in log or f"--- SKIP: {required} " in log:
            raise SystemExit(f"native browser contract did not explicitly PASS: {required}")
    rows = profile.read_text().splitlines()
    if not rows or rows[0] != "mode: atomic":
        raise SystemExit("expected nonempty atomic coverage profile")
    target = "github.com/javinizer/javinizer-go/internal/scraper/dmm/browser.go"
    expected = {162, 165, 166, 181, 182, 183, 184, 187, 188, 189, 192}
    public_target = "github.com/javinizer/javinizer-go/internal/scraper/dmm/search.go"
    expected_by_file = {target: expected, public_target: {456}}
    covered = {filename: set() for filename in expected_by_file}
    for row in rows[1:]:
        match = re.fullmatch(r"(.+):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)", row)
        if match is None:
            raise SystemExit(f"invalid coverage row: {row!r}")
        filename, start, end, statements, count = match.groups()
        if filename in covered and int(statements) > 0 and int(count) > 0:
            covered[filename].update(range(int(start), int(end) + 1))
    for filename, lines in expected_by_file.items():
        missed = lines - covered[filename]
        if missed:
            raise SystemExit(f"native browser executable targets not hit in {filename}: {sorted(missed)}")
    print(json.dumps({
        "test": test, "file": target, "native_lines_hit": sorted(expected),
        "cancellation_tests": cancellation_tests,
        "public_cancellation_file": public_target, "public_cancellation_lines_hit": [456],
    }))


if __name__ == "__main__":
    main()
