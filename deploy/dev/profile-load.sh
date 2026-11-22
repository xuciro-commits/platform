#!/usr/bin/env bash
# 在 make load-sql 压测的同时抓 Go 性能分析（CPU、执行轨迹、堆、协程），并生成文本报告。
# 仅供本机开发排查性能用：靠 PLATFORM_PPROF=1 打开宿主自带的 /debug/pprof（只接受本机访问）。
#
# 在线压测：bash deploy/dev/profile-load.sh                       （PLATFORM_LOAD_ROWS 调灌入行数，默认 2000）
# CSV 导入：PLATFORM_LOAD_IMPORT_ROWS=1000,10000 bash deploy/dev/profile-load.sh
#   导入模式在第一份文件导入时抓窗口 A（已有记录少），在最后一份文件导入进行到一半时抓窗口 B（已有记录多），
#   每个窗口记录前后记录数与 PostgreSQL 各表的扫描计数，报告给出每行的 CPU 与 SQL 读取量，以及 B 相对 A 的差异。
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

GO="${GO:-go}"
port="${PLATFORM_LOAD_PORT:-20296}"
base="http://127.0.0.1:${port}"
out="$PWD/.build/profile/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$out"
log="$out/load.log"
import_mode=""
[[ -n "${PLATFORM_LOAD_IMPORT_ROWS:-}" ]] && import_mode=1

export PLATFORM_PPROF=1
export PLATFORM_LOAD_OUTPUT="$out/load"
if [[ -z "$import_mode" ]]; then
  export PLATFORM_LOAD_ROWS="${PLATFORM_LOAD_ROWS:-2000}"
fi

( make load-sql 2>&1; echo "load-sql exit=$?" ) | tee "$log" &
load_pid=$!

alive() { kill -0 "$load_pid" 2>/dev/null; }

grab() { # 文件名 路径
  if curl -fsS --max-time 180 "$base$2" -o "$out/$1" 2>>"$out/capture.err"; then
    echo "  [profile] 已抓取 $1"
  else
    echo "  [profile] 抓取失败 $1（见 capture.err）"
  fi
}

wait_for() { # 正则 超时秒 [第几次出现，默认 1]
  local deadline=$((SECONDS + $2)) want="${3:-1}"
  until [[ "$(grep -Ec -- "$1" "$log" 2>/dev/null || true)" -ge "$want" ]]; do
    alive || return 1
    (( SECONDS > deadline )) && return 1
    sleep 0.2
  done
}

wait_healthy() {
  until curl -fsS --max-time 1 "$base/healthz" >/dev/null 2>&1; do
    alive || return 1
    sleep 0.2
  done
}

records() { # 打印压测对象当前的记录数
  local fixture="$out/load/fixture.json"
  [[ -s "$fixture" ]] || { echo 0; return; }
  local tenant object
  tenant=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["tenant"])' "$fixture")
  object=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["object"])' "$fixture")
  curl -fsS --max-time 30 -H "Authorization: Bearer loadadmin" -H "Platform-Tenant: $tenant" "$base/v1/records/$object?limit=1" 2>>"$out/capture.err" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["total"])' 2>/dev/null || echo 0
}

pg_stats() { # 文件名：压测库各表的累计扫描与写入计数
  docker exec -i platform-postgres-1 sh -c 'db=$(psql -U "$POSTGRES_USER" -d postgres -tAc "select datname from pg_database where datname like '"'"'platform_load_%'"'"' order by oid desc limit 1"); psql -U "$POSTGRES_USER" -d "$db" -A -F, -P footer=off' \
    >"$out/$1" 2>>"$out/capture.err" <<'SQL'
select relname, seq_scan, seq_tup_read, coalesce(idx_scan,0) as idx_scan, coalesce(idx_tup_fetch,0) as idx_tup_fetch, n_tup_ins, n_tup_upd, n_live_tup from pg_stat_user_tables order by relname;
SQL
}

import_window() { # 名称 CPU 秒数
  local name=$1 seconds=$2 before after started
  before=$(records)
  pg_stats "$name.pg-before.csv"
  started=$SECONDS
  grab "$name.cpu.pprof" "/debug/pprof/profile?seconds=$seconds" & local cpu=$!
  sleep 2
  grab "$name.trace" "/debug/pprof/trace?seconds=4"
  wait "$cpu"
  after=$(records)
  pg_stats "$name.pg-after.csv"
  echo "$name rows_before=$before rows_after=$after seconds=$((SECONDS - started))" | tee -a "$out/import-windows.txt"
}

if [[ -n "$import_mode" ]]; then
  # 窗口 A：第一份文件开始导入（已有记录少）
  if wait_for '^import [0-9]+ rows: apply…' 1800 1; then
    sleep 1
    echo "  [profile] 导入窗口 A 开始抓取"
    import_window importA "${PLATFORM_PROFILE_IMPORT_WINDOW_A:-8}"
  fi
  # 窗口 B：最后一份文件导入进行到一段时间之后（已有记录多）
  files=$(awk -F, '{print NF}' <<<"$PLATFORM_LOAD_IMPORT_ROWS")
  if (( files > 1 )) && wait_for '^import [0-9]+ rows: apply…' 5400 "$files"; then
    sleep "${PLATFORM_PROFILE_IMPORT_LATE:-150}"
    echo "  [profile] 导入窗口 B 开始抓取"
    import_window importB "${PLATFORM_PROFILE_IMPORT_WINDOW_B:-15}"
  fi
else
  # 1) 灌数据阶段：管道逐行写入，看单租户写入路径
  if wait_healthy; then
    sleep 6
    echo "  [profile] 灌数据阶段开始抓取"
    grab seed.cpu.pprof "/debug/pprof/profile?seconds=25" & cpu=$!
    sleep 5
    grab seed.trace "/debug/pprof/trace?seconds=5"
    grab seed.goroutines.txt "/debug/pprof/goroutine?debug=1"
    wait "$cpu"
    grab seed.heap.pprof "/debug/pprof/heap"
  fi

  # 2) 压测阶段：各档在线人数下的内存，5000 人档的 CPU 与轨迹
  if wait_for '^500 users: 500 SSE ready' 900; then
    sleep 3; grab run500.heap.pprof "/debug/pprof/heap?gc=1"
  fi
  if wait_for '^1000 users: 1000 SSE ready' 600; then
    sleep 3; grab run1000.heap.pprof "/debug/pprof/heap?gc=1"
  fi
  if wait_for '^5000 users: 5000 SSE ready' 600; then
    echo "  [profile] 5000 人档开始抓取"
    grab run5000.cpu.pprof "/debug/pprof/profile?seconds=30" & cpu=$!
    sleep 10
    grab run5000.trace "/debug/pprof/trace?seconds=5"
    grab run5000.goroutines.txt "/debug/pprof/goroutine?debug=1"
    wait "$cpu"
    grab run5000.heap.pprof "/debug/pprof/heap?gc=1"
  fi
  if wait_for '^5000 users: .* ops/s' 300; then
    sleep 3; grab after5000.heap.pprof "/debug/pprof/heap?gc=1"
  fi

  # 3) 串行写入基准：platform.member.add，一次一个，纯写入路径
  if wait_for '^Starting platform\.member\.add' 300; then
    echo "  [profile] member.add 开始抓取"
    grab memberadd.cpu.pprof "/debug/pprof/profile?seconds=9" & cpu=$!
    sleep 2
    grab memberadd.trace "/debug/pprof/trace?seconds=4"
    wait "$cpu"
  fi
fi

wait "$load_pid"

# 4) 生成文本报告
bin=".build/host/platform-server"
pp() { "$GO" tool pprof "$@" 2>>"$out/report.err"; }
focus='Tenant\)\.submit$|Tenant\)\.accept$|endLane|forkReadState|AppendAccepted|writeState|recordActions|selectedSourceValue|CurrentClaims|PromoteRecords|ForkRecords|readFromImage|readFromLane|ImageDecision|Tenant\)\.Import$|Importer\)\.Rows$'
echo "  [profile] 生成报告……"
for p in "$out"/*.cpu.pprof; do
  [ -s "$p" ] || continue
  n="${p%.pprof}"
  pp -top -nodecount=60 "$bin" "$p" >"$n.top.txt"
  pp -top -cum -nodecount=120 "$bin" "$p" >"$n.cum.txt"
  pp -peek "$focus" "$bin" "$p" >"$n.peek.txt"
done
for p in "$out"/*.heap.pprof; do
  [ -s "$p" ] || continue
  n="${p%.pprof}"
  pp -sample_index=inuse_space -top -nodecount=50 "$bin" "$p" >"$n.inuse.txt"
  pp -sample_index=inuse_space -top -cum -nodecount=80 "$bin" "$p" >"$n.inuse-cum.txt"
  pp -sample_index=alloc_space -top -nodecount=50 "$bin" "$p" >"$n.alloc.txt"
done
for t in "$out"/*.trace; do
  [ -s "$t" ] || continue
  n="${t%.trace}"
  for kind in net sync syscall sched; do
    if "$GO" tool trace -pprof="$kind" "$t" >"$n.$kind.pprof" 2>>"$out/report.err" && [ -s "$n.$kind.pprof" ]; then
      pp -top -cum -nodecount=80 "$bin" "$n.$kind.pprof" >"$n.$kind.txt"
      pp -peek "$focus" "$bin" "$n.$kind.pprof" >"$n.$kind.peek.txt"
    fi
  done
done
if [[ -n "$import_mode" && -s "$out/importA.cpu.pprof" && -s "$out/importB.cpu.pprof" ]]; then
  pp -top -cum -nodecount=80 -normalize -diff_base="$out/importA.cpu.pprof" "$bin" "$out/importB.cpu.pprof" >"$out/importB-vs-A.diff.txt"
fi
if [[ -n "$import_mode" && -s "$out/import-windows.txt" ]]; then
  python3 - "$out" <<'PY' | tee "$out/import-summary.txt"
import csv, os, re, sys
out = sys.argv[1]
def table(path):
    if not os.path.exists(path) or os.path.getsize(path) == 0:
        return {}
    with open(path) as f:
        return {row["relname"]: row for row in csv.DictReader(f)}
for line in open(os.path.join(out, "import-windows.txt")):
    m = re.match(r"(\S+) rows_before=(\d+) rows_after=(\d+) seconds=(\d+)", line.strip())
    if not m:
        continue
    name, before, after, seconds = m.group(1), int(m.group(2)), int(m.group(3)), int(m.group(4))
    rows = max(after - before, 1)
    print(f"{name}: 已有记录 {before} → {after}，窗口内导入 {after - before} 行，{seconds} 秒，{seconds * 1000 / rows:.1f} ms/行")
    a, b = table(os.path.join(out, f"{name}.pg-before.csv")), table(os.path.join(out, f"{name}.pg-after.csv"))
    rows_out = []
    for rel, row in b.items():
        prior = a.get(rel, {})
        delta = {k: int(row[k]) - int(prior.get(k, 0) or 0) for k in ("seq_scan", "seq_tup_read", "idx_scan", "idx_tup_fetch", "n_tup_ins", "n_tup_upd")}
        if any(delta.values()):
            rows_out.append((delta["seq_tup_read"], rel, delta, row["n_live_tup"]))
    rows_out.sort(reverse=True)
    print(f"  {'表':24s} {'顺序扫描/行':>10s} {'扫描读出行/行':>14s} {'索引扫描/行':>10s} {'插入/行':>8s} {'当前行数':>10s}")
    for _, rel, d, live in rows_out[:12]:
        print(f"  {rel:24s} {d['seq_scan']/rows:10.2f} {d['seq_tup_read']/rows:14.1f} {d['idx_scan']/rows:10.2f} {d['n_tup_ins']/rows:8.2f} {live:>10s}")
PY
fi

echo
echo "性能分析完成：$out"
