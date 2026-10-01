import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("editors keep drafts across navigation and require a choice before discarding", async ({ page, request }, testInfo) => {
  const name = fresh("guard").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const objectID = fresh("OBJ"), pageID = fresh("PAGE"), flowID = fresh("FLOW");
  const headers = { Authorization: "Bearer manager" };
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, {
    name, title: "Draft guard object", fields: [{ name: "note", title: "Note", type: "text" }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id: pageID }, {
    name: `${name}page`, title: "Saved page", object: `build.${name}`, sections: [{ widget: "table", fields: ["note"] }],
  });
  await decide(request, "manager", "build", "build.process.create", { type: "build.process", id: flowID }, {
    name: `${name}flow`, title: "Saved workflow", manual: true, input: {},
    inputSchema: { type: "object", properties: {} }, steps: [{ name: "done", kind: "result", value: { source: "input" } }],
  });

  const confirmation = page.getByRole("dialog", { name: "Unsaved changes", exact: true });
  const closeTab = (title: string) => page.getByRole("tab", { name: title, exact: true }).getByRole("button").click();
  await open(page, "manager", `/compose?id=${pageID}`);
  await page.getByRole("button", { name: "Page settings", exact: true }).click();
  const title = page.getByRole("textbox", { name: "What people call it", exact: true });
  await title.fill("Cancelled page title");
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  await page.getByRole("button", { name: "Page settings", exact: true }).click();
  await expect(title).toHaveValue("Saved page");
  await title.fill("Local page draft");
  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Application launcher", exact: true }).click();
  await expect(confirmation).toHaveCount(0); // navigation leaves the editor mounted
  await page.getByRole("tab", { name: "Compose a page", exact: true }).click();
  await expect(title).toHaveValue("Local page draft");

  let warned = false;
  page.once("dialog", async (dialog) => { warned = dialog.type() === "beforeunload"; await dialog.dismiss(); });
  await page.reload({ timeout: 5000 }).catch(() => undefined);
  expect(warned).toBe(true);
  await expect(title).toHaveValue("Local page draft");
  await closeTab("Compose a page");
  await expect(confirmation).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("unsaved-changes.png") });
  await confirmation.getByRole("button", { name: "Keep editing", exact: true }).click();
  await expect(title).toHaveValue("Local page draft");
  await title.fill("Saved page revision");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await closeTab("Compose a page");
  await expect(confirmation).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "Compose a page", exact: true })).toHaveCount(0);

  await page.goto(`/#/compose?id=${pageID}`);
  await page.getByRole("button", { name: "Page settings", exact: true }).click();
  await expect(title).toHaveValue("Saved page revision");
  await title.fill("Discard this page draft");
  await closeTab("Compose a page");
  await confirmation.getByRole("button", { name: "Discard changes", exact: true }).click();
  const saved = await (await request.get(`/v1/records/build.page/${pageID}`, { headers })).json();
  expect(saved.record.title).toBe("Saved page revision");

  await page.goto(`/#/process?id=${objectID}`);
  await page.getByRole("button", { name: /^Note ·/ }).click();
  await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Local field name");
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  await page.getByRole("button", { name: /^Note ·/ }).click();
  await expect(page.getByRole("textbox", { name: "What people call it", exact: true })).toHaveValue("Note");
  await page.getByRole("button", { name: "Remove field", exact: true }).click();
  await expect(page.getByRole("button", { name: /^Note ·/ })).toHaveCount(0);
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  await page.getByRole("button", { name: /^Note ·/ }).click();
  await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Local field name");
  await closeTab("Object design");
  await confirmation.getByRole("button", { name: "Keep editing", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "What people call it", exact: true })).toHaveValue("Local field name");
  await closeTab("Object design");
  await confirmation.getByRole("button", { name: "Discard changes", exact: true }).click();

  await page.goto(`/#/workflow?id=${flowID}`);
  const workflowTitle = page.getByRole("textbox", { name: "Workflow title", exact: true });
  await workflowTitle.fill("Cancelled workflow title");
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  await expect(workflowTitle).toHaveValue("Saved workflow");
  await workflowTitle.fill("Local workflow draft");
  await page.getByRole("button", { name: "Reload saved workflow", exact: true }).click();
  await confirmation.getByRole("button", { name: "Keep editing", exact: true }).click();
  await expect(workflowTitle).toHaveValue("Local workflow draft");
  await page.getByRole("button", { name: "Reload saved workflow", exact: true }).click();
  await confirmation.getByRole("button", { name: "Discard changes", exact: true }).click();
  await expect(workflowTitle).toHaveValue("Saved workflow");
  await workflowTitle.fill("Discard this workflow draft");
  await closeTab("Workflows");
  await confirmation.getByRole("button", { name: "Discard changes", exact: true }).click();

  await page.goto("/#/code?id=new");
  await page.getByRole("textbox", { name: "Function title", exact: true }).fill("Local code draft");
  await closeTab("Code functions");
  await confirmation.getByRole("button", { name: "Keep editing", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Function title", exact: true })).toHaveValue("Local code draft");
  await page.getByRole("textbox", { name: "Function name", exact: true }).fill(`${name}code`);
  await page.getByRole("textbox", { name: "Function description", exact: true }).fill("A draft guard probe; no compilation or execution.");
  await page.getByRole("button", { name: "Save function", exact: true }).click();
  // Saving also disables the button; the persisted editor must be loaded first.
  await expect(page.getByRole("button", { name: "Compile function", exact: true })).toBeEnabled();
  await expect(page.getByRole("button", { name: "Save function", exact: true })).toBeDisabled();
  await expect(confirmation).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "Code functions", exact: true })).toHaveCount(1);
  await page.getByRole("textbox", { name: "Function title", exact: true }).fill("Cancelled code title");
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Function title", exact: true })).toHaveValue("Local code draft");
  const codes = await (await request.get("/v1/records/build.code?limit=500", { headers })).json();
  const code = codes.records.find((record: { name: string }) => record.name === `${name}code`);
  await page.getByRole("button", { name: "Archive", exact: true }).click();
  const archival = page.getByRole("dialog").filter({ hasText: "Archive this saved record?" });
  await archival.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Function title", exact: true })).toHaveValue("Local code draft");
  await page.getByRole("button", { name: "Archive", exact: true }).click();
  await archival.getByRole("button", { name: /Archive/ }).click();
  await expect(archival).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "Code functions", exact: true })).toHaveCount(0);
  const archived = await (await request.get(`/v1/records/build.code/${code.id}`, { headers })).json();
  expect(archived.record.archived).toBe(true);
  await expect(confirmation).toHaveCount(0);
  await expect(page.getByRole("tab", { name: "Code functions", exact: true })).toHaveCount(0);
});
