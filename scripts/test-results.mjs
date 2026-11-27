export function selectTests(output, pattern = "") {
  const filter = new RegExp(pattern);
  return output.split(/\r?\n/).filter(name => /^Test\w+$/.test(name) && filter.test(name));
}

export function summarizeGo(output, expected = []) {
  const tests = new Map();
  const packages = new Map();
  for (const line of output.split(/\r?\n/)) {
    if (!line.trim()) continue;
    const event = JSON.parse(line);
    if (!event.Test) {
      if (event.Package && ["pass", "fail", "skip"].includes(event.Action)) packages.set(event.Package, event.Action);
      continue;
    }
    const key = `${event.Package ?? ""}/${event.Test}`;
    if (event.Action === "run") tests.set(key, { name: event.Test, package: event.Package, status: "not-run" });
    if (["pass", "fail", "skip"].includes(event.Action)) tests.set(key, { name: event.Test, package: event.Package, status: event.Action });
  }
  const rows = [...tests.values()];
  const roots = rows.filter(row => !row.name.includes("/"));
  const counts = { passed: 0, partial: 0, skipped: 0, failed: 0, notRun: 0 };
  const leaves = { pass: 0, fail: 0, skip: 0, "not-run": 0 };
  for (const row of rows) {
    const children = rows.filter(child => child.package === row.package && child.name.startsWith(row.name + "/"));
    if (!children.length) leaves[row.status]++;
    if (row.name.includes("/")) continue;
    if (row.status === "fail" || children.some(child => child.status === "fail")) counts.failed++;
    else if (row.status === "not-run" || children.some(child => child.status === "not-run")) counts.notRun++;
    else if (row.status === "skip") counts.skipped++;
    else if (children.some(child => child.status === "skip")) counts.partial++;
    else counts.passed++;
  }
  for (const name of expected) if (!roots.some(row => row.name === name)) counts.notRun++;
  return { ...counts, leaves, packageFailures: [...packages.values()].filter(status => status === "fail").length,
    skippedTests: rows.filter(row => row.status === "skip").map(row => `${row.package}/${row.name}`) };
}

export function testGate(result, { requireNoSkip = false, allowSkippedReport = false } = {}) {
  return result.failed === 0 && result.notRun === 0 && result.packageFailures === 0
    && ((result.leaves?.pass ?? 0) > 0 || (allowSkippedReport && result.skipped + result.partial > 0))
    && (!requireNoSkip || result.skipped + result.partial === 0);
}
