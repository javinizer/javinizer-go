#!/usr/bin/env python3
"""Run all core Windows packages, isolating the two database-heavy suites.

Keep Go's existing cache behavior and ten-minute package budget. The separate
native offline workflow remains responsible for enforcing the network guard.
"""

import collections
import json
import pathlib
import subprocess


BATCH = "github.com/javinizer/javinizer-go/internal/api/batch"
WORKFLOW = "github.com/javinizer/javinizer-go/internal/workflow"


def partition(packages):
    groups = [
        ("batch", [p for p in packages if p == BATCH]),
        ("workflow", [p for p in packages if p == WORKFLOW]),
        ("rest", [p for p in packages if p not in (BATCH, WORKFLOW)]),
    ]
    flattened = [p for _, members in groups for p in members]
    if (not packages or any(not p.strip() for p in packages)
            or len(set(packages)) != len(packages)
            or len(set(flattened)) != len(flattened)
            or collections.Counter(flattened) != collections.Counter(packages)
            or groups[0][1] != [BATCH] or groups[1][1] != [WORKFLOW]
            or not groups[2][1]):
        raise ValueError("Expected a unique, disjoint, exhaustive package partition")
    return groups


def validate_events(events, members):
    actions = {"start", "run", "pause", "cont", "pass", "bench", "fail", "output", "skip"}
    for event in events:
        if not isinstance(event, dict):
            raise ValueError("Malformed test JSON event")
        action = event.get("Action")
        if action in ("build-output", "build-fail"):
            # Go interleaves BuildEvents, including dependency diagnostics.
            # ImportPath may be empty for a toolchain-wide diagnostic.
            if not isinstance(event.get("ImportPath"), str):
                raise ValueError("Malformed build JSON event")
            if action == "build-fail":
                raise ValueError("Build failure in JSON results")
            if not isinstance(event.get("Output"), str) or not event["Output"]:
                raise ValueError("Malformed build output event")
            continue
        if (not isinstance(action, str) or action not in actions
                or not isinstance(event.get("Package"), str) or not event["Package"]
                or ("Test" in event and (not isinstance(event["Test"], str) or not event["Test"]))):
            raise ValueError("Malformed test JSON event")
    observed = {e["Package"] for e in events if "Package" in e}
    terminals = [e["Package"] for e in events
                 if not e.get("Test") and e.get("Action") in ("pass", "skip", "fail")]
    if observed != set(members) or collections.Counter(terminals) != collections.Counter(members):
        raise ValueError("Missing, extra, or duplicate package results")
    if any(e.get("Action") == "fail" for e in events):
        raise ValueError("Package or test failure in JSON results")
    return terminals


def run(evidence):
    evidence.mkdir(parents=True, exist_ok=False)
    errors = []
    terminals = []
    packages = []
    try:
        inventory = subprocess.run(["go", "list", "./..."], capture_output=True, text=True)
        (evidence / "inventory.txt").write_text(inventory.stdout, encoding="utf-8")
        (evidence / "inventory.stderr.log").write_text(inventory.stderr, encoding="utf-8")
        (evidence / "inventory.exit").write_text(str(inventory.returncode) + "\n", encoding="utf-8")
        if inventory.returncode:
            raise ValueError("Could not enumerate all packages")
        packages = inventory.stdout.splitlines()
        groups = partition(packages)
        (evidence / "partition.json").write_text(json.dumps({
            "inventory": packages,
            "groups": [{"name": name, "packages": members, "timeout": "10m"}
                       for name, members in groups],
            "unique": True, "disjoint": True, "exhaustive": True,
        }, indent=2) + "\n", encoding="utf-8")
        for name, members in groups:
            # A failed or malformed group must not prevent the remaining groups.
            try:
                command = ["go", "test", "-short", "-timeout=10m", "-json", *members]
                (evidence / (name + ".command.json")).write_text(json.dumps(command) + "\n", encoding="utf-8")
                with (evidence / (name + ".jsonl")).open("w", encoding="utf-8") as stdout, \
                        (evidence / (name + ".stderr.log")).open("w", encoding="utf-8") as stderr:
                    result = subprocess.run(command, stdout=stdout, stderr=stderr)
                (evidence / (name + ".exit")).write_text(str(result.returncode) + "\n", encoding="utf-8")
                events = [json.loads(line) for line in (evidence / (name + ".jsonl")).read_text(encoding="utf-8").splitlines() if line.strip()]
                terminals.extend(validate_events(events, members))
                if result.returncode:
                    raise ValueError("go test exited " + str(result.returncode))
                print(name + ": passed (" + str(len(members)) + " packages)", flush=True)
            except (OSError, ValueError, KeyError, TypeError) as exc:
                errors.append(name + ": " + str(exc))
                print(errors[-1], flush=True)
        if collections.Counter(terminals) != collections.Counter(packages):
            errors.append("Full-suite terminal package accounting failed")
    except (OSError, ValueError) as exc:
        errors.append("inventory: " + str(exc))
    (evidence / "summary.json").write_text(json.dumps({
        "inventory_count": len(packages), "validated_terminals": terminals,
        "errors": errors,
    }, indent=2) + "\n", encoding="utf-8")
    for error in errors:
        print(error, flush=True)
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(run(pathlib.Path("windows-unit-evidence")))
