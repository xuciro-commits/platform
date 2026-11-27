#!/usr/bin/env bash
# 内核改动的验收门槛：静态检查、内核不变量、竞态检查、PostgreSQL 写入与恢复、CSV 导入压测。
# 用法：bash deploy/dev/kernel-gates.sh quick      （定点 K1/K13 不变量）
#       GATES="race sql" bash deploy/dev/kernel-gates.sh   （明确选择专项）
#       GATES="snapshot import" bash deploy/dev/kernel-gates.sh
#        snapshot：导入 1000 行，快照每 200 条一次，看写入不停时快照是否都成功；
#        import：导入 300、3000、再 300 行（不试算、快照用默认间隔）。首尾两次都是 300 行，
#        只差在表里已有的数据量，所以首尾倍数就是"表变大后每行变慢多少"，不受单次导入长短影响；
#        每次导入还打印每行在各表上读了多少行（PostgreSQL 统计），看有没有查询随表变大。
#        两者分开跑：快照本身随状态变大而变贵，混在一起会把快照成本算进"越写越慢"。
# 注意：脚本运行中不要修改它（bash 边读边执行，改动会让正在跑的那次报语法错误）。
# 每一项的输出写到 .build/gates/<时间>/<项>.log，最后打印汇总；任一项失败则退出码非零。
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

GO="${GO:-go}"
gates="${GATES:-}"
if [[ "${1:-}" == "quick" ]]; then
  gates="${GATES:-invariants}"
  export PLATFORM_LOAD_IMPORT_ROWS="${PLATFORM_LOAD_IMPORT_ROWS:-300,3000,300}"
  export PLATFORM_LOAD_IMPORT_PREVIEW="${PLATFORM_LOAD_IMPORT_PREVIEW:-0}"
fi
if [[ -z "$gates" ]]; then
  echo '请选择专项：GATES="race sql" bash deploy/dev/kernel-gates.sh；发版全量用 make verify。' >&2
  exit 2
fi

out="$PWD/.build/gates/$(date +%Y%m%d-%H%M%S)"
mkdir -p "$out"
declare -a summary
failed=0

run() { # 名称 命令…
  local name=$1; shift
  local started=$SECONDS
  echo "== $name"
  if "$@" >"$out/$name.log" 2>&1; then
    summary+=("通过  $name  $((SECONDS - started))s")
  else
    summary+=("失败  $name  $((SECONDS - started))s  （见 $out/$name.log）")
    failed=1
    tail -n 30 "$out/$name.log"
  fi
}

snapshots() { # 统计最近一次压测里的快照结果；有"因状态前进失败"则判不通过
  local latest
  latest=$(ls -td .build/load/*/ 2>/dev/null | head -1)
  [[ -n "$latest" && -f "$latest/server.log" ]] || return 0
  local saved refused
  saved=$(grep -c 'saved a snapshot' "$latest/server.log" || true)
  refused=$(grep -c 'checkpoint is at' "$latest/server.log" || true)
  summary+=("      快照：成功 $saved 次，因状态前进失败 $refused 次（$latest/server.log）")
  if (( refused > 0 )); then
    failed=1
  elif [[ "${1:-}" == "required" ]] && (( saved == 0 )); then
    summary+=("      快照一次都没存：这一项没有验证到快照"); failed=1
  fi
}

for gate in $gates; do
  case "$gate" in
    build)
      unformatted=$(cd capabilities/server && gofmt -l . 2>&1)
      if [[ -n "$unformatted" ]]; then
        summary+=("失败  格式  $(echo "$unformatted" | tr '\n' ' ')（修复：cd capabilities/server && gofmt -w <文件>）")
        failed=1
      fi
      run build bash -c "cd capabilities/server && $GO build ./... && $GO vet ./kernel/... ./platform/... ./host/..."
      ;;
    invariants) run invariants node scripts/check-invariants.mjs "${PLATFORM_GATE_INVARIANTS:-K1[A-Z]|K13}" ;;
    check)
      # make check 的第一步只报"失败"，不列出未格式化的文件；先单独列出来。
      unformatted=$(cd capabilities/server && gofmt -l . 2>&1)
      if [[ -n "$unformatted" ]]; then
        echo "$unformatted" >"$out/gofmt.txt"
        summary+=("      未格式化：$(tr '\n' ' ' <"$out/gofmt.txt")（修复：cd capabilities/server && gofmt -w <文件>）")
      fi
      run check make check
      ;;
    test) run test make test ;;
    race) run race bash -c "cd capabilities/server && $GO test -race -count=1 ./kernel/... ./platform/... && $GO test -race -count=1 -run 'TestInvariantK1|TestInvariantK13' ./host/" ;;
    sql) run sql make verify-sql ;;
    snapshot)
      run snapshot env PLATFORM_LOAD_IMPORT_ROWS=1000 PLATFORM_LOAD_SNAPSHOT_EVERY=200 make load-sql
      snapshots required
      ;;
    import)
      run import env PLATFORM_LOAD_IMPORT_ROWS="${PLATFORM_LOAD_IMPORT_ROWS:-1000,10000}" make load-sql
      snapshots
      grep -E '^import [0-9]+ rows: (apply [0-9]|sql )|^import scaling' "$out/import.log" | sed 's/^/      /' >"$out/import.summary" || true
      while IFS= read -r line; do summary+=("$line"); done <"$out/import.summary"
      held=$(ls -td .build/load/*/ 2>/dev/null | head -1)
      [[ -n "$held" ]] && summary+=("      车道持有超 200ms：$(grep -c 'lane held .* by submit' "$held/server.log" || true) 次")
      ;;
    *) echo "未知的门槛 $gate"; failed=1 ;;
  esac
done

echo
printf '%s\n' "${summary[@]}"
echo "日志：$out"
exit "$failed"
