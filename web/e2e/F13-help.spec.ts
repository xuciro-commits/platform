import { expect, signIn, test } from "./kit";

test("F13 rail Help shows the current page help and returns to the page", async ({ page }) => {
  await signIn(page, "builder", "/files");
  const title = await page.getByRole("main").getByRole("heading", { level: 1 }).textContent();
  await page.getByRole("button", { name: "Help", exact: true }).click();
  await expect(page.getByRole("main").getByRole("heading", { name: title || "Files", level: 1 })).toBeVisible();
  await expect(page.getByText("Every project and the resources it owns. Open a project to edit its resources, or use the folder tree to move them.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Back to page", exact: true }).click();
  await expect(page).toHaveURL(/#\/files/);
});
