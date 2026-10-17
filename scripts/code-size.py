#!/usr/bin/env python3
"""Count handwritten production source separately from tests and generated code."""
import argparse
import collections
import io
import json
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SOURCE = {".go", ".rs", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".mts",
          ".py", ".sh", ".css", ".lean", ".proto", ".wat", ".html"}


def category(path, content):
    if path.parts[0] == "docs":
        return "data/docs/config"
    name = path.as_posix()
    if "/gen/" in "/" + name or "/generated/" in "/" + name or re.search(
            r"^\s*(?://|#|/\*|\*)\s*(?:Code generated|@generated)\b", content[:700], re.MULTILINE):
        return "generated"
    if path.suffix not in SOURCE:
        return "data/docs/config"
    if any(marker in name for marker in (".test.", ".spec.", ".fixture.", "/e2e/",
                                        "/testdata/", "/tests/", "/test/")) or name.endswith(("_test.go", "_test.py")):
        return "tests"
    if name.startswith(("scripts/", "deploy/", ".github/")) or ".config." in name or "catalog.examples." in name or "catalog.fixtures." in name:
        return "build/examples"
    return "production"


def measure(root, paths):
    selected = sorted({name for name in paths if name and (root / name).is_file()})
    with tempfile.NamedTemporaryFile(mode="w") as listing:
        listing.write("\n".join(str(root / name) for name in selected))
        listing.flush()
        result = json.loads(subprocess.check_output(["cloc", "--by-file", "--json", "--quiet",
                            "--skip-uniqueness", "--list-file=" + listing.name], text=True))
    totals = collections.defaultdict(collections.Counter)
    groups = collections.defaultdict(collections.Counter)
    files = {}
    for absolute, counts in result.items():
        if absolute in ("header", "SUM"):
            continue
        path = Path(absolute).relative_to(root)
        content = (root / path).read_text(errors="replace")
        kind = category(path, content)
        metrics = {"files": 1, "physical": len(content.splitlines()),
                   "code": counts["code"], "comment": counts["comment"], "blank": counts["blank"]}
        totals[kind].update(metrics)
        group = "/".join(path.parts[:3]) if path.parts[0] == "web" else "/".join(path.parts[:2]) if path.parts[0] == "capabilities" else path.parts[0]
        if kind == "production":
            groups[group].update(metrics)
        files[str(path)] = {"category": kind, **metrics}
    return {"categories": dict(totals), "production": dict(groups), "files": files}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--revision", help="Measure an immutable Git revision instead of the working tree")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    with tempfile.TemporaryDirectory() as directory:
        if args.revision:
            revision = subprocess.check_output(["git", "rev-parse", args.revision], cwd=ROOT, text=True).strip()
            archive = subprocess.check_output(["git", "archive", revision], cwd=ROOT)
            with tarfile.open(fileobj=io.BytesIO(archive)) as source:
                source.extractall(directory)
            root = Path(directory)
            paths = subprocess.check_output(["git", "ls-tree", "-rz", "--name-only", revision], cwd=ROOT).decode().split("\0")
        else:
            root, revision = ROOT, "working-tree"
            paths = subprocess.check_output(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"], cwd=ROOT).decode().split("\0")
        report = {"revision": revision, "method": "cloc code lines; tracked source; tests, generated code, examples, build and data counted separately", **measure(root, paths)}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
    print(json.dumps({"revision": revision, "categories": report["categories"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
