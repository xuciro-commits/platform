import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("resource variables bind parent selections, authorized query windows and filters", async ({ page, request }, testInfo) => {
  const name = fresh("resources").replace(/[^a-z0-9]/gi, "").toLowerCase(), id = fresh("PAGE");
  const a = fresh("ACC"), b = fresh("ACC"), empty = fresh("ACC"), oa = fresh("OPP"), ob = fresh("OPP");
  for (const [account, title, kind] of [[a, "RESOURCE-A", "company"], [b, "RESOURCE-B", "person"], [empty, "RESOURCE-EMPTY", "company"]]) {
    await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: title, kind });
  }
  for (const [account, opportunity, title] of [[a, oa, "RESOURCE-OPP-A"], [b, ob, "RESOURCE-OPP-B"]]) {
    await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opportunity }, { account, title });
  }
  await decide(request, "manager", "platform", "platform.member.grant", { type: "platform.member", id: "sales-1" }, { app: "build", role: "user" });
  try {
    await decide(request, "manager", "build", "build.page.create", { type: "build.page", id }, {
      name, title: "Resource variable desk", object: "crm.account",
      selections: [{ name: "parent", object: { app: "crm", kind: "object", name: "crm.account" } }, { name: "child", object: { app: "crm", kind: "object", name: "crm.opportunity" } }],
      sections: [
        { widget: "table", title: "Parents", fields: ["name"], selection: "parent" },
        { widget: "filter", title: "Account kind", fields: ["kind"] },
        { widget: "table", title: "Children", object: "crm.opportunity", fields: ["title", "stage"], query: "crm.open-opportunities", parentSelection: "parent", selection: "child" },
        { widget: "detail", title: "Selected child", object: "crm.opportunity", fields: ["title"], selection: "child" },
        { widget: "text", title: "Query status", text: "Matching opportunities are loaded." },
        { widget: "text", title: "Filter status", text: "An account filter is active." },
      ],
    });
    await open(page, "manager", `/compose?id=${id}`);
    const tree = page.getByRole("region", { name: "Widgets and layout", exact: true });
    const inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
    const bind = async (kind: string, source: string, consumer: string) => {
      await tree.getByRole("button", { name: "Page variables", exact: true }).click();
      await inspector.getByRole("button", { name: "Add variable", exact: true }).click();
      await inspector.getByLabel("Variable label", { exact: true }).fill(`${kind} output`);
      await inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("resource");
      await inspector.getByRole("combobox", { name: "Resource output kind", exact: true }).selectOption(kind);
      await inspector.getByRole("combobox", { name: "Source widget", exact: true }).selectOption({ label: source });
      const variable = await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).inputValue();
      await inspector.getByRole("button", { name: "Add variable", exact: true }).click();
      await inspector.getByLabel("Variable label", { exact: true }).fill(`${kind} has value`);
      await inspector.getByRole("combobox", { name: "Value type", exact: true }).selectOption("boolean");
      await inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("derived");
      await inspector.getByRole("combobox", { name: "Operator", exact: true }).selectOption("present");
      await inspector.getByRole("combobox", { name: "Argument source", exact: true }).selectOption(variable);
      const condition = await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).inputValue();
      await tree.getByRole("button", { name: consumer, exact: true }).click();
      await inspector.getByRole("combobox", { name: "Visible when", exact: true }).selectOption(condition);
      return variable;
    };
    await bind("record", "Children", "Selected child");
    const queryVariable = await bind("query", "Children", "Query status");
    await bind("filter", "Account kind", "Filter status");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    const saved = (await (await request.get(`/v1/records/build.page/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
    expect(saved.document.uiProfile).toBe("platform.page.v2.5");
    expect(Object.values(saved.document.variables).filter((variable: any) => variable.mode === "resource")).toHaveLength(3);
    if (process.env.PLATFORM_SCREENSHOTS) {
      const canvas = page.getByRole("region", { name: "The page", exact: true });
      await canvas.getByRole("row").filter({ has: page.getByRole("cell", { name: "RESOURCE-A", exact: true }) }).click();
      await expect(canvas.getByRole("cell", { name: "RESOURCE-OPP-A", exact: true })).toBeVisible();
      await tree.getByRole("button", { name: "Page variables", exact: true }).click();
      await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).selectOption(queryVariable);
      const value = inspector.getByRole("region", { name: "Current variable value", exact: true });
      await expect(value.getByText("crm.opportunity", { exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
      await value.scrollIntoViewIfNeeded();
      await page.screenshot({ path: testInfo.outputPath("resource-variable-inspector.png"), fullPage: true, animations: "disabled" });
    }
    await page.getByRole("button", { name: "Review release", exact: true }).click();
    await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    const operation = await page.context().newPage();
    await open(operation, "sales", `/page?app=build&kind=page&name=${name}`);
    const parent = (title: string) => operation.getByRole("row").filter({ has: operation.getByRole("cell", { name: title, exact: true }) });
    await parent("RESOURCE-A").click();
    await expect(parent("RESOURCE-OPP-A")).toBeVisible();
    await expect(operation.getByText("Matching opportunities are loaded.", { exact: true })).toBeVisible();
    await parent("RESOURCE-OPP-A").click();
    await expect(operation.getByRole("heading", { name: "RESOURCE-OPP-A", exact: true })).toBeVisible();
    await parent("RESOURCE-B").click();
    await expect(operation.getByRole("heading", { name: "RESOURCE-OPP-A", exact: true })).toHaveCount(0);
    await expect(parent("RESOURCE-OPP-B")).toBeVisible();
    await expect(parent("RESOURCE-OPP-A")).toHaveCount(0);
    await parent("RESOURCE-EMPTY").click();
    await expect(operation.getByText("Matching opportunities are loaded.", { exact: true })).toHaveCount(0);
    await operation.getByRole("search", { name: "Account kind", exact: true }).getByRole("combobox").selectOption("company");
    await expect(operation.getByText("An account filter is active.", { exact: true })).toBeVisible();
    await expect(parent("RESOURCE-B")).toHaveCount(0);
    await parent("RESOURCE-A").click();
    await expect(parent("RESOURCE-OPP-A")).toBeVisible();
    if (process.env.PLATFORM_SCREENSHOTS) {
      await parent("RESOURCE-OPP-A").click();
      const selected = operation.getByRole("heading", { name: "RESOURCE-OPP-A", exact: true });
      await selected.scrollIntoViewIfNeeded();
      await operation.screenshot({ path: testInfo.outputPath("resource-variables.png"), fullPage: true, animations: "disabled" });
      await operation.setViewportSize({ width: 390, height: 844 });
      await selected.scrollIntoViewIfNeeded();
      await operation.screenshot({ path: testInfo.outputPath("resource-variables-narrow.png"), fullPage: true, animations: "disabled" });
    }
    await operation.reload();
    await expect(operation.getByText("An account filter is active.", { exact: true })).toHaveCount(0);
    await expect(operation.getByText("Matching opportunities are loaded.", { exact: true })).toHaveCount(0);
  } finally {
    await decide(request, "manager", "platform", "platform.member.revoke", { type: "platform.member", id: "sales-1" }, { app: "build" });
  }
});
