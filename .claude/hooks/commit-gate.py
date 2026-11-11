#!/usr/bin/env python3
import glob
import hashlib
import json
import os
import re
import subprocess
import sys


def run(cmd, cwd, timeout=600, env=None):
    try:
        p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout, env=env)
    except subprocess.TimeoutExpired:
        return 1, " ".join(cmd) + " timed out"
    return p.returncode, (p.stdout + p.stderr).strip()


def pattern(text):
    out, i = "", 0
    while i < len(text):
        if text.startswith("**", i):
            out, i = out + ".*", i + 2
        elif text[i] == "*":
            out, i = out + "[^/]*", i + 1
        else:
            out, i = out + re.escape(text[i]), i + 1
    return re.compile(out + "$")


def covers(path):
    with open(path, encoding="utf-8") as f:
        head = re.match(r"---\n(.*?)\n---\n", f.read(), re.S)
    if not head:
        return []
    for line in head.group(1).splitlines():
        if line.startswith("covers:"):
            body = line.split(":", 1)[1].strip().strip("[]")
            return [item.strip() for item in body.split(",") if item.strip()]
    return []


def fingerprint(root, changed):
    state = hashlib.sha256()
    for name in sorted(changed):
        path = os.path.join(root, name)
        state.update(name.encode() + b"\0")
        if os.path.isfile(path):
            with open(path, "rb") as f:
                state.update(hashlib.sha256(f.read()).digest())
    return state.hexdigest()


def main():
    command = json.load(sys.stdin).get("tool_input", {}).get("command", "")
    if not re.search(r"\bgit\s+commit\b", command):
        return 0
    code, root = run(["git", "rev-parse", "--show-toplevel"], os.getcwd())
    if code:
        return 0
    tracked = run(["git", "diff", "--name-only", "HEAD"], root)[1].splitlines()
    untracked = run(["git", "ls-files", "--others", "--exclude-standard"], root)[1].splitlines()
    changed = {f for f in tracked + untracked if f}
    digest = fingerprint(root, changed)
    gated = [f for f in changed if not f.startswith("docs/") or f == "docs/7-system/README.md"]
    passed = os.path.join(root, ".git", "claude-check-pass")
    if gated and not (os.path.isfile(passed) and open(passed).read() == digest):
        env = dict(os.environ, GOPROXY="https://goproxy.cn,direct")
        for target in ["check"] + (["check-web"] if any(f.startswith("web/") for f in gated) else []):
            code, out = run(["make", target], root, timeout=300, env=env)
            if code:
                print(f"make {target} failed:\n" + out[-6000:], file=sys.stderr)
                return 2
        with open(passed, "w") as f:
            f.write(digest)

    staged = {f for f in run(["git", "diff", "--cached", "--name-only"], root)[1].splitlines() if f}
    args = re.search(r"\bgit\s+commit\b([^\n|;&<]*)", command).group(1)
    whole = re.search(r"\s(--all|-(?!-)[A-Za-z]*a[A-Za-z]*)(\s|$)", args)
    if staged and not whole:
        changed, digest = staged, run(["git", "write-tree"], root)[1]
    code_changed = [f for f in changed if not f.startswith("docs/")]
    stale = []
    for doc in sorted(glob.glob(os.path.join(root, "docs", "**", "*.md"), recursive=True)):
        name = os.path.relpath(doc, root)
        if name in changed:
            continue
        rules = [pattern(c) for c in covers(doc)]
        hits = sorted(f for f in code_changed if any(r.match(f) for r in rules))
        if hits:
            stale.append(f"{name}: " + ", ".join(hits[:6]) + (" …" if len(hits) > 6 else ""))
    if not stale:
        return 0
    mark = os.path.join(root, ".git", "claude-doc-gate")
    if os.path.isfile(mark) and open(mark).read() == digest:
        return 0
    with open(mark, "w") as f:
        f.write(digest)
    print("These specs cover the changed code but are not part of this change:\n" + "\n".join(stale) +
          "\nIf the change alters what a spec states (rules, specs or its gap list), update it in this commit."
          " If not, run the same commit again unchanged to proceed.", file=sys.stderr)
    return 2


sys.exit(main())
