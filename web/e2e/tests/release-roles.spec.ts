import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("publisher reviews and activates sealed bytes while auditor can only inspect governance", async ({ page, request }) => {
  const suffix = fresh("roles").toLowerCase();
  const publisher = `${suffix}-publisher`, auditor = `${suffix}-auditor`;
  const pubToken = `user:${publisher}@test`, audToken = `user:${auditor}@test`;
  for (const [id, subject, app, role] of [[publisher, pubToken, "build", "publisher"], [auditor, audToken, "platform", "auditor"]]) {
    await decide(request, "manager", "platform", "platform.member.add", { type: "platform.member", id: id! }, { subject });
    await decide(request, "manager", "platform", "platform.member.grant", { type: "platform.member", id: id! }, { app, role });
  }
  const objectID = fresh("OBJ");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, {
    name: suffix.replace(/-/g, ""), title: "Original publisher sample",
    fields: [{ name: "note", title: "Note", type: "text" }], states: [{ name: "open", title: "Open" }],
  });
  await open(page, "manager", `/release-review?kind=object&id=${objectID}`);
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  const id = await page.getByText("Draft candidate:").locator("code").innerText();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await expect(page.getByText("Candidate saved; not active for operators.", { exact: false })).toBeVisible();
  await decide(request, "manager", "build", "build.object.edit", { type: "build.object", id: objectID }, { title: "Changed after sealing" });

  const pub = await page.context().newPage();
  const draftReads: string[] = [];
  pub.on("request", request => { if (new URL(request.url()).pathname.startsWith("/v1/records/build.object")) draftReads.push(request.url()); });
  await open(pub, pubToken, "/release-review?surface=tenant");
  await expect(pub.getByRole("heading", { name: "Release review", exact: true })).toBeVisible();
  await expect(pub.getByRole("button", { name: "Check draft and dependencies", exact: true })).toHaveCount(0);
  await expect(pub.getByRole("button", { name: "Applications", exact: true })).toHaveCount(0);
  await pub.getByLabel("Saved candidate", { exact: true }).selectOption(id);
  await expect(pub.getByText("Saved candidate review", { exact: true })).toBeVisible();
  await pub.getByText("Sealed definitions", { exact: false }).click();
  const sealed = await (await request.get(`/v1/releases/candidates/${encodeURIComponent(id)}`, { headers: { Authorization: `Bearer ${pubToken}` } })).json();
  expect(JSON.stringify(sealed.assets)).toContain("Original publisher sample");
  expect(JSON.stringify(sealed.assets)).not.toContain("Changed after sealing");
  expect((await request.get("/v1/records/build.evaluation?limit=1", { headers: { Authorization: `Bearer ${pubToken}` } })).status()).toBe(200);
  await pub.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(pub.getByText("Release active for operators.", { exact: false })).toBeVisible();
  expect(draftReads).toEqual([]);
  await expect(decide(request, pubToken, "build", "build.object.edit", { type: "build.object", id: objectID }, { title: "Forbidden" })).rejects.toThrow(/POLICY_DENIED/);

  const aud = await page.context().newPage();
  await open(aud, audToken, "/members?surface=tenant");
  await expect(aud.getByRole("heading", { name: "Members and access", exact: true })).toBeVisible();
  await expect(aud.getByRole("button", { name: "Add member", exact: true })).toHaveCount(0);
  await aud.getByText(publisher, { exact: true }).click();
  await expect(aud.getByText("publisher", { exact: true })).toBeVisible();
  await expect(aud.getByRole("combobox", { name: /Role in/ })).toHaveCount(0);
  await aud.goto("/#/apps?surface=tenant");
  await expect(aud.getByRole("heading", { name: "Packages", exact: true })).toBeVisible();
  await expect(aud.getByText("No controlled packages installed", { exact: true })).toBeVisible();
  await aud.goto("/#/audit?surface=tenant");
  await expect(aud.getByRole("heading", { name: "Audit", exact: true })).toBeVisible();
  await expect(decide(request, audToken, "platform", "platform.member.grant", { type: "platform.member", id: publisher }, { app: "build", role: "builder" })).rejects.toThrow(/POLICY_DENIED/);
  expect((await request.get("/v1/releases/candidates?limit=1", { headers: { Authorization: `Bearer ${audToken}` } })).status()).toBe(403);
});
