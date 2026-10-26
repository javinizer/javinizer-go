#!/usr/bin/env python3
"""Fail closed unless the exact collected browser suite actually passed once."""
import json
import os
from pathlib import Path
import subprocess
import sys

REQUIRED = {
    "explicit review applies once, preserves historical credits, and survives reload/restart",
    "lost confirmation response retries the same committed nonce after restart",
    "explicit same-digest evidence reload preserves reviewed consent but never submits",
    "changed evidence invalidates UI choices and consent without changing database state",
    "failed evidence reload preserves review and retries only by explicit action",
}


def tests(report):
    result = {}

    def walk(suites, parents=()):
        for suite in suites:
            path = parents + (suite.get("title", ""),)
            for spec in suite.get("specs", []):
                for test in spec.get("tests", []):
                    key = (spec["file"], *path, spec["title"], test["projectName"])
                    if key in result:
                        raise ValueError("duplicate collected browser test")
                    result[key] = (spec["title"], test)
            walk(suite.get("suites", []), path)

    walk(report.get("suites", []))
    return result


def verify(inventory, report):
    if inventory.get("errors") or report.get("errors"):
        raise ValueError("browser suite has top-level errors")
    collected, finished = tests(inventory), tests(report)
    if not collected or collected.keys() != finished.keys():
        raise ValueError("browser results differ from collected suite")
    if not REQUIRED <= {title for title, _ in finished.values()}:
        raise ValueError("required offline reload/lifecycle cases missing")
    for _, test in finished.values():
        outcomes = test.get("results", [])
        if test.get("expectedStatus") != "passed" or test.get("status") != "expected":
            raise ValueError("unexpected, skipped or flaky browser case")
        if len(outcomes) != 1 or outcomes[0].get("status") != "passed" or outcomes[0].get("retry", 0) != 0:
            raise ValueError("browser case did not pass exactly once without retries")
    return len(finished)


def main():
    evidence = Path(sys.argv[1]).resolve()
    expected = os.environ["OFFLINE_SYNC_EXPECTED_SHA"]
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
    if actual != expected:
        raise ValueError("browser evidence is not for the requested head")
    count = verify(json.loads((evidence / "inventory.json").read_text()),
                   json.loads((evidence / "results.json").read_text()))
    for name in ("backend.audit", "node.audit"):
        if (evidence / name).read_text().strip():
            raise ValueError("browser fixture attempted external network")
    proof = {"head": actual, "platform": sys.platform, "passed": count, "skipped": 0,
             "retries": 0, "external_network_events": 0, "required": sorted(REQUIRED)}
    (evidence / "proof.json").write_text(json.dumps(proof, indent=2) + "\n")
    print(json.dumps(proof))


if __name__ == "__main__":
    main()
