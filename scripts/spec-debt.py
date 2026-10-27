#!/usr/bin/env python3
"""Spec-to-source consistency gate.

OpenSpec validates formats; nothing checked whether a change that declares a
removal actually removed it, and whether the live specs still order the next
reader to maintain code the owner already cancelled. That gap is how retired
concepts survive: the change archives as done, the module spec keeps its
实现定位 list, and the next session rebuilds what was meant to die.

Three checks, all read-only:

1. phantom deletion - a checked task whose clause orders a removal, where the
   named path still exists. The work was reported, not done.
2. cancelled but required - an active change lists a module under Removed
   Capabilities while openspec/specs/<module> still lives. The cancellation has
   no standing instruction, so nothing in the repo says "do not maintain this".
3. dangling pointer - a live spec's 实现定位 names a file that no longer
   exists, so the spec documents a place nobody can open.

usage: python3 scripts/spec-debt.py [--root DIR]
"""
import argparse
import os
import re
import sys

REMOVE_VERB = re.compile(r"删除|移除|退出|退役|拆掉|整包删除|推倒|retire|delete", re.I)
# a clause that merely regenerates, references or re-points a path is not a
# removal order; without this the gate cries wolf and gets ignored
NOT_REMOVAL = re.compile(r"生成|引用|重生成|更新|改为|同步|参数|双轨|regenerate|reference|regenerat", re.I)
TASK_DONE = re.compile(r"^\s*-\s*\[[xX]\]\s*(.*)$")
# split on sentence punctuation only: a removal order enumerates its paths with
# 、 inside one clause, and cutting there would strand the verb away from them
CLAUSE = re.compile(r"[^，。;；\n]+")
BACKTICK = re.compile(r"`([^`\n]+)`")
MODULE = re.compile(r"`([a-z0-9]+(?:/[a-z0-9-]+)+)`")
LOCATE = re.compile(r"实现定位[^:：]*[:：](.*)")
PATHISH = re.compile(r"[\w./-]+\.(?:go|ts|tsx|json|proto|lean|md|sh|mjs|py)\b|[\w./-]+/")
SKIP_DIR = {".git", "node_modules", "vendor", ".build", "dist", "__pycache__"}


def repo_paths(root):
    """Every tracked-looking path, so a module-relative `cmd/uaf-gen` still resolves."""
    exact, suffixes = set(), set()
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIR]
        rel = os.path.relpath(dirpath, root)
        if rel != ".":
            exact.add(rel)
            suffixes.add(rel)
        for name in filenames:
            rel = os.path.join(dirpath, name)
            rel = os.path.relpath(rel, root)
            exact.add(rel)
            suffixes.add(rel)
    return exact, suffixes


def sections(text, wanted):
    """Return {wanted header: body} using line scanning, not one greedy match."""
    found, current, body = {}, None, []
    for line in text.splitlines():
        header = re.match(r"^### (.+?)\s*$", line)
        top = re.match(r"^## ", line)
        if header and header.group(1) in wanted:
            current, body = header.group(1), []
        elif header or top:
            if current:
                found[current] = "\n".join(body)
            current, body = None, []
        elif current is not None:
            body.append(line)
    if current:
        found[current] = "\n".join(body)
    return found


def active_changes(root):
    base = os.path.join(root, "openspec", "changes")
    if not os.path.isdir(base):
        return []
    return sorted(d for d in os.listdir(base) if d != "archive" and os.path.isdir(os.path.join(base, d)))


def read(path):
    with open(path, encoding="utf-8") as handle:
        return handle.read()


def removal_paths(text):
    """Paths that removal clauses in `text` order deleted.

    A clause counts when it carries a removal verb and is not merely regenerating,
    updating or re-pointing the same path.
    """
    paths = []
    for clause in CLAUSE.findall(text):
        if not REMOVE_VERB.search(clause) or NOT_REMOVAL.search(clause):
            continue
        for token in BACKTICK.findall(clause):
            for part in re.split(r"\s+", token):
                part = part.strip("、，。;；()（）*`")
                # a bare filename cannot be attributed to a directory from the task
                # text alone, so only path-shaped tokens are held accountable
                if re.match(r"^[\w][\w./-]*$", part) and "/" in part:
                    paths.append(part)
    return paths


def phantom_deletions(root):
    exact, suffixes = repo_paths(root)
    found = []
    for change in active_changes(root):
        path = os.path.join(root, "openspec", "changes", change, "tasks.md")
        if not os.path.isfile(path):
            continue
        for line in read(path).splitlines():
            match = TASK_DONE.match(line)
            if not match:
                continue
            surviving = []
            for token in removal_paths(match.group(1)):
                if token in exact or any(s == token or s.endswith("/" + token) for s in suffixes):
                    surviving.append(token)
            if surviving:
                found.append((change, line.strip(), sorted(set(surviving))))
    return found


def cancelled_required(root):
    found = []
    for change in active_changes(root):
        path = os.path.join(root, "openspec", "changes", change, "proposal.md")
        if not os.path.isfile(path):
            continue
        body = sections(read(path), {"Removed Capabilities"}).get("Removed Capabilities", "")
        for module in MODULE.findall(body):
            live = "openspec/specs/%s/spec.md" % module
            if os.path.isfile(os.path.join(root, live)):
                found.append((change, module, live))
    return found


def dangling_pointers(root):
    found = []
    base = os.path.join(root, "openspec", "specs")
    for dirpath, dirnames, filenames in os.walk(base):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIR]
        if "spec.md" not in filenames:
            continue
        path = os.path.join(dirpath, "spec.md")
        locate = LOCATE.search(read(path))
        if not locate:
            continue
        for token in PATHISH.findall(locate.group(1)):
            token = token.strip("`、，。;； 　")
            if not token or token.startswith("http") or ".." in token:
                continue
            if not os.path.exists(os.path.join(root, token)):
                found.append((os.path.relpath(path, root), token))
    return found


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
    args = parser.parse_args()
    root = os.path.abspath(args.root)
    debt = 0
    print("spec-debt: 核对 change 声明、现行规范与源码是否一致")

    for change, _line, surviving in phantom_deletions(root):
        debt += len(surviving)
        print("  phantom-deletion   %s 声称已删除，仍在树中: %s" % (change, " ".join(surviving)))
    if not phantom_deletions(root):
        print("  phantom-deletion   ok: 已勾删除任务无遗留")

    for change, module, live in cancelled_required(root):
        debt += 1
        print("  cancelled-required %s 声明移除 %s，但 %s 仍要求它（含实现定位，会被重新施工）" % (change, module, live))
    if not cancelled_required(root):
        print("  cancelled-required ok: 无仍被现行规范要求的已移除模块")

    for spec, token in dangling_pointers(root):
        debt += 1
        print("  dangling-pointer   %s 的实现定位指向不存在的 %s" % (spec, token))
    if not dangling_pointers(root):
        print("  dangling-pointer   ok: 实现定位全部可达")

    if debt:
        print("spec-debt: %d 处不一致。现行规范是下一个会话的工单：执行删除或撤回声明，不留中间态。" % debt)
        return 1
    print("spec-debt ok: 声明、规范与源码一致")
    return 0


if __name__ == "__main__":
    sys.exit(main())
