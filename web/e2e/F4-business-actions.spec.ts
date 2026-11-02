import { expect, signIn, test, unique } from "./kit";
import type { ReleasePreview } from "../packages/kernel/src/gen/host";

test.use({ actionTimeout: 15_000 });

test("F4 a business user finds an object, runs an action, and sees it in the activity", async ({ page, browser }) => {
  test.setTimeout(240_000);
  const name = unique("tickets");
  const objectName = `${name}ticket`;
  const type = `build.${objectName}`;
  const title = `Ticket ${name.slice(7)}`;
  const headers = { authorization: "Bearer builder" };
  let objectID = "", recordID = "", authority = "";

  const read = async <T,>(path: string): Promise<T> => {
    const response = await page.request.get(path, { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    return response.json();
  };

  const schemaVersions = new Map<string, { version: string; family: number }>();
  const schemaVersionOf = async (schema: string) => {
    if (!schemaVersions.has(schema)) {
      const catalog = await read<{ schema: string; schemaVersion?: { version: string; family: number } }[]>("/v1/actions");
      for (const entry of catalog) if (entry.schemaVersion) schemaVersions.set(entry.schema, entry.schemaVersion);
    }
    return schemaVersions.get(schema);
  };

  const submit = async (target: { type: string; id: string }, auth: string, action: string, payload: unknown, expectedRevision: number) => {
    const identity = await read<{ tenantId: string; principalId: string }>("/v1/me");
    const schema = `${target.type}.${action}`;
    const version = await schemaVersionOf(schema);
    const response = await page.request.post("/v1/submissions", {
      headers: { ...headers, ...(version ? { "Platform-Schema-Version": version.version } : {}) },
      data: {
        tenantId: identity.tenantId, principalId: identity.principalId, authority: auth, target,
        schema: { name: schema, version: version?.family ?? 1 }, idempotencyKey: unique("submit"),
        payload: Buffer.from(JSON.stringify(payload)).toString("base64"), expectedRevision,
      },
    });
    const answer = await response.json();
    expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
    expect(answer.error).toBeUndefined();
    expect(answer.record?.changeId).toEqual(expect.any(String));
    expect(answer.record.changeId).not.toBe("");
    return answer.record as { changeId: string };
  };

  const publish = async (kind: string, id: string) => {
    const drafts = [{ kind, id }];
    const previewResponse = await page.request.post("/v1/releases/preview", { headers, data: { drafts } });
    expect(previewResponse.ok(), await previewResponse.text()).toBeTruthy();
    const preview = await previewResponse.json() as ReleasePreview;
    expect(preview.diagnostic).toBeUndefined();
    expect(preview.candidateId).toEqual(expect.any(String));
    for (const draft of preview.draftReviews ?? []) {
      expect(draft.checked).toBe(true);
      expect(draft.issues).toEqual([]);
    }
    for (const path of ["/v1/releases/candidates", "/v1/releases/active"]) {
      const response = await page.request.post(path, { headers, data: { ...(path === "/v1/releases/candidates" ? { drafts } : {}), candidateId: preview.candidateId, key: unique("publish") } });
      const answer = await response.json();
      expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
      expect(answer.id).toBe(preview.candidateId);
    }
  };

  await test.step("setup: a builder defines and publishes an object with actions a business user may take", async () => {
    await signIn(page, "builder", "/projects");
    objectID = unique("OBJ");
    await submit({ type: "build.object", id: objectID }, "build", "create", {
      name: objectName, title: "Ticket", plural: "Tickets", display: "subject",
      fields: [
        { name: "subject", title: "Subject", type: "text", required: true, search: true },
        { name: "note", title: "Resolution note", type: "text" },
        { name: "priority", title: "Priority", type: "choice", choices: "low,high" },
      ],
      states: [{ name: "open", title: "Open" }],
      actions: [
        { name: "resolve", title: "Resolve ticket", description: "Record how the ticket was resolved.", from: ["open"], to: "",
          inputs: [{ name: "note", title: "Resolution note", type: "text", required: true }], sets: [{ field: "note", from: "note" }] },
      ],
    }, 0);
    await expect.poll(async () => (await read<{ record: { revision: number } }>(`/v1/records/build.object/${objectID}`)).record.revision).toBe(1);
    const draft = (await read<{ record: { actions?: unknown[]; actionTypes: { ref: { name: string } }[] } }>(`/v1/records/build.object/${objectID}`)).record;
    expect(draft.actions ?? []).toEqual([]);
    expect(draft.actionTypes).toHaveLength(1);
    await submit({ type: "build.actiontype", id: unique("ACT") }, "build", "create", {
      name: `${type}.escalate`, object: type,
      definition: { name: "escalate", title: "Escalate ticket", description: "Escalate a high-priority ticket.", from: ["open"], to: "",
        conditions: [{ field: "priority", operator: "=", value: "high", message: "Only high-priority tickets can be escalated." }],
        inputs: [{ name: "reason", title: "Escalation reason", type: "text", required: true }], sets: [{ field: "note", from: "reason" }] },
    }, 0);
    await publish("object", objectID);
    const action = (await read<{ record: { id: string; version: number } }>(`/v1/records/build.actiontype/${type}.resolve`)).record;
    expect(action.version).toBe(1);
    await page.goto(`/#/action-type?id=${encodeURIComponent(action.id)}`);
    await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Resolve ticket now");
    await page.getByRole("group", { name: "Branch", exact: true }).getByRole("button", { name: "Review and save", exact: true }).click();
    const review = page.getByRole("dialog", { name: "Review working state", exact: true });
    await review.getByRole("button", { name: "Save to main", exact: true }).click();
    await expect(review).toBeHidden();
    await expect.poll(async () => (await read<{ record: { version: number } }>(`/v1/records/build.actiontype/${action.id}`)).record.version).toBe(2);
    await publish("action", action.id);
    const retained = (await read<{ record: { versions: { definition: { title: string } }[] } }>(`/v1/records/build.actiontype/${action.id}`)).record;
    expect(retained.versions.map(version => version.definition.title)).toEqual(["Resolve ticket", "Resolve ticket now"]);
    const installed = await read<{ record: { published: string } }>(`/v1/records/build.object/${objectID}`);
    expect(installed.record.published).not.toBe("");
    const declarations = await read<{ tenantId: string; dataClass: string; authorityId: string }[]>("/v1/declarations");
    const identity = await read<{ tenantId: string }>("/v1/me");
    authority = declarations.find(d => d.tenantId === identity.tenantId && d.dataClass === type)!.authorityId;
    expect(authority).not.toBe("");
    recordID = unique("TCK");
    const created = await submit({ type, id: recordID }, authority, "create", { subject: title }, 0);
    expect(created.changeId).not.toBe("");
    const record = (await read<{ record: { id: string; state: string; subject: string } }>(`/v1/records/${type}/${recordID}`)).record;
    expect(record).toMatchObject({ id: recordID, state: "open", subject: title });
  });

  const user = await (await browser.newContext()).newPage();
  try {
    await test.step("1 the business user reaches the object within three interactions with ⌘K", async () => {
      await signIn(user, "user");
      await user.keyboard.press("ControlOrMeta+K");
      const palette = user.getByRole("dialog", { name: "Command palette" });
      await expect(palette).toBeVisible();
      await palette.getByRole("combobox").fill(title);
      const option = palette.getByRole("option", { name: new RegExp(`${title}\\b.*${recordID}`) });
      await expect(option).toBeVisible({ timeout: 30_000 });
      await option.click();
      await expect(user).toHaveURL(new RegExp(`/#/record\\?.*id=${recordID}`));
      await expect(user.getByRole("main").getByRole("heading", { name: title, level: 1 })).toBeVisible({ timeout: 30_000 });
    });

    await test.step("2 the action bar opens the action form and a button named for the action runs it", async () => {
      const view = user.getByRole("main");
      const escalate = view.getByRole("button", { name: "Escalate ticket", exact: true });
      await expect(escalate).toBeDisabled();
      await expect(view.getByText("Only high-priority tickets can be escalated.", { exact: false })).toBeVisible();

      await view.getByRole("button", { name: "Resolve ticket now", exact: true }).click();
      const form = user.getByRole("dialog", { name: `Resolve ticket now ${recordID}`, exact: true });
      await expect(form).toBeVisible();
      await form.getByRole("textbox", { name: /^Resolution note/ }).fill("Closed after a call with the customer.");
      const accepted = user.waitForResponse(response => response.url().endsWith("/v1/submissions") && response.request().method() === "POST"
        && response.request().postDataJSON().schema.name === `${type}.resolve`);
      await form.getByRole("button", { name: "Resolve ticket now", exact: true }).click();
      const response = await accepted;
      const answer = await response.json();
      expect(response.ok(), JSON.stringify(answer)).toBeTruthy();
      expect(answer.error).toBeUndefined();
      expect(answer.record?.changeId).toEqual(expect.any(String));
      await expect(form).toBeHidden();
      await expect.poll(async () => (await read<{ record: { note?: string; revision: number } }>(`/v1/records/${type}/${recordID}`)).record.note).toBe("Closed after a call with the customer.");
    });

    await test.step("3 the activity shows the edit immediately", async () => {
      const view = user.getByRole("main");
      await view.getByRole("tab", { name: "History", exact: true }).click();
      const history = view.getByRole("region", { name: "History", exact: true });
      await expect(history.getByText(`${type}.resolve`, { exact: true })).toBeVisible({ timeout: 30_000 });
      await expect(history.getByText("Closed after a call with the customer.")).toBeVisible();
    });
  } finally {
    await user.context().close();
  }

  await test.step("cleanup: archive the business record this case created", async () => {
    const current = (await read<{ record: { revision: number } }>(`/v1/records/${type}/${recordID}`)).record;
    await submit({ type, id: recordID }, authority, "archive", {}, current.revision);
    await expect.poll(async () => (await read<{ record: { archived?: boolean } }>(`/v1/records/${type}/${recordID}`)).record.archived).toBe(true);
  });
});
