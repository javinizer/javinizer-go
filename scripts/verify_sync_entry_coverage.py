#!/usr/bin/env python3
"""Verify production-binary refusal receipts; never manufacture coverage."""
import argparse
import hashlib
import json
from pathlib import Path, PurePosixPath
import re

from sync_entry_auth import authenticate_sources, replay_counters

PACKAGE = "github.com/javinizer/javinizer-go/cmd/javinizer-sync-e2e"
GUARD_PACKAGE = "github.com/javinizer/javinizer-go/test/offlineguard"
REFUSAL = "fixture marker must be a fixed regular file"


def verify(directory: Path, expected_commit: str, trusted_go: Path, trusted_go_sha256: str, trusted_covdata: Path, trusted_covdata_sha256: str) -> dict:
    directory = directory.resolve()
    if (directory / "receipt.json").is_symlink():
        raise ValueError("unsafe receipt")
    proof = json.loads((directory / "receipt.json").read_text())
    execution = Path(proof["execution_root"])
    if not execution.is_absolute() or ".." in execution.parts:
        raise ValueError("invalid execution root")
    if proof["commit"] != expected_commit or not re.fullmatch(r"[0-9a-f]{40}", expected_commit):
        raise ValueError("source commit mismatch")
    required = {"binary", "build.stdout", "build.stderr", "child.stdout", "child.stderr", "child.audit", "coverage-sync-entry.out", "canary.stdout", "canary.stderr", "canary.audit", "canary.exit", "build.exit", "child.exit", "convert.exit", "overlay.exit", "commands.json", "toolchain.json", "environment.json", "overlay-proof.json", "overlay/overlay.json", "overlay/manifest.json", "refusal.json", "source-proof.json", "stdlib-originals/LICENSE"}
    manifest = proof["files"]
    if not required <= manifest.keys():
        raise ValueError("missing required artifact")
    for name, digest in manifest.items():
        path = directory / name
        if not name or PurePosixPath(name).as_posix() != name or "\\" in name or Path(name).is_absolute() or ".." in Path(name).parts or any(parent.is_symlink() for parent in [path, *path.parents] if parent != directory and directory in parent.parents) or not path.is_file() or not path.resolve().is_relative_to(directory):
            raise ValueError("unsafe or absent artifact")
        if hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            raise ValueError("artifact hash mismatch: " + name)
    expected_canaries = {"TestExternalBeforeDNS", "TestDirectDialAndUDPWrites", "TestLoopbackAllowed", "TestHTTPExternalTargetBlockedBeforeProxy", "TestHTTPExternalTargetBlockedBeforeSOCKS", "TestInfrastructureProxyEndpointBlocked", "TestHTTPLoopbackAllowed"}
    events = [json.loads(line) for line in (directory / "canary.stdout").read_text().splitlines()]
    terminals = []
    for event in events:
        if event.get("Package") != GUARD_PACKAGE:
            raise ValueError("wrong guard package")
        if event.get("Action") in {"pass", "fail", "skip"}:
            if event["Action"] != "pass":
                raise ValueError("guard failure or skip")
            terminals.append(event.get("Test", ""))
    if len(terminals) != 8 or set(terminals) != expected_canaries | {""}:
        raise ValueError("guard canaries need unique roots and package PASS")
    for stage, expected in [("overlay", 0), ("canary", 0), ("build", 0), ("child", 1), ("convert", 0)]:
        if (directory / (stage + ".exit")).read_text() != str(expected) + "\n":
            raise ValueError("stage exit file mismatch")
        if stage != "overlay" and proof[stage + "_exit"] != expected:
            raise ValueError("stage exit receipt mismatch")
    toolchain = json.loads((directory / "toolchain.json").read_text())
    go = toolchain["go_executable"]
    goroot = Path(toolchain["GOROOT"])
    arch = {"x86_64": "amd64", "aarch64": "arm64", "ppc64le": "ppc64le", "s390x": "s390x", "riscv64": "riscv64"}.get(toolchain["native_machine"])
    if toolchain["GOVERSION"] != "go1.27.2" or not arch or toolchain["GOOS"] != "linux" or toolchain["GOHOSTOS"] != "linux" or toolchain["GOARCH"] != arch or toolchain["GOHOSTARCH"] != arch or toolchain["CGO_ENABLED"] != "1":
        raise ValueError("toolchain is not reviewed native Linux")
    if not goroot.is_absolute() or Path(go) != goroot / "bin/go" or not re.fullmatch(r"[0-9a-f]{64}", toolchain["go_sha256"]):
        raise ValueError("invalid selected tool identity")
    if toolchain["discovery_command"] != [go, "env", "-json", "GOVERSION", "GOROOT", "GOCACHE", "GOMODCACHE", "GOOS", "GOARCH", "GOHOSTOS", "GOHOSTARCH", "CGO_ENABLED"]:
        raise ValueError("unexpected discovery command")
    environments = json.loads((directory / "environment.json").read_text())
    if set(environments) != {"overlay", "canary", "build", "child", "convert"}:
        raise ValueError("missing effective environment")
    allowed = {"PATH", "HOME", "GOENV", "GOWORK", "GOFLAGS", "GOTOOLCHAIN", "GOPROXY", "GONOPROXY", "GOSUMDB", "GOVCS", "GOMAXPROCS", "GODEBUG", "GOCACHE", "GOMODCACHE", "GOROOT", "GOOS", "GOARCH", "CGO_ENABLED", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "TMPDIR", "GOTMPDIR", "OFFLINE279_AUDIT_LOG", "GOCOVERDIR", "JAVINIZER_SYNC_E2E_ROOT"}
    for stage, environment in environments.items():
        expected = {"GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local", "GOPROXY": "off", "GONOPROXY": "none", "GOSUMDB": "off", "GOVCS": "*:off", "GOMAXPROCS": "2", "GODEBUG": "netdns=go"}
        expected.update({key: toolchain[key] for key in ("GOROOT", "GOCACHE", "GOMODCACHE", "GOOS", "GOARCH", "CGO_ENABLED")})
        expected.update({key: str(execution / name) for key, name in [("HOME", "home"), ("XDG_CONFIG_HOME", "config"), ("XDG_CACHE_HOME", "cache"), ("XDG_DATA_HOME", "data"), ("TMPDIR", "tmp"), ("GOTMPDIR", "tmp")]})
        expected["GOFLAGS"] = "" if stage == "overlay" else "-overlay=" + str(execution / "overlay/overlay.json")
        if stage != "overlay":
            expected["OFFLINE279_AUDIT_LOG"] = str(execution / ("canary.audit" if stage == "canary" else "child.audit"))
        if stage == "child":
            expected.update(GOCOVERDIR=str(execution / "counters"), JAVINIZER_SYNC_E2E_ROOT=str(execution / "fixture"))
        if set(environment) - allowed or set(environment) != set(expected) | {"PATH"} or any(environment.get(k) != v for k, v in expected.items()) or environment["PATH"].split(":")[0] != str(goroot / "bin"):
            raise ValueError("effective environment mismatch")
    overlay = json.loads((directory / "overlay/overlay.json").read_text())["Replace"]
    overlay_manifest = json.loads((directory / "overlay/manifest.json").read_text())
    overlay_proof = json.loads((directory / "overlay-proof.json").read_text())
    if overlay_manifest["go_version"] != "go1.27.2" or len(overlay_proof) != 7 or len(overlay) != 7:
        raise ValueError("overlay inventory mismatch")
    names = {"dial.go", "lookup.go", "tcpsock_posix.go", "udpsock_posix.go", "iprawsock_posix.go", "sock_posix.go", "http/transport.go"}
    if set(overlay_manifest["source_sha256"]) != names or set(overlay_manifest["overlay_sha256"]) != names:
        raise ValueError("overlay source inventory mismatch")
    seen_originals = set()
    for row in overlay_proof:
        original = row["original"]
        name = Path(original).relative_to(goroot / "src/net").as_posix()
        replacement = "overlay/" + name
        if name not in names or original in seen_originals or row["replacement"] != replacement or overlay.get(original) != str(execution / replacement) or replacement not in manifest or row["original_sha256"] != overlay_manifest["source_sha256"][name] or row["replacement_sha256"] != overlay_manifest["overlay_sha256"][name] or manifest[replacement] != row["replacement_sha256"]:
            raise ValueError("overlay attribution mismatch")
        snapshot = "stdlib-originals/" + name
        if row.get("original_snapshot") != snapshot or manifest.get(snapshot) != row["original_sha256"]:
            raise ValueError("overlay original snapshot mismatch")
        seen_originals.add(original)
    source = json.loads((directory / "source-proof.json").read_text())
    if source["commit"] != proof["commit"] or source["tree"] != proof["tree"] or source["repo"] != proof["repo"]:
        raise ValueError("source snapshot attribution mismatch")
    rows = source["files"]
    required_sources = {"go.mod", "go.sum", "cmd/javinizer-sync-e2e/main.go", "cmd/javinizer-sync-e2e/fixture.go", "cmd/javinizer-sync-e2e/gate.go", "scripts/build_offline_test_overlay.py", "scripts/run_sync_entry_coverage.py", "scripts/verify_sync_entry_coverage.py", "scripts/sync_entry_auth.py", "test/offlineguard/go.mod", "test/offlineguard/guard_test.go", "test/offlineguard/proxy_test.go"}
    if {"source/" + name for name in rows} != {name for name in manifest if name.startswith("source/")}:
        raise ValueError("source snapshot inventory mismatch")
    if not required_sources <= rows.keys():
        raise ValueError("missing required source snapshot")
    for name, row in rows.items():
        snapshot = "source/" + name
        if snapshot not in manifest or row["sha256"] != manifest[snapshot]:
            raise ValueError("source snapshot hash mismatch")
        data = (directory / snapshot).read_bytes()
        blob = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        if row["git_blob"] != blob:
            raise ValueError("source Git blob mismatch")
    if {row["artifact"] for row in source["git_objects"].values()} != {name for name in manifest if name.startswith("git-objects/")}:
        raise ValueError("Git object manifest mismatch")
    authenticate_sources(directory, source, expected_commit)
    commands = json.loads((directory / "commands.json").read_text())
    if [row["stage"] for row in commands] != ["overlay", "canary", "build", "child", "convert"]:
        raise ValueError("command stage sequence mismatch")
    repo = Path(proof["repo"])
    if not repo.is_absolute() or not re.fullmatch(r"[0-9a-f]{40}", proof["tree"]):
        raise ValueError("invalid source tree attribution")
    for row in commands:
        expected_cwd = repo / "test/offlineguard" if row["stage"] == "canary" else repo
        if row["cwd"] != str(expected_cwd):
            raise ValueError("command working directory mismatch")
        if row["stage"] in {"build", "child", "convert"} and row["command"] != proof[row["stage"] + "_command"]:
            raise ValueError("command receipt mismatch")
    if commands[1]["command"] != [go, "test", "-short", "-p=1", "-count=1", "-timeout=120s", "-json", "./..."]:
        raise ValueError("wrong canary command")
    if commands[0]["command"][1:] != [str(repo / "scripts/build_offline_test_overlay.py"), "--output", str(execution / "overlay")]:
        raise ValueError("wrong overlay builder command")
    refusal = json.loads((directory / "refusal.json").read_text())
    if refusal != {"exit": proof["child_exit"], "before": proof["before"], "after": proof["after"]}:
        raise ValueError("refusal receipt mismatch")
    if not (directory / "canary.audit").read_bytes():
        raise ValueError("missing guard rejection audit")
    if (directory / "counters").is_symlink():
        raise ValueError("unsafe counter directory")
    counter_paths = list((directory / "counters").iterdir())
    if any(p.is_symlink() or not p.is_file() for p in counter_paths):
        raise ValueError("counter entries must be regular files")
    counters = {p.relative_to(directory).as_posix() for p in counter_paths}
    if counters != {n for n in manifest if n.startswith("counters/")}:
        raise ValueError("counter inventory mismatch")
    if not any(Path(n).name.startswith("covmeta.") for n in counters) or not any(Path(n).name.startswith("covcounters.") for n in counters):
        raise ValueError("missing real counter/metadata artifacts")
    if proof["build_exit"] != 0 or proof["convert_exit"] != 0 or proof["child_exit"] != 1:
        raise ValueError("unexpected process exit")
    expected_build = [go, "build", "-p=1", "-cover", "-covermode=atomic", "-coverpkg=" + PACKAGE, "-o", str(execution / "binary"), "./cmd/javinizer-sync-e2e"]
    if proof["build_command"] != expected_build or proof["child_command"] != [str(execution / "binary")]:
        raise ValueError("not the recorded production build/entry command")
    if proof["convert_command"] != [go, "tool", "covdata", "textfmt", "-i=" + str(execution / "counters"), "-o=" + str(execution / "coverage-sync-entry.out")]:
        raise ValueError("unexpected conversion command")
    if (directory / "child.stdout").read_bytes() or (directory / "child.audit").read_bytes():
        raise ValueError("unexpected stdout or network audit")
    stderr = (directory / "child.stderr").read_text()
    if not re.fullmatch(r"\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} " + re.escape(REFUSAL) + r"\n", stderr):
        raise ValueError("wrong refusal diagnostic")
    if proof["before"] != proof["after"] or set(proof["before"]) != {".", "foreign.txt"}:
        raise ValueError("fixture changed or wrong admission fixture")
    if proof["before"]["."]["mode"] != 0o700 or proof["before"]["foreign.txt"]["mode"] != 0o600:
        raise ValueError("fixture permissions not private")
    rows = (directory / "coverage-sync-entry.out").read_text().splitlines()
    if not rows or rows[0] != "mode: atomic":
        raise ValueError("not atomic coverage")
    hit = set()
    target_blocks = set()
    seen = set()
    for row in rows[1:]:
        match = re.fullmatch(r"(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)", row)
        if not match:
            raise ValueError("malformed coverage row")
        file, start, start_column, end, end_column, statements, count = match.groups()
        key = (file, start, start_column, end, end_column)
        if key in seen:
            raise ValueError("duplicate coverage block")
        seen.add(key)
        if not file.startswith(PACKAGE + "/"):
            raise ValueError("unexpected covered package")
        if file == PACKAGE + "/main.go" and int(statements) > 0 and int(count) > 0:
            hit.update(range(int(start), int(end) + 1))
            target_blocks.add((int(start), int(start_column), int(end), int(end_column), int(statements)))
    if not {38, 39} <= hit or not {(38, 2, 38, 30, 1), (39, 3, 40, 1, 1)} <= target_blocks:
        raise ValueError("production main refusal targets not hit")
    replay = replay_counters(directory, trusted_go, trusted_go_sha256, trusted_covdata, trusted_covdata_sha256)
    return {"counter_replay": replay, "commit": expected_commit, "contract": "production CLI refuses unmarked private root", "exit": 1, "main_lines_hit": [38, 39], "fixture_unchanged": True}


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("directory", type=Path)
    parser.add_argument("--expected-commit", required=True)
    parser.add_argument("--trusted-go", required=True, type=Path, help="caller-trusted Go1.27.2 executable outside archive")
    parser.add_argument("--trusted-go-sha256", required=True, help="trusted executable digest supplied independently of archive")
    parser.add_argument("--trusted-covdata", required=True, type=Path, help="caller-trusted covdata executable from same reviewed distribution, outside archive")
    parser.add_argument("--trusted-covdata-sha256", required=True, help="independently trusted covdata executable digest")
    args = parser.parse_args()
    print(json.dumps(verify(args.directory, args.expected_commit, args.trusted_go, args.trusted_go_sha256, args.trusted_covdata, args.trusted_covdata_sha256), sort_keys=True))
