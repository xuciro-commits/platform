import { expect, pageUIProfile, runtime, slug, test } from "./kit";

test("published Expr controls visibility through the shared rule engine", async ({ page, builder, operator }) => {
  const { type } = await builder.object({ name: slug("rule"), title: "Rule records", fields: [{ name: "name", title: "Name", type: "text" }] });
  const document = {
    formatVersion: 2,
    uiProfile: pageUIProfile,
    root: "root",
    nodes: {
      root: { kind: "rows", children: ["toggle", "conditional"] },
      toggle: { kind: "widget", section: "toggle" },
      conditional: { kind: "rows", visibleWhen: "visible", children: ["notice"] },
      notice: { kind: "widget", section: "notice" },
    },
    variables: {
      enabled: { scope: "page", type: "boolean", mode: "state", initial: true },
      visible: {
        scope: "page", type: "boolean", mode: "derived",
        rule: { op: "and", kind: "boolean", args: [{ kind: "boolean", ref: "enabled" }, { kind: "boolean", lit: { value: true } }] },
      },
    },
  };
  const draft = await builder.page({
    title: "Rule visibility", object: type, document,
    sections: [
      { id: "toggle", widget: "boolean-input", configVersion: 1, title: "Enable rule", booleanVariable: "enabled", booleanLabel: "Enable rule" },
      { id: "notice", widget: "notice", configVersion: 1, title: "Rule result", notice: { title: "Allowed", message: "The shared rule allows this section.", tone: "info" } },
    ],
  });
  await builder.decide("build.page.publish", { type: "build.page", id: draft.id }, {});
  const tab = await runtime(page, operator, draft.name);
  const notice = tab.getByRole("note", { name: "Rule result", exact: true });
  await expect(notice).toBeVisible();
  const toggle = tab.getByRole("switch", { name: "Enable rule", exact: true });
  await toggle.click();
  await expect(notice).toBeHidden();
  await toggle.click();
  await expect(notice).toBeVisible();
  expect(await tab.evaluate(() => ({ version: (globalThis as any).platformRuleVersion, eval: typeof (globalThis as any).platformRuleEval }))).toEqual({ version: "platform.expr.v1", eval: "function" });
  const budget=await tab.evaluate(()=>{
    const run=(globalThis as any).platformRuleEval as (raw:string)=>string;
    const request={expr:{op:"sum",kind:"integer",agg:{source:"rows",field:["qty"]}},schema:{sets:{rows:{policy:"complete",element:{kind:"reference",fields:{qty:{kind:"integer"}}}}}},context:{sets:{rows:{complete:true,items:Array.from({length:10000},(_,i)=>({id:`row-${i}`,fields:{qty:{kind:"integer",status:"value",value:1}}}))}}}};
    const full=JSON.parse(run(JSON.stringify(request)));
    request.context.sets.rows.items.push({id:"overflow",fields:{qty:{kind:"integer",status:"value",value:1}}});
    const overflow=JSON.parse(run(JSON.stringify(request)));
    return {full,overflow};
  });
  expect(budget.full.value).toMatchObject({kind:"integer",status:"value",value:"10000"});
  expect(budget.overflow.error).toMatchObject({code:"budget"});
  expect(budget.overflow.error.message).toContain("10001");
});

test("builder publishes a model invariant and a rejected form keeps its values", async ({ page, builder, operator, editor }) => {
  const object = await builder.object({ name: slug("invariant"), title: "Invariant entry", fields: [{ name: "code", title: "Code", type: "text" }] });
  await builder.open(page, `/object-type?id=${object.id}&tab=constraints`);
  await page.getByRole("button", { name: "Add a constraint", exact: true }).click();
  await page.getByRole("textbox", { name: "Constraint name", exact: true }).fill("uniquecode");
  await page.getByRole("combobox", { name: "Constraint kind", exact: true }).selectOption("unique");
  await page.getByRole("textbox", { name: "Failure message (English)", exact: true }).fill("Code must be unique.");
  await page.getByRole("textbox", { name: "Failure message (Simplified Chinese)", exact: true }).fill("编码必须唯一。");
  await editor.release();
  await expect.poll(async () => JSON.parse((await builder.record("build.object", object.id)).published).constraints?.[0]?.name).toBe("uniquecode");
  const saved = await builder.record("build.object", object.id);
  expect(JSON.parse(saved.published).constraints[0]).toMatchObject({ name: "uniquecode", kind: "unique", fields: ["code"] });
  await builder.records(object.type, [{ code: "EXISTING" }]);
  const tab = await runtime(page, operator, object.name);
  await tab.getByRole("button", { name: "Create invariant entry", exact: true }).click();
  const form = tab.getByRole("dialog", { name: "Create invariant entry", exact: true });
  await form.getByRole("textbox", { name: "Code", exact: true }).fill("EXISTING");
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(form).toBeVisible();
  await expect(form.getByText("Code must be unique.", { exact: true })).toBeVisible();
  await expect(form.getByRole("textbox", { name: "Code", exact: true })).toHaveValue("EXISTING");
  await form.getByRole("textbox", { name: "Code", exact: true }).fill("CORRECTED");
  await form.getByRole("button", { name: "Submit corrected inputs", exact: true }).click();
  await expect(form).toBeHidden();
  await expect(tab.getByText("CORRECTED", { exact: true }).first()).toBeVisible();
});
