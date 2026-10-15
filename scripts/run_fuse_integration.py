#!/usr/bin/env python3
"""Run mandatory kernel-FUSE integration tests against owned synthetic files.

Dependency download is deliberately separate. This runner performs no package
installation, network setup, privileged mount, or device/security modification.
"""
import argparse
import collections
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
CANARIES = {
    "TestExternalBeforeDNS", "TestDirectDialAndUDPWrites", "TestLoopbackAllowed",
    "TestHTTPExternalTargetBlockedBeforeProxy", "TestHTTPExternalTargetBlockedBeforeSOCKS",
    "TestInfrastructureProxyEndpointBlocked", "TestHTTPLoopbackAllowed",
}
OPERATIONS = {
    "copy", "copy-mode", "copy-install", "copy-digest", "copy-digest-mode",
    "cross-device-move", "cross-device-move-mode",
}
REQUIRED_FUSE = {
    "TestFUSEKernelProtocol", "TestFUSEUnsupportedPreflight", "TestFUSEPublicationSuccess",
    "TestFUSEPublicationFailuresPreserveBytes", "TestFUSECleanupUnmounts", "TestMountIdentityParser",
}
REQUIRED_FUSE |= {"TestFUSEKernelProtocol/" + case for case in (
    "capable-rename-and-link", "rename-enosys-maps-einval", "rename-eopnotsupp", "link-enosys-kernel-policy",
)}
REQUIRED_FUSE |= {
    "TestFUSEUnsupportedPreflight/errno-" + code + "/" + op
    for code in ("38", "95") for op in OPERATIONS
}
REQUIRED_FUSE |= {
    "TestFUSEPublicationSuccess/" + mode + "/" + op
    for mode in ("rename-capable", "hardlink-fallback-capable") for op in OPERATIONS
}
REQUIRED_FUSE |= {
    "TestFUSEPublicationFailuresPreserveBytes/" + case + "/" + op
    for case in ("existing-collision", "collision-after-positive-probe", "stream-eio",
                 "final-publish-eio", "fallback-collision-after-positive-probe", "fallback-final-publish-eio", "admission-canceled") for op in OPERATIONS
}


def run_suite(name, directory, env, evidence, required, race=False):
    audit = evidence / (name + ".audit.log")
    audit.write_text("", encoding="utf-8")
    env = dict(env, OFFLINE279_AUDIT_LOG=str(audit))
    command = ["go", "test", "-count=1", "-timeout=5m", "-json"]
    if race:
        command += ["-race"]
    command += ["./..."]
    with (evidence / (name + ".jsonl")).open("w", encoding="utf-8") as stdout, \
            (evidence / (name + ".stderr.log")).open("w", encoding="utf-8") as stderr:
        completed = subprocess.run(command, cwd=directory, env=env, stdout=stdout, stderr=stderr)
    events = [json.loads(line) for line in (evidence / (name + ".jsonl")).read_text(encoding="utf-8").splitlines() if line.strip()]
    passed = {event.get("Test") for event in events if event.get("Action") == "pass"}
    skipped = [event.get("Test") for event in events if event.get("Action") == "skip"]
    missing = sorted(required - passed)
    markers = collections.Counter(audit.read_text(encoding="utf-8").splitlines())
    expected = {"OFFLINE279_BLOCKED": 26, "OFFLINE279_BLOCKED_HTTP_TARGET": 4} if name == "guard-canaries" else {}
    result = {"exit_code": completed.returncode, "missing_tests": missing, "skipped_tests": skipped,
              "audit": dict(markers), "passed_tests": len(passed - {None}), "required_tests": sorted(required)}
    (evidence / (name + ".result.json")).write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    print(name, "exit", completed.returncode, "passed", result["passed_tests"], "missing", len(missing), "skipped", len(skipped), "audit", dict(markers), flush=True)
    if completed.returncode != 0 or missing or skipped or markers != expected:
        raise RuntimeError(name + " failed closed; see " + str(evidence))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--evidence", type=Path, required=True)
    args = parser.parse_args()
    evidence = args.evidence.resolve()
    evidence.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    # Select the installed distribution helper ahead of /usr/local shadows.
    # This changes command lookup only; it never edits the helper or permissions.
    go = shutil.which("go")
    if go is None:
        raise RuntimeError("Go 1.27.2 must be installed during setup")
    env["PATH"] = str(Path(go).resolve().parent) + ":/usr/bin:/bin:" + env.get("PATH", "")
    env.update({"GOTOOLCHAIN": "local", "GOPROXY": "off", "GONOPROXY": "none",
                "GOSUMDB": "off", "GOVCS": "*:off", "GODEBUG": "netdns=go",
                "JAVINIZER_FUSE_INTEGRATION": "1"})
    # Preserve inherited proxy/routing settings. The strict guard blocks
    # external targets and configured proxy endpoints before DNS/connect.
    for key in ("JAVINIZER_RUN_LIVE_API_TESTS", "JAVINIZER_RUN_FLARESOLVERR_TESTS"):
        env.pop(key, None)
    subprocess.run(["python3", str(ROOT / "scripts/build_offline_test_overlay.py"),
                    "--output", str(evidence / "_overlay")], env=env, cwd=ROOT, check=True)
    env["GOFLAGS"] = "'-overlay=" + str(evidence / "_overlay/overlay.json") + "' -mod=readonly"
    meta = {
        "go": subprocess.check_output(["go", "version"], env=env, text=True).strip(),
        "kernel": os.uname().release,
        "commit": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
        "runner_image": os.environ.get("ImageVersion", "unknown"),
        "scope": "owned synthetic local files; no NAS, network mounts, containers, or security changes",
    }
    (evidence / "environment.json").write_text(json.dumps(meta, indent=2) + "\n", encoding="utf-8")
    run_suite("guard-canaries", ROOT / "test/offlineguard", env, evidence, CANARIES)
    run_suite("kernel-fuse", ROOT / "tests/fuseintegration", env, evidence, REQUIRED_FUSE, race=True)
    print("All required kernel-FUSE cases and cleanup checks passed; no skipped tests or runtime network attempts.", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, RuntimeError, subprocess.CalledProcessError) as error:
        print(error, file=sys.stderr)
        sys.exit(1)
