import { acceptedClient, expect, listDetailPage, signIn, test, unique, subjectUpdate } from "./kit";

test.use({ actionTimeout: 15_000 });

test("F11 Files reaches viewer and discoverer scopes without the builder package, and RID addresses survive a move", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const { read, submit } = acceptedClient(page);
  const name = unique("c6");
  const project = unique("c6project"), second = unique("c6project");
  const projectTitle = `Files ${name}`, secondTitle = `Files moved ${name}`;
  const object = unique("c6object");
  const objectName = `${name}record`, objectTitle = `Resource ${name}`;
  const viewer = unique("c6viewer"), discoverer = unique("c6discoverer");
  const viewerSubject = `user:${viewer}@example.test`, discovererSubject = `user:${discoverer}@example.test`;
  const editor = unique("c3editor");
  const editorSubject = `user:${editor}@example.test`;
  const resourceProject = (directory: { resources: { id: string; draft?: string; project: string; ref: { kind: string } }[] }, draft: string) =>
    directory.resources.find(resource => resource.draft === draft);

  await test.step("seed two projects, two project members and one movable object", async () => {
    const admin = (await read<{ principalId: string }>("/v1/me")).principalId;
    await submit("platform.member", viewer, "invite", { subject: viewerSubject }, null);
    await submit("platform.member", discoverer, "invite", { subject: discovererSubject }, null);
    await submit("platform.member", editor, "invite", { subject: editorSubject }, null);
    await submit("build.app", project, "create", { name: project, title: projectTitle }, 0);
    await submit("build.app", second, "create", { name: second, title: secondTitle }, 0);
    await submit("platform.project", project, "save", { title: projectTitle, approvals: 1, members: [
      { member: admin, role: "owner" },
      { member: viewer, role: "viewer" },
      { member: discoverer, role: "discoverer" },
      { member: editor, role: "editor" },
    ] }, null);
    await submit("build.object", object, "save", { project, spec: {
      name: objectName, title: objectTitle, plural: "Resources", display: "subject",
      fields: [{ name: "subject", title: "Subject", type: "text", required: true }],
      states: [{ name: "open", title: "Open" }],
    } }, 0);
    await expect.poll(async () => resourceProject(await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string } }[] }>("/v1/resources"), object)?.id, { timeout: 20_000 }).toEqual(expect.stringMatching(/^ri\./));
  });

  const directory = await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string } }[] }>("/v1/resources");
  const projectRID = resourceProject(directory, project)!.id;
  const objectRID = resourceProject(directory, object)!.id;
  expect(projectRID).toMatch(/^ri\./);
  expect(objectRID).toMatch(/^ri\./);
  expect(resourceProject(directory, object)!.project).toBe(project);

  await test.step("a project editor creates an object, action and page that stay in the project", async () => {
    const editorPage = await browser.newPage();
    try {
      await signIn(editorPage, editorSubject, `/files/${projectRID}`);
      await expect(editorPage.getByRole("heading", { name: projectTitle, level: 1 })).toBeVisible({ timeout: 20_000 });
      const editorClient = acceptedClient(editorPage, editorSubject);
      const created = unique("c3object");
      const objectName = `${created}record`, objectType = `build.${objectName}`;
      const actionName = `${objectType}.close`, pageName = `${created}page`;
      await editorClient.submit("build.object", created, "create", {
        project,
        name: objectName, title: `Editor resource ${created}`, plural: "Editor resources", display: "subject",
        fields: [{ name: "subject", title: "Subject", type: "text", required: true }],
        states: [{ name: "open", title: "Open" }],
      }, 0);
      await editorClient.submit("build.actiontype", actionName, "create", {
        project, name: actionName, object: objectType,
        definition: { name: "close", title: "Close", from: ["open"], transaction: subjectUpdate(objectType) },
      }, 0);
      await editorClient.submit("build.page", pageName, "create", {
        project, name: pageName, title: `Editor page ${created}`, ...listDetailPage(objectType, ["subject"], [actionName]),
      }, 0);
      const before = await editorClient.read<{ record: { revision: number; resources?: { app: string; kind: string; name: string }[]; pages?: string[] } }>(`/v1/records/build.app/${project}`);
      await editorClient.submit("build.app", project, "edit", {
        resources: [
          ...(before.record.resources ?? []),
          { app: "build", kind: "object", name: objectType },
          { app: "build", kind: "action", name: actionName },
        ],
        pages: [...new Set([...(before.record.pages ?? []), pageName])],
      }, before.record.revision);
      const editorDirectory = await editorClient.read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string } }[] }>("/v1/resources");
      for (const draft of [created, actionName, pageName]) expect(resourceProject(editorDirectory, draft)?.project).toBe(project);
      const projectRecord = (await editorClient.read<{ record: { resources?: { app: string; kind: string; name: string }[]; pages?: string[] } }>(`/v1/records/build.app/${project}`)).record;
      expect(projectRecord.resources).toEqual(expect.arrayContaining([
        expect.objectContaining({ app: "build", kind: "object", name: objectType }),
        expect.objectContaining({ app: "build", kind: "action", name: actionName }),
      ]));
      expect(projectRecord.pages).toContain(pageName);
    } finally {
      await editorPage.context().close();
    }
  });

  await test.step("a viewer opens Files, the project and the same address as a RID deep link", async () => {
    const viewerPage = await browser.newPage();
    try {
      await signIn(viewerPage, viewerSubject);
      await viewerPage.getByRole("button", { name: "Files", exact: true }).first().click();
      await expect(viewerPage.getByRole("cell", { name: projectTitle, exact: true })).toBeVisible({ timeout: 20_000 });
      await viewerPage.getByRole("cell", { name: projectTitle, exact: true }).click();
      await expect(viewerPage).toHaveURL(new RegExp(`/#/files/${projectRID.replaceAll(".", "\\.")}`));
      await expect(viewerPage.getByRole("heading", { name: projectTitle, level: 1 })).toBeVisible();
      await expect(viewerPage.getByRole("cell", { name: objectTitle, exact: true })).toBeVisible();

      await viewerPage.goto(`/#/files/${projectRID}`);
      await expect(viewerPage.getByRole("heading", { name: projectTitle, level: 1 })).toBeVisible();
      expect(new URL(viewerPage.url()).hash).toBe(`#/files/${projectRID}`);
      await expect(viewerPage.getByRole("cell", { name: objectTitle, exact: true })).toBeVisible();
    } finally {
      await viewerPage.context().close();
    }
  });

  await test.step("a discoverer sees the project name and type without opening its resources", async () => {
    const discovererPage = await browser.newPage();
    try {
      await signIn(discovererPage, discovererSubject);
      await discovererPage.getByRole("button", { name: "Files", exact: true }).first().click();
      await expect(discovererPage.getByRole("cell", { name: projectTitle, exact: true })).toBeVisible({ timeout: 20_000 });
      await discovererPage.getByRole("cell", { name: projectTitle, exact: true }).click();
      await expect(discovererPage.getByRole("heading", { name: projectTitle, level: 1 })).toBeVisible();
      await expect(discovererPage.getByRole("cell", { name: objectTitle, exact: true })).toBeVisible();
      await discovererPage.getByRole("cell", { name: objectTitle, exact: true }).click();
      await expect(discovererPage.getByText("Request project access to open this resource.", { exact: true })).toBeVisible();
    } finally {
      await discovererPage.context().close();
    }
  });

  await test.step("the resource keeps its RID address after moving to another project", async () => {
    const builderPage = await browser.newPage();
    try {
      await signIn(builderPage, "builder", `/object-type/${objectRID}`);
      await expect(builderPage.getByRole("heading", { name: objectTitle, level: 1 })).toBeVisible({ timeout: 20_000 });
      expect(new URL(builderPage.url()).hash).toBe(`#/object-type/${objectRID}`);
      await submit("build.resource", objectRID, "move", { project: second, folder: "" });
      await expect.poll(async () => resourceProject(await read<{ resources: { id: string; draft?: string; project: string; ref: { kind: string } }[] }>("/v1/resources"), object)?.project, { timeout: 20_000 }).toBe(second);
      await builderPage.reload();
      await expect(builderPage.getByRole("heading", { name: objectTitle, level: 1 })).toBeVisible({ timeout: 20_000 });
      expect(new URL(builderPage.url()).hash).toBe(`#/object-type/${objectRID}`);
    } finally {
      await builderPage.context().close();
    }
  });

  await test.step("object addresses are canonical and the old record link redirects once", async () => {
    const routePage = await browser.newPage();
    try {
      await signIn(routePage, "admin", "/record?type=platform.member&id=admin-1");
      await expect.poll(() => new URL(routePage.url()).hash).toBe("#/objects/platform.member/admin-1");
      const canonical = routePage.url();
      await routePage.goto("/#/objects/platform.member/admin-1");
      await expect(routePage).toHaveURL(canonical);
    } finally {
      await routePage.context().close();
    }
  });

  await test.step("an auditor opens read-only member details and legacy marking links resolve to their RID", async () => {
    const auditor = unique("f11auditor"), subject = `user:${auditor}@example.test`;
    await submit("platform.member", auditor, "invite", { subject }, null);
    await submit("platform.member", auditor, "grant", { app: "platform", role: "auditor" }, null);
    const auditPage = await browser.newPage();
    try {
      await signIn(auditPage, subject, "/members");
      await auditPage.getByRole("row").filter({ has: auditPage.getByRole("cell", { name: "admin-1", exact: true }) }).click();
      await expect(auditPage.getByRole("heading", { name: "Roles", exact: true })).toBeVisible();
      await expect(auditPage.getByRole("button", { name: "Add role", exact: true })).toHaveCount(0);
      expect(new URL(auditPage.url()).hash).toBe("#/members");
      await auditPage.getByRole("button", { name: "Open as page", exact: true }).click();
      await expect(auditPage).toHaveURL(/#\/member\?id=admin-1$/);
      const marking = (await acceptedClient(auditPage, subject).read<{ id: string; rid: string }[]>("/v1/markings")).find(item => item.id === "confidential")!;
      await auditPage.goto("/#/marking?id=confidential");
      await expect.poll(() => new URL(auditPage.url()).hash).toBe(`#/marking/${marking.rid}`);
      await expect(auditPage.getByRole("heading", { name: "Confidential", exact: true, level: 1 })).toBeVisible();
      await expect(auditPage.getByRole("button", { name: "Save", exact: true })).toHaveCount(0);
      await expect(auditPage.getByRole("textbox", { name: "Marking title", exact: true })).toBeDisabled();
    } finally {
      await auditPage.context().close();
      await submit("platform.member", auditor, "offboard", { reason: "F11 acceptance cleanup", successor: "" }, null);
    }
  });
});
