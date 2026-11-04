import { expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F14 a publisher explicitly rolls back an active release to the previous candidate", async ({ page }) => {
  test.setTimeout(240_000);
  const tag = unique("rollback");
  const objectName = `obj${tag}`;
  const headers = { authorization: "Bearer builder" };

  const read = async <T,>(path: string): Promise<T> => {
    const response = await page.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };

  const publishObject = async (id: string, fields: unknown[]) => {
    await submit({ type: "build.object", id }, "create", {
      name: objectName, title: `Object ${tag}`, plural: "Objects", display: "subject",
      fields, states: [{ name: "open", title: "Open" }],
    }, 0);
    const preview = await page.request.post("/v1/releases/preview", { headers, data: { drafts: [{ kind: "object", id }] } });
    expect(preview.ok(), await preview.text()).toBeTruthy();
    const { candidateId } = await preview.json();
    await page.request.post("/v1/releases/candidates", { headers, data: { drafts: [{ kind: "object", id }], candidateId, key: unique("publish") } });
    const active = await page.request.post("/v1/releases/active", { headers, data: { candidateId, key: unique("publish") } });
    expect(active.ok(), await active.text()).toBeTruthy();
    return candidateId;
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
  await test.step("setup: activate release v1 with one field", async () => {
    await signIn(page, "builder", "/release-review");
    await publishObject(objectID, [{ name: "subject", title: "Subject", type: "text", required: true }]);
    const active = await read<{ activeId: string }>("/v1/releases/active");
    const v1 = active.activeId;
    expect(v1).not.toBe("");
    await expect.poll(async () => (await read<{ record: { published?: string } }>(`/v1/records/build.object/${objectID}`)).record.published, { timeout: 30_000 }).not.toBe("");
  });

  await test.step("1 activate release v2 with an added field", async () => {
    const current = await read<{ record: { revision: number } }>(`/v1/records/build.object/${objectID}`);
    await submit({ type: "build.object", id: objectID }, "edit", {
      fields: [
        { name: "subject", title: "Subject", type: "text", required: true },
        { name: "note", title: "Note", type: "text" },
      ],
    }, current.record.revision);
    const preview = await page.request.post("/v1/releases/preview", { headers, data: { drafts: [{ kind: "object", id: objectID }] } });
    expect(preview.ok(), await preview.text()).toBeTruthy();
    const { candidateId } = await preview.json();
    await page.request.post("/v1/releases/candidates", { headers, data: { drafts: [{ kind: "object", id: objectID }], candidateId, key: unique("publish") } });
    const active = await page.request.post("/v1/releases/active", { headers, data: { candidateId, key: unique("publish") } });
    expect(active.ok(), await active.text()).toBeTruthy();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
  });

  await test.step("2 success standard: explicit rollback returns to v1", async () => {
    await page.getByRole("button", { name: /Rollback to release/ }).click();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
    const active = await read<{ activeId: string }>("/v1/releases/active");
    // After rollback the active candidate is the previous one (v1); confirm by re-reading the object definition:
    const installed = await read<{ record: { fields: { name: string }[]; published?: string } }>(`/v1/records/build.object/${objectID}`);
    expect(installed.record.fields.map(f => f.name)).toEqual(["subject"]);
    expect(installed.record.published).not.toBe("");
  });
});
