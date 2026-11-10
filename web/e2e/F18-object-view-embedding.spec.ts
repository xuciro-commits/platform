import { acceptedClient, expect, listDetailPage, signIn, test, unique, subjectUpdate } from "./kit";

test("F18 an embedded object view keeps application navigation and loads its sections on demand", async ({ page }) => {
  test.setTimeout(120_000);
  const client = acceptedClient(page, "builder");
  const project = unique("debugproject");
  const object = unique("debugobject");
  const objectType = `build.${object}`;
  const action = `${objectType}.close`;
  const pageName = unique("debugpage");
  const composedPage = unique("composedpage");
  const composedTitle = "Composed debug objects";
  const record = unique("debugrecord");
  const appTitle = "Debug objects app";
  await client.submit("build.app", project, "create", { name: project, title: appTitle }, 0);
  await client.submit("build.object", object, "create", {
    project, name: object, title: "Debug object", plural: "Debug objects", display: "subject",
    fields: [{ name: "subject", title: "Subject", type: "text", required: true }],
    states: [{ name: "open", title: "Open" }],
  }, 0);
  await client.submit("build.actiontype", action, "create", {
    project, name: action, object: objectType,
    definition: { name: "close", title: "Close", from: ["open"], transaction: subjectUpdate(objectType) },
  }, 0);
  await client.submit("build.page", pageName, "create", {
    project, name: pageName, title: "Debug objects", ...listDetailPage(objectType, ["subject"], [action]),
  }, 0);
  const objectRef = { app: "build", kind: "object", name: objectType };
  await client.submit("build.page", composedPage, "create", {
    project, name: composedPage, title: composedTitle, format: 4, object: objectType,
    dataBindings: [
      { id: "records", owner: "binding:records", kind: "record", object: objectRef, model: { type: objectType, fields: [{ name: "subject" }] }, fields: [{ name: "subject" }],
        ports: { parentable: "true", selection: "current", selectionMode: "write", widget: "table" } },
      { id: "record", owner: "binding:record", kind: "record", object: objectRef, model: { type: objectType, fields: [{ name: "subject" }] }, fields: [{ name: "subject" }],
        ports: { selection: "current", selectionMode: "read", widget: "record-view" } },
    ],
    actionTriggers: [{ id: "page", binding: "page", actions: [] }, { id: "records", owner: "binding:records", binding: "records" }, { id: "record", owner: "binding:record", binding: "record" }],
    inputSchema: { selections: [{ name: "current", object: objectRef }] },
    scopeVisibility: { root: "page", owners: { records: "binding:records", record: "binding:record" },
      scopes: [{ id: "page", kind: "page" }, { id: "binding:records", parent: "page", kind: "page", binding: "records" }, { id: "binding:record", parent: "page", kind: "page", binding: "record" }] },
    widgetSpec: { format: "platform.page.web.v1", body: { document: null, sections: [
      { binding: "records", configVersion: 1, id: "records", title: "Debug records", triggers: ["records"] },
      { binding: "record", configVersion: 1, id: "record", title: "Debug record", triggers: ["record"] },
    ] } },
  }, 0);
  const app = await client.read<{ record: { revision: number; resources?: unknown[]; pages?: string[] } }>(`/v1/records/build.app/${project}`);
  await client.submit("build.app", project, "edit", {
    resources: [...(app.record.resources ?? []), { app: "build", kind: "object", name: objectType }, { app: "build", kind: "action", name: action }],
    pages: [...new Set([...(app.record.pages ?? []), pageName, composedPage])],
  }, app.record.revision);
  await client.publish([{ kind: "object", id: object }, { kind: "action", id: action }, { kind: "page", id: pageName }, { kind: "page", id: composedPage }, { kind: "app", id: project }]);
  await client.submit(objectType, record, "create", { subject: "Debug record" }, 0);

  const requests: string[] = [];
  page.on("request", request => {
    const url = new URL(request.url());
    if (url.pathname.includes("/sections/")) requests.push(url.pathname);
  });
  await signIn(page, "builder", "/portal");
  await page.getByRole("button", { name: appTitle, exact: true }).click();
  await expect(page.getByRole("table")).toBeVisible();
  await page.getByRole("table").getByRole("row").filter({ hasText: record }).click();
  await expect(page.getByText("Back to list", { exact: true })).toBeVisible();
  await expect(page.getByRole("navigation", { name: appTitle, exact: true })).toBeVisible();
  await page.getByRole("tablist", { name: "Debug object", exact: true }).getByRole("tab", { name: "Activity", exact: true }).click();
  await expect.poll(() => requests).toContain(`/v1/records/${encodeURIComponent(objectType)}/${encodeURIComponent(record)}/sections/activity`);
  await expect(page.getByRole("tabpanel", { name: "Activity", exact: true })).toBeVisible();
  await page.getByRole("navigation", { name: appTitle, exact: true }).getByRole("button", { name: composedTitle, exact: true }).click();
  await expect(page.getByRole("table")).toBeVisible();
  await page.getByRole("table").getByRole("row").filter({ hasText: record }).click();
  const composedTabs = page.getByRole("tablist", { name: "Debug object", exact: true });
  await expect(composedTabs).toBeVisible();
  await composedTabs.getByRole("tab", { name: "Activity", exact: true }).click();
  await expect.poll(() => requests.filter(path => path.endsWith("/sections/activity")).length).toBeGreaterThanOrEqual(2);
});
