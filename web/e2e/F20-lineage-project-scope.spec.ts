import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F20 a project-delegated builder sees only their project, anonymous cross-project boundaries, and a guard that does not fall on save", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const { read, submit } = acceptedClient(page);
  const admin = (await read<{ principalId: string }>("/v1/me")).principalId;
  const project = unique("f20p"), otherProject = unique("f20q");
  const projectTitle = `Lineage ${project}`, otherTitle = `Lineage ${otherProject}`;
  const delegated = unique("f20builder"), viewer = unique("f20viewer");
  const delegatedSubject = `user:${delegated}@example.test`, viewerSubject = `user:${viewer}@example.test`;
  const dataset = unique("f20ds"), otherDataset = unique("f20remote"), connection = unique("f20conn");
  const object = unique("f20object"), objectName = `${object}record`;
  const resource = (directory: { resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }, draft: string) =>
    directory.resources.find(item => item.draft === draft);

  await test.step("seed two projects, a delegated builder, a cross-project source and a guarded object", async () => {
    await submit("platform.member", delegated, "invite", { subject: delegatedSubject }, null);
    await submit("platform.member", viewer, "invite", { subject: viewerSubject }, null);
    await submit("build.app", project, "create", { name: project, title: projectTitle }, 0);
    await submit("build.app", otherProject, "create", { name: otherProject, title: otherTitle }, 0);
    await submit("platform.project", project, "save", { title: projectTitle, approvals: 1, members: [
      { member: admin, role: "owner" },
      { member: delegated, role: "editor" },
      { member: viewer, role: "viewer" },
    ] }, null);
    await submit("platform.project", otherProject, "save", { title: otherTitle, approvals: 1, members: [{ member: admin, role: "owner" }] }, null);
    await submit("build.dataset", otherDataset, "create", { name: `${otherDataset}data`, title: "Other project dataset", key: "id", project: otherProject }, 0);
    await submit("build.connection", connection, "create", { name: `${connection}link`, title: "Project connection", kind: "http", address: "https://example.test", project }, 0);
    await submit("build.source", unique("f20source"), "create", { name: `${connection}feed`, title: "Project feed", connection, dataset: otherDataset, project }, 0);

    const secret = unique("f20secret");
    await submit("build.dataset", dataset, "create", { name: `${dataset}data`, title: "Guarded dataset", marking: "restricted", key: "id", project }, 0);
    await submit("build.dataset", dataset, "load", { rows: [{ id: "A1", note: secret }], marking: "restricted" }, null);
    await submit("build.object", object, "create", {
      project,
      name: objectName, title: "Guarded object", plural: "Guarded objects", display: "note",
      fields: [{ name: "note", title: "Note", type: "text" }],
      datasources: [{ kind: "dataset", source: dataset, key: "id", columns: [{ column: "note", attribute: "note" }] }],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    const created = await read<{ record: { revision: number } }>(`/v1/records/build.object/${object}`);
    await submit("build.object", object, "publish", {}, created.record.revision);
    const published = await read<{ record: { revision: number } }>(`/v1/records/build.object/${object}`);
    await submit("build.object", object, "save", { project, spec: {
      name: objectName, title: "Guarded object", plural: "Guarded objects", display: "note",
      fields: [{ name: "note", title: "Note", type: "text" }],
      datasources: [],
      states: [{ name: "open", title: "Open" }],
    } }, published.record.revision);
    await expect.poll(async () => resource(await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }>("/v1/resources"), object)?.id, { timeout: 20_000 }).toEqual(expect.stringMatching(/^ri\./));
  });

  const directory = await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string; name: string } }[] }>("/v1/resources");
  const projectRID = resource(directory, project)!.id;
  const otherRID = resource(directory, otherProject)!.id;
  const objectRID = resource(directory, object)!.id;
  const sourceRID = directory.resources.find(item => item.ref.kind === "source" && item.project === project)?.id;
  expect(projectRID).toMatch(/^ri\./);
  expect(otherRID).toMatch(/^ri\./);
  expect(objectRID).toMatch(/^ri\./);
  expect(sourceRID).toMatch(/^ri\./);

  const delegatedPage = await browser.newPage();
  try {
    await signIn(delegatedPage, delegatedSubject, "/lineage");
    await expect(delegatedPage.getByRole("heading", { name: "Data Lineage", level: 1 })).toBeVisible();
    await expect(delegatedPage.getByRole("cell", { name: projectRID, exact: true })).toBeVisible();
    await expect(delegatedPage.getByRole("cell", { name: otherRID, exact: true })).toHaveCount(0);

    await delegatedPage.getByRole("cell", { name: projectRID, exact: true }).click();
    await expect(delegatedPage).toHaveURL(new RegExp(`project=${encodeURIComponent(projectRID).replaceAll(".", "\\.")}`));
    await expect(delegatedPage.getByText(/downstream boundary/)).toBeVisible();
    await delegatedPage.getByText("Project feed", { exact: true }).click();
    await delegatedPage.getByRole("button", { name: "Toggle inspector panel" }).click();
    await expect(delegatedPage.getByText("Boundaries", { exact: true })).toBeVisible();
    await expect(delegatedPage.getByText(/other-project/)).toBeVisible();

    await delegatedPage.goto(`/#/lineage?mode=focus&focus=${encodeURIComponent(objectRID)}`);
    await expect(delegatedPage.getByText("Protection differs from preview. The saved definition is not active yet.")).toBeVisible();
    await delegatedPage.getByRole("button", { name: "Toggle inspector panel" }).click();
    await expect(delegatedPage.getByText("Marking state", { exact: true })).toBeVisible();
    await expect(delegatedPage.getByText("preview-only", { exact: true })).toBeVisible();
  } finally {
    await delegatedPage.context().close();
  }

  await test.step("an uncleared viewer cannot read the guarded object", async () => {
    const record = unique("f20record");
    const cleared = acceptedClient(page, "cleared");
    await cleared.submit(`build.${objectName}`, record, "create", { note: "secret" }, 0);
    const refusal = await page.request.get(`/v1/records/build.${objectName}/${record}`, {
      headers: { authorization: `Bearer ${viewerSubject}` },
    });
    expect(refusal.status()).toBeGreaterThanOrEqual(400);
  });
});
