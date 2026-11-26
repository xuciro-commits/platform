#!/usr/bin/env python3
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


def names(root, *args):
    return {f for f in run(["git", *args], root)[1].splitlines() if f}


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
    commit = re.search(r"\bgit\s+commit\b([^\n|;&<]*)", command)
    if not commit:
        return 0
    code, root = run(["git", "rev-parse", "--show-toplevel"], os.getcwd())
    if code:
        return 0
    tree = names(root, "diff", "--name-only", "HEAD") | names(root, "ls-files", "--others", "--exclude-standard")
    staged = names(root, "diff", "--cached", "--name-only")
    whole = re.search(r"\s(--all|-(?!-)[A-Za-z]*a[A-Za-z]*)(\s|$)", commit.group(1))
    scope = staged if staged and not whole else tree
    if not scope:
        return 0
    targets = ["check"] + [f"check-{side}" for side in ("frontend",) if any(f.startswith(side + "/") for f in scope)]
    state = fingerprint(root, tree) + " " + " ".join(targets)
    passed = os.path.join(run(["git", "rev-parse", "--absolute-git-dir"], root)[1], "claude-check-pass")
    if os.path.isfile(passed) and open(passed).read() == state:
        return 0
    env = dict(os.environ, GOPROXY="https://goproxy.cn,direct")
    for target in targets:
        code, out = run(["make", target], root, timeout=300, env=env)
        if code:
            print(f"make {target} failed:\n" + out[-6000:], file=sys.stderr)
            return 2
    with open(passed, "w") as f:
        f.write(state)
    return 0


sys.exit(main())
