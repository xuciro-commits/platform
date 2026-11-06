import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F21 a builder reads a branch-scoped lineage graph while main stays unchanged", async ({ page }) => {
  test.setTimeout(180_000);
  const client = acceptedClient(page, "builder");
  const project = unique("f21p");
  const object = unique("f21object");
  const objectName = `${object}record`;
  const branchName = unique("f21branch");
  const branchTitle = `F21 ${branchName}`;
  const objectSpec = (title: string) => ({
    name: objectName, title, plural: "F21 objects", display: "note",
    fields: [{ name: "note", title: "Note", type: "text" }],
    states: [{ name: "open", title: "Open" }],
  });

  await client.submit("build.app", project, "create", { name: project, title: "F21 project" }, 0);
  await client.submit("build.object", object, "create", { ...objectSpec("F21 main object"), project }, 0);
  const created = await page.request.post("/v1/branches", {
    headers: client.headers,
    data: { name: branchName, title: branchTitle },
  });
  expect(created.ok(), await created.text()).toBeTruthy();
  const branch = await created.json() as { id: string };
  const saved = await page.request.put(`/v1/branches/${branch.id}/resources/build.object/${object}`, {
    headers: client.headers,
    data: { op: "update", project, spec: objectSpec("F21 branch object") },
  });
  expect(saved.ok(), await saved.text()).toBeTruthy();

  await signIn(page, "builder", `/lineage?branch=${encodeURIComponent(branch.id)}&focus=${encodeURIComponent(`object:build.${objectName}`)}`);
  await expect(page.getByText("F21 branch object", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Branch", exact: true })).toHaveValue(branch.id);
  await expect(page.getByText(/branch .* saved definitions/)).toBeVisible();
  await expect(page.getByText("F21 main object", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Save view", exact: true }).click();
  const save = page.getByRole("dialog", { name: "Save view", exact: true });
  await save.getByRole("textbox", { name: "Name", exact: true }).fill("F21 branch view");
  const saving = page.waitForResponse(response => response.url().endsWith("/v1/submissions") && response.request().method() === "POST"
    && response.request().postDataJSON()?.schema?.name === "work.view.save");
  await save.getByRole("button", { name: "Save", exact: true }).click();
  await expect(save).toBeHidden();
  const savedView = await saving;
  expect(savedView.ok(), await savedView.text()).toBeTruthy();
  const savedViewBody = await savedView.json();
  const savedViewID = savedViewBody.record.submission.target.id as string;
  const savedRecord = await page.request.get(`/v1/records/work.view/${encodeURIComponent(savedViewID)}`, { headers: client.headers });
  expect(savedRecord.ok(), await savedRecord.text()).toBeTruthy();
  expect((await savedRecord.json()).record.owner).toBe("builder-1");
  const views = await page.request.get("/v1/views", { headers: client.headers });
  expect(views.ok(), await views.text()).toBeTruthy();
  const viewBody = await views.json();
  expect(viewBody.some((view: { entity?: string; title?: string }) => view.entity === "lineage" && view.title === "F21 branch view"), JSON.stringify(viewBody)).toBeTruthy();
  await page.getByRole("button", { name: "Toggle structure panel" }).click();
  await expect(page.getByRole("combobox", { name: "Saved views", exact: true })).toBeVisible();
});
