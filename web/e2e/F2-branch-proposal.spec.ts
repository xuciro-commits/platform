import { expect, signIn, test, unique, type Page } from "./kit";
import type { ReleasePreview } from "../packages/api/src/gen/host";

test.use({ actionTimeout: 15_000 });

test("F2 a builder modifies a published object type on a branch and proposes it for review and merge", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const name = unique("tickets");
  const objectName = `${name}ticket`;
  const objectTitle = "Ticket";
  const newField = unique("severity");
  const newFieldTitle = "Severity";
  const branchTitle = `Add ${newFieldTitle} ${name.slice(7)}`;
  const branchName = unique("severitybranch");
  const project = unique("ticketproject");
  const headers = { authorization: "Bearer builder" };
  let objectID = "";
  const relatedID = unique("RELATED"), relatedName = `${objectName}details`, relatedTitle = "Ticket details", relatedField = `${newField}details`;

  const read = async <T,>(path: string): Promise<T> => {
    const response = await page.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };

  const submit = async (target: { type: string; id: string }, action: string, payload: unknown, expectedRevision: number) => {
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const response = await page.request.post("/v1/submissions", {
      headers,
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority: "build", target,
        schema: { name: `${target.type}.${action}`, version: 1 }, idempotencyKey: unique("submit"),
        payload: Buffer.from(JSON.stringify(payload)).toString("base64"), expectedRevision,
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
    expect(answer.error).toBeUndefined();
    expect(answer.record?.changeId).toEqual(expect.any(String));
    expect(answer.record.changeId).not.toBe("");
    return answer.record as { changeId: string };
  };

  const publish = async (kind: string, id: string) => {
    const drafts = [{ kind, id }];
    const previewResponse = await page.request.post("/v1/releases/preview", { headers, data: { drafts } });
    expect(previewResponse.ok(), await previewResponse.text()).toBeTruthy();
    const preview = await previewResponse.json() as ReleasePreview;
    expect(preview.diagnostic).toBeUndefined();
    expect(preview.candidateId).toEqual(expect.any(String));
    for (const draft of preview.draftReviews ?? []) {
      expect(draft.checked).toBe(true);
      expect(draft.issues).toEqual([]);
    }
    for (const path of ["/v1/releases/candidates", "/v1/releases/active"]) {
      const response = await page.request.post(path, { headers, data: { ...(path === "/v1/releases/candidates" ? { drafts } : {}), candidateId: preview.candidateId, key: unique("publish") } });
      const answer = await response.json();
      expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
      expect(answer.id).toBe(preview.candidateId);
    }
  };

  const objectFields = async () => (await read<{ record: { fields: { name: string }[] } }>(`/v1/records/build.object/${objectID}`)).record.fields.map(field => field.name);

  await test.step("setup: a builder defines and publishes a baseline object type on main", async () => {
    await signIn(page, "builder", "/projects");
    await submit({ type: "build.app", id: project }, "create", { name: project, title: "Ticket project" }, 0);
    objectID = unique("OBJ");
    await submit({ type: "build.object", id: objectID }, "create", {
      name: objectName, title: objectTitle, plural: "Tickets", display: "subject", project,
      fields: [
        { name: "subject", title: "Subject", type: "text", required: true, search: true },
        { name: "note", title: "Resolution note", type: "text" },
      ],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.object/${objectID}`)).record.revision).toBe(1);
    await publish("object", objectID);
    const installed = await read<{ record: { published: string } }>(`/v1/records/build.object/${objectID}`);
    expect(installed.record.published).not.toBe("");
    expect(await objectFields()).toEqual(["subject", "note"]);
    await submit({ type: "build.object", id: relatedID }, "create", { name: relatedName, title: relatedTitle, project, fields: [{ name: "subject", title: "Subject", type: "text" }] }, 0);
    await publish("object", relatedID);
  });

  const openEditor = async (view: Page, id = objectID, title = objectTitle) => {
    await view.goto(`/#/object-type?id=${id}`);
    await expect(view.getByRole("heading", { name: title, level: 2 })).toBeVisible({ timeout: 30_000 });
  };

  await test.step("1 the builder opens the published object type and starts a new branch from the branch bar", async () => {
    await openEditor(page);
    const bar = page.getByRole("group", { name: "Branch", exact: true });
    await expect(bar).toBeVisible();
    await bar.getByRole("button", { name: "Branch actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "New branch…", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New branch", exact: true });
    await dialog.getByRole("textbox", { name: "Title", exact: true }).fill(branchTitle);
    await dialog.getByRole("textbox", { name: "Name", exact: true }).fill(branchName);
    await dialog.getByRole("button", { name: "Create branch", exact: true }).click();
    await expect(dialog).toBeHidden();
    await expect(bar.getByRole("combobox", { name: "Branch", exact: true })).toHaveValue(/.+/);
  });

  await test.step("2 the builder adds a property on the branch and the work state stays private until it is saved", async () => {
    await page.getByRole("navigation", { name: objectTitle, exact: true }).getByRole("button", { name: /^Properties/ }).click();
    await page.getByRole("button", { name: "Add a property", exact: true }).click();
    const inspector = page.getByRole("textbox", { name: "What people call it", exact: true });
    await expect(inspector).toBeVisible();
    await inspector.fill(newFieldTitle);
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(newField);
    await expect(page.getByRole("status").filter({ hasText: "Working state saved" })).toBeVisible({ timeout: 30_000 });
    const branch = await page.getByRole("group", { name: "Branch", exact: true }).getByRole("combobox", { name: "Branch", exact: true }).inputValue();
    const working = await read<{ id: string; record?: { spec: unknown } }>(`/v1/working-state/${branch}/build.object/${objectID}`);
    expect(working.record).toBeDefined();
    const another = await page.request.get(`/v1/records/build.workingstate/${working.id}`, { headers: { authorization: "Bearer admin" } });
    expect([403, 404]).toContain(another.status());
    const shared = await read<{ resources: unknown[] }>(`/v1/branches/${branch}`);
    expect(shared.resources).toHaveLength(0);
    await openEditor(page, relatedID, relatedTitle);
    await page.getByRole("navigation", { name: relatedTitle, exact: true }).getByRole("button", { name: /^Properties/ }).click();
    await page.getByRole("button", { name: "Add a property", exact: true }).click();
    await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Severity details");
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(relatedField);
    await expect(page.getByRole("status").filter({ hasText: "Working state saved" })).toBeVisible();
    const privateEdits = await read<{resources:{id:string}[]}>(`/v1/working-state/${branch}`);
    expect(privateEdits.resources).toHaveLength(2);
    const adminState = await page.request.get(`/v1/working-state/${branch}`, { headers: { authorization: "Bearer admin" } });
    expect((await adminState.json()).resources).toEqual([]);
    await page.getByRole("group", { name: "Branch", exact: true }).getByRole("button", { name: "Review and save", exact: true }).click();
    const save = page.getByRole("dialog", { name: "Review working state", exact: true });
    await expect(save.getByRole("list", { name: "Private resource edits" })).toContainText(objectTitle);
    await expect(save.getByRole("list", { name: "Private resource edits" })).toContainText(relatedTitle);
    await save.getByRole("button", { name: "Save to branch", exact: true }).click();
    await expect(save).toBeHidden();
    expect((await read<{resources:unknown[]}>(`/v1/working-state/${branch}`)).resources).toEqual([]);
    expect((await read<{resources:unknown[]}>(`/v1/branches/${branch}`)).resources).toHaveLength(2);
    expect(await objectFields()).toEqual(["subject", "note"]);
  });

  await test.step("3 Review and save shows the field-level diff and the builder proposes the branch", async () => {
    const dialog = page.getByRole("dialog", { name: `Review changes · ${branchTitle}`, exact: true });
    await expect(dialog).toBeVisible();
    const changed = dialog.getByRole("list", { name: "Changed resources", exact: true });
    const resource = changed.getByRole("region", { name: objectTitle, exact: true });
    await expect(resource).toBeVisible();
    await resource.getByRole("button", { name: /^Modified/ }).click();
    const branchChanges = resource.getByRole("list", { name: "Changes in this branch", exact: true });
    await expect(branchChanges.getByText(newField, { exact: false }).first()).toBeVisible();
    await expect(dialog.getByRole("region", { name: "Checks", exact: true }).getByText("Passed", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
    await dialog.getByRole("checkbox",{name:"admin-1",exact:true}).check();
    await dialog.getByRole("textbox", { name: "Note for reviewers", exact: true }).fill("Please review the added property.");
    const proposed = page.waitForResponse(response => response.url().includes("/proposal") && response.request().method() === "POST");
    await dialog.getByRole("button", { name: "Create proposal", exact: true }).click();
    await proposed;
    await expect(dialog).toBeHidden();
    await expect(page).toHaveURL(/\/#\/proposal\?.*id=/);
  });

  await test.step("4 success standard: before the merge, main is unaffected (the new property is not on main)", async () => {
    const main = await read<{ record: { fields: { name: string }[]; revision: number; published: string } }>(`/v1/records/build.object/${objectID}`);
    expect(main.record.fields.map(field => field.name)).toEqual(["subject", "note"]);
    expect(main.record.fields.some(field => field.name === newField)).toBe(false);
    expect(JSON.parse(main.record.published).fields.some((field: { name: string }) => field.name === newField)).toBe(false);
  });

  const admin = await (await browser.newContext()).newPage();
  try {
    await test.step("5 success standard: the reviewer judges the change from Home without opening any editor, then merges", async () => {
      await signIn(admin, "admin");
      const home = admin.getByRole("region", { name: "Home", exact: true });
      await expect(home.getByRole("heading", { name: "Branches and proposals", level: 2 })).toBeVisible({ timeout: 30_000 });
      await home.getByRole("button", { name: new RegExp(`${branchTitle}.*Proposed`) }).click();
      await expect(admin).toHaveURL(/\/#\/proposal\?.*id=/);

      const changed = admin.getByRole("list", { name: "Changed resources", exact: true });
      const resource = changed.getByRole("region", { name: objectTitle, exact: true });
      await expect(resource).toBeVisible({ timeout: 30_000 });
      await resource.getByRole("button", { name: /^Modified/ }).click();
      const reviewerChanges = resource.getByRole("list", { name: "Changes in this branch", exact: true });
      await expect(reviewerChanges.getByText(newField, { exact: false }).first()).toBeVisible();

      await admin.getByRole("button",{name:"Approve",exact:true}).click();
      const merge = admin.getByRole("button", { name: "Merge", exact: true });
      await expect(merge).toBeEnabled({ timeout: 30_000 });
      const merged = admin.waitForResponse(response => response.url().endsWith("/merge") && response.request().method() === "POST");
      await merge.click();
      const response = await merged;
      expect(response.ok(), await response.text()).toBeTruthy();
    });

    await test.step("6 after the merge, main carries the new property", async () => {
      await expect.poll(async () => await objectFields(), { timeout: 30_000 }).toEqual(["subject", "note", newField]);
      const field = (await read<{ record: { fields: { name: string; title: string }[] } }>(`/v1/records/build.object/${objectID}`)).record.fields.find(entry => entry.name === newField);
      expect(field?.title).toBe(newFieldTitle);
      const related = (await read<{record:{fields:{name:string}[]}}>(`/v1/records/build.object/${relatedID}`)).record;
      expect(related.fields.some(field => field.name === relatedField)).toBe(true);
    });
  } finally {
    await admin.context().close();
  }
});
