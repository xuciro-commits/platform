import { expect, fresh, shots, test } from "./kit";

// The directory edits only organisational facts: units, posts, people and their
// assignments. One route: add, rename, assign and close through the UI.
test("the organisation directory adds, assigns and closes directory elements", async ({ page, builder }, info) => {
  test.setTimeout(90_000);
  const suffix = fresh("org").toLowerCase();
  const unit = `Operations ${suffix}`, renamed = `Operations renamed ${suffix}`, post = `Shift lead ${suffix}`;
  const model = () => builder.read<any>("enterprise");
  const find = async (name: string) => (await model()).elements.find((element: any) => element.name === name);
  await builder.open(page, "/enterprise");
  await expect(page.getByLabel("Effective date", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Add element", exact: true }).click();
  const creation = page.getByRole("dialog", { name: "Add element", exact: true });
  await creation.getByRole("textbox", { name: "Name", exact: true }).fill(unit);
  await creation.getByRole("textbox", { name: "Kind", exact: true }).fill("team");
  await creation.getByRole("button", { name: "Save", exact: true }).click();
  await expect(creation).toHaveCount(0);
  await expect.poll(async () => (await find(unit))?.stereotype).toBe("ActualOrganization");
  const unitID = (await find(unit)).id;

  await page.getByRole("button", { name: unit, exact: true }).click();
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const edit = page.getByRole("dialog", { name: "Edit element", exact: true });
  await edit.getByRole("textbox", { name: "Name", exact: true }).fill(renamed);
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await model()).elements.find((element: any) => element.id === unitID)?.name).toBe(renamed);

  await builder.decide("enterprise.element.add", { type: "enterprise.element", id: fresh("post") }, { stereotype: "ActualPost", name: post, kind: "role" }, "enterprise");
  await expect.poll(async () => (await find(post))?.stereotype).toBe("ActualPost");
  await page.reload();
  await page.getByRole("button", { name: "Add assignment", exact: true }).click();
  const assignment = page.getByRole("dialog", { name: "Add assignment", exact: true });
  await assignment.getByRole("combobox", { name: "Type", exact: true }).selectOption({ label: "Responsible for" });
  await assignment.getByRole("textbox", { name: "Source", exact: true }).fill(unitID);
  await assignment.getByRole("combobox", { name: "Target", exact: true }).selectOption({ label: post });
  await assignment.getByRole("button", { name: "Save", exact: true }).click();
  await expect(assignment).toHaveCount(0);
  await expect.poll(async () => (await model()).relationships.some((relation: any) => relation.stereotype === "ResponsibleFor" && relation.source === unitID)).toBe(true);
  await shots(page, page.locator("body"), "organisation-directory-en", info);

  // A unit with active assignments cannot be closed; an unused one can.
  const spare = `Spare ${suffix}`;
  await builder.decide("enterprise.element.add", { type: "enterprise.element", id: fresh("spare") }, { stereotype: "ActualOrganization", name: spare, kind: "team" }, "enterprise");
  await page.reload();
  await page.getByRole("button", { name: spare, exact: true }).click();
  await page.getByRole("button", { name: "Close element", exact: true }).click();
  const closing = page.getByRole("dialog", { name: `Close element: ${spare}`, exact: true });
  await closing.getByLabel("Effective date", { exact: true }).fill(new Date(Date.now() + 2 * 86_400_000).toISOString().slice(0, 10));
  await closing.getByRole("textbox", { name: "Reason", exact: true }).fill("No longer needed");
  await closing.getByRole("button", { name: "Close element", exact: true }).last().click();
  await expect(closing).toHaveCount(0);
  await expect.poll(async () => (await find(spare))?.validTo ?? (await find(spare))?.until ?? "").not.toBe("");
});
