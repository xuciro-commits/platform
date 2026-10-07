#!/usr/bin/env bash
# Structure inventory for ADR-0080: how big each home is, how fat Tenant is,
# which functions are long, which web exports nobody imports. Read-only.
set -euo pipefail
cd "$(dirname "$0")/.."

echo "== Go packages (non-test files / lines)"
for d in capabilities/server capabilities/server/platform capabilities/server/platform/* capabilities/server/internal/* capabilities/server/apps/* capabilities/server/[a-z]*/; do
  [[ -d $d ]] || continue
  files=$(ls "$d"/*.go 2>/dev/null | grep -v _test.go || true)
  [[ -n $files ]] || continue
  printf '%6d files %7d lines  %s\n' "$(echo "$files" | wc -l)" "$(cat $files | wc -l)" "$d"
done | sort -k3 -rn | awk '!seen[$NF]++'

echo; echo "== Tenant / Host / Console methods"
for r in 't \*Tenant' 'h \*Host' 'd \*Console'; do
  printf '%4d  %s\n' "$(grep -h "^func ($r)" capabilities/server/*.go | grep -v _test | wc -l)" "$r"
done

echo; echo "== Functions over 150 lines (Go, non-test)"
for f in $(find capabilities/server -name '*.go' -not -name '*_test.go' -not -path '*/vendor/*'); do
  awk -v f="$f" '/^func /{name=$0; start=NR} /^}/{ if (start && NR-start>150) printf "%5d  %s:%d %s\n", NR-start, f, start, substr(name,1,70); start=0 }' "$f"
done | sort -rn

echo; echo "== Web exports nobody imports (functions/consts/classes; types omitted)"
python3 - <<'EOF'
import re,os
os.chdir('web')
files=[]
for d,_,fs in os.walk('.'):
    if 'node_modules' in d or '/dist' in d or '/gen/' in d or d.startswith('./e2e'): continue
    files += [os.path.join(d,f) for f in fs if f.endswith(('.ts','.tsx','.mjs')) and not f.endswith('.d.ts')]
text={f:open(f,encoding='utf-8',errors='ignore').read() for f in files}
alltext='\n'.join(text.values())
exp=re.compile(r'^export (?:async )?(?:function|const|class)\s+([A-Za-z_]\w*)',re.M)
for f,s in sorted(text.items()):
    if f.endswith(('index.ts','index.tsx')) or 'catalog.' in f or '.test.' in f: continue
    for name in exp.findall(s):
        pat=r'\b'+re.escape(name)+r'\b'
        if len(re.findall(pat,alltext))==len(re.findall(pat,s)):
            print(f"  {f} {name} ({len(re.findall(pat,s))} in file)")
EOF
