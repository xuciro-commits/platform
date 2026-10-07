import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("object additions stay local until Save, refusals retain the draft without retry, and actions need a local state", async ({ page, request }) => {
  const id = fresh("OBJECT-SAVE");
  const name = id.toLowerCase().replace(/[^a-z0-9]/g, "");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id }, {
    name, title: "Manual object draft", fields: [{ name: "label", title: "Label", type: "text" }],
  });
  const writes: Record<string, unknown>[] = [];
  let refuse = true;
  await page.route("**/v1/submissions", async (route) => {
    const body = route.request().postDataJSON();
    if (body.schema?.name !== "build.object.edit" || body.target?.id !== id) return route.continue();
    writes.push(JSON.parse(Buffer.from(body.payload, "base64").toString()));
    if (refuse) {
      refuse = false;
      return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "INVALID_ARGUMENT", message: "Review fixture refuses this save" } }) });
    }
    return route.continue();
  });
  await open(page, "manager", `/object-type?id=${id}`);
  const structure = page.getByRole("region", { name: "Object structure", exact: true });
  await expect(structure.getByRole("button", { name: "Add an action", exact: true })).toBeDisabled();
  await structure.getByRole("button", { name: "Add a property", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  await page.waitForTimeout(2200); // More than two intervals of the former autosave timer.
  expect(writes).toHaveLength(0);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Review fixture refuses" })).toBeVisible();
  await page.waitForTimeout(2200);
  expect(writes).toHaveLength(1);
  expect(Object.keys(writes[0])).toEqual(["fields"]); // No unchanged, newer host fields in the wire edit.
  expect((writes[0].fields as { name: string }[]).map((f) => f.name)).toEqual(["label", "field"]);
  await structure.getByRole("button", { name: "Add a state", exact: true }).click();
  await expect(structure.getByRole("button", { name: "Add an action", exact: true })).toBeEnabled();
  await structure.getByRole("button", { name: "Add an action", exact: true }).click();
  await page.waitForTimeout(2200);
  expect(writes).toHaveLength(1);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  expect(writes).toHaveLength(2);
  expect(Object.keys(writes[1]).sort()).toEqual(["actions", "fields", "states"]);
  const saved = (await (await request.get(`/v1/records/build.object/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(saved.fields).toHaveLength(2);
  expect(saved.states).toHaveLength(1);
  expect(saved.actions[0].from).toEqual([saved.states[0].name]);
  await page.reload();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  expect(writes).toHaveLength(2);
});
