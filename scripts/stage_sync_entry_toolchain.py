#!/usr/bin/env python3
"""Copy a verified whole Go distribution outside its immutable module cache."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import stat

HEADROOM = 512 * 1024 * 1024


def inventory(root: Path) -> dict:
    result = {}
    for path in [root, *sorted(root.rglob("*"))]:
        info = path.lstat()
        row = {"mode": stat.S_IMODE(info.st_mode)}
        if stat.S_ISDIR(info.st_mode):
            row["kind"] = "directory"
        elif stat.S_ISREG(info.st_mode):
            row.update(kind="file", size=info.st_size)
            with path.open("rb") as stream:
                row["sha256"] = hashlib.file_digest(stream, "sha256").hexdigest()
        else:
            raise ValueError("toolchain contains symlink or special file: " + str(path))
        result[path.relative_to(root).as_posix()] = row
    return result


def stage(source: Path, destination: Path, module_cache: Path, receipt: Path) -> dict:
    source = source.resolve(strict=True)
    module_cache = module_cache.resolve(strict=True)
    destination = destination.absolute()
    if destination.exists() or destination.is_symlink():
        raise ValueError("destination must initially be absent")
    parent = destination.parent.resolve(strict=True)
    if parent != destination.parent or parent.stat().st_uid != os.getuid():
        raise ValueError("destination parent must be canonical and owned")
    if destination.is_relative_to(source) or source.is_relative_to(destination) or destination.is_relative_to(module_cache):
        raise ValueError("destination must be separate from source and module cache")
    if receipt.exists() or receipt.is_symlink() or receipt.absolute().is_relative_to(destination):
        raise ValueError("receipt must be new and outside destination")
    before = inventory(source)
    for required in ("bin/go", "src/net/dial.go", "pkg/tool", "VERSION", "LICENSE"):
        if required not in before:
            raise ValueError("incomplete Go distribution: " + required)
    size = sum(row.get("size", 0) for row in before.values())
    # Include per-entry filesystem overhead as well as a fixed remaining reserve.
    required = size + len(before) * 4096 + HEADROOM
    free = shutil.disk_usage(parent).free
    proof = {"source": str(source), "destination": str(destination), "module_cache": str(module_cache),
             "source_bytes": size, "required_free_bytes": required, "free_bytes_before": free,
             "inventory": before, "status": "preflight"}
    def save():
        receipt.write_text(json.dumps(proof, indent=2) + "\n")
    save()
    if free < required:
        proof["status"] = "insufficient-capacity"
        save()
        raise RuntimeError("insufficient space for whole toolchain plus reserve")
    try:
        shutil.copytree(source, destination, symlinks=True, copy_function=shutil.copy2)
        if inventory(source) != before or inventory(destination) != before:
            raise RuntimeError("whole toolchain inventory changed during staging")
        proof["status"] = "verified"
        save()
        return proof
    except Exception as error:
        proof.update(status="failed", error=str(error))
        save()
        # Retain partial output for diagnosis; it is never admitted as verified.
        raise


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--destination", type=Path, required=True)
    parser.add_argument("--module-cache", type=Path, required=True)
    parser.add_argument("--receipt", type=Path, required=True)
    args = parser.parse_args()
    stage(args.source, args.destination, args.module_cache, args.receipt)


if __name__ == "__main__":
    main()
