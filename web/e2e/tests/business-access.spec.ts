import { expect, test } from "./kit";
import { decide, fresh, open } from "./host";

test("business forms keep refused input and a separate supervisor approves without builder access", async ({ browser, page, request }) => {
  const name = fresh("access").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`;
  const objectID = fresh("OBJ"), taken = fresh("REC"), id = fresh("REC");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, {
    name, title: "Access request", fields: [{ name: "note", title: "Note", type: "text", search: true },
      { name: "value", title: "Protected value", type: "integer", write: ["supervisor"] }],
    access: [{ role: "user", read: "all", create: true, edit: true }, { role: "supervisor", read: "all", edit: true }],
    states: [{ name: "open", title: "Open" }, { name: "pending", title: "Pending" }, { name: "approved", title: "Approved" }],
    actions: [{ name: "submit", title: "Request approval", from: ["open"], to: "approved", roles: ["user"],
      approval: { pending: "pending", levels: [{ title: "Business supervisor", role: "supervisor" }] } }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  await decide(request, "manager", "platform", "platform.member.grant", { type: "platform.member", id: "business-supervisor-1" }, { app: "build", role: "supervisor" });
  await decide(request, "manager", "build", `${type}.create`, { type, id: taken }, { note: "Existing request" });

  await open(page, "desk", `/page?app=build&kind=page&name=${name}`);
  await page.getByRole("button", { name: "Create access request" }).click();
  const create = page.getByRole("dialog");
  await expect(create.getByLabel("Protected value", { exact: true })).toHaveCount(0);
  await create.getByRole("textbox", { name: "ID *", exact: true }).fill(taken);
  await create.getByRole("textbox", { name: "Note", exact: true }).fill("Keep my input");
  const creates: {
    body: { idempotencyKey: string; target: { type: string; id: string }; payload: string };
    correction?: string;
  }[] = [];
  page.on("request", request => {
    if (request.method() !== "POST" || !request.url().endsWith("/v1/submissions")) return;
    const body = request.postDataJSON();
    if (body.schema?.name === `${type}.create`) {
      creates.push({ body, correction: request.headers()["platform-correction-of"] });
    }
  });
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(create.getByRole("alert")).toBeVisible();
  await expect(create.getByRole("textbox", { name: "Note", exact: true })).toHaveValue("Keep my input");
  await create.getByRole("textbox", { name: "ID *", exact: true }).fill(id);
  await create.getByRole("textbox", { name: "Note", exact: true }).fill("Corrected request");
  await create.getByRole("button", { name: "Retry original inputs", exact: true }).click();
  await expect.poll(() => creates.length).toBe(2);
  await expect(create.getByRole("button", { name: "Retry original inputs", exact: true })).toBeEnabled();
  expect(creates[1]!.body).toEqual(creates[0]!.body);
  expect(creates[1]!.body.target).toEqual({ type, id: taken });
  expect(JSON.parse(Buffer.from(creates[1]!.body.payload, "base64").toString()))
    .toMatchObject({ note: "Keep my input" });
  expect(creates[1]!.correction).toBeUndefined();
  await expect(create.getByRole("textbox", { name: "ID *", exact: true })).toHaveValue(id);
  await expect(create.getByRole("textbox", { name: "Note", exact: true })).toHaveValue("Corrected request");
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(create).toHaveCount(0);
  expect(creates).toHaveLength(3);
  expect(creates[2]!.body.idempotencyKey).not.toBe(creates[0]!.body.idempotencyKey);
  expect(creates[2]!.body.target).toEqual({ type, id });
  expect(JSON.parse(Buffer.from(creates[2]!.body.payload, "base64").toString()))
    .toMatchObject({ note: "Corrected request" });
  expect(creates[2]!.correction).toBeUndefined();
  const created = await (await request.get(`/v1/records/${type}/${id}`, { headers: { Authorization: "Bearer desk" } })).json();
  expect(created.record).toMatchObject({ note: "Corrected request", revision: 1 });
  await decide(request, "manager", "build", `${type}.edit`, { type, id }, { value: 7 });
  await page.getByRole("row").filter({ hasText: id }).click();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const edit = page.getByRole("dialog");
  await expect(edit.getByLabel("Protected value", { exact: true })).toHaveCount(0);
  await edit.getByRole("textbox", { name: "Note", exact: true }).fill("Updated note");
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(edit).toHaveCount(0);
  const saved = await (await request.get(`/v1/records/${type}/${id}`, { headers: { Authorization: "Bearer desk" } })).json();
  expect(saved.record).toMatchObject({ note: "Updated note", value: 7 });
  await expect(decide(request, "desk", "build", "build.object.edit", { type: "build.object", id: objectID }, { title: "Unauthorized" })).rejects.toThrow(/POLICY_DENIED/);
  await page.getByRole("button", { name: "Request approval", exact: true }).click();

  const supervisor = await browser.newContext({ baseURL: "http://127.0.0.1:18496", locale: "en-US" });
  try {
    const inbox = await supervisor.newPage();
    await open(inbox, "business-supervisor", "/inbox");
    await inbox.getByRole("listitem").filter({ hasText: id }).getByRole("button", { name: "Approve", exact: true }).click();
    const actions = await (await request.get("/v1/actions", { headers: { Authorization: "Bearer business-supervisor" } })).json();
    expect(actions.some((action: { schema: string }) => action.schema.startsWith("build.object."))).toBe(false);
    await inbox.getByRole("button", { name: "Home", exact: true }).first().click();
    await expect(inbox.getByRole("heading", { name: /^Good (morning|afternoon|evening)/ })).toBeVisible();
    await expect(inbox.getByRole("button", { name: "Projects", exact: true })).toHaveCount(0);
    const approved = await (await request.get(`/v1/records/${type}/${id}`, { headers: { Authorization: "Bearer desk" } })).json();
    expect(approved.record.state).toBe("approved");
  } finally { await supervisor.close(); }
});
