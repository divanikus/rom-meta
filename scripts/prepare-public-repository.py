#!/usr/bin/env python3
"""Copy an explicit selection of tracked files, without Git history or user data."""
import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import subprocess
import tempfile


def safe_path(value):
    path = PurePosixPath(value)
    if not value or "\\" in value or path.is_absolute() or any(p in (".", "..", ".git") for p in value.rstrip("/").split("/")) or ":" in value:
        raise ValueError("Unsafe manifest path: " + value)
    return value


def selection(root, config, tracked):
    if config.get("schema_version") != 1 or set(config) != {"schema_version", "source_roots", "files"}:
        raise ValueError("Unknown public manifest schema")
    exact = {safe_path(v) for v in config["files"]}
    prefixes = [safe_path(v) for v in config["source_roots"]]
    if any(not v.endswith("/") for v in prefixes):
        raise ValueError("Source roots must end with /")
    tracked = {safe_path(v) for v in tracked}
    missing = exact - tracked
    if missing:
        raise ValueError("Public files must be tracked: " + ", ".join(sorted(missing)))
    if any(not any(p.startswith(v) for p in tracked) for v in prefixes):
        raise ValueError("A public source root has no tracked files")
    selected = sorted(p for p in tracked if p in exact or any(p.startswith(v) for v in prefixes))
    result = []
    for relative in selected:
        path = root / relative
        if any(part in ("tmp", "bin", "__pycache__", ".rommeta", "private", "work") for part in PurePosixPath(relative).parts):
            raise ValueError("Runtime path selected: " + relative)
        if path.name in (".env", "deployment.env", "secrets.yaml") or path.suffix.lower() in (".pyc", ".exe", ".db", ".sqlite", ".zip", ".chd", ".iso"):
            raise ValueError("Runtime/data file selected: " + relative)
        if path.is_symlink() or any(p.is_symlink() for p in path.parents if p != root.parent) or not path.is_file():
            raise ValueError("Public input must be a regular file: " + relative)
        data = path.read_bytes()
        result.append(dict(path=relative, size=len(data), sha256=hashlib.sha256(data).hexdigest()))
    return result


def materialize(root, out, rows):
    if out.exists():
        raise FileExistsError("Output already exists: " + str(out))
    out.parent.mkdir(parents=True, exist_ok=True)
    stage = Path(tempfile.mkdtemp(prefix="." + out.name + "-", dir=out.parent))
    try:
        for row in rows:
            source = root / row["path"]
            target = stage / row["path"]
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source, target)
            if hashlib.sha256(target.read_bytes()).hexdigest() != row["sha256"]:
                raise ValueError("Input changed during preparation: " + row["path"])
        if out.exists():
            raise FileExistsError("Output appeared during preparation: " + str(out))
        os.rename(stage, out)
    finally:
        if stage.exists():
            shutil.rmtree(stage)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, type=Path, help="New directory, without .git")
    parser.add_argument("--apply", action="store_true", help="Copy after validation; default is preview")
    parser.add_argument("--report", type=Path, help="New private selection/hash report, outside output")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    config = json.loads((root / "packaging/public-files.json").read_text(encoding="utf-8"))
    tracked = subprocess.check_output(["git", "ls-files", "-z"], cwd=root).decode("utf-8").rstrip("\0").split("\0")
    rows = selection(root, config, tracked)
    out = args.out.resolve()
    if out.exists():
        parser.error("Output already exists")
    if args.report and (args.report.exists() or args.report.resolve().is_relative_to(out)):
        parser.error("Report must be new and outside output")
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root).decode().strip()
    if args.apply:
        materialize(root, out, rows)
    if args.report:
        report = dict(source_revision=revision, applied=args.apply, files=rows)
        with args.report.open("x", encoding="utf-8") as stream:
            json.dump(report, stream, indent=2)
            stream.write("\n")
    print(f"{len(rows)} selected tracked files, {sum(v['size'] for v in rows)} bytes; " + ("copied without .git" if args.apply else "preview only, no files copied"))
    print(out)


if __name__ == "__main__":
    main()
