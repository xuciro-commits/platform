import { expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F13 a destructive branch change shows downstream impact before review", async ({ page }) => {
  test.setTimeout(240_000);
  const tag = unique("impact");
  const headers = { authorization: "Bearer builder" };

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
    return answer.record as { changeId: string };
  };

  const parentID = unique("PARENT"), childID = unique("CHILD");
  await test.step("setup: publish a parent object and a child object that references it", async () => {
    await signIn(page, "builder", "/projects");
    await submit({ type: "build.object", id: parentID }, "create", {
      name: `parent${tag}`, title: `Parent ${tag}`, plural: "Parents", display: "subject",
      fields: [
        { name: "subject", title: "Subject", type: "text", required: true },
        { name: "note", title: "Note", type: "text" },
      ],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await submit({ type: "build.object", id: childID }, "create", {
      name: `child${tag}`, title: `Child ${tag}`, plural: "Children", display: "subject",
      fields: [
        { name: "subject", title: "Subject", type: "text", required: true },
        { name: "parent", title: "Parent", type: "reference", ref: `build.parent${tag}` },
      ],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    for (const id of [parentID, childID]) {
      const preview = await page.request.post("/v1/releases/preview", { headers, data: { drafts: [{ kind: "object", id }] } });
      expect(preview.ok(), await preview.text()).toBeTruthy();
      const { candidateId } = await preview.json();
      await page.request.post("/v1/releases/candidates", { headers, data: { drafts: [{ kind: "object", id }], candidateId, key: unique("publish") } });
      await page.request.post("/v1/releases/active", { headers, data: { candidateId, key: unique("publish") } });
    }
  });

  await test.step("1 the builder starts a branch and removes the referenced field", async () => {
    await page.goto(`/#/object-type?id=${parentID}`);
    const bar = page.getByRole("group", { name: "Branch", exact: true });
    await expect(bar).toBeVisible();
    await bar.getByRole("button", { name: "Branch actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "New branch…", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New branch", exact: true });
    await dialog.getByRole("textbox", { name: "Title", exact: true }).fill(`Remove note ${tag}`);
    await dialog.getByRole("textbox", { name: "Name", exact: true }).fill(`remove${tag}`);
    await dialog.getByRole("button", { name: "Create branch", exact: true }).click();
    await expect(dialog).toBeHidden();
    await page.getByRole("navigation", { name: `Parent ${tag}`, exact: true }).getByRole("button", { name: /^Properties/ }).click();
    await page.getByRole("button", { name: /^Remove/ }).last().click();
    await expect(page.getByRole("status").filter({ hasText: "Working state saved" })).toBeVisible({ timeout: 30_000 });
  });

  await test.step("2 success standard: the branch diff shows downstream impact and requires confirmation", async () => {
    await page.getByRole("group", { name: "Branch", exact: true }).getByRole("button", { name: "Review and save", exact: true }).click();
    const save = page.getByRole("dialog", { name: "Review working state", exact: true });
    await expect(save).toBeVisible();
    await save.getByRole("button", { name: "Create proposal", exact: true }).click();
    await expect(page).toHaveURL(/\\/#\\/proposal\\?.*id=/);
    const impact = page.getByRole("button", { name: /Review dependencies/ });
    await expect(impact).toBeVisible();
  });
});
