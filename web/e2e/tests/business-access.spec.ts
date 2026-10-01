import { expect, test } from "@playwright/test";
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
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(create.getByRole("alert")).toBeVisible();
  await expect(create.getByRole("textbox", { name: "Note", exact: true })).toHaveValue("Keep my input");
  await create.getByRole("textbox", { name: "ID *", exact: true }).fill(id);
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(create).toHaveCount(0);
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
    await inbox.getByRole("button", { name: "Application launcher", exact: true }).click();
    await expect(inbox.getByRole("heading", { name: /Welcome/ })).toBeVisible();
    await expect(inbox.getByRole("button", { name: "Application Studio", exact: true })).toHaveCount(0);
    const approved = await (await request.get(`/v1/records/${type}/${id}`, { headers: { Authorization: "Bearer desk" } })).json();
    expect(approved.record.state).toBe("approved");
  } finally { await supervisor.close(); }
});
