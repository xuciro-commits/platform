import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { fileURLToPath } from "node:url";
import { performance } from "node:perf_hooks";

const root = fileURLToPath(new URL("../", import.meta.url));
const records = "capabilities/server/kernel/records/";
const protocol = "capabilities/server/kernel/protocol/";
const ledger = records + "ledger_projection.go";
const store = records + "record_store.go";
const rules = protocol + "rules.go";
const schema = protocol + "rule_schema.go";
const k47 = "TestInvariantK47LedgerFactsAndProjection";
const k48 = "TestInvariantK48LedgerReversalAndClose";
const k49 = "TestInvariantK49LiveDerivedReadsCurrentSources";
const k55 = "TestInvariantK55LedgerValuationAndDepletion";
const k48Record = "TestInvariantK48LedgerRecordClose";
const mutations = [
  ["record-closed-date", records + "ledger_records.go", "if day > plan.closedBefore {", "if false && day > plan.closedBefore {", k48Record, "want closed before 2026-10-01"],
  ["prior-balance", schema, "if completeKeys {", "if false && completeKeys {", k55, "weighted outbound amt ="],
  ["balance-constraint", ledger, 'if err != nil || string(result.Value.Data) != "true" {', 'if err != nil || (false && string(result.Value.Data) != "true") {', k47, "concurrent 6+6:"],
  ["immutable-fact", store, "} else if !reflect.DeepEqual(prior.Value.Interface(), value.Value.Interface()) {", "} else if false && !reflect.DeepEqual(prior.Value.Interface(), value.Value.Interface()) {", k47, "ledger store allowed editing an accepted fact"],
  ["reverse-once", ledger, "if f.reverses == f.id || p.reversed.Has(f.reverses) {", "if f.reverses == f.id {", k48, "reverse B refusal:"],
  ["closed-date", ledger, 'if plan.closedBefore != "" && f.on < plan.closedBefore {', 'if false && plan.closedBefore != "" && f.on < plan.closedBefore {', k48, "create C refusal:"],
  ["single-balance-writer", store, 'if et.info.BalanceOf != "" {\n\t\treturn fmt.Errorf("ledger balance is read-only")', 'if false && et.info.BalanceOf != "" {\n\t\treturn fmt.Errorf("ledger balance is read-only")', k47, "second balance writer allowed"],
  ["stored-foreign-reference", "capabilities/server/services/ontology/stored_formulas.go", 'if e.Op == "path" {', 'if false && e.Op == "path" {', "TestInvariantK49StoredFormulaRejectsForeignReference", "foreign formula diagnostic:"],
  ["withheld-guard", rules, "if len(strict) > 0 && strict[0] && ruleWithheld(expr, context) {", "if false && len(strict) > 0 && strict[0] && ruleWithheld(expr, context) {", k49, "withheld source passed guard:"],
  ["live-guard", schema, "ctx.Refs[e.Ref] = value", 'ctx.Refs[e.Ref] = platform.ExprValue{Kind: value.Kind, Status: "value", Data: platform.Raw(0)}', k49, "stale approval guard:"],
];
const requested = process.argv.slice(2);
if (requested.length && requested.some(name => !mutations.some(m => m[0] === name))) {
  throw Error("Select mutation by name: " + mutations.map(m => m[0]).join(", "));
}
const selected = mutations.filter(m => !requested.length || requested.includes(m[0]));
const scratch = mkdtempSync(join(tmpdir(), "platform-ledger-mutate-"));
const server = join(scratch, "capabilities/server");
const binary = join(scratch, "invariants");
const run = (file, args, cwd) => {
  const result = spawnSync(file, args, { cwd, encoding: "utf8", timeout: 60000, maxBuffer: 16 * 1024 * 1024 });
  const output = (result.stdout ?? "") + (result.stderr ?? "");
  if (result.error || result.signal) throw Error(file + " interrupted: " + (result.error ?? result.signal) + "\n" + output);
  return { status: result.status, output };
};
function compile() {
  const result = run(process.env.GO || "go", ["test", "-c", "-o", binary, "./host"], server);
  if (result.status !== 0) throw Error("Mutation failed to compile; not a detection\n" + result.output);
}
function test(pattern) {
  return run(binary, ["-test.run", pattern, "-test.count=1", "-test.v"], join(server, "host"));
}
try {
  const files = execFileSync("git", ["ls-files", "-z", "--", "capabilities/server", "contract/go"], { cwd: root, encoding: "utf8", maxBuffer: 16 * 1024 * 1024 }).split("\0").filter(Boolean);
  for (const path of files) {
    const target = join(scratch, path);
    mkdirSync(dirname(target), { recursive: true });
    copyFileSync(join(root, path), target);
  }
  compile();
  const original = test("^TestInvariantK(47|48|49|55)");
  if (original.status !== 0 || !/^--- PASS: TestInvariantK47LedgerFactsAndProjection /m.test(original.output) ||
      !/^--- PASS: TestInvariantK48LedgerReversalAndClose /m.test(original.output) ||
      !/^--- PASS: TestInvariantK48LedgerRecordClose /m.test(original.output) ||
      !/^--- PASS: TestInvariantK49LiveDerivedReadsCurrentSources /m.test(original.output) ||
      !/^--- PASS: TestInvariantK49StoredFormulaRejectsForeignReference /m.test(original.output) ||
      !/^--- PASS: TestInvariantK55LedgerValuationAndDepletion /m.test(original.output)) {
    throw Error("Original invariants did not pass\n" + original.output);
  }
  console.log("baseline passed\n" + original.output);
  let missed = 0;
  for (const [name, path, before, after, invariant, evidence] of selected) {
    const target = join(scratch, path);
    const originalSource = readFileSync(target, "utf8");
    if (originalSource.split(before).length !== 2) throw Error(name + ": mutation anchor missing or ambiguous: " + path);
    const started = performance.now();
    try {
      writeFileSync(target, originalSource.replace(before, after));
      compile();
      const result = test("^" + invariant + "$");
      const detected = result.status === 1 && result.output.includes("--- FAIL: " + invariant) && result.output.includes(evidence);
      console.log(name + ": " + (detected ? "DETECTED" : "MISSED") + " (" + ((performance.now() - started) / 1000).toFixed(1) + "s)\n" + result.output);
      if (!detected) missed++;
    } finally {
      writeFileSync(target, originalSource);
    }
  }
  console.log((selected.length - missed) + "/" + selected.length + " mutations detected");
  if (missed) process.exitCode = 1;
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
