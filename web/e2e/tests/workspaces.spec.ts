import { expect, test } from "@playwright/test";
import { decide, fresh, open, switchWorkspace } from "./host";

test("task workspaces preserve native PMS and hand off a controlled receiving application", async ({ browser, page, request }, testInfo) => {
  test.setTimeout(90_000);
  const suffix = fresh("workspace").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const objectID = fresh("OBJ"), pageID = fresh("PAGE"), appID = fresh("APP"), flowID = fresh("FLOW");
  const objectName = `receipt${suffix}`, pageName = `receiving${suffix}`, appName = `wms${suffix}`, flowName = `routing${suffix}`;
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, {
    name: objectName, title: "Receipt", plural: "Receipts", fields: [{ name: "number", title: "Receipt number", type: "text", required: true, search: true }],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id: pageID }, {
    name: pageName, title: "Receiving workspace", object: `build.${objectName}`, sections: [{ widget: "table", fields: ["number"] }],
  });
  await decide(request, "manager", "build", "build.page.publish", { type: "build.page", id: pageID }, {});
  await decide(request, "manager", "build", "build.process.create", { type: "build.process", id: flowID }, {
    name: flowName, title: "Receiving process", manual: true, steps: [{ name: "done", kind: "end" }],
  });
  await decide(request, "manager", "build", "build.process.publish", { type: "build.process", id: flowID }, {});
  await decide(request, "manager", "build", "build.app.create", { type: "build.app", id: appID }, {
    name: appName, title: "WMS entry probe", icon: "clipboard", pages: [pageName], resources: [{ app: "build", kind: "flow", name: `build.${flowName}` }],
  });

  await open(page, "manager", "/home");
  await expect(page.getByRole("button", { name: /^PMS\b/ }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Platform Catalog", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: /^PMS\b/ }).first().click();
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Reservations", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Switch application", exact: true }).click();
  await page.getByRole("menuitemradio", { name: "Knowledge", exact: true }).click();
  await expect(page.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Documents", exact: true })).toBeVisible();

  await switchWorkspace(page, "Projects");
  await page.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Shared resources", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Shared resources", exact: true })).toBeVisible();
  await page.goto(`/#/application?id=${appID}`); // an existing deep link
  await page.getByRole("button", { name: "Review application release", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();

  // Operations are their own applications now (ADR-0052 P3): Control Panel governs,
  // Runs operates, Releases delivers. None of them carries business navigation.
  await switchWorkspace(page, "Control Panel");
  const nav = page.getByRole("navigation", { name: "Main", exact: true });
  await expect(nav.getByRole("button", { name: "Members", exact: true })).toBeVisible();
  await expect(nav.getByRole("button", { name: "Documents", exact: true })).toHaveCount(0);
  await switchWorkspace(page, "Runs");
  await expect(nav.getByRole("button", { name: "Workflow runs", exact: true })).toBeVisible();
  await switchWorkspace(page, "Releases");
  await nav.getByRole("button", { name: "Release review", exact: true }).click();
  await expect(page.getByRole("button", { name: "Switch application", exact: true })).toContainText("Releases");
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("tenant-operations.png"), fullPage: true });

  const operatorContext = await browser.newContext({ baseURL: "http://127.0.0.1:18496", locale: "en-US" });
  try {
    const operator = await operatorContext.newPage();
    await open(operator, "desk", "/home");
    await expect(operator.getByRole("button", { name: "Projects", exact: true })).toHaveCount(0);
    await operator.getByRole("button", { name: "WMS entry probe", exact: true }).click();
    await expect(operator.getByRole("heading", { name: "Receiving workspace", exact: true })).toBeVisible();
    const recordID = fresh("RECEIPT");
    await decide(request, "desk", "build", `build.${objectName}.create`, { type: `build.${objectName}`, id: recordID }, { number: "IN-M1" });
    await expect(operator.getByRole("row").filter({ hasText: "IN-M1" })).toBeVisible();
    await operator.reload();
    await expect(operator.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Receiving workspace", exact: true })).toBeVisible();
    await expect(decide(request, "desk", "build", "build.object.edit", { type: "build.object", id: objectID }, { title: "Unauthorized" })).rejects.toThrow(/POLICY_DENIED/);
    if (process.env.PLATFORM_SCREENSHOTS) {
      await operator.screenshot({ path: testInfo.outputPath("business-receiving.png"), fullPage: true });
      await operator.setViewportSize({ width: 390, height: 844 });
      await operator.screenshot({ path: testInfo.outputPath("business-receiving-narrow.png"), fullPage: true });
    }
  } finally { await operatorContext.close(); }
});
