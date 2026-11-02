#!/usr/bin/env python3
"""Run one guarded production CLI refusal; preserve first logs and counters."""
import argparse
import hashlib
import json
import os
import platform
import shutil
from pathlib import Path
import stat
import subprocess
import sys

from verify_sync_entry_coverage import PACKAGE, verify


def snapshot(root: Path) -> dict:
    result = {}
    for path in [root, *sorted(root.rglob("*"))]:
        if path.is_symlink():
            raise RuntimeError("unexpected fixture symlink")
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        try:
            info = os.fstat(fd)  # eager identity, no lazy path-based comparison
            digest = None
            if stat.S_ISREG(info.st_mode):
                with os.fdopen(os.dup(fd), "rb") as stream:
                    digest = hashlib.file_digest(stream, "sha256").hexdigest()
            elif not stat.S_ISDIR(info.st_mode):
                raise RuntimeError("unexpected fixture file kind")
            result[path.relative_to(root).as_posix()] = {"device": info.st_dev, "inode": info.st_ino, "mode": stat.S_IMODE(info.st_mode), "size": info.st_size, "mtime_ns": info.st_mtime_ns, "sha256": digest}
        finally:
            os.close(fd)
    return result


def initial_environment() -> dict:
    # An allowlist, not inherited Go configuration: ambient GOENV, GOWORK,
    # GOFLAGS, architecture overrides and compiler flags cannot replace source.
    env = {key: os.environ[key] for key in ("PATH", "HOME") if key in os.environ}
    env.update(GOENV="off", GOWORK="off", GOFLAGS="", GOTOOLCHAIN="local", GOPROXY="off", GONOPROXY="none", GOSUMDB="off", GOVCS="*:off", GOMAXPROCS="2", GODEBUG="netdns=go")
    for key in ("GOCACHE", "GOMODCACHE"):
        if key in os.environ:
            if not Path(os.environ[key]).is_absolute():
                raise RuntimeError("explicit cache must be absolute: " + key)
            env[key] = os.environ[key]
    return env


def git_output(repo: Path, env: dict, *args: str) -> bytes:
    return subprocess.check_output(["git", *args], cwd=repo, env=env)


def record_and_validate_toolchain(out: Path, selected_go: str, discovery: list[str], toolchain: dict) -> None:
    # Archive the actual discovered values before admission can reject them.
    # The existing always-upload step includes *.json, including this receipt
    # when no overlay, build, child execution or coverage profile exists yet.
    toolchain.update(go_executable=selected_go, go_sha256=hashlib.sha256(Path(selected_go).read_bytes()).hexdigest(), discovery_command=discovery, native_machine=platform.machine())
    (out / "toolchain.json").write_text(json.dumps(toolchain, indent=2) + "\n")
    native_arch = {"x86_64": "amd64", "aarch64": "arm64", "ppc64le": "ppc64le", "s390x": "s390x", "riscv64": "riscv64"}.get(platform.machine())
    if toolchain["GOVERSION"] != "go1.27.2" or toolchain["GOOS"] != "linux" or toolchain["GOHOSTOS"] != "linux" or not native_arch or toolchain["GOARCH"] != native_arch or toolchain["GOHOSTARCH"] != native_arch or toolchain["CGO_ENABLED"] != "1":
        raise RuntimeError("reviewed native Linux cgo toolchain required; actual values preserved in toolchain.json")
    if Path(selected_go) != (Path(toolchain["GOROOT"]) / "bin/go").resolve():
        raise RuntimeError("selected Go executable disagrees with GOROOT; actual values preserved in toolchain.json")
    toolchain["GOROOT"] = str(Path(toolchain["GOROOT"]).resolve())
    (out / "toolchain.json").write_text(json.dumps(toolchain, indent=2) + "\n")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", required=True, type=Path, help="new evidence directory; never reused")
    parser.add_argument("--trusted-covdata", required=True, type=Path, help="external converter prepared from trusted reviewed distribution")
    parser.add_argument("--trusted-covdata-sha256", required=True)
    args = parser.parse_args()
    if sys.platform != "linux":
        raise SystemExit("native Linux refusal contract required (not a skipped success)")
    repo = Path(__file__).resolve().parents[1]
    out = args.output.absolute()
    out.mkdir(mode=0o700, parents=False, exist_ok=False)
    out = out.resolve()
    env = initial_environment()
    selected_go = shutil.which("go", path=env.get("PATH"))
    if not selected_go:
        raise RuntimeError("no selected Go executable")
    selected_go = str(Path(selected_go).resolve())
    discovery = [selected_go, "env", "-json", "GOVERSION", "GOROOT", "GOCACHE", "GOMODCACHE", "GOOS", "GOARCH", "GOHOSTOS", "GOHOSTARCH", "CGO_ENABLED"]
    toolchain = json.loads(subprocess.check_output(discovery, cwd=repo, env=env, text=True))
    record_and_validate_toolchain(out, selected_go, discovery, toolchain)
    for key in ("GOCACHE", "GOMODCACHE", "GOROOT", "GOOS", "GOARCH", "CGO_ENABLED"):
        env[key] = toolchain[key]
    env["PATH"] = str(Path(selected_go).parent) + os.pathsep + env.get("PATH", "")
    for key, name in [("HOME", "home"), ("XDG_CONFIG_HOME", "config"), ("XDG_CACHE_HOME", "cache"), ("XDG_DATA_HOME", "data"), ("TMPDIR", "tmp"), ("GOTMPDIR", "tmp")]:
        path = out / name
        path.mkdir(mode=0o700, exist_ok=True)
        env[key] = str(path)
    commands = []
    environments = {}

    def run(label: str, command: list[str], run_env: dict, timeout: int, cwd: Path = repo) -> int:
        commands.append({"stage": label, "command": command, "cwd": str(cwd)})
        # run_env is constructed from the explicit nonsecret allowlist above.
        environments[label] = dict(run_env)
        (out / "environment.json").write_text(json.dumps(environments, indent=2) + "\n")
        (out / "commands.json").write_text(json.dumps(commands, indent=2) + "\n")
        with (out / (label + ".stdout")).open("xb") as stdout, (out / (label + ".stderr")).open("xb") as stderr:
            try:
                completed = subprocess.run(command, cwd=cwd, env=run_env, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr, timeout=timeout, check=False)
            except subprocess.TimeoutExpired:
                (out / (label + ".timeout")).write_text(str(timeout) + "\n")
                raise
        (out / (label + ".exit")).write_text(str(completed.returncode) + "\n")
        return completed.returncode

    commit = git_output(repo, env, "rev-parse", "HEAD").decode().strip()
    if git_output(repo, env, "status", "--porcelain", "--untracked-files=no"):
        raise RuntimeError("tracked source must be frozen")
    if git_output(repo, env, "ls-files", "--others", "--", "*.go"):
        raise RuntimeError("untracked Go source prevents source attribution")
    # Reuse the reviewed repository overlay builder, including its pinned source
    # hashes. The helper never edits the toolchain or production repository files.
    overlay_command = [sys.executable, str(repo / "scripts/build_offline_test_overlay.py"), "--output", str(out / "overlay")]
    if run("overlay", overlay_command, env, 120) != 0:
        raise RuntimeError("offline overlay preparation failed")
    env["GOFLAGS"] = "-overlay=" + str(out / "overlay/overlay.json")
    env["OFFLINE279_AUDIT_LOG"] = str(out / "canary.audit")
    (out / "canary.audit").touch()
    if run("canary", [selected_go, "test", "-short", "-p=1", "-count=1", "-timeout=120s", "-json", "./..."], env, 180, repo / "test/offlineguard") != 0:
        raise RuntimeError("offline guard admission failed")
    fixture = out / "fixture"
    fixture.mkdir(mode=0o700)
    fixture.chmod(0o700)
    (fixture / "foreign.txt").write_bytes(b"unrelated owned data must survive refusal\n")
    (fixture / "foreign.txt").chmod(0o600)
    before = snapshot(fixture)
    counters = out / "counters"
    counters.mkdir(mode=0o700)
    env["OFFLINE279_AUDIT_LOG"] = str(out / "child.audit")
    (out / "child.audit").touch()
    build = [selected_go, "build", "-p=1", "-cover", "-covermode=atomic", "-coverpkg=" + PACKAGE, "-o", str(out / "binary"), "./cmd/javinizer-sync-e2e"]
    build_exit = run("build", build, env, 300)
    if build_exit != 0:
        raise RuntimeError("production build failed")
    child_env = {**env, "GOCOVERDIR": str(counters), "JAVINIZER_SYNC_E2E_ROOT": str(fixture)}
    child = [str(out / "binary")]
    child_exit = run("child", child, child_env, 30)
    after = snapshot(fixture)
    # Record the refusal before conversion, so an unexpected exit or timeout
    # is never hidden by a rerun or discarded counter directory.
    (out / "refusal.json").write_text(json.dumps({"exit": child_exit, "before": before, "after": after}, indent=2) + "\n")
    if child_exit != 1 or before != after:
        raise RuntimeError("production refusal contract failed")
    convert = [selected_go, "tool", "covdata", "textfmt", "-i=" + str(counters), "-o=" + str(out / "coverage-sync-entry.out")]
    convert_exit = run("convert", convert, env, 120)
    if convert_exit != 0:
        raise RuntimeError("counter conversion failed")
    overlay = json.loads((out / "overlay/overlay.json").read_text())["Replace"]
    overlay_sources = []
    for original, replacement in sorted(overlay.items()):
        original_path, replacement_path = Path(original), Path(replacement)
        relative = original_path.relative_to(Path(toolchain["GOROOT"]) / "src/net")
        if replacement_path != out / "overlay" / relative:
            raise RuntimeError("overlay replacement outside owned mapping")
        original_snapshot = "stdlib-originals/" + relative.as_posix()
        (out / original_snapshot).parent.mkdir(parents=True, exist_ok=True)
        (out / original_snapshot).write_bytes(original_path.read_bytes())
        overlay_sources.append({"original": str(original_path), "original_snapshot": original_snapshot, "replacement": replacement_path.relative_to(out).as_posix(), "original_sha256": hashlib.sha256(original_path.read_bytes()).hexdigest(), "replacement_sha256": hashlib.sha256(replacement_path.read_bytes()).hexdigest()})
    (out / "overlay-proof.json").write_text(json.dumps(overlay_sources, indent=2) + "\n")
    tree = git_output(repo, env, "rev-parse", "HEAD^{tree}").decode().strip()
    paths = git_output(repo, env, "ls-files", "--", "go.mod", "go.sum", "cmd/javinizer-sync-e2e", "test/offlineguard", "scripts/build_offline_test_overlay.py", "scripts/run_sync_entry_coverage.py", "scripts/verify_sync_entry_coverage.py", "scripts/sync_entry_auth.py").decode().splitlines()
    source_files = {}
    for name in paths:
        data = (repo / name).read_bytes()
        blob = hashlib.sha1(b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
        if blob != git_output(repo, env, "rev-parse", "HEAD:" + name).decode().strip():
            raise RuntimeError("source snapshot differs from frozen commit")
        target = out / "source" / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(data)
        source_files[name] = {"sha256": hashlib.sha256(data).hexdigest(), "git_blob": blob}
    git_objects = {}
    (out / "git-objects").mkdir()
    object_ids = {commit: "commit", tree: "tree"}
    for name in paths:
        for parent in Path(name).parents:
            if parent.as_posix() != ".":
                oid = git_output(repo, env, "rev-parse", "HEAD:" + parent.as_posix()).decode().strip()
                object_ids[oid] = "tree"
    for oid, kind in object_ids.items():
        artifact = "git-objects/" + oid
        (out / artifact).write_bytes(git_output(repo, env, "cat-file", kind, oid))
        git_objects[oid] = {"type": kind, "artifact": artifact}
    (out / "source-proof.json").write_text(json.dumps({"commit": commit, "tree": tree, "repo": str(repo), "files": source_files, "git_objects": git_objects}, indent=2) + "\n")
    artifacts = ["binary", "build.stdout", "build.stderr", "child.stdout", "child.stderr", "child.audit", "canary.stdout", "canary.stderr", "canary.audit", "coverage-sync-entry.out", "commands.json", "refusal.json", "toolchain.json", "environment.json", "overlay-proof.json", "overlay/overlay.json", "overlay/manifest.json", "canary.exit", "build.exit", "child.exit", "convert.exit", "overlay.exit"]
    license_data = (Path(toolchain["GOROOT"]) / "LICENSE").read_bytes()
    (out / "stdlib-originals/LICENSE").write_bytes(license_data)
    artifacts += ["stdlib-originals/LICENSE"]
    artifacts += [row["artifact"] for row in git_objects.values()]
    artifacts += ["source-proof.json"] + ["source/" + name for name in source_files]
    artifacts += [row["original_snapshot"] for row in overlay_sources]
    artifacts += [row["replacement"] for row in overlay_sources]
    artifacts += [p.relative_to(out).as_posix() for p in sorted(counters.iterdir())]
    files = {name: hashlib.sha256((out / name).read_bytes()).hexdigest() for name in artifacts}
    if git_output(repo, env, "rev-parse", "HEAD").decode().strip() != commit or git_output(repo, env, "diff", "HEAD", "--name-only"):
        raise RuntimeError("source changed during the first execution")
    if hashlib.sha256(Path(selected_go).read_bytes()).hexdigest() != toolchain["go_sha256"]:
        raise RuntimeError("selected tool changed during execution")
    receipt = {"execution_root": str(out), "commit": commit, "tree": git_output(repo, env, "rev-parse", "HEAD^{tree}").decode().strip(), "repo": str(repo), "canary_exit": 0, "build_exit": build_exit, "convert_exit": convert_exit, "child_exit": child_exit, "build_command": build, "child_command": child, "convert_command": convert, "before": before, "after": after, "files": files}
    (out / "receipt.json").write_text(json.dumps(receipt, indent=2) + "\n")
    result = verify(out, commit, Path(selected_go), toolchain["go_sha256"], args.trusted_covdata, args.trusted_covdata_sha256)
    (out / "proof.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
