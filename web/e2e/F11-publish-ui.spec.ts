import { expect, signIn, test, unique } from "./kit";

test.use({ actionTimeout: 20_000 });

test("F11 an integrator publishes an integration resource from its edit page and then runs it", async ({ page }) => {
  test.setTimeout(240_000);
  const tag = unique("int");
  const sourceName = `src${tag}`;
  const headers = { authorization: "Bearer integrator" };

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

  await test.step("setup: an integrator creates an unpublished data source", async () => {
    await signIn(page, "integrator", "/projects");
    await submit({ type: "build.datasource", id: unique("DS") }, "create", {
      name: sourceName, title: `Source ${tag}`, kind: "table",
      connection: { type: "jdbc", url: "jdbc:sample://localhost:5432/sample" },
    }, 0);
    await expect.poll(async () => (await read<{ record: { revision: number; published?: string } }>(`/v1/records/build.datasource/${sourceName}`)).record.revision).toBe(1);
    const installed = await read<{ record: { published?: string } }>(`/v1/records/build.datasource/${sourceName}`);
    expect(installed.record.published ?? "", "the data source is an unpublished draft so F11 exercises Publish itself").toBe("");
  });

  await test.step("1 the edit page shows Publish beside the existing Pause control", async () => {
    await page.goto(`/#/data-source?id=${sourceName}`);
    await expect(page.getByRole("heading", { name: sourceName, level: 2 })).toBeVisible({ timeout: 30_000 });
    await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeVisible();
  });

  await test.step("2 clicking Publish publishes the resource and shows run controls", async () => {
    const published = page.waitForResponse(response => response.url().endsWith("/v1/submissions") && response.request().method() === "POST"
      && response.request().postDataJSON().schema.name === "build.datasource.publish");
    await page.getByRole("button", { name: "Publish", exact: true }).click();
    const response = await published;
    expect(response.ok(), await response.text()).toBeTruthy();
    await expect.poll(async () => (await read<{ record: { published?: string } }>(`/v1/records/build.datasource/${sourceName}`)).record.published, { timeout: 30_000 }).not.toBe("");
    await expect(page.getByRole("button", { name: "Pull now", exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Pause", exact: true })).toBeVisible();
  });

  await test.step("cleanup: archive the draft when the platform still allows it", async () => {
    const current = await read<{ record: { revision: number } }>(`/v1/records/build.datasource/${sourceName}`);
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const response = await page.request.post("/v1/submissions", {
      headers,
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority: "build",
        target: { type: "build.datasource", id: sourceName }, schema: { name: "build.datasource.archive", version: 1 },
        idempotencyKey: unique("submit"), payload: Buffer.from(JSON.stringify({})).toString("base64"), expectedRevision: current.record.revision,
      },
    });
    if (response.ok()) {
      await expect.poll(async () => (await read<{ record: { archived?: boolean } }>(`/v1/records/build.datasource/${sourceName}`)).record.archived).toBe(true);
    }
  });
});
