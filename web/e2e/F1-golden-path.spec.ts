import { expect, signIn, test, unique } from "./kit";
import type { ObjectView, PageInputSchema, ReleaseDraftRestore, ReleasePreview } from "../packages/kernel/src/gen/host";
import { pageUIManifest } from "../packages/app/src/pageui/manifest";

test.use({ actionTimeout: 15_000 });

test("F1 builder goes from an empty project to an application a business user acts in", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const name = unique("orders");
  const objectName = `${name}order`;
  const label = `Orders ${name.slice(6)}`;
  const headers = { authorization: "Bearer builder" };
  const viewName = `${name}view`;
  let objectID = "", viewID = "";
  const objectView: ObjectView = { application: { app: "build", kind: "app", name }, page: { app: "build", kind: "page", name: viewName } };
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

  const saveCurrentResource = async () => {
    await page.getByRole("button", { name: "Save", exact: true }).first().click();
    const review = page.getByRole("dialog", { name: "Review changes", exact: true });
    await review.getByRole("button", { name: "Save", exact: true }).click();
    await expect(review).toBeHidden();
  };

  await test.step("1 create a project", async () => {
    await signIn(page, "builder", "/projects");
    const projects = page.getByRole("table");
    await expect(projects).toBeVisible();
    expect(await projects.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
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
    await expect(page.getByRole("textbox", { name: "What people call it", exact: true })).toBeVisible();
    await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Step 1");
    await expect(page.getByText("Select a row to edit it here.", { exact: true })).not.toBeVisible();
    await page.getByRole("button", { name: "Create", exact: true }).first().click();
    await expect(page).not.toHaveURL(/id=new/);
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
    await expect(page.getByText("Publish the object type and page together to preview business data. You can continue editing this widget.").first()).toBeVisible();
    await expect(page.getByText(/HTTP 404/)).toHaveCount(0);
    await expect(page.getByRole("alert").filter({ hasText: "module membership is unconfirmed" })).toHaveCount(0);
    await page.getByRole("button", { name: "Toggle structure panel" }).click();
    await page.getByRole("tab", { name: "Widgets", exact: true }).click();
    const widgetSearch = page.getByRole("searchbox", { name: "Search widgets" });
    for (const title of ["Record timeline", "Kanban board", "Record chart", "Record leaderboard", "Record scatter plot", "Sparkline KPI", "Record map"]) {
      await widgetSearch.fill(title);
      await expect(page.getByRole("button", { name: "Records · 1", exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: title, exact: true })).toBeVisible();
    }
    await widgetSearch.fill("");
    await page.getByRole("button", { name: "Toggle inspector panel" }).click();
    await expect(page.getByRole("region", { name: "Page settings" })).toBeVisible();
  });

  await test.step("5b create and bind a Workshop object view before its object is published", async () => {
    await page.getByRole("button", { name: "Object views", exact: true }).click();
    await page.getByRole("button", { name: "New object view", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New object view" });
    await dialog.getByRole("combobox", { name: "Module", exact: true }).selectOption({ label: `${label} · ${name}` });
    await dialog.getByRole("combobox", { name: "Object type", exact: true }).selectOption(`build.${objectName}`);
    await dialog.getByRole("textbox", { name: "Page name", exact: true }).fill(viewName);
    await dialog.getByRole("textbox", { name: "Page title", exact: true }).fill("Order object view");
    await dialog.getByRole("button", { name: "Create object view", exact: true }).click();
    await expect(dialog).toBeHidden();
    const objects = await read<{ records: { id: string; name: string; published?: string }[] }>("/v1/records/build.object?limit=500");
    const object = objects.records.find(record => record.name === objectName)!;
    expect(object.published ?? "").toBe("");
    objectID = object.id;
    const pages = await read<{ records: { id: string; name: string }[] }>("/v1/records/build.page?limit=500");
    viewID = pages.records.find(record => record.name === viewName)!.id;
    const modules = await read<{ records: { id: string; revision: number; name: string; pages: string[]; resources: { name: string }[] }[] }>("/v1/records/build.app?limit=500");
    const module = modules.records.find(record => record.name === name)!;
    expect(module).toMatchObject({ pages: expect.arrayContaining([`${name}list`, viewName]), resources: expect.arrayContaining([expect.objectContaining({ name: `build.${objectName}` })]) });
    await submit("build.app", module.id, "edit", { uiProfile: pageUIManifest.uiProfile, header: { variant: "horizontal", title: label, items: [{ kind: "title" }, { kind: "tabs", pages: [`${name}list`, viewName] }] } }, module.revision);
    await page.goto(`/#/object-type?id=${objectID}`);
    await page.getByRole("navigation", { name: "Order", exact: true }).getByRole("button", { name: "Display", exact: true }).click();
    await page.getByRole("combobox", { name: "Object view", exact: true }).selectOption({ label: `${label} / Order object view · ${name}/${viewName}` });
    await saveCurrentResource();
    await expect.poll(async () => (await read<{ record: { objectView?: ObjectView } }>(`/v1/records/build.object/${objectID}`)).record.objectView).toEqual(objectView);
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
    const portal = user.getByRole("region", { name: "Applications", exact: true });
    await expect(portal.getByRole("button", { name: /^Control Panel/ })).toHaveCount(0);
    await expect(portal.getByRole("button", { name: /^Asset Library/ })).toHaveCount(0);
    await user.getByRole("region", { name: "Business applications" }).getByRole("button", { name: label, exact: true }).click();
    const app = user.getByRole("region", { name: "Orders" });
    await expect(app.getByRole("heading", { name: "Orders", level: 1 })).toBeVisible();
    await expect(app.getByRole("cell", { name: "Globex" })).toBeVisible({ timeout: 30_000 });
    const resizer = app.getByRole("separator", { name: "Resize column Customer", exact: true });
    await resizer.press("ArrowRight");
    const width = await resizer.getAttribute("aria-valuenow");
    await user.reload();
    await expect(resizer).toHaveAttribute("aria-valuenow", width!);
    await app.getByRole("button", { name: "Create order", exact: true }).click();
    const form = user.getByRole("dialog", { name: "Create order" });
    await expect(form.getByRole("button", { name: "Source editor" })).toHaveCount(0);
    await form.getByRole("textbox", { name: /^Order no/ }).fill("SO-3");
    await form.getByRole("textbox", { name: /^Customer/ }).fill("Initech");
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(app.getByRole("cell", { name: "Initech" })).toBeVisible({ timeout: 30_000 });
    const records = (await read<{ records: { id: string; revision: number; customer: string }[] }>(`/v1/records/build.${objectName}?limit=500`)).records;
    const acme = records.find(record => record.customer === "Acme")!, globex = records.find(record => record.customer === "Globex")!;
    await user.goto(`/#/explorer?type=build.${objectName}`);
    await user.getByRole("main").getByRole("row").filter({ has: user.getByRole("cell", { name: "Acme", exact: true }) }).click();
    const aroundPanel = user.getByRole("complementary", { name: "Search around", exact: true });
    await expect(aroundPanel).toBeVisible();
    const inspect = user.getByRole("complementary").filter({ has: user.getByRole("button", { name: "Open record", exact: true }) });
    const inspectedView = inspect.getByRole("region", { name: "Order object view", exact: true });
    await expect(inspectedView.getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
    await expect(inspectedView.getByRole("heading", { name: "Globex", exact: true })).toHaveCount(0);
    await user.getByRole("button", { name: "Save exploration", exact: true }).click();
    const saveDialog = user.getByRole("dialog", { name: "Save exploration", exact: true });
    await saveDialog.getByRole("textbox", { name: "Name", exact: true }).fill(`Acme around ${name.slice(6)}`);
    await saveDialog.getByRole("button", { name: "Save", exact: true }).click();
    await expect(saveDialog).toBeHidden();
    await expect(user).toHaveURL(/\/#\/explorer\?.*saved=VIEW/);
    await user.goto(`/#/explorer?type=build.${objectName}`);
    await user.getByRole("main").getByRole("row").filter({ has: user.getByRole("cell", { name: "Acme", exact: true }) }).click();
    await inspect.getByRole("button", { name: "Open record", exact: true }).click();
    await expect(user).toHaveURL(new RegExp(`/#/record\\?.*id=${acme.id}`));
    const acmeView = user.getByRole("main").getByRole("region", { name: "Order object view", exact: true });
    await expect(acmeView.getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
    await expect(inspect).toHaveCount(0);
    const recordURL = user.url();
    const second = await user.context().newPage();
    await signIn(second, "user", `/record?id=${globex.id}&type=build.${objectName}`);
    const globexView = second.getByRole("main").getByRole("region", { name: "Order object view", exact: true });
    await expect(globexView.getByRole("heading", { name: "Globex", exact: true })).toBeVisible();
    await expect(globexView.getByRole("heading", { name: "Acme", exact: true })).toHaveCount(0);
    const applicationPages = user.getByRole("main").getByRole("navigation", { name: "Application pages", exact: true });
    await applicationPages.getByRole("button", { name: "Order object view", exact: true }).click();
    await expect(acmeView.getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
    await expect(user).toHaveURL(recordURL);
    await applicationPages.getByRole("button", { name: "Orders", exact: true }).click();
    const ordersPage = user.getByRole("main").getByRole("region", { name: "Orders", exact: true });
    await expect(ordersPage.getByRole("cell", { name: "Acme", exact: true })).toBeVisible();
    await expect(ordersPage.getByRole("cell", { name: "Globex", exact: true })).toBeVisible();
    await expect(user).toHaveURL(recordURL);
    await applicationPages.getByRole("button", { name: "Order object view", exact: true }).click();
    await expect(acmeView.getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
    await expect(user).toHaveURL(recordURL);
    await expect(globexView.getByRole("heading", { name: "Globex", exact: true })).toBeVisible();
    await expect(globexView.getByRole("heading", { name: "Acme", exact: true })).toHaveCount(0);
    const edit = (await read<{ schema: string; title: string }[]>("/v1/actions")).find(action => action.schema === `build.${objectName}.edit`)!;
    expect(edit).toBeDefined();
    await acmeView.getByRole("button", { name: "Edit", exact: true }).click();
    const editForm = user.getByRole("dialog", { name: `${edit.title} ${acme.id}`, exact: true });
    await expect(editForm.getByRole("textbox", { name: /^Customer/ })).toHaveValue("Acme");
    await editForm.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(editForm).toBeHidden();
    await acmeView.getByRole("button", { name: "More", exact: true }).click();
    await expect(user.getByRole("menuitem", { name: "Archive", exact: true })).toBeVisible();
    await user.keyboard.press("Escape");
    const acceptedResponse = user.waitForResponse(response => response.url().endsWith("/v1/submissions") && response.request().method() === "POST" && response.request().postDataJSON().target.type === `build.${objectName}`);
    await acmeView.getByRole("button", { name: "Step 1", exact: true }).click();
    const response = await acceptedResponse;
    expect(response.request().postDataJSON()).toMatchObject({ target: { type: `build.${objectName}`, id: acme.id }, schema: { name: `build.${objectName}.action`, version: 1 }, expectedRevision: acme.revision });
    const accepted = await response.json();
    expect(response.ok(), JSON.stringify(accepted)).toBeTruthy();
    expect(accepted.error).toBeUndefined();
    expect(accepted.record?.changeId).toEqual(expect.any(String));
    expect(accepted.record.changeId).not.toBe("");
    await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.${objectName}/${acme.id}`)).record.revision).toBe(acme.revision + 1);
    expect((await read<{ record: { revision: number; customer: string } }>(`/v1/records/build.${objectName}/${globex.id}`)).record).toMatchObject({ revision: globex.revision, customer: "Globex" });
    await expect(globexView.getByRole("heading", { name: "Globex", exact: true })).toBeVisible();
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
        await expect(row.getByText(`${title} · ${kind === "page" ? "Pages" : kind === "app" ? "Applications" : "Action types"}`, { exact: true })).toBeVisible();
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

  await test.step("11 published object views isolate drafts and restore the generated view after saving", async () => {
    const order = (await read<{ records: { id: string; customer: string }[] }>(`/v1/records/build.${objectName}?limit=500`)).records.find(record => record.customer === "Acme")!;
    const user = await (await browser.newContext()).newPage();
    const route = `/record?id=${order.id}&type=build.${objectName}`;
    const activate = async () => {
      await page.reload();
      await expect(page.getByRole("button", { name: "Review and save", exact: true })).toBeEnabled();
      const response = page.waitForResponse(answer => answer.url().endsWith("/v1/releases/preview") && answer.request().method() === "POST");
      await page.getByRole("button", { name: "Review and save", exact: true }).click();
      const previewResponse = await response;
      expect(previewResponse.ok(), await previewResponse.text()).toBeTruthy();
      const preview = await previewResponse.json() as ReleasePreview;
      expect(preview.diagnostic).toBeUndefined();
      for (const draft of preview.draftReviews!) {
        expect(draft.checked).toBe(true);
        expect(draft.issues).toEqual([]);
      }
      await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
      await page.getByRole("button", { name: "Activate release", exact: true }).click();
      await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
    };
    try {
      await signIn(user, "user", route);
      await expect(user.getByRole("region", { name: "Order object view", exact: true }).getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
      const view = (await read<{ record: { revision: number } }>(`/v1/records/build.page/${viewID}`)).record;
      await submit("build.page", viewID, "edit", { title: "Revised order object view" }, view.revision);
      await user.reload();
      await expect(user.getByRole("region", { name: "Order object view", exact: true }).getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
      await expect(user.getByRole("region", { name: "Revised order object view", exact: true })).toHaveCount(0);
      await activate();
      await user.reload();
      await expect(user.getByRole("region", { name: "Revised order object view", exact: true }).getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
      await expect(user.getByRole("region", { name: "Order object view", exact: true })).toHaveCount(0);
      await page.goto(`/#/object-type?id=${objectID}`);
      await page.getByRole("navigation", { name: "Order", exact: true }).getByRole("button", { name: "Display", exact: true }).click();
      const clearing = page.waitForResponse(response => response.url().endsWith("/v1/submissions") && response.request().method() === "POST" && response.request().postDataJSON().target.type === "build.object" && response.request().postDataJSON().target.id === objectID && response.request().postDataJSON().schema.name === "build.object.save");
      await page.getByRole("combobox", { name: "Object view", exact: true }).selectOption({ label: "Default generated view" });
      await saveCurrentResource();
      const cleared = await clearing;
      expect(JSON.parse(Buffer.from(cleared.request().postDataJSON().payload, "base64").toString())).toMatchObject({ spec:{objectView: null} });
      const answer = await cleared.json();
      expect(cleared.ok(), JSON.stringify(answer)).toBeTruthy();
      expect(answer.error).toBeUndefined();
      await expect.poll(async () => (await read<{ record: { objectView?: ObjectView } }>(`/v1/records/build.object/${objectID}`)).record.objectView).toBeUndefined();
      const object = (await read<{ record: { published: string } }>(`/v1/records/build.object/${objectID}`)).record;
      expect(JSON.parse(object.published).objectView).toEqual(objectView);
      await user.reload();
      await expect(user.getByRole("region", { name: "Revised order object view", exact: true }).getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
      await activate();
      await user.reload();
      await expect(user.getByRole("heading", { name: "Acme", exact: true })).toBeVisible();
      await expect(user.getByRole("region", { name: "Revised order object view", exact: true })).toHaveCount(0);
      await expect(user.getByRole("button", { name: "Edit", exact: true })).toBeVisible();
      const restored = (await read<{ record: { published: string } }>(`/v1/records/build.object/${objectID}`)).record;
      expect(JSON.parse(restored.published).objectView).toBeUndefined();
    } finally {
      await user.context().close();
    }
  });

  await test.step("12 reject object view interfaces for the wrong object or extra required inputs", async () => {
    const original = (await read<{ record: { revision: number; inputSchema: PageInputSchema } }>(`/v1/records/build.page/${viewID}`)).record;
    const object = (await read<{ record: { revision: number } }>(`/v1/records/build.object/${objectID}`)).record;
    await submit("build.object", objectID, "edit", { objectView }, object.revision);
    try {
      for (const variant of ["wrong object", "extra input"]) {
        const inputSchema = structuredClone(original.inputSchema);
        const inputs = inputSchema.interface!.inputs!;
        if (variant === "wrong object") inputs.currentObject!.object = { app: "build", kind: "object", name: "build.app" };
        else {
          inputSchema.variables!.confirmation = { title: "Confirmation", scope: "page", type: "string", mode: "input" };
          inputs.confirmation = { type: "string", variable: "confirmation", required: true };
        }
        const current = (await read<{ record: { revision: number } }>(`/v1/records/build.page/${viewID}`)).record;
        await submit("build.page", viewID, "edit", { inputSchema }, current.revision);
        await page.reload();
        const response = page.waitForResponse(answer => answer.url().endsWith("/v1/releases/preview") && answer.request().method() === "POST");
        await page.getByRole("button", { name: "Review and save", exact: true }).click();
        const previewResponse = await response;
        expect(previewResponse.ok(), await previewResponse.text()).toBeTruthy();
        const preview = await previewResponse.json() as ReleasePreview;
        expect(JSON.stringify(preview)).toContain(variant === "wrong object" ? "required currentObject record input for its bound object type" : "cannot require inputs other than currentObject");
        expect(preview.diagnostic || preview.draftReviews?.some(draft => draft.issues.length > 0)).toBeTruthy();
        await expect(page.getByRole("button", { name: "Save immutable candidate", exact: true })).toHaveCount(0);
      }
    } finally {
      for (const draft of [{ kind: "page", type: "build.page", id: viewID }, { kind: "object", type: "build.object", id: objectID }]) {
        const restore = await read<ReleaseDraftRestore>(`/v1/releases/drafts/restore?kind=${draft.kind}&id=${draft.id}`);
        const current = await read<{ record: { revision: number } }>(`/v1/records/${draft.type}/${draft.id}`);
        await submit(restore.type, draft.id, restore.action, restore.payload, current.record.revision);
      }
    }
  });

});
