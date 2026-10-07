import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("untouched project settings follow another editor without submitting stale values", async ({ page, context, request }) => {
  const id = fresh("PROJECT-SYNC");
  await decide(request, "manager", "build", "build.app.create", { type: "build.app", id }, {
    name: id.toLowerCase().replace(/[^a-z0-9]/g, ""), title: "Project settings sync", description: "Original description",
  });
  const other = await context.newPage();
  const writes: Record<string, unknown>[] = [];
  for (const editor of [page, other]) {
    editor.on("request", (r) => {
      if (!r.url().endsWith("/v1/submissions") || r.method() !== "POST") return;
      const body = r.postDataJSON();
      if (body.schema?.name === "build.app.edit" && body.target?.id === id) writes.push(JSON.parse(Buffer.from(body.payload, "base64").toString()));
    });
    await open(editor, "manager", `/project?id=${id}`);
    await editor.getByRole("tab", { name: "Settings", exact: true }).click();
    await expect(editor.getByRole("textbox", { name: "Description", exact: true })).toHaveValue("Original description");
  }
  await page.waitForTimeout(2200);
  expect(writes).toHaveLength(0);
  await page.getByRole("textbox", { name: "Description", exact: true }).fill("Updated in the first editor");
  await expect.poll(async () => (await (await request.get(`/v1/records/build.app/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record.description).toBe("Updated in the first editor");
  await expect(other.getByRole("textbox", { name: "Description", exact: true })).toHaveValue("Updated in the first editor");
  await other.waitForTimeout(2200);
  expect(writes).toEqual([{ description: "Updated in the first editor" }]);
  await other.getByRole("textbox", { name: "Title", exact: true }).fill("Updated in the second editor");
  await expect(page.getByRole("textbox", { name: "Title", exact: true })).toHaveValue("Updated in the second editor");
  await page.waitForTimeout(2200);
  expect(writes).toEqual([{ description: "Updated in the first editor" }, { title: "Updated in the second editor" }]);
  const saved = (await (await request.get(`/v1/records/build.app/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(saved.description).toBe("Updated in the first editor");
  expect(saved.title).toBe("Updated in the second editor");
  await other.close();
});
