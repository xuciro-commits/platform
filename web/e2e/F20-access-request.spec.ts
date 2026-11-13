import { acceptedClient, expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 15_000 });

test("F20 a discoverer requests project access, an owner approves it, and the same session no longer asks for access", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const admin = acceptedClient(page);
  const name = unique("access");
  const project = unique("accessproject"), object = unique("accessobject"), objectType = unique("accessrecord");
  const projectTitle = `Access ${name}`, objectTitle = `Access resource ${name}`;
  const owner = unique("accessowner"), requester = unique("accessrequester");
  const ownerSubject = `user:${owner}@example.test`, requesterSubject = `user:${requester}@example.test`;

  await admin.submit("platform.member", owner, "invite", { subject: ownerSubject }, null);
  await admin.submit("platform.member", requester, "invite", { subject: requesterSubject }, null);
  await admin.submit("build.app", project, "create", { name: project, title: projectTitle }, 0);
  await admin.submit("platform.project", project, "save", {
    title: projectTitle, approvals: 1,
    members: [{ member: owner, role: "owner" }, { member: requester, role: "discoverer" }],
  }, null);
  await admin.submit("build.object", object, "save", {
    project,
    spec: {
      name: objectType, title: objectTitle, plural: "Access resources", display: "subject",
      fields: [{ name: "subject", title: "Subject", type: "text", required: true }],
      states: [{ name: "open", title: "Open" }],
    },
  }, 0);
  const before = await admin.read<{ record: { revision: number } }>(`/v1/records/build.app/${project}`);
  await admin.submit("build.app", project, "edit", { resources: [{ app: "build", kind: "object", name: `build.${objectType}` }] }, before.record.revision);
  await admin.publish([{ kind: "app", id: project }, { kind: "object", id: object }]);
  const definitions = await admin.read<{ ref: { kind: string; name: string }; application?: { name: string } }[]>("/v1/definitions");
  expect(definitions.some(item => item.ref.kind === "app" && item.application?.name === project)).toBeTruthy();
  const resource = async () => (await admin.read<{ resources: { id: string; draft?: string; project: string }[] }>("/v1/resources"))
    .resources.find(item => item.draft === project)!;
  await expect.poll(async () => (await resource()).id, { timeout: 20_000 }).toMatch(/^ri\./);
  const projectRID = (await resource()).id;

  await signIn(page, requesterSubject, `/files/${projectRID}`);
  const requesterClient = acceptedClient(page, requesterSubject);
  await expect(page.getByRole("cell", { name: objectTitle, exact: true })).toBeVisible({ timeout: 20_000 });
  await page.getByRole("cell", { name: objectTitle, exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("heading", { name: "Request project access", exact: true })).toBeVisible();
  await page.getByLabel("Reason").fill("Open the resource to inspect the line.");
  await page.getByRole("button", { name: "Send request", exact: true }).click();

  await expect.poll(async () => (await requesterClient.read<{ state: string; role: string }[]>("/v1/access-requests"))
    .some(item => item.state === "pending" && item.role === "viewer"), { timeout: 20_000 }).toBeTruthy();

  const ownerPage = await browser.newPage();
  try {
    await signIn(ownerPage, ownerSubject, "/inbox");
    const task = ownerPage.getByRole("listitem").filter({ hasText: `Access request: project ${project} as viewer` });
    await expect(task).toBeVisible({ timeout: 20_000 });
    await task.getByRole("button", { name: "Approve", exact: true }).click();
    await expect(task).toBeHidden();
  } finally {
    await ownerPage.context().close();
  }

  await expect.poll(async () => (await requesterClient.read<{ apps: { id: string; role: string }[] }>("/v1/me"))
    .apps.some(app => app.id === "build" && app.role === "viewer"), { timeout: 20_000 }).toBeTruthy();

  await page.reload();
  await expect(page.getByRole("cell", { name: objectTitle, exact: true })).toBeVisible({ timeout: 20_000 });
  await expect.poll(async () => (await requesterClient.read<{ resources: { draft?: string; canOpen: boolean }[] }>("/v1/resources"))
    .resources.find(item => item.draft === object)?.canOpen, { timeout: 20_000 }).toBeTruthy();
  await page.getByRole("cell", { name: objectTitle, exact: true }).click();
  await expect(page.getByRole("dialog")).toHaveCount(0);
});
