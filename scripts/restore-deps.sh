#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
TARGETS=${1:-go web}

fetch() {
  curl -fsSL --retry 2 --connect-timeout 20 -o "$2" "$1"
}

restore_go() {
python3 - "$PWD" <<'PY'
import base64, hashlib, io, json, os, re, shutil, sys, tarfile, tempfile, zipfile
from pathlib import Path
from urllib.request import urlopen, Request

root = Path(sys.argv[1])
gomod = (root / "capabilities/server/go.mod").read_text()
cache = Path(os.environ.get("GOPATH", Path.home() / ".gopath")) / "pkg" / "mod" / "cache" / "download"

REPOS = {
    "google.golang.org/protobuf": ("protocolbuffers/protobuf-go", []),
    "google.golang.org/grpc": ("grpc/grpc-go", []),
    "google.golang.org/genproto/googleapis/api": ("googleapis/go-genproto", ["googleapis/api"]),
    "google.golang.org/genproto/googleapis/rpc": ("googleapis/go-genproto", ["googleapis/rpc"]),
    "gopkg.in/ini.v1": ("go-ini/ini", []),
    "go.yaml.in/yaml/v3": ("yaml/go-yaml", []),
    "go.yaml.in/yaml/v4": ("yaml/go-yaml", []),
    "go.opentelemetry.io/auto/sdk": ("open-telemetry/opentelemetry-go-instrumentation", ["sdk"]),
    "go.opentelemetry.io/proto/otlp": ("open-telemetry/opentelemetry-proto-go", ["otlp"]),
    "gopkg.in/yaml.v2": ("yaml/go-yaml", []),
    "gopkg.in/yaml.v3": ("yaml/go-yaml", []),
    "gopkg.in/check.v1": ("go-check/check", []),
    "go.uber.org/goleak": ("uber-go/goleak", []),
    "gonum.org/v1/gonum": ("gonum/gonum", []),
}

def repo_for(path):
    if path.startswith("go.opentelemetry.io/otel"):
        return "open-telemetry/opentelemetry-go", path.removeprefix("go.opentelemetry.io/otel").removeprefix("/")
    if path in REPOS:
        return REPOS[path]
    if path.startswith("github.com/"):
        major = re.search(r"/v\d+$", path)
        base = path.removesuffix(major.group(0)) if major else path
        parts = base.split("/")
        return f"{parts[1]}/{parts[2]}", "/".join(parts[3:])
    if path.startswith("golang.org/x/"):
        return f"golang/{path.split('/')[2]}", ""
    raise SystemExit(f"restore-deps: no GitHub mapping for {path}")

def get(url):
    with urlopen(Request(url, headers={"User-Agent": "restore-deps"}), timeout=180) as r:
        return r.read()

def hunt(repo, sub, path, version, want):
    if not isinstance(sub, str):
        sub = (sub[0] + "/") if sub else ""
    """A maintainer may re-point a tag after the proxy recorded it. Walk the
    commits that touched the module's go.mod — few in old repos — and hash
    each tree until one matches the go.sum value."""
    try:
        with urlopen(Request(f"https://api.github.com/repos/{repo}/commits?per_page=100&path=" + ("go.mod" if not sub else sub + "go.mod"), headers={"User-Agent": "restore-deps", "Accept": "application/vnd.github+json"}), timeout=60) as r:
            commits = json.loads(r.read())
    except Exception as e:
        print(f"hunt    {path}: commit list unavailable ({e})", file=sys.stderr)
        return None
    for c in commits:
        sha = c["sha"]
        try:
            tar = get(f"https://codeload.github.com/{repo}/tar.gz/{sha}")
        except Exception:
            continue
        with tarfile.open(fileobj=io.BytesIO(tar), mode="r:gz") as tf:
            src = {m.name: tf.extractfile(m).read() for m in tf.getmembers() if m.isfile()}
        top = src and min(src).split("/")[0] + "/"
        found = None
        for name, data in src.items():
            if name.endswith("go.mod"):
                text = data.decode("utf-8", "replace")
                if re.search(r"^module\s+\"?" + re.escape(path) + r"\"?\s*$", text, re.M):
                    found = (name.removeprefix(top).removesuffix("go.mod"), data)
                    break
        if not found:
            continue
        root, go_mod = found
        entries = build(src, top, root)
        entries["go.mod"] = go_mod
        entries = {f"{path}@{version}/{k}": v for k, v in entries.items()}
        if hash_zip(entries) == want:
            print(f"hunted  {path} {version} -> {sha[:12]}")
            return src, top, root, go_mod
    return None

pseudo = re.compile(r"v\d+\.\d+\.\d+-(\d{14})-([0-9a-f]+)$")
expected = {}
for line in (root / "capabilities/server/go.sum").read_text().splitlines():
    parts = line.split()
    if len(parts) == 3 and parts[2].startswith("h1:"):
        expected[(parts[0], parts[1])] = parts[2]
fails = []

def hash_zip(entries):
    h = hashlib.sha256()
    for name in sorted(entries):
        hf = hashlib.sha256(entries[name]).hexdigest()
        h.update(f"{hf}  {name}\n".encode())
    return "h1:" + base64.b64encode(h.digest()).decode()

def build(src, top, root):
    """The official x/mod/zip file set (cmd/vendor/golang.org/x/mod/zip):
    everything under the module root except VCS internals, submodules and
    vendored packages — vendor's *direct* files and vendor/modules.txt stay in
    until the module's go directive reaches 1.24 — plus the repository LICENSE
    copied in for subdirectory modules that lack their own (coderepo.go)."""
    pre = top + root
    files = {name[len(pre):]: data for name, data in src.items()
             if name.startswith(pre) and name[len(pre):]}
    have_gomod = set()
    for rel in files:
        if rel.endswith("go.mod"):
            have_gomod.add(rel.rsplit("/", 1)[0] + "/" if "/" in rel else "")
    vers = ""
    if mt := re.search(rb"^go\s+([0-9]+(?:\.[0-9]+)*)", files.get("go.mod", b""), re.M):
        vers = mt.group(1).decode()
    def ge124(v):
        if not v:
            return False
        try:
            t = tuple(int(x) for x in v.split("."))
        except ValueError:
            return False
        return (t + (0, 0))[:2] >= (1, 24)
    g = ge124(vers)
    def vendored(p):
        if g and p == "vendor/modules.txt":
            return True
        if p.startswith("vendor/"):
            i = 7
        elif "/vendor/" in p:
            i = p.index("/vendor/") + 7 if g else 7
        else:
            return False
        return "/" in p[i:]
    entries = {}
    for rel, data in files.items():
        parts = rel.split("/")
        if any(part in (".git", ".hg", ".svn", ".bzr") for part in parts):
            continue
        if rel == ".hg_archival.txt":
            continue
        p, insub = rel, False
        while "/" in p:
            d = p.rsplit("/", 1)[0] + "/"
            if d in have_gomod:
                insub = True
                break
            p = d.rstrip("/")
        if insub:
            continue
        if vendored(rel):
            continue
        entries[rel] = data
    if root and "LICENSE" not in entries and top + "LICENSE" in src:
        entries["LICENSE"] = src[top + "LICENSE"]
    return entries

worklist = set()
for line in gomod.splitlines():
    m = re.match(r"^\t(\S+) (\S+)", line)
    if m and m.group(1) != "platformkernel":
        worklist.add((m.group(1), m.group(2)))
for path, ver_full in expected:
    worklist.add((path, ver_full.removesuffix("/go.mod")))

for path, version in sorted(worklist):
    out = cache / path / "@v"
    if (out / f"{version}.zip").is_file() and (out / f"{version}.mod").is_file():
        print(f"cached  {path} {version}")
        continue
    repo, sub = repo_for(path)
    if pm := pseudo.match(version):
        refs = [pm.group(2)]
    else:
        prefixes = [sub] if sub and path not in REPOS else REPOS.get(path, ("", []))[1]
        refs = [f"{pfx}/{version}" for pfx in prefixes] + [version]
    tar = None
    for ref in refs:
        try:
            tar = get(f"https://codeload.github.com/{repo}/tar.gz/{ref}")
            break
        except Exception:
            tar = None
    if tar is None:
        fails.append(f"{path} {version} ({repo}: {' , '.join(refs)})")
        continue
    with tarfile.open(fileobj=io.BytesIO(tar), mode="r:gz") as tf:
        src = {m.name: tf.extractfile(m).read() for m in tf.getmembers() if m.isfile()}
    top = src and min(src).split("/")[0] + "/"
    rootmod = None
    for name, data in src.items():
        if name.endswith("go.mod"):
            text = data.decode("utf-8", "replace")
            if re.search(r"^module\s+\"?" + re.escape(path) + r"\"?\s*$", text, re.M):
                rootmod = (name.removeprefix(top).removesuffix("go.mod"), data)
                break
    if rootmod is None:
        if any(name.endswith("go.mod") for name in src):
            fails.append(f"{path} {version}: no ref in {repo} ({' , '.join(refs)}) declares module {path}")
            continue
        root, go_mod = "", f"module {path}\n".encode()
    else:
        root, go_mod = rootmod
    entries = build(src, top, root)
    entries["go.mod"] = go_mod
    entries = {f"{path}@{version}/{k}": v for k, v in entries.items()}
    want = expected.get((path, version))
    hunted = None
    if want and hash_zip(entries) != want:
        hunted = hunt(repo, sub, path, version, want)
        if hunted is None:
            fails.append(f"{path} {version}: tree at {repo} does not match go.sum and no commit in history does")
            continue
        src, top, root, go_mod = hunted
        rootmod = (root, go_mod)
        entries = build(src, top, root)
        entries["go.mod"] = go_mod
        entries = {f"{path}@{version}/{k}": v for k, v in entries.items()}
    want_mod = expected.get((path, version + "/go.mod"))
    if want_mod:
        mh = hashlib.sha256()
        mh.update(f"{hashlib.sha256(go_mod).hexdigest()}  go.mod\n".encode())
        calc = "h1:" + base64.b64encode(mh.digest()).decode()
        if calc != want_mod:
            fails.append(f"{path} {version}: go.mod hash {calc[:20]} does not match go.sum {want_mod[:20]}")
            continue
    out.mkdir(parents=True, exist_ok=True)
    (out / f"{version}.mod").write_bytes(go_mod)
    (out / f"{version}.info").write_bytes(json.dumps({"Version": version, "Time": "1970-01-01T00:00:00Z"}).encode())
    with zipfile.ZipFile(out / f"{version}.zip", "w", zipfile.ZIP_DEFLATED) as z:
        for rel in sorted(entries):
            z.writestr(rel, entries[rel])
    print(f"restored {path} {version} <- {repo}@{refs[0]}")
if fails:
    print("restore-deps: could not restore:", file=sys.stderr)
    for f in fails:
        print("  " + f, file=sys.stderr)
    sys.exit(1)
shutil.rmtree(tempfile.gettempdir() + "/restore-deps", ignore_errors=True)
PY
  export GOPROXY=off GOSUMDB=off
  (cd capabilities/server && go mod download && go mod vendor)
  echo "vendor/ restored and verified against go.sum."
}

restore_web() {
  command -v corepack >/dev/null && (cd web && corepack pnpm install --frozen-lockfile) || echo "restore-deps: corepack not found; run the web install yourself."
}

for target in $TARGETS; do
  case "$target" in
    go) restore_go ;;
    web) restore_web ;;
    *) echo "unknown target $target (use: go web)" >&2; exit 2 ;;
  esac
done
