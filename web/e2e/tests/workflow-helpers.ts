import { expect, type Page } from "@playwright/test";

/** Discover through the same typed palette a builder uses, without canvas coordinates. */
export async function addBlock(page: Page, reference: string) {
  await page.getByRole("button", { name: "Add block", exact: true }).click();
  const palette = page.getByRole("dialog", { name: "Add block", exact: true });
  await palette.getByRole("combobox", { name: "Search blocks" }).fill(reference);
  const option = palette.getByRole("option");
  await expect(option).toHaveCount(1);
  await option.click();
}

export async function chooseBlock(page: Page, id: string, settings = false) {
  const node = page.getByRole("region", { name: "Workflow map", exact: true }).locator(`.react-flow__node[data-id="${id}"]`);
  await node.focus();
  await node.press("Enter");
  const inspector = page.getByRole("region", { name: "Workflow properties" });
  await inspector.getByRole("tab", { name: settings ? "Settings" : "Input", exact: true }).click();
  return inspector;
}
