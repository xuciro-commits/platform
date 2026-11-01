import { expect, signIn, test, unique } from "./kit";
import type { ReleaseDraftRestore, ReleasePreview } from "../packages/kernel/src/gen/host";

test("F1 builder goes from an empty project to an application a business user acts in", async ({ page, browser }) => {
  test.setTimeout(180_000);
  const name = unique("orders");
  const objectName = `${name}order`;
  const label = `Orders ${name.slice(6)}`;
  const headers = { authorization: "Bearer builder" };
  const read = async <T,>(path: string): Promise<T> => {
    const response = await page.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };

  const submit = async (type: string, id: string, action: string, payload: unknown, expectedRevision: number) => {
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const response = await page.request.post("/v1/submissions", {
      headers,
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority: "build", target: { type, id }, schema: { name: `${type}.${action}`, version: 1 },
        idempotencyKey: unique("review"), payload: Buffer.from(JSON.stringify(payload)).toString("base64"), expectedRevision,
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
    expect(answer.error).toBeUndefined();
    expect(answer.record?.changeId).toEqual(expect.any(String));
    expect(answer.record.changeId).not.toBe("");
  };

  await test.step("1 create a project", async () => {
    await signIn(page, "builder", "/projects");
    await page.keyboard.press("?");
    await expect(page.getByRole("dialog", { name: "Keyboard shortcuts" })).toBeVisible();
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "New project", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "Create project" });
    await sheet.getByRole("textbox", { name: "Name *" }).fill(name);
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill(label);
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Next steps" })).toBeVisible();
  });

  await test.step("2 bring data in as a dataset", async () => {
    await page.getByRole("button", { name: "New", exact: true }).first().click();
    await page.getByRole("menuitem", { name: "Dataset", exact: true }).click();
    await page.getByRole("textbox", { name: "Dataset name" }).fill(`${name}rows`);
    await page.getByRole("textbox", { name: "Dataset title" }).fill("Order rows");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("button", { name: "Upload CSV" })).toBeVisible();
    await page.locator('input[type="file"]').setInputFiles({ name: "orders.csv", mimeType: "text/csv", buffer: Buffer.from("Order no,Customer,Amount\nSO-1,Acme,120\nSO-2,Globex,80\n") });
    await expect(page.getByRole("cell", { name: "Globex" })).toBeVisible();
  });

  await test.step("3 create an object type from the dataset", async () => {
    await page.getByRole("button", { name: "Create object type", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "Create object type" });
    await expect(sheet.getByRole("combobox", { name: "Primary key column *" })).toHaveValue("Order no");
    await expect(sheet.getByRole("combobox", { name: "Title column *" })).toHaveValue("Customer");
    await expect(sheet.getByText("→ amount · decimal")).toBeVisible();
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill("Order");
    await sheet.getByRole("textbox", { name: "Name *" }).fill(objectName);
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByText(`build.${objectName}`, { exact: true })).toBeVisible();
    await page.keyboard.press("ControlOrMeta+K");
    await page.getByRole("combobox").fill(`:${objectName}`);
    await expect(page.getByRole("group", { name: "Resources" }).getByRole("option").first()).toBeVisible();
    await page.keyboard.press("Escape");
  });

  await test.step("4 add an action type", async () => {
    await page.getByRole("navigation", { name: "Order" }).getByRole("button", { name: "Actions", exact: true }).click();
    await page.getByRole("button", { name: "Add an action", exact: true }).click();
    await expect(page.getByRole("button", { name: "Open action type", exact: true })).toBeVisible();
    await expect(page.getByRole("status").filter({ hasText: "Unsaved changes" })).toBeVisible();
  });

  await test.step("5 build a page in Workshop from a template bound to the draft object type", async () => {
    await page.getByRole("button", { name: label, exact: true }).first().click();
    await page.getByRole("button", { name: "Open Workshop", exact: true }).click();
    await page.getByRole("button", { name: "Toggle inspector panel" }).click();
    await page.getByRole("button", { name: "New page from template…", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New page from template" });
    await dialog.getByRole("button", { name: /^List report/ }).click();
    const picker = dialog.getByRole("combobox", { name: "Object" });
    await expect(picker.locator("option", { hasText: "build.app" })).toHaveCount(0);
    const option = picker.locator("option", { hasText: `build.${objectName}` }).first();
    await expect(option).toBeAttached();
    await picker.selectOption((await option.getAttribute("value")) ?? "");
    await expect(dialog.getByRole("checkbox", { name: /^Create/ })).toBeChecked();
    await expect(dialog.getByRole("group", { name: "Offered actions" }).getByRole("checkbox")).toHaveCount(4);
    await dialog.getByRole("textbox", { name: "Page name" }).fill(`${name}list`);
    await dialog.getByRole("textbox", { name: "Page title" }).fill("Orders");
    await dialog.getByRole("button", { name: "Create draft in Studio", exact: true }).click();
    await expect(page.getByRole("region", { name: "Page settings" })).toBeVisible();
  });

  await test.step("6 review and save once, leaving another builder's drafts out", async () => {
    const other = await (await browser.newContext()).newPage();
    await signIn(other, "admin", "/projects");
    await other.getByRole("button", { name: "New project", exact: true }).click();
    const sheet = other.getByRole("dialog", { name: "Create project" });
    await sheet.getByRole("textbox", { name: "Name *" }).fill(`${name}other`);
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill(`Other ${name.slice(6)}`);
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(other.getByRole("heading", { name: "Next steps" })).toBeVisible();
    await other.context().close();
    const previewResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/preview") && response.request().method() === "POST");
    await page.keyboard.press("ControlOrMeta+Shift+S");
    const preview = await previewResponse;
    expect(preview.ok(), await preview.text()).toBeTruthy();
    const review = await preview.json() as ReleasePreview;
    expect(review.diagnostic).toBeUndefined();
    expect(review.draftReviews).toHaveLength(review.drafts!.length);
    for (const draft of review.draftReviews!) {
      expect(draft.checked).toBe(true);
      expect(draft.issues).toEqual([]);
    }
    const provenance = page.getByRole("group", { name: "Draft provenance" });
    for (const kind of ["Applications", "Pages", "Object types", "Datasets"]) await expect(provenance.getByText(`${kind} ·`).first()).toBeVisible();
    await expect(provenance.getByText("Applications ·")).toHaveCount(1);
    await expect(provenance.getByText("0 issues", { exact: true })).toHaveCount(review.drafts!.length);
    await provenance.getByRole("treeitem", { name: /^Pages ·/ }).last().click();
    const selected = page.getByRole("region", { name: "Selected change" });
    await expect(selected.getByRole("heading", { name: /^Pages ·/ })).toBeVisible();
    await expect(selected.getByText("New: nothing of it is installed yet.")).toBeVisible();
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
    await expect.poll(() => page.evaluate(async (dataset) => {
      const answer = await fetch(`/v1/records/build.dataset?limit=500`, { headers: { authorization: "Bearer builder" } });
      const record = (await answer.json()).records.find((entry: { name: string }) => entry.name === dataset);
      return record?.projections?.[0]?.written ?? 0;
    }, `${name}rows`), { timeout: 30_000 }).toBe(2);
  });

  await test.step("7 a business user sees the dataset rows as orders and creates one", async () => {
    const user = await (await browser.newContext()).newPage();
    await signIn(user, "user");
    await user.getByRole("button", { name: "Applications", exact: true }).first().click();
    await user.getByRole("region", { name: "Business applications" }).getByRole("button", { name: label, exact: true }).click();
    const app = user.getByRole("region", { name: "Orders" });
    await expect(app.getByRole("heading", { name: "Orders", level: 1 })).toBeVisible();
    await expect(app.getByRole("cell", { name: "Globex" })).toBeVisible({ timeout: 30_000 });
    await app.getByRole("button", { name: "Create order", exact: true }).click();
    const form = user.getByRole("dialog", { name: "Create order" });
    await expect(form.getByRole("button", { name: "Source editor" })).toHaveCount(0);
    await form.getByRole("textbox", { name: /^Order no/ }).fill("SO-3");
    await form.getByRole("textbox", { name: /^Customer/ }).fill("Initech");
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(app.getByRole("cell", { name: "Initech" })).toBeVisible({ timeout: 30_000 });
    await user.context().close();
  });

  await test.step("8 review every rejected draft and its published downstream resources", async () => {
    type ObjectDraft = { id: string; revision: number; name: string; published: string; fields: { name: string; type: string }[] };
    const object = (await read<{ records: ObjectDraft[] }>("/v1/records/build.object?limit=500")).records.find(record => record.name === objectName)!;
    expect(object).toMatchObject({ published: expect.any(String), fields: expect.arrayContaining([expect.objectContaining({ name: "amount", type: "decimal" })]) });
    expect(object.published).not.toBe("");
    const badPageID = unique("PAGE");
    const badPageName = `Invalid${name}`;
    const badPageTitle = `Invalid orders ${name}`;
    let pageCreated = false, objectEdited = false;
    try {
      await submit("build.page", badPageID, "create", { name: badPageName, title: badPageTitle, object: `build.${objectName}`, list: ["customer"], detail: ["customer"] }, 0);
      pageCreated = true;
      await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.page/${badPageID}`)).record.revision).toBe(1);
      await submit("build.object", object.id, "edit", { fields: object.fields.filter(field => field.name !== "amount") }, object.revision);
      objectEdited = true;
      await expect.poll(async () => (await read<{ record: ObjectDraft }>(`/v1/records/build.object/${object.id}`)).record.revision).toBe(object.revision + 1);

      await page.reload();
      await expect(page.getByRole("button", { name: "Review and save", exact: true })).toBeEnabled();
      const previewResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/preview") && response.request().method() === "POST" && response.request().postDataJSON().drafts.some((draft: { id: string }) => draft.id === badPageID));
      await page.getByRole("button", { name: "Review and save", exact: true }).click();
      const preview = await previewResponse;
      expect(preview.ok(), await preview.text()).toBeTruthy();
      const review = await preview.json() as ReleasePreview;
      expect(review.diagnostic).toBeTruthy();
      const objectReview = review.draftReviews!.find(draft => draft.kind === "object" && draft.id === object.id)!;
      const pageReview = review.draftReviews!.find(draft => draft.kind === "page" && draft.id === badPageID)!;
      expect(objectReview).toBeDefined();
      expect(pageReview).toBeDefined();
      for (const draft of [objectReview, pageReview]) {
        expect(draft.checked).toBe(true);
        expect(draft.issues.length).toBeGreaterThan(0);
      }
      expect(objectReview.issues.join("\n")).toContain("column mapping");
      expect(pageReview.issues).toContain(`the name "${badPageName}" is not lower-case letters and digits`);
      expect(objectReview.change).toMatchObject({ published: true, fields: expect.arrayContaining([expect.objectContaining({ name: "fields", removed: expect.arrayContaining(["amount"]) })]) });

      const provenance = page.getByRole("group", { name: "Draft provenance" });
      const selected = page.getByRole("region", { name: "Selected change" });
      for (const [draft, title] of [[pageReview, `Pages · ${badPageTitle}`], [objectReview, "Object types · Order"]] as const) {
        const row = provenance.getByRole("treeitem").filter({ hasText: title }).last();
        await expect(row.getByText(`${draft.issues.length} issues`, { exact: true })).toBeVisible();
        await row.click();
        await expect(selected.getByRole("heading", { name: title, exact: true })).toBeVisible();
        const issues = selected.getByRole("list", { name: `${draft.issues.length} issues`, exact: true });
        await expect(issues.getByRole("listitem")).toHaveText(draft.issues);
      }
      await expect(provenance.getByRole("treeitem").filter({ hasText: "Object types · Order" }).last().getByText("Breaking", { exact: true })).toBeVisible();
      await expect(selected.getByRole("note")).toContainText("Removes amount.");
      const downstream = selected.getByRole("region", { name: "Published downstream resources" });
      for (const [kind, resourceName, title] of [["page", `${name}list`, "Orders"], ["action", `build.${objectName}.create`, "Create order"], ["app", name, label]] as const) {
        const resource = objectReview.downstream.find(resource => resource.ref.kind === kind && resource.ref.name === resourceName)!;
        expect(resource).toMatchObject({ title, ref: { app: "build", kind, name: resourceName } });
        const row = downstream.getByRole("listitem").filter({ has: page.getByText(resourceName, { exact: true }) });
        await expect(row.getByText(resourceName, { exact: true })).toBeVisible();
        await expect(row.getByText(`${title} · ${kind === "page" ? "Pages" : kind === "app" ? "Applications" : "action"}`, { exact: true })).toBeVisible();
      }
      await expect(page.getByRole("button", { name: "Save immutable candidate", exact: true })).toHaveCount(0);
    } finally {
      for (const draft of [
        ...(objectEdited ? [{ kind: "object", type: "build.object", id: object.id }] : []),
        ...(pageCreated ? [{ kind: "page", type: "build.page", id: badPageID }] : []),
      ]) {
        const restore = await read<ReleaseDraftRestore>(`/v1/releases/drafts/restore?kind=${draft.kind}&id=${draft.id}`);
        expect(restore.type).toBe(draft.type);
        const current = await read<{ record: { revision: number } }>(`/v1/records/${draft.type}/${draft.id}`);
        await submit(restore.type, draft.id, restore.action, restore.payload, current.record.revision);
        await expect.poll(async () => (await read<{ drafts: { kind: string; id: string }[] }>("/v1/releases/drafts/working")).drafts.some(entry => entry.kind === draft.kind && entry.id === draft.id)).toBe(false);
      }
    }
  });

  await test.step("9 candidate dependency failures leave every check incomplete", async () => {
    const appID = unique("APP"), pageID = unique("PAGE");
    const created: { type: string; id: string }[] = [];
    try {
      await submit("build.app", appID, "create", { name: unique("dependency"), title: "Unresolved resources", pages: [], resources: [{ app: "build", kind: "object", name: `build.${unique("missing")}` }] }, 0);
      created.push({ type: "build.app", id: appID });
      await submit("build.page", pageID, "create", { name: unique("dependencyorders"), title: "Dependency review orders", object: `build.${objectName}`, list: ["customer"], detail: ["customer"] }, 0);
      created.push({ type: "build.page", id: pageID });
      await page.reload();
      await expect(page.getByRole("button", { name: "Review and save", exact: true })).toBeEnabled();
      const previewResponse = page.waitForResponse(response => response.url().endsWith("/v1/releases/preview") && response.request().method() === "POST" && [appID, pageID].every(id => response.request().postDataJSON().drafts.some((draft: { id: string }) => draft.id === id)));
      await page.getByRole("button", { name: "Review and save", exact: true }).click();
      const response = await previewResponse;
      expect(response.ok(), await response.text()).toBeTruthy();
      const review = await response.json() as ReleasePreview;
      expect(review.diagnostic).toBeTruthy();
      const provenance = page.getByRole("group", { name: "Draft provenance" });
      for (const [kind, id, title] of [["app", appID, "Unresolved resources"], ["page", pageID, "Dependency review orders"]]) {
        const draft = review.draftReviews!.find(draft => draft.kind === kind && draft.id === id);
        expect(draft).toBeDefined();
        expect(draft!.issues).toEqual([]);
        expect(draft!.checked).toBe(false);
        await expect(provenance.getByRole("treeitem").filter({ hasText: title }).last().getByText("Checks incomplete", { exact: true })).toBeVisible();
      }
      await expect(page.getByRole("button", { name: "Save immutable candidate", exact: true })).toHaveCount(0);
    } finally {
      for (const draft of created.reverse()) {
        const current = await read<{ record: { revision: number } }>(`/v1/records/${draft.type}/${draft.id}`);
        await submit(draft.type, draft.id, "archive", {}, current.record.revision);
      }
    }
  });

  await test.step("10 jointly preview a new function version and its pinned evaluation twice", async () => {
    const functionID = unique("FN"), planID = unique("PLAN"), functionName = unique("answer");
    const created: { kind: string; type: string; id: string }[] = [];
    let failed = false;
    const revise = async (type: string, id: string, action: string, payload: unknown) => {
      const current = await read<{ record: { revision: number } }>(`/v1/records/${type}/${id}`);
      await submit(type, id, action, payload, current.record.revision);
    };
    const publish = async (kind: string, id: string) => {
      const drafts = [{ kind, id }];
      const previewResponse = await page.request.post("/v1/releases/preview", { headers, data: { drafts } });
      expect(previewResponse.ok(), await previewResponse.text()).toBeTruthy();
      const preview = await previewResponse.json() as ReleasePreview;
      expect(preview.diagnostic).toBeUndefined();
      expect(preview.candidateId).toEqual(expect.any(String));
      for (const path of ["/v1/releases/candidates", "/v1/releases/active"]) {
        const response = await page.request.post(path, { headers, data: { ...(path === "/v1/releases/candidates" ? { drafts } : {}), candidateId: preview.candidateId, key: unique("publish") } });
        const answer = await response.json();
        expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
        expect(answer.id).toBe(preview.candidateId);
      }
    };
    try {
      await submit("build.function", functionID, "create", {
        name: functionName, title: "Order answer", description: "Return a typed order answer", object: `build.${objectName}`, fields: ["customer"], instructions: "Return the answer for the customer.",
        output: [{ name: "answer", type: "string", description: "Answer" }], roles: ["builder"], maxInputBytes: 1024, maxOutputBytes: 1024, maxTokens: 100,
      }, 0);
      created.push({ kind: "function", type: "build.function", id: functionID });
      await publish("function", functionID);
      const installedFunction = await read<{ record: { version: number; published: string } }>(`/v1/records/build.function/${functionID}`);
      expect(installedFunction.record.version).toBe(1);
      expect(JSON.parse(installedFunction.record.published)).toMatchObject({ name: functionName, version: 1, instructions: "Return the answer for the customer." });
      await submit("build.testplan", planID, "create", {
        name: unique("evaluation"), title: "Order answer evaluation", function: functionID, functionVersion: 1, at: "2000-01-01T00:00:00Z",
        steps: [{ type: "build.function-call", id: "evaluation", action: "build.function-call.start", payload: "{}", expect: "accepted" }],
        evaluation: [{ minQuality: 1, maxCostUsd: 1, maxLatencyMillis: 1000, cases: [{ name: "case", input: { customer: "Acme" }, expected: { answer: "ok" } }] }],
      }, 0);
      created.push({ kind: "evaluation-set", type: "build.testplan", id: planID });
      await publish("evaluation-set", planID);
      const installedPlan = await read<{ record: { definitionVersion: number } }>(`/v1/records/build.testplan/${planID}`);
      expect(installedPlan.record.definitionVersion).toBe(1);
      await revise("build.function", functionID, "edit", { instructions: "Return the revised answer for the customer." });
      await revise("build.testplan", planID, "edit", { functionVersion: 2, title: "Order answer evaluation v2" });
      const drafts = [{ kind: "function", id: functionID }, { kind: "evaluation-set", id: planID }];
      let candidateID: string | undefined;
      for (let attempt = 0; attempt < 2; attempt++) {
        const response = await page.request.post("/v1/releases/preview", { headers, data: { drafts } });
        expect(response.ok(), await response.text()).toBeTruthy();
        const review = await response.json() as ReleasePreview;
        expect(review.candidateId).toEqual(expect.any(String));
        expect(review.candidateId).not.toBe("");
        expect(review.diagnostic).toBeUndefined();
        expect(review.draftReviews).toHaveLength(2);
        for (const draft of drafts) {
          const checked = review.draftReviews!.find(item => item.kind === draft.kind && item.id === draft.id);
          expect(checked).toBeDefined();
          expect(checked!.checked).toBe(true);
          expect(checked!.issues).toEqual([]);
          expect(checked!.change?.diagnostic).toBeUndefined();
        }
        const evaluation = review.draftReviews!.find(item => item.id === planID)!;
        const binding = evaluation.change!.fields.find(field => field.name === "function");
        expect(binding).toBeDefined();
        expect(binding!.before).toMatchObject({ ref: { app: "build", kind: "function", name: functionName }, sourceVersion: expect.stringMatching(/\.function-1$/) });
        expect(binding!.after).toMatchObject({ ref: { app: "build", kind: "function", name: functionName }, sourceVersion: expect.stringMatching(/\.function-2$/) });
        if (attempt === 0) candidateID = review.candidateId;
        else expect(review.candidateId).toBe(candidateID);
      }
    } catch (error) {
      failed = true;
      throw error;
    } finally {
      const cleanupErrors: unknown[] = [];
      for (const draft of created.reverse()) {
        try {
          const restore = await read<ReleaseDraftRestore>(`/v1/releases/drafts/restore?kind=${draft.kind}&id=${draft.id}`);
          await revise(draft.type, draft.id, restore.action, restore.payload);
        } catch (error) {
          cleanupErrors.push(error);
          await test.info().attach(`cleanup ${draft.kind} ${draft.id}`, { body: error instanceof Error ? error.stack ?? error.message : String(error), contentType: "text/plain" });
        }
      }
      if (!failed && cleanupErrors.length) throw cleanupErrors[0];
    }
  });

});
