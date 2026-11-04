import { expect, signIn, test, unique, type Page } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F12 a proposal appears only in its reviewers' 'Pending review' queue on Home", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const tag = unique("prop");
  const objectName = `obj${tag}`;
  const branchName = `branch${tag}`;
  const branchTitle = `Add field ${tag}`;
  const reviewer = "reviewer";
  const nonReviewer = "auditor";
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

  const objectID = unique("OBJ");
  await test.step("setup: a builder defines and publishes a baseline object type", async () => {
    await signIn(page, "builder", "/projects");
    await submit({ type: "build.object", id: objectID }, "create", {
      name: objectName, title: `Object ${tag}`, plural: "Objects", display: "subject",
      fields: [{ name: "subject", title: "Subject", type: "text", required: true }],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.object/${objectID}`)).record.revision).toBe(1);
    const preview = await page.request.post("/v1/releases/preview", {
      headers, data: { drafts: [{ kind: "object", id: objectID }] },
    });
    expect(preview.ok(), await preview.text()).toBeTruthy();
    const { candidateId } = await preview.json();
    await page.request.post("/v1/releases/candidates", { headers, data: { drafts: [{ kind: "object", id: objectID }], candidateId, key: unique("publish") } });
    await page.request.post("/v1/releases/active", { headers, data: { candidateId, key: unique("publish") } });
  });

  await test.step("1 the builder starts a branch, edits it and proposes it with reviewer", async () => {
    await page.goto(`/#/object-type?id=${objectID}`);
    const bar = page.getByRole("group", { name: "Branch", exact: true });
    await expect(bar).toBeVisible();
    await bar.getByRole("button", { name: "Branch actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "New branch…", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New branch", exact: true });
    await dialog.getByRole("textbox", { name: "Title", exact: true }).fill(branchTitle);
    await dialog.getByRole("textbox", { name: "Name", exact: true }).fill(branchName);
    await dialog.getByRole("button", { name: "Create branch", exact: true }).click();
    await expect(dialog).toBeHidden();
    await page.getByRole("navigation", { name: `Object ${tag}`, exact: true }).getByRole("button", { name: /^Properties/ }).click();
    await page.getByRole("button", { name: "Add a property", exact: true }).click();
    await page.getByRole("textbox", { name: "What people call it", exact: true }).fill(`Field ${tag}`);
    await page.getByRole("textbox", { name: "Name", exact: true }).fill(`field${tag}`);
    await expect(page.getByRole("status").filter({ hasText: "Working state saved" })).toBeVisible({ timeout: 30_000 });
    await page.getByRole("group", { name: "Branch", exact: true }).getByRole("button", { name: "Review and save", exact: true }).click();
    const save = page.getByRole("dialog", { name: "Review working state", exact: true });
    await expect(save).toBeVisible();
    await save.getByRole("checkbox", { name: reviewer, exact: true }).check();
    await save.getByRole("textbox", { name: "Note for reviewers", exact: true }).fill("Please review.");
    const proposed = page.waitForResponse(response => response.url().includes("/proposal") && response.request().method() === "POST");
    await save.getByRole("button", { name: "Create proposal", exact: true }).click();
    await proposed;
    await expect(save).toBeHidden();
  });

  const reviewerPage = await (await browser.newContext()).newPage();
  const nonReviewerPage = await (await browser.newContext()).newPage();
  try {
    await test.step("2 success standard: the reviewer sees it in Home 'Pending review', the non-reviewer does not", async () => {
      await signIn(reviewerPage, reviewer, "/home");
      const home = reviewerPage.getByRole("region", { name: "Home", exact: true });
      await expect(home.getByRole("heading", { name: "Pending review", level: 2 })).toBeVisible({ timeout: 30_000 });
      await expect(home.getByRole("button", { name: new RegExp(branchTitle) })).toBeVisible();

      await signIn(nonReviewerPage, nonReviewer, "/home");
      const otherHome = nonReviewerPage.getByRole("region", { name: "Home", exact: true });
      await expect(otherHome.getByRole("heading", { name: "Pending review", level: 2 })).toBeVisible({ timeout: 30_000 });
      await expect(otherHome.getByRole("button", { name: new RegExp(branchTitle) })).toHaveCount(0);
    });
  } finally {
    await reviewerPage.context().close();
    await nonReviewerPage.context().close();
  }
});
