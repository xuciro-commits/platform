import { expect, fresh, pageUIProfile, test } from "./kit";
import { switchWorkspace } from "./host";

test("task workspaces preserve native PMS and hand off a controlled receiving application", async ({ browser, page, builder, operator, editor }, testInfo) => {
  test.setTimeout(90_000);
  const suffix = fresh("workspace").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const appID = fresh("APP"), flowID = fresh("FLOW");
  const objectName = `receipt${suffix}`, pageName = `receiving${suffix}`, appName = `wms${suffix}`, flowName = `routing${suffix}`;
  const object = await builder.object({
    name: objectName, title: "Receipt", plural: "Receipts",
    fields: [{ name: "number", title: "Receipt number", type: "text", required: true, search: true }],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  const receiving = await builder.page({
    name: pageName, title: "Receiving workspace", object: object.type,
    sections: [{ id: "receipts", widget: "table", configVersion: 1, fields: ["number"] }],
    document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: {
      root: { kind: "rows", children: ["receipts"] }, receipts: { kind: "widget", section: "receipts" },
    } },
  });
  await builder.decide("build.page.publish", { type: "build.page", id: receiving.id }, {});
  await builder.decide("build.process.create", { type: "build.process", id: flowID }, {
    name: flowName, title: "Receiving process", manual: true, steps: [{ name: "done", kind: "end" }],
  });
  await builder.decide("build.process.publish", { type: "build.process", id: flowID }, {});
  await builder.decide("build.app.create", { type: "build.app", id: appID }, {
    uiProfile: pageUIProfile, header: { variant: "horizontal", title: "WMS", logo: "wms", items: [{ kind: "logo" }, { kind: "title" }] },
    name: appName, title: "WMS entry probe", icon: "clipboard", pages: [pageName],
    resources: [{ app: "build", kind: "flow", name: `build.${flowName}` }],
  });

  // Header edits retain the existing descriptor: removing a logo clears its
  // URL; disabling the header sends an explicit null to its original owner.
  await builder.open(page, `/module?id=${appID}`);
  await page.getByRole("tab", { name: "Header", exact: true }).click();
  const header = page.getByRole("group", { name: "Application header", exact: true });
  await header.locator("div").filter({ has: page.locator("strong").filter({ hasText: /^logo$/ }) }).first().getByRole("button", { name: "Remove", exact: true }).click();
  const savedHeader = async () => (await builder.record("build.app", appID)).header;
  await expect.poll(async () => (await savedHeader())?.logo ?? "").toBe("");
  await header.getByRole("checkbox", { name: "Show application header", exact: true }).uncheck();
  await expect.poll(savedHeader).toBeUndefined();

  // Rejection returns to the original navigation, preserving each diagnostic.
  await builder.decide("build.app.edit", { type: "build.app", id: appID }, {
    groups: [{ title: "Empty receiving group", pages: [] }, { title: "Receiving", pages: [pageName] }, { title: "Receiving", pages: [pageName] }],
  });
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "holds no page" })).toBeVisible();
  await page.getByRole("button", { name: "Open the module's navigation", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Navigation", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.getByRole("button", { name: "Toggle dock", exact: true }).click();
  await expect(page.getByRole("button", { name: "The group “Empty receiving group” holds no page. Put a page under it or remove the group.", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: `The page “${pageName}” is under two groups: “Receiving” and “Receiving”.`, exact: true })).toBeVisible();
  for (let i = 0; i < 3; i++) await page.getByRole("button", { name: "Remove group", exact: true }).first().click();
  await expect.poll(async () => (await builder.record("build.app", appID)).groups ?? []).toEqual([]);

  // Native PMS and Knowledge remain task-owned, not builder-only aliases.
  await builder.open(page, "/home");
  await expect(page.getByRole("button", { name: /^PMS\b/ }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Platform Catalog", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: /^PMS\b/ }).first().click();
  await expect(page.getByRole("heading", { name: "Reservations", exact: true })).toBeVisible();
  await switchWorkspace(page, "Knowledge");
  await expect(page.getByRole("heading", { name: "Documents", exact: true })).toBeVisible();

  await switchWorkspace(page, "Builder");
  await expect(page.getByRole("button", { name: "Switch application", exact: true })).toContainText("Builder");
  await page.goto(`/#/project?id=${appID}`); // preserve the supported old deep link
  await editor.release();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();

  // Navigation is retained state, not assumed collapsed. Open it through the
  // original accessible toggle only when needed; retain all owner assertions.
  await switchWorkspace(page, "Control Panel");
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  const nav = page.getByRole("navigation", { name: "Main", exact: true });
  await expect(nav.getByRole("button", { name: "Members", exact: true })).toBeVisible();
  await expect(nav.getByRole("button", { name: "Documents", exact: true })).toHaveCount(0);
  await switchWorkspace(page, "Runs");
  await expect(nav.getByRole("button", { name: "Workflow runs", exact: true })).toBeVisible();
  await switchWorkspace(page, "Builder");
  await nav.getByRole("button", { name: "Release history", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Release review", exact: true })).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("tenant-operations.png"), fullPage: true });

  // The ordinary operator receives the released application, sees native
  // writes after refresh, and remains unable to edit the builder's definitions.
  const operatorContext = await browser.newContext({ baseURL: "http://127.0.0.1:18496", locale: "en-US" });
  try {
    const business = await operatorContext.newPage();
    await operator.open(business, "/home");
    await expect(business.getByRole("button", { name: "Projects", exact: true })).toHaveCount(0);
    await business.getByRole("button", { name: "WMS entry probe", exact: true }).click();
    await expect(business.getByRole("heading", { name: "Receiving workspace", exact: true })).toBeVisible();
    const recordID = fresh("RECEIPT");
    await operator.decide(`${object.type}.create`, { type: object.type, id: recordID }, { number: "IN-M1" });
    await expect(business.getByRole("row").filter({ hasText: "IN-M1" })).toBeVisible();
    await business.reload();
    await expect(business.getByRole("heading", { name: "Receiving workspace", exact: true })).toBeVisible();
    await expect(operator.decide("build.object.edit", { type: "build.object", id: object.id }, { title: "Unauthorized" })).rejects.toThrow(/POLICY_DENIED/);
    if (process.env.PLATFORM_SCREENSHOTS) {
      await business.screenshot({ path: testInfo.outputPath("business-receiving.png"), fullPage: true });
      await business.setViewportSize({ width: 390, height: 844 });
      await business.screenshot({ path: testInfo.outputPath("business-receiving-narrow.png"), fullPage: true });
    }
  } finally { await operatorContext.close(); }
});
