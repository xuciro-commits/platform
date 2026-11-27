import { testPurpose } from "./test-purposes.mjs";

function diagnosticLines(output) {
  return output.join("").split(/\r?\n/).map(line => line.trimEnd())
    .filter(line => line.trim() && !/^\s*(=== (?:RUN|PAUSE|CONT|NAME)|--- (?:PASS|FAIL|SKIP):|PASS$|FAIL$|ok\s|\?\s)/.test(line));
}

function skipReason(lines) {
  const reason = lines.map(line => line.trim()).filter(Boolean).join("；");
  if (/requires (?:an )?isolated PostgreSQL(?: database)?/i.test(reason)) return "需要独立 PostgreSQL 数据库（当前未配置 PLATFORM_BENCH_DATABASE）";
  return reason || "Go 测试未提供跳过原因";
}

export function createTestProgress(shard, emit, expected = []) {
  const rows = new Map();
  const numbers = new Map();
  const packages = new Map();
  const keyOf = event => (event.Package ?? "") + "/" + event.Test;
  const getRow = event => {
    const key = keyOf(event);
    if (!rows.has(key)) rows.set(key, { name: event.Test, package: event.Package, purpose: testPurpose(event.Test), status: "not-run", output: [] });
    return rows.get(key);
  };
  const label = row => {
    const root = row.name.split("/")[0];
    const key = row.package + "/" + root;
    if (!numbers.has(key)) numbers.set(key, numbers.size + 1);
    const progress = numbers.get(key) + (expected.length ? "/" + expected.length : "");
    const group = shard.replace(/^host shard /, "分片 ").replace("other server packages", "非宿主");
    const code = root.match(/^TestInvariant(K[0-9]+(?:K[0-9]+)*)/)?.[1];
    return "[" + group + " · " + progress + (expected.length ? "" : " · " + (row.package ?? "?").replace(/^platformserver\//, "")) + "] " + (code ? code + " · " : "") + row.purpose;
  };
  function accept(event) {
    if (!event.Test) {
      const pkg = event.Package ?? "go";
      if (!packages.has(pkg)) packages.set(pkg, []);
      if (event.Action === "output" || event.Action === "build-output") packages.get(pkg).push(event.Output ?? "");
      if (event.Action === "fail" || event.Action === "build-fail") {
        const failed = [...rows.values()].some(row => row.package === pkg && row.status === "fail");
        if (!failed) {
          emit("✗ [" + shard + "] " + pkg + " 包构建或执行失败");
          for (const line of diagnosticLines(packages.get(pkg)).slice(0, 20)) emit("    " + line.trim());
        }
      }
      return;
    }
    const row = getRow(event);
    if (event.Action === "run") emit("→ " + label(row));
    if (event.Action === "output") row.output.push(event.Output ?? "");
    if (!["pass", "fail", "skip"].includes(event.Action)) return;
    const descendants = [...rows.values()].filter(other => other.package === row.package && other.name.startsWith(row.name + "/"));
    row.status = event.Action === "pass" && descendants.some(other => other.status === "skip" || other.status === "partial") ? "partial" : event.Action;
    row.seconds = event.Elapsed ?? 0;
    const lines = diagnosticLines(row.output);
    if (row.status === "skip") {
      row.reason = skipReason(lines);
      row.reasonProvided = lines.length > 0;
    }
    if (row.status === "fail") row.diagnostics = lines;
    const mark = { pass: "✓", fail: "✗", skip: "↷", partial: "△" }[row.status];
    const status = { pass: "通过", fail: "失败", skip: "跳过", partial: "部分通过（子项跳过）" }[row.status];
    emit(mark + " " + label(row) + " · " + status + " (" + row.seconds.toFixed(3) + "s)" + (row.reason ? " · 原因：" + row.reason : ""));
    if (row.status === "fail") {
      emit("    " + row.package + "/" + row.name);
      for (const line of lines.slice(0, 24)) emit("    " + line.trim());
      if (lines.length > 24) emit("    …其余诊断见本次原始日志");
    }
  }
  return {
    accept,
    details: () => [...rows.values()].map(({ output, ...row }) => row),
  };
}
