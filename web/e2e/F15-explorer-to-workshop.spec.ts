import { expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F15 an object selected in Object Explorer is sent to Workshop as a real object set", async ({ page }) => {
  test.setTimeout(240_000);
  const tag = unique("ws");
  const objectName = `obj${tag}`;
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
  await test.step("setup: a builder defines and publishes an object type with one record", async () => {
    await signIn(page, "builder", "/projects");
    await submit({ type: "build.object", id: objectID }, "create", {
      name: objectName, title: `Object ${tag}`, plural: "Objects", display: "subject",
      fields: [{ name: "subject", title: "Subject", type: "text", required: true }],
      states: [{ name: "open", title: "Open" }],
    }, 0);
    await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.object/${objectID}`)).record.revision).toBe(1);
    const preview = await page.request.post("/v1/releases/preview", { headers, data: { drafts: [{ kind: "object", id: objectID }] } });
    expect(preview.ok(), await preview.text()).toBeTruthy();
    const { candidateId } = await preview.json();
    await page.request.post("/v1/releases/candidates", { headers, data: { drafts: [{ kind: "object", id: objectID }], candidateId, key: unique("publish") } });
    await page.request.post("/v1/releases/active", { headers, data: { candidateId, key: unique("publish") } });
    await submit({ type: `build.${objectName}`, id: unique("REC") }, "create", { subject: `Record ${tag}` }, 0);
  });

  await test.step("1 the builder opens Object Explorer, picks the object type and selects a record", async () => {
    await page.goto(`/#/explorer`);
    await expect(page.getByRole("heading", { name: "Object Explorer", level: 1 })).toBeVisible({ timeout: 30_000 });
    await page.getByRole("button", { name: `Object ${tag}`, exact: true }).click();
    await expect(page.getByRole("heading", { name: `Object ${tag}` })).toBeVisible();
    await page.getByRole("row", { name: `Record ${tag}` }).click();
    await expect(page.getByRole("button", { name: "Send to Workshop", exact: true })).toBeVisible();
  });

  await test.step("2 success standard: Send to Workshop writes the object set as a real Workshop variable", async () => {
    const sent = page.waitForResponse(response => response.url().endsWith("/v1/submissions") && response.request().method() === "POST"
      && response.request().postDataJSON().schema.name === "work.view.save");
    await page.getByRole("button", { name: "Send to Workshop", exact: true }).click();
    const saved = await sent;
    expect(saved.ok(), await saved.text()).toBeTruthy();
    const answer = await saved.json();
    const viewID = answer.record?.id ?? answer.changeId;
    expect(viewID).toEqual(expect.any(String));
    await expect.poll(async () => {
      const view = await read<{ record: { state: string } }>(`/v1/records/work.view/${encodeURIComponent(viewID)}`);
      return view.record.state;
    }, { timeout: 30_000 }).toBe("done");
    await page.goto(`/#/explorer?saved=${encodeURIComponent(viewID)}`);
    await expect(page.getByRole("heading", { name: `Object set from ${objectName}`, exact: true })).toBeVisible();
  });
});
