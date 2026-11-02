"""Authenticate source Git membership and replay counters with a caller-trusted tool."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import shutil
import tempfile


def git_hash(kind: str, data: bytes) -> str:
    return hashlib.sha1(kind.encode() + b" " + str(len(data)).encode() + b"\0" + data).hexdigest()


def authenticate_sources(directory: Path, source: dict, expected_commit: str) -> None:
    objects = source["git_objects"]
    visited = set()
    def read(oid, kind):
        if not re.fullmatch(r"[0-9a-f]{40}", oid):
            raise ValueError("invalid Git object identity")
        row = objects.get(oid)
        if row != {"type": kind, "artifact": "git-objects/" + oid}:
            raise ValueError("missing Git ancestry object")
        data = (directory / row["artifact"]).read_bytes()
        if git_hash(kind, data) != oid:
            raise ValueError("Git object hash mismatch")
        visited.add(oid)
        return data
    commit = read(expected_commit, "commit")
    header = commit.split(b"\n\n", 1)[0].splitlines()
    tree_rows = [line[5:] for line in header if line.startswith(b"tree ")]
    if len(tree_rows) != 1:
        raise ValueError("invalid commit tree")
    root = tree_rows[0].decode("ascii")
    if root != source["tree"]:
        raise ValueError("commit root tree mismatch")
    cache = {}
    def tree(oid):
        if oid in cache:
            return cache[oid]
        raw, entries = read(oid, "tree"), {}
        while raw:
            prefix, separator, tail = raw.partition(b"\0")
            if not separator or len(tail) < 20:
                raise ValueError("malformed Git tree")
            mode, separator, name = prefix.partition(b" ")
            if not separator or not name or b"/" in name or name in {b".", b".."} or name in entries:
                raise ValueError("invalid Git tree entry")
            entries[name] = (mode, tail[:20].hex())
            raw = tail[20:]
        cache[oid] = entries
        return entries
    for name, row in source["files"].items():
        path = PurePosixPath(name)
        if path.is_absolute() or path.as_posix() != name or ".." in path.parts:
            raise ValueError("invalid source path")
        oid = root
        for index, part in enumerate(path.parts):
            mode, child = tree(oid).get(part.encode(), (None, None))
            if index < len(path.parts) - 1:
                if mode != b"40000":
                    raise ValueError("source ancestor is not a Git tree")
                oid = child
            elif mode not in {b"100644", b"100755"} or child != row["git_blob"]:
                raise ValueError("source blob is not a member of expected commit")
    if set(objects) != visited:
        raise ValueError("unexpected Git ancestry objects")


def replay_counters(directory: Path, trusted_go: Path, trusted_sha256: str, trusted_covdata: Path, trusted_covdata_sha256: str) -> dict:
    # Tool identity comes only from explicit caller arguments, never the archive.
    if not trusted_go.is_absolute() or not re.fullmatch(r"[0-9a-f]{64}", trusted_sha256):
        raise ValueError("explicit absolute trusted tool and SHA256 required")
    go = trusted_go.resolve(strict=True)
    if go.is_relative_to(directory.resolve()) or not go.is_file() or not os.access(go, os.X_OK):
        raise ValueError("trusted tool must be executable outside archive")
    digest = hashlib.sha256(go.read_bytes()).hexdigest()
    if digest != trusted_sha256:
        raise ValueError("trusted tool hash mismatch")
    if not trusted_covdata.is_absolute() or not re.fullmatch(r"[0-9a-f]{64}", trusted_covdata_sha256):
        raise ValueError("explicit absolute trusted covdata and SHA256 required")
    covdata = trusted_covdata.resolve(strict=True)
    if covdata.is_relative_to(directory.resolve()) or not covdata.is_file() or not os.access(covdata, os.X_OK):
        raise ValueError("trusted covdata must be executable outside archive")
    if hashlib.sha256(covdata.read_bytes()).hexdigest() != trusted_covdata_sha256:
        raise ValueError("trusted covdata hash mismatch")
    root = Path(tempfile.mkdtemp(prefix="sync-entry-replay-"))
    try:
        env = {"PATH": str(go.parent), "HOME": str(root), "TMPDIR": str(root),
               "GOENV": "off", "GOWORK": "off", "GOFLAGS": "", "GOTOOLCHAIN": "local",
               "GOPROXY": "off", "GONOPROXY": "none", "GOSUMDB": "off", "GOVCS": "*:off"}
        (root / "environment.json").write_text(json.dumps(env, indent=2))
        version_command = [str(go), "env", "GOVERSION"]
        (root / "version-command.json").write_text(json.dumps(version_command))
        version = subprocess.run(version_command, cwd=root, env=env,
                                 stdin=subprocess.DEVNULL, capture_output=True, timeout=30, check=False)
        (root / "version.stdout").write_bytes(version.stdout)
        (root / "version.stderr").write_bytes(version.stderr)
        (root / "version.exit").write_text(str(version.returncode))
        if version.returncode or version.stdout != b"go1.27.2\n":
            raise ValueError("trusted converter must be reviewed Go1.27.2")
        metadata_command = [str(go), "version", "-m", str(covdata)]
        (root / "converter-command.json").write_text(json.dumps(metadata_command))
        metadata = subprocess.run(metadata_command, cwd=root, env=env, stdin=subprocess.DEVNULL,
                                  capture_output=True, timeout=30, check=False)
        (root / "converter.stdout").write_bytes(metadata.stdout)
        (root / "converter.stderr").write_bytes(metadata.stderr)
        (root / "converter.exit").write_text(str(metadata.returncode))
        lines = metadata.stdout.decode().splitlines()
        if metadata.returncode or not lines or not lines[0].endswith(": go1.27.2") or "\tpath\tcmd/covdata" not in lines:
            raise ValueError("trusted converter is not Go1.27.2 cmd/covdata")
        output = root / "replayed.out"
        command = [str(covdata), "textfmt", "-i=" + str(directory / "counters"), "-o=" + str(output)]
        (root / "convert-command.json").write_text(json.dumps(command))
        result = subprocess.run(command, cwd=root, env=env, stdin=subprocess.DEVNULL,
                                capture_output=True, timeout=120, check=False)
        (root / "convert.stdout").write_bytes(result.stdout)
        (root / "convert.stderr").write_bytes(result.stderr)
        (root / "convert.exit").write_text(str(result.returncode))
        if result.returncode or not output.is_file():
            raise ValueError("raw counter conversion failed: " + result.stderr.decode(errors="replace")[:2000])
        if output.read_bytes() != (directory / "coverage-sync-entry.out").read_bytes():
            raise ValueError("raw counters disagree with archived text profile")
        if hashlib.sha256(go.read_bytes()).hexdigest() != digest or hashlib.sha256(covdata.read_bytes()).hexdigest() != trusted_covdata_sha256:
            raise ValueError("trusted tool changed during replay")
        proof = {"trusted_go": str(go), "trusted_go_sha256": digest, "trusted_covdata": str(covdata), "trusted_covdata_sha256": trusted_covdata_sha256, "version": "go1.27.2",
                "command": command, "exit": result.returncode, "stderr": result.stderr.decode(errors="replace"),
                "profile_sha256": hashlib.sha256(output.read_bytes()).hexdigest()}
        shutil.rmtree(root)
        return proof
    except Exception as error:
        (root / "failure.txt").write_text(str(error) + "\n")
        raise ValueError(str(error) + "; replay diagnostics retained at " + str(root)) from error
