#!/usr/bin/env python3
"""Architecture measures: the ten shapes that let one capability be patched in
five places. Every measure reads the source, compares against a checked-in
baseline, and fails in both directions:

  * above the baseline  the patch grew; say which measure, by how much, why
  * below the baseline  the patch shrank; lower the baseline in this change

Zero-tolerance measures never appear in the baseline: their value must be 0.

  python3 scripts/architecture.py            check (default)
  python3 scripts/architecture.py --write    record the current values

Each measure carries the reason it exists; a measure without a reason is a
number nobody acts on.
"""
from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
BASELINE = ROOT / ".architecture-baseline.json"
SERVER = ROOT / "capabilities/server"
WEB = ROOT / "web"

GO_SKIP = ("_test.go",)


def go_files(base: Path) -> list[Path]:
    return sorted(p for p in base.rglob("*.go") if not p.name.endswith(GO_SKIP) and "/vendor/" not in str(p) and "/gen/" not in str(p))


def read(path: Path) -> str:
    try:
        return path.read_text(encoding="utf8")
    except (OSError, UnicodeDecodeError):
        return ""


def web_sources() -> list[Path]:
    out: list[Path] = []
    for base in (WEB / "packages", WEB / "apps"):
        for p in sorted(base.rglob("*")):
            if p.suffix not in (".ts", ".tsx") or "/node_modules/" in str(p) or "/gen/" in str(p) or ".test." in p.name:
                continue
            out.append(p)
    return out


# --- measures -------------------------------------------------------------

def asset_kind_fanout() -> dict[str, int]:
    """Why: the host enumerating a kind is the patch. Count only files outside
    the definition API — inside it, a scanner per kind is the intended shape and
    splitting those files must not move the measure."""
    definition = read(SERVER / "platform/definition.go")
    kinds = re.findall(r'Asset\w+\s+AssetKind\s*=\s*"([a-z0-9-]+)"', definition)
    files = [f for f in go_files(SERVER) if not str(f).startswith(str(SERVER / "platform"))]
    out: dict[str, int] = {}
    for kind in sorted(set(kinds)):
        needle = f'"{kind}"'
        out[kind] = sum(1 for f in files if needle in read(f))
    return out


def describe_asset_lines() -> int:
    """Why: one function that names every kind is the closed-world dispatch.
    It may only fall; the registry replaces it case by case."""
    text = read(SERVER / "platform/dependencies.go")
    start = text.find("func describeAsset(")
    if start < 0:
        return 0
    end = text.find("\n}\n", start)
    return text[start:end].count("\n") + 1


def root_construction_importers() -> int:
    """Why: the host knows one application's definition model. Each importer is a
    host file that must change when that model changes."""
    return sum(1 for f in SERVER.glob("*.go") if not f.name.endswith(GO_SKIP)
               and '"platformserver/internal/construction"' in read(f))


def action_legacy_fields() -> int:
    """Why: two shapes in one struct means every reader must know both. Only falls."""
    text = read(SERVER / "internal/construction/actions.go")
    start = text.find("type Action struct {")
    if start < 0:
        return 0
    end = text.find("\n}\n", start)
    body = text[start:end]
    legacy = ("sets", "creates", "posts", "conditions", "journal", "reverses", "toInput", "reads")
    return sum(1 for name in legacy if re.search(rf'json:"{name}[,"]', body))


def effective_merge_methods() -> int:
    """Why: merging an object default with an action override at read time hides
    which shape is authoritative."""
    count = 0
    for f in go_files(SERVER):
        count += len(re.findall(r"^func \([a-z]+ \*?[A-Z]\w*\) Effective[A-Z]\w*\(", read(f), re.M))
    return count


def legacy_shape_producers() -> int:
    """Why: the old shape stays alive only while something produces it. Zero is
    reachable, so this is a zero-tolerance measure, not a baseline."""
    pattern = re.compile(r"actions:\s*\[\{[^\n]*\bsets:")
    return sum(1 for f in web_sources() if pattern.search(read(f)))


def root_package() -> dict[str, int]:
    """Why: the root package is 'everything else'. Files, lines, prefixes and
    single-use prefixes say how much of it has no owner."""
    files = [f for f in SERVER.glob("*.go") if not f.name.endswith(GO_SKIP)]
    prefixes = Counter(f.name.split("_")[0] for f in files)
    text = "\n".join(read(f) for f in files)
    return {
        "files": len(files),
        "lines": sum(read(f).count("\n") + 1 for f in files),
        "prefixes": len(prefixes),
        "singleton_prefixes": sum(1 for n in prefixes.values() if n == 1),
        "exported": len(re.findall(r"^(?:func|type) [A-Z]\w*", text, re.M)),
        "tenant_methods": len(re.findall(r"^func \(t \*Tenant\) [A-Z]\w*\(", text, re.M)),
    }


def platform_dynamic() -> dict[str, int]:
    """Why: reflection and untyped maps are where a validation cannot be written
    once, so it is written again in each layer."""
    text = "\n".join(read(f) for f in go_files(SERVER / "platform"))
    return {
        "reflect_calls": len(re.findall(r"\breflect\.", text)),
        "map_any": len(re.findall(r"map\[string\]any", text)),
    }


def app_size() -> dict[str, dict[str, int]]:
    """Why: an application that grew past its responsibility keeps absorbing new
    ones. Size only falls until a kind registry and a platform owner exist."""
    out: dict[str, dict[str, int]] = {}
    apps = SERVER / "apps"
    for app in sorted(p for p in apps.iterdir() if p.is_dir()):
        files = [f for f in app.rglob("*.go") if not f.name.endswith(GO_SKIP)]
        out[app.name] = {"files": len(files), "lines": sum(read(f).count("\n") + 1 for f in files)}
    return out


def string_error_sites() -> int:
    """Why: re-deriving semantics from an error's text is a patch per call site;
    the structured status replaces them one by one."""
    return sum(read(f).count("String(error)") for f in web_sources())


def route_params_used() -> int:
    """Why: a route with no declared parameters is read by hand everywhere and
    re-guessed by the retired-alias table."""
    text = "\n".join(read(f) for f in web_sources() if "/packages/app/src/" in str(f) or "/apps/workspace/src/" in str(f))
    return len(re.findall(r"params\?\.", text))


# --- zero-tolerance checks ------------------------------------------------

def http_text_matches() -> list[str]:
    """Why: matching an HTTP status in a message is how a protocol becomes a string."""
    pattern = re.compile(r"HTTP \d{3}")
    return [str(f.relative_to(ROOT)) for f in web_sources() if pattern.search(read(f))]


def undeclared_views() -> list[str]:
    """Why: a view outside the declaration cannot be routed, titled or retired safely."""
    declared = set(re.findall(r'\{ id: "([a-z0-9-]+)", owner:', read(ROOT / "web/apps/workspace/src/shell/views.ts")))
    chrome = read(ROOT / "web/apps/workspace/src/chrome.tsx")
    start = chrome.find("export const chromeViews =")
    body = chrome[start:chrome.find("\n];", start)] if start >= 0 else ""
    registered = set(re.findall(r'\n  \{ id: "([a-z0-9-]+)"', body))
    problems = [f"registered but undeclared: {v}" for v in sorted(registered - declared)]
    problems += [f"declared but not registered: {v}" for v in sorted(v for v in declared - registered if v not in build_owned_views())]
    return problems


def build_owned_views() -> set[str]:
    text = read(ROOT / "web/apps/workspace/src/shell/views.ts")
    return set(re.findall(r'\{ id: "([a-z0-9-]+)", owner: "build"', text))


def undeclared_retired_params() -> list[str]:
    """Why: the retired table is where a view's parameters were guessed. Each
    name it asks for must be declared by the view it redirects to."""
    legacy = read(ROOT / "web/apps/workspace/src/shell/legacy.ts")
    views = read(ROOT / "web/apps/workspace/src/shell/views.ts")
    declared: dict[str, set[str]] = {}
    for m in re.finditer(r'\{ id: "([a-z0-9-]+)", owner: "\w+", params: \[([^\]]*)\] \}', views):
        declared[m.group(1)] = set(re.findall(r'"([a-z0-9-]+)"', m.group(2)))
    problems = []
    for target, names in re.findall(r'keep\("([a-z0-9-]+)",\s*params,\s*([^)]*)\)', legacy):
        for name in re.findall(r'"([a-z0-9-]+)"', names):
            if name not in declared.get(target, set()):
                problems.append(f'legacy redirect to {target} asks for {name}, which {target} does not declare')
    return problems


def legacy_registry_drift() -> list[str]:
    """Why: a compatibility decoder without a retirement condition is permanent."""
    found = {str(p.relative_to(ROOT)) for p in list(SERVER.rglob("*legacy*.go")) + list(WEB.rglob("*legacy*.ts"))
             if "node_modules" not in str(p) and "/gen/" not in str(p)}
    # The registry rows are file paths; a measure named in another table is not one.
    listed = set(re.findall(r"^\| `([^`]+)`", read(ROOT / "docs/Architecture.md"), re.M))
    listed = {p for p in listed if "legacy" in p and "/" in p and p.endswith((".go", ".ts"))}
    problems = [f"decoder not registered in docs/Architecture.md: {p}" for p in sorted(found - listed)]
    problems += [f"registered but missing: {p}" for p in sorted(listed - found)]
    return problems


def platform_imports_internal() -> list[str]:
    """Why: the application API is the only surface an app may use; the moment it
    reads a host internal package, every app inherits that internal shape."""
    problems = []
    for f in go_files(SERVER / "platform"):
        for imp in re.findall(r'"platformserver/internal/[^"]+"', read(f)):
            problems.append(f"{f.relative_to(ROOT)} imports {imp.strip(chr(34))}")
    return problems


def internal_imports_apps() -> list[str]:
    """Why: an internal host piece that knows one application turns that
    application's model into host behaviour."""
    problems = []
    for f in go_files(SERVER / "internal"):
        for imp in re.findall(r'"platformserver/apps/[^"]+"', read(f)):
            problems.append(f"{f.relative_to(ROOT)} imports {imp.strip(chr(34))}")
    return problems


def duplicate_requirements() -> list[str]:
    """Why: the same invariant written twice is two owners and two chances to drift."""
    seen: dict[str, str] = {}
    problems = []
    # Only the live specs: a change delta and an archived change legitimately
    # restate a requirement they modify.
    for spec in sorted((ROOT / "openspec/specs").rglob("spec.md")):
        for title in re.findall(r"^### Requirement: (.+)$", read(spec), re.M):
            title = title.strip()
            if title in seen and seen[title] != str(spec):
                problems.append(f"requirement {title!r} in both {seen[title]} and {spec}")
            seen.setdefault(title, str(spec))
    return problems


def missing_spec_sources() -> list[str]:
    """Why: a spec that cites code which no longer exists cannot be verified."""
    problems = []
    for spec in sorted((ROOT / "openspec/specs").rglob("spec.md")):
        for cited in re.findall(r"`((?:docs|capabilities|contract|web|scripts)/[^`\s]+)`", read(spec)):
            if "*" in cited or "<" in cited:
                continue  # a family, not one path
            if not (ROOT / cited).exists():
                problems.append(f"{spec.relative_to(ROOT)} cites missing {cited}")
    return problems


MEASURES = {
    "asset_kind_fanout": asset_kind_fanout,
    "describe_asset_lines": describe_asset_lines,
    "root_construction_importers": root_construction_importers,
    "action_legacy_fields": action_legacy_fields,
    "effective_merge_methods": effective_merge_methods,
    "root_package": root_package,
    "platform_dynamic": platform_dynamic,
    "app_size": app_size,
    "string_error_sites": string_error_sites,
    "route_params_used": route_params_used,
}

ZERO = {
    "platform_imports_internal": platform_imports_internal,
    "internal_imports_apps": internal_imports_apps,
    "legacy_shape_producers": legacy_shape_producers,
    "http_text_matches": http_text_matches,
    "undeclared_views": undeclared_views,
    "undeclared_retired_params": undeclared_retired_params,
    "legacy_registry_drift": legacy_registry_drift,
    "duplicate_requirements": duplicate_requirements,
    "missing_spec_sources": missing_spec_sources,
}


def compare(name: str, now, was):
    """Recursive: a measure is a number or a mapping (a kind, an app, a package).
    Every leaf may only fall; a missing leaf means the baseline must change."""
    problems: list[str] = []
    if isinstance(now, dict):
        was = was or {}
        for key in sorted(set(now) | set(was)):
            path = f"{name}.{key}"
            if key not in now:
                problems.append(f"{path}: recorded {was[key]}, now absent (lower the baseline)")
            elif key not in was:
                continue  # a new kind or app is allowed; its own leaf now applies
            else:
                problems += compare(path, now[key], was[key])
        return problems
    if now > was:
        problems.append(f"{name}: {was} -> {now}")
    elif now < was:
        problems.append(f"{name}: {was} -> {now} (lower the baseline)")
    return problems


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--write", action="store_true", help="record the current values as the baseline")
    args = parser.parse_args()

    now = {name: measure() for name, measure in MEASURES.items()}
    zero_now = {name: measure() for name, measure in ZERO.items()}

    if args.write:
        BASELINE.write_text(json.dumps(now, indent=2, sort_keys=True) + "\n", encoding="utf8")
        print(f"architecture baseline written: {len(now)} measures")
        for name, value in zero_now.items():
            if value:
                print(f"  zero-tolerance {name} is not zero: {value}", file=sys.stderr)
        return 0

    if not BASELINE.exists():
        print(f"architecture: no baseline at {BASELINE.name}; run --write once and commit it", file=sys.stderr)
        return 1
    was = json.loads(BASELINE.read_text(encoding="utf8"))

    failures: list[str] = []
    for name, measure in MEASURES.items():
        failures += [f"{name}: {text}" for text in compare(name, now[name], was.get(name))]

    for name, value in zero_now.items():
        if isinstance(value, list):
            failures += [f"{name}: {item}" for item in value]
        elif value:
            failures.append(f"{name}: {value}, must be 0")

    if name := (set(now) ^ set(was)):
        failures.append(f"measures differ from the baseline: {sorted(name)}")

    if failures:
        print("architecture: the patch surface grew or a baseline drifted", file=sys.stderr)
        for failure in failures:
            print(f"  {failure}", file=sys.stderr)
        print("  A measure above its baseline means one capability is being patched in more places;"
              "\n  fix the shape rather than the number. A measure below it means: lower the baseline here.", file=sys.stderr)
        return 1

    total = sum(sum(v.values()) if isinstance(v, dict) else v for v in now.values() if not isinstance(v, dict))
    print(f"architecture ok: {len(MEASURES)} measures at baseline, {len(ZERO)} zero-tolerance at 0")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
