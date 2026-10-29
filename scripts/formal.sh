#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
version=4.34.1
[[ $(cat contract/lean/lean-toolchain) == "leanprover/lean4:v$version" ]]
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64)
    platform=darwin_aarch64
    digest=65f22a4f047738ec742667b3247e836a86ebf06eabc12655c3dee00637e37866 ;;
  Linux-x86_64)
    platform=linux
    digest=47bf4bbd78f70c2e9670598ab7124d92b6efb7330ff33e5fbb4030f6fd72e4e4 ;;
  *) echo 'Unsupported proof platform / 不支持的证明平台' >&2; exit 1 ;;
esac
name=lean-$version-$platform
root=$PWD/.build/formal-tools
archive=$root/$name.tar.zst
mkdir -p "$root"
if [[ ! -f $archive ]]; then
  curl -fsSL --retry 3 "https://github.com/leanprover/lean4/releases/download/v$version/$name.tar.zst" -o "$archive.part"
  mv "$archive.part" "$archive"
fi
actual=$(shasum -a 256 "$archive")
[[ ${actual%% *} == "$digest" ]] || { echo 'Lean archive checksum mismatch / Lean 制品摘要不符' >&2; exit 1; }
if [[ ! -f $root/$name/.verified-archive ]]; then
  zstd -dc "$archive" | tar -xf - -C "$root"
  printf '%s\n' "$digest" > "$root/$name/.verified-archive"
fi
[[ $(cat "$root/$name/.verified-archive") == "$digest" ]]
export PATH="$root/$name/bin:$PATH"
if rg -n '\b(sorry|sorryAx|admit|axiom|native_decide|unsafe|implemented_by)\b' contract/lean --glob '*.lean'; then
  echo 'Unfinished proof or extra trust primitive / 未完成证明或额外可信原语' >&2
  exit 1
fi
cd contract/lean
lean --version
lake build
lake env lean -DwarningAsError=true KernelProofs/Idempotency.lean | tee "$root/proof-axioms.log"
python3 - "$root/proof-axioms.log" <<'PY'
import pathlib, re, sys
expected = {"replay_original", "conflict_unchanged", "refusal_key_free", "accepted_once",
            "accept_then_replay", "repeat_replay_unchanged", "repeat_replay_length", "other_tenant_unchanged"}
seen = set()
for line in pathlib.Path(sys.argv[1]).read_text().splitlines():
    match = re.fullmatch(r"'KernelProofs\.(\w+)' (?:does not depend on any axioms|depends on axioms: \[(.*)\])", line)
    if not match:
        raise SystemExit("Unrecognized axiom audit output / 无法识别的公理检查输出: " + line)
    name, dependencies = match.groups()
    if name in seen or name not in expected:
        raise SystemExit("Unexpected theorem audit / 非预期的定理检查: " + name)
    seen.add(name)
    axioms = set(dependencies.split(", ")) if dependencies else set()
    if not axioms <= {"propext", "Quot.sound", "Classical.choice"}:
        raise SystemExit("Extra proof axiom / 额外证明公理: " + str(axioms))
if seen != expected:
    raise SystemExit("Missing theorem audit / 缺少定理检查: " + str(expected - seen))
PY
