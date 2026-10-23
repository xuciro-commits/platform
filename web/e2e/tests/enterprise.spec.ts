import { expect, fresh, shots, test } from "./kit";

test("enterprise daily management preserves identity, usage history and source ownership", async ({ page, builder }, info) => {
  test.setTimeout(120_000);
  const suffix = fresh("enterprise").toLowerCase();
  let parent = "";
  const child = `child-${suffix}`, mistake = `mistake-${suffix}`, external = `source-${suffix}`;
  const name = `Operations ${suffix}`, renamed = `Operations renamed ${suffix}`;
  const day = new Date().toISOString().slice(0, 10);
  const yesterday = new Date(Date.now() - 86_400_000).toISOString().slice(0, 10);
  await builder.decide("enterprise.kind.add", { type: "enterprise.model", id: `management-${suffix}` }, { name: "Management", kind: "management" }, "enterprise");
  const model = () => builder.read<any>("enterprise");
  const me = await builder.read<any>("me");
  await builder.decide("platform.member.grant", { type: "platform.member", id: me.principalId }, { app: "core", role: "steward" }, "platform");
  await builder.open(page, "/enterprise");
  await page.getByLabel("As of", { exact: true }).fill("2026-01-01");
  await page.getByRole("button", { name: "Add", exact: true }).click();
  await page.getByRole("button").filter({ hasText: "ActualOrganization" }).click();
  const creation = page.getByRole("dialog", { name: "New Organisation", exact: true });
  await creation.getByRole("textbox", { name: "Name", exact: true }).fill(name);
  await creation.getByRole("combobox", { name: "Kind", exact: true }).fill("team");
  await creation.getByRole("button", { name: "Add", exact: true }).click();
  await expect(creation).toHaveCount(0);
  await expect.poll(async () => {
    parent = (await model()).elements.find((element: any) => element.name === name)?.id ?? "";
    return parent;
  }).not.toBe("");
  await page.getByLabel("As of", { exact: true }).fill(day);
  await builder.decide("enterprise.element.edit", { type: "enterprise.element", id: parent }, { shortName: "OPS", calendar: "office" }, "enterprise");
  await builder.open(page, `/enterprise?element=${parent}&section=organization`);
  const inspector = page.getByRole("region", { name: "Inspector", exact: true });
  const nameInput = inspector.getByRole("textbox", { name: "Name", exact: true });
  await expect(nameInput).toHaveValue(name);
  await nameInput.focus();
  await nameInput.press("Tab");
  await expect(inspector.getByRole("combobox", { name: "Kind", exact: true })).toBeFocused();
  await shots(page, page.locator("body"), "enterprise-management-en", info);

  // A real owner refusal retains the typed value until the user corrects it.
  await nameInput.fill("   ");
  await inspector.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("an element needs a name", { exact: false }).first()).toBeVisible();
  await expect(nameInput).toHaveValue("   ");
  expect((await model()).elements.find((element: any) => element.id === parent).name).toBe(name);
  await nameInput.fill(renamed);
  await inspector.getByRole("textbox", { name: "Short name", exact: true }).fill("OPS2");
  await inspector.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await model()).elements.find((element: any) => element.id === parent)?.name).toBe(renamed);
  await inspector.getByRole("textbox", { name: "Short name", exact: true }).fill("");
  await inspector.getByRole("combobox", { name: "Kind", exact: true }).fill("");
  await inspector.getByRole("textbox", { name: "Working calendar", exact: true }).fill("");
  await inspector.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => { const element = (await model()).elements.find((item: any) => item.id === parent); return [element?.shortName ?? "", element?.kind ?? "", element?.calendar ?? ""]; }).toEqual(["", "", ""]);

  // An accidental unreferenced identity can be deleted; it never enters a view.
  await builder.decide("enterprise.element.add", { type: "enterprise.element", id: mistake }, { stereotype: "ActualOrganization", name: `Mistaken ${suffix}`, from: yesterday }, "enterprise");
  await builder.open(page, `/enterprise?element=${mistake}&section=organization`);
  await inspector.getByRole("button", { name: "Delete unused element", exact: true }).click();
  const deletion = page.getByRole("dialog", { name: `Delete unused element: Mistaken ${suffix}`, exact: true });
  await deletion.getByRole("textbox", { name: "Reason", exact: true }).fill("Created by mistake");
  await deletion.getByRole("button", { name: "Delete unused element", exact: true }).click();
  await expect(deletion).toHaveCount(0);
  await page.getByRole("combobox", { name: "Element status", exact: true }).selectOption("All");
  await page.getByRole("textbox", { name: "Find an enterprise element", exact: true }).fill(mistake);
  await expect(page.getByRole("row").filter({ hasText: `Mistaken ${suffix}` })).toHaveCount(0);

  // Current child and account membership must end before the organisation closes.
  const kind = (await model()).kinds.find((item: any) => item.kind === "management").id;
  const placement = `placement-${suffix}`, membership = `membership-${suffix}`;
  await builder.decide("enterprise.element.add", { type: "enterprise.element", id: child }, { stereotype: "ActualOrganization", name: `Team ${suffix}`, from: "2026-01-01" }, "enterprise");
  await builder.decide("enterprise.relationship.add", { type: "enterprise.relationship", id: placement }, { stereotype: "ActualResourceRelationship", kind, source: child, target: parent, from: "2026-01-01" }, "enterprise");
  await builder.decide("enterprise.relationship.add", { type: "enterprise.relationship", id: membership }, { stereotype: "ActualOrganizationRole", source: `member:${me.principalId}`, target: parent, role: "employee", from: "2026-01-01" }, "enterprise");
  await builder.open(page, `/enterprise?element=${parent}&section=organization`);
  await inspector.getByRole("button", { name: "Close…", exact: true }).click();
  const closure = page.getByRole("dialog", { name: `Close element: ${renamed}`, exact: true });
  await closure.getByRole("textbox", { name: "Reason", exact: true }).fill("Pause operations");
  await expect(closure.getByRole("button", { name: "Close element", exact: true })).toBeDisabled();
  await expect(closure.getByRole("alert")).toContainText("Current usage prevents");
  await shots(page, closure, "enterprise-close-impact-en", info);
  await closure.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(builder.decide("enterprise.element.close", { type: "enterprise.element", id: parent }, { until: day, reason: "Still has a child and member" }, "enterprise")).rejects.toThrow(/CONFLICT/);
  await builder.decide("enterprise.relationship.end", { type: "enterprise.relationship", id: placement }, { until: day }, "enterprise");
  await builder.decide("enterprise.relationship.end", { type: "enterprise.relationship", id: membership }, { until: day }, "enterprise");

  // A live business record blocks closure independently of diagram relationships.
  const site = `SITE-${suffix}`;
  await builder.decide("core.site.create", { type: "core.site", id: site }, { name: `Site ${suffix}`, code: suffix, kind: "office", unit: parent, active: true }, "core");
  expect((await builder.read<any>(`enterprise-usage?element=${parent}&until=${day}`)).canClose).toBe(false);
  await expect(builder.decide("enterprise.element.close", { type: "enterprise.element", id: parent }, { until: day, reason: "Still owns an active site" }, "enterprise")).rejects.toThrow(/CONFLICT/);
  await builder.decide("core.site.edit", { type: "core.site", id: site }, { active: false }, "core");
  await builder.open(page, `/enterprise?element=${parent}&section=organization`);
  await inspector.getByRole("button", { name: "Close…", exact: true }).click();
  await closure.getByRole("textbox", { name: "Reason", exact: true }).fill("Pause after ending current uses");
  await closure.getByRole("button", { name: "Close element", exact: true }).click();
  await expect(closure).toHaveCount(0);
  await expect.poll(async () => (await model()).elements.find((item: any) => item.id === parent)?.until).toBe(day);
  await page.getByRole("combobox", { name: "Element status", exact: true }).selectOption("Inactive");
  await page.getByRole("textbox", { name: "Find an enterprise element", exact: true }).fill(renamed);
  await expect(page.getByRole("row").filter({ hasText: renamed })).toBeVisible();
  await builder.decide("core.site.edit", { type: "core.site", id: site }, { name: `Old site renamed ${suffix}` }, "core");
  expect((await builder.record("core.site", site)).unit).toBe(parent);
  await builder.open(page, `/enterprise?element=${parent}&section=organization`);
  await expect(nameInput).toHaveValue(renamed);
  await expect(inspector.getByRole("button", { name: "Reopen element", exact: true })).toBeVisible();

  // Viewing an earlier date shows the old period; reopening adds another period.
  await page.locator('input[aria-label="As of"]:visible').fill(yesterday);
  await page.getByRole("combobox", { name: "Element status", exact: true }).selectOption("Active");
  await page.getByRole("textbox", { name: "Find an enterprise element", exact: true }).fill(renamed);
  await expect(page.getByRole("row").filter({ hasText: renamed })).toBeVisible();
  await page.locator('input[aria-label="As of"]:visible').fill(day);
  await inspector.getByRole("button", { name: "Reopen element", exact: true }).click();
  const reopening = page.getByRole("dialog", { name: `Reopen element: ${renamed}`, exact: true });
  await reopening.getByRole("textbox", { name: "Reason", exact: true }).fill("Resume operations");
  await reopening.getByRole("button", { name: "Reopen element", exact: true }).click();
  await expect(reopening).toHaveCount(0);
  await expect.poll(async () => (await model()).elements.find((item: any) => item.id === parent)?.periods?.length).toBe(2);
  await page.getByRole("button", { name: "Save view", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save view", exact: true })).toBeDisabled();
  const tomorrow = new Date(Date.now() + 86_400_000).toISOString().slice(0, 10);
  await builder.decide("enterprise.element.close", { type: "enterprise.element", id: parent }, { until: tomorrow, reason: "Accidental scheduled closure" }, "enterprise");
  await builder.open(page, `/enterprise?element=${parent}&section=organization`);
  await inspector.getByRole("button", { name: "Reinstate element", exact: true }).click();
  const reinstate = page.getByRole("dialog", { name: `Reinstate element: ${renamed}`, exact: true });
  await reinstate.getByRole("textbox", { name: "Reason", exact: true }).fill("Undo mistaken scheduled closure");
  await reinstate.getByRole("button", { name: "Reinstate element", exact: true }).click();
  await expect(reinstate).toHaveCount(0);
  expect((await model()).elements.find((item: any) => item.id === parent).periods).toEqual([{ from: "2026-01-01", until: day }, { from: day }]);

  // A source-owned identity is inspectable, with no local edit/lifecycle buttons.
  await builder.decide("enterprise.slice.sync", { type: "enterprise.model", id: `source-${suffix}` }, { source: suffix, elements: [{ id: external, name: `External organisation ${suffix}`, stereotype: "ActualOrganization" }], relationships: [] }, "enterprise");
  await builder.open(page, `/enterprise?element=${external}&section=organization`);
  await expect(nameInput).toBeDisabled();
  for (const label of ["Edit", "Close…", "Delete unused element", "Reopen element", "Reinstate element"]) await expect(inspector.getByRole("button", { name: label, exact: true })).toHaveCount(0);
  await expect(inspector.getByRole("button", { name: "Hide in this view", exact: true })).toBeEnabled();
  await builder.decide("enterprise.relationship.add", { type: "enterprise.relationship", id: `current-${membership}` }, { stereotype: "ActualOrganizationRole", source: `member:${me.principalId}`, target: parent, role: "employee", from: day }, "enterprise");
  await builder.open(page, `/member?id=${me.principalId}`);
  await page.getByRole("dialog").filter({ has: page.getByRole("heading", { name: "Unsaved changes", exact: true }) }).getByRole("button", { name: "Discard changes", exact: true }).click();
  const membershipPanel = page.locator("section").filter({ has: page.getByRole("heading", { name: "Organisation", exact: true }) });
  await membershipPanel.getByRole("button", { name: renamed, exact: true }).first().click();
  await expect.poll(() => Object.fromEntries(new URLSearchParams(new URL(page.url()).hash.split("?")[1]))).toMatchObject({ element: parent, section: "organization" });

  // Configuring a tenant link requires explicit source identity and a connection.
  await builder.open(page, "/enterprise?section=tenant-links");
  await page.getByRole("button", { name: "Add tenant link", exact: true }).click();
  const link = page.getByRole("dialog", { name: "Tenant link settings", exact: true });
  await link.getByRole("textbox", { name: "Link ID", exact: true }).fill(`link-${suffix}`);
  await link.getByRole("combobox", { name: "Organisation", exact: true }).selectOption(parent);
  await link.getByRole("textbox", { name: "Source tenant ID", exact: true }).fill("explicit-source");
  await expect(link.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await link.getByRole("textbox", { name: "Source root ID", exact: true }).fill("explicit-root");
  await expect(link.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await link.getByRole("button", { name: "Cancel", exact: true }).click();
  await builder.decide("platform.profile.update", { type: "platform.profile", id: me.principalId }, { language: "zh-CN" }, "platform");
  try {
    await page.reload();
    await page.getByRole("button", { name: "添加租户关联", exact: true }).click();
    const chinese = page.getByRole("dialog", { name: "租户关联设置", exact: true });
    await expect(chinese.getByRole("textbox", { name: "来源租户标识", exact: true })).toBeVisible();
    await shots(page, chinese, "enterprise-tenant-link-zh", info);
    await chinese.getByRole("button", { name: "取消", exact: true }).click();
    await builder.open(page, `/enterprise?element=${parent}&section=organization`);
    await expect(page.getByRole("combobox", { name: "节点状态", exact: true })).toBeVisible();
    await shots(page, page.locator("body"), "enterprise-management-zh", info);
  } finally {
    await builder.decide("platform.profile.update", { type: "platform.profile", id: me.principalId }, { language: "en" }, "platform");
    await page.evaluate(() => localStorage.setItem("platform.language", "en"));
  }
});
