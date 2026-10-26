import { expect, test } from "./kit";
import { decide, fresh, open } from "./host";
import type * as Api from "../../packages/kernel/src/gen/host";

test("publisher reviews and activates sealed bytes while auditor can only inspect governance", async ({ page, request }) => {
  test.setTimeout(90_000);
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
  await page.getByRole("button", { name: "Candidate identity", exact: true }).click();
  const id = await page.getByRole("term").filter({ hasText: /^Draft candidate$/ }).locator("xpath=following-sibling::dd").locator("code").innerText();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await expect(page.getByText("Candidate saved; not active for operators.", { exact: false })).toBeVisible();
  await decide(request, "manager", "build", "build.object.edit", { type: "build.object", id: objectID }, { title: "Changed after sealing" });

  const pub = await page.context().newPage();
  const draftReads: string[] = [];
  pub.on("request", request => {
    const path = new URL(request.url()).pathname;
    if (path.startsWith("/v1/records/build.") && !path.startsWith("/v1/records/build.evaluation") || path.startsWith("/v1/releases/drafts/") || path.startsWith("/v1/build/drafts/")) draftReads.push(request.url());
  });
  await open(pub, pubToken, "/release-review?surface=tenant");
  const review = pub.getByRole("region", { name: "Release review", exact: true });
  await expect(pub.getByRole("heading", { name: "Release review", exact: true })).toBeVisible();
  await expect(pub.getByRole("button", { name: "Check draft and dependencies", exact: true })).toHaveCount(0);
  await expect(pub.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Applications", exact: true })).toHaveCount(0);
  await pub.getByLabel("Saved candidate", { exact: true }).selectOption(id);
  await expect(review.getByText("Saved candidate review", { exact: true })).toBeVisible();
  await open(pub, pubToken, `/release-review?candidate=${encodeURIComponent(id)}&surface=tenant`);
  await expect(review.getByText("Saved candidate review", { exact: true })).toBeVisible();
  await pub.reload();
  await expect(review.getByText("Saved candidate review", { exact: true })).toBeVisible();
  await pub.getByRole("button", { name: "Candidate identity", exact: true }).click();
  await expect(pub.getByRole("term").filter({ hasText: /^Saved candidate$/ }).locator("xpath=following-sibling::dd").locator("code")).toHaveText(id);
  await expect(pub.getByRole("button", { name: "Check draft and dependencies", exact: true })).toHaveCount(0);
  await review.getByText("Sealed definitions", { exact: false }).click();
  const sealed = await (await request.get(`/v1/releases/candidates/${encodeURIComponent(id)}`, { headers: { Authorization: `Bearer ${pubToken}` } })).json();
  expect(JSON.stringify(sealed.assets)).toContain("Original publisher sample");
  expect(JSON.stringify(sealed.assets)).not.toContain("Changed after sealing");
  expect((await request.get("/v1/records/build.evaluation?limit=1", { headers: { Authorization: `Bearer ${pubToken}` } })).status()).toBe(200);
  await pub.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(review.getByText("Release active for operators.", { exact: false })).toBeVisible();
  expect(draftReads).toEqual([]);
  await expect(decide(request, pubToken, "build", "build.object.edit", { type: "build.object", id: objectID }, { title: "Forbidden" })).rejects.toThrow(/POLICY_DENIED/);
  await pub.close();

  const aud = await page.context().newPage();
  await open(aud, audToken, "/members?surface=tenant");
  await expect(aud.getByRole("heading", { name: "Members and access", exact: true })).toBeVisible();
  await expect(aud.getByRole("button", { name: "Add member", exact: true })).toHaveCount(0);
  await aud.getByText(publisher, { exact: true }).click();
  await expect(aud.getByText("publisher", { exact: true })).toBeVisible();
  await expect(aud.getByRole("combobox", { name: /Role in/ })).toHaveCount(0);
  await open(aud, audToken, "/apps?surface=tenant");
  await expect(aud.getByRole("heading", { name: "Packages", exact: true })).toBeVisible();
  const inventory = await request.get("/v1/packages", { headers: { Authorization: `Bearer ${audToken}` } });
  expect(inventory.status()).toBe(200);
  const packages: Api.PackageView[] = await inventory.json();
  await expect(aud.getByRole("button", { name: "Add package", exact: true })).toHaveCount(0);
  if (packages.length) {
    const ids = [...new Set(packages.map(item => item.descriptor.id))].sort();
    const library = aud.getByRole("region", { name: "Package library", exact: true });
    await expect(library.getByRole("button")).toHaveCount(ids.length);
    const selected = packages.find(item => item.descriptor.id === ids[0])!;
    await library.getByRole("button").filter({ has: aud.getByText(selected.descriptor.id, { exact: true }) }).click();
    const details = aud.getByRole("region", { name: `Package ${selected.descriptor.id}`, exact: true });
    await details.getByRole("combobox", { name: "Exact package version", exact: true }).selectOption(selected.descriptor.version);
    await expect(details).toBeVisible();
    await details.getByRole("button", { name: "Technical details", exact: true }).click();
    await expect(details.getByRole("term").filter({ hasText: /^Source$/ })).toBeVisible();
    await expect(details.getByRole("button", { name: /^(Install|Reinstall|Prepare to uninstall|Uninstall|Remove source|Release protocol resources|Create application drafts|Switch to .+)$/ })).toHaveCount(0);
  } else {
    await expect(aud.getByText("This library is empty. Download a package or import a local file to begin.", { exact: true })).toBeVisible();
  }
  await open(aud, audToken, "/audit?surface=tenant");
  await expect(aud.getByRole("heading", { name: "Audit", exact: true })).toBeVisible();
  await expect(decide(request, audToken, "platform", "platform.member.grant", { type: "platform.member", id: publisher }, { app: "build", role: "builder" })).rejects.toThrow(/POLICY_DENIED/);
  expect((await request.get("/v1/releases/candidates?limit=1", { headers: { Authorization: `Bearer ${audToken}` } })).status()).toBe(403);

  await open(aud, audToken, "/permissions?surface=tenant");
  await aud.getByLabel("Member", { exact: true }).selectOption(publisher);
  await aud.getByLabel("Permission", { exact: true }).fill("build.object.edit");
  await aud.getByLabel("Object type", { exact: true }).fill("build.object");
  await aud.getByLabel("Record ID", { exact: true }).fill(objectID);
  await aud.getByRole("button", { name: "Explain", exact: true }).click();
  await expect(aud.getByText("Operation grant", { exact: true })).toBeVisible();
  await expect(aud.getByText("No role or policy grants this operation", { exact: true })).toHaveCount(2);
  await expect(aud.getByText("Final allow", { exact: true })).toHaveCount(0);
  await aud.getByLabel("Permission", { exact: true }).fill("");
  await aud.getByLabel("Object type", { exact: true }).fill("build.evaluation");
  await aud.getByLabel("Record ID", { exact: true }).fill("");
  await aud.getByRole("button", { name: "Explain", exact: true }).click();
  await expect(aud.getByText("Source closure", { exact: true })).toBeVisible();
  await expect(aud.getByText("Final allow", { exact: true })).toBeVisible();
});
