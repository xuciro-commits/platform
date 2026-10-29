import { expect, fresh, shots, slug, test } from "./kit";
import type * as Api from "../../packages/kernel/src/gen/host";

test("source mappings retain both editors' input and use only permitted model metadata", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const reference = await builder.object({ title: "Mapping reference", fields: [{ name: "name", title: "Name", type: "text" }] });
  const object = await builder.object({
    title: "Mapping target",
    fields: [{ name: "parent", title: "Published parent", type: "reference", ref: reference.type, required: true }],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  await builder.decide("build.object.edit", { type: "build.object", id: object.id }, {
    fields: [
      { name: "parent", title: "Saved parent draft", type: "reference", ref: reference.type, required: true },
      { name: "private", title: "Draft-only field", type: "text" },
    ],
    constraints: [{ name: "parent-required", kind: "required", fields: ["parent"], message: "The original parent must be present." }],
  });
  const source = fresh("SOURCE");
  await builder.decide("build.source.create", { type: "build.source", id: source }, {
    name: slug("mapping"), title: "Source mapping task", url: "https://example.com/rows", object: object.type, key: "id", mapping: [{ from: "parent", to: "parent" }],
  });
  const member = await operator.read<Api.MeView>("me");
  const originalRoles = new Set([member.profile.roles.build, ...(member.profile.grants ?? []).filter(grant => grant.app === "build").map(grant => grant.role)]);
  const addedRoles = new Set<string>();
  const revoke = (role: string) => builder.decide("platform.member.revoke", { type: "platform.member", id: member.principalId }, { app: "build", role }, "platform");
  const grant = async (role: string) => {
    if (originalRoles.has(role)) return;
    await builder.decide("platform.member.grant", { type: "platform.member", id: member.principalId }, { app: "build", role }, "platform");
    addedRoles.add(role);
  };
  try {
    await grant("builder");
    await operator.open(page, `/data-source?id=${source}`);
    await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
    const mapping = page.getByRole("group", { name: "Field mapping 1", exact: true });
    await expect(page.getByText("Saved model draft", { exact: true })).toBeVisible();
    await expect(mapping.getByRole("combobox", { name: "Object field", exact: true }).locator("option:checked")).toHaveText("Saved parent draft");
    await expect(mapping.getByRole("term").filter({ hasText: /^Field type$/ })).toBeVisible();
    await expect(mapping.getByRole("definition").filter({ hasText: /^reference$/ })).toBeVisible();
    await expect(mapping.getByRole("definition").filter({ hasText: /^Yes$/ })).toBeVisible();
    await expect(mapping.getByRole("definition").filter({ hasText: /^Mapping reference$/ })).toBeVisible();
    await expect(mapping.getByRole("list", { name: "Business constraints", exact: true })).toContainText("The original parent must be present.");
    await shots(page, mapping, "source-mapping-details", info);
    await mapping.getByRole("textbox", { name: "Row field", exact: true }).fill("unsaved.parent.path");
    await mapping.getByRole("button", { name: "Edit field definition", exact: true }).click();
    const objectEditor = page.getByRole("region", { name: "Object type", exact: true });
    await objectEditor.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
    const title = objectEditor.getByRole("textbox", { name: "What people call it", exact: true });
    await expect(title).toHaveValue("Saved parent draft");
    await title.fill("Unsaved object field title");
    await objectEditor.getByRole("button", { name: "Return to source", exact: true }).click();
    const confirmation = page.getByRole("dialog", { name: "Unsaved changes", exact: true });
    await expect(confirmation).toBeVisible();
    await confirmation.getByRole("button", { name: "Keep editing", exact: true }).click();
    await expect(title).toHaveValue("Unsaved object field title");
    await objectEditor.getByRole("button", { name: "Return to source", exact: true }).click();
    await confirmation.getByRole("button", { name: "Discard changes", exact: true }).click();
    await expect(mapping.getByRole("textbox", { name: "Row field", exact: true })).toHaveValue("unsaved.parent.path");
    expect((await builder.record("build.source", source)).mapping[0].from).toBe("parent");
    expect((await builder.record("build.object", object.id)).fields[0].title).toBe("Saved parent draft");

    // The same browser has already cached builder drafts. Downgrading its
    // original member must remove those answers without requesting them again.
    await grant("integrator");
    if (addedRoles.has("builder")) await revoke("builder");
    const downgraded = (await operator.read<typeof member>("me")).profile;
    expect(downgraded.roles.build).toBe(member.profile.roles.build);
    expect(downgraded.grants).toEqual(expect.arrayContaining([{ app: "build", role: "integrator", by: expect.any(String), at: expect.any(String) }]));
    expect(downgraded.grants?.some(grant => grant.app === "build" && grant.role === "builder")).toBe(false);
    await expect(page.getByRole("button", { name: "Edit target object", exact: true })).toHaveCount(0);
    await expect(page.getByText("Published model", { exact: true })).toBeVisible();
    await expect(mapping.getByRole("combobox", { name: "Object field", exact: true }).locator("option:checked")).toHaveText("Published parent");
    await expect(mapping.getByRole("combobox", { name: "Object field", exact: true }).locator("option").filter({ hasText: "Draft-only field" })).toHaveCount(0);
    const draftReads: string[] = [];
    page.on("request", request => {
      const url = new URL(request.url());
      if (request.method() === "GET" && (url.pathname === "/v1/records/build.object" || url.pathname === "/v1/build/drafts/actions")) draftReads.push(url.pathname);
    });
    await mapping.getByRole("textbox", { name: "Row field", exact: true }).fill("integrator.parent.path");
    await page.getByRole("button", { name: "Save data source", exact: true }).click();
    await expect.poll(async () => (await builder.record("build.source", source)).mapping[0].from).toBe("integrator.parent.path");
    expect(draftReads).toEqual([]);
  } finally {
    // Grants add up. Remove only this test's grants; the original user grant
    // and its provenance remain untouched throughout the task.
    for (const role of addedRoles) await revoke(role);
  }
});
