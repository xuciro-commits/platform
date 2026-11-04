import { expect, signIn, test } from "./kit";

test("F15 the application registry has one title and canonical home for each tool", async ({ page }) => {
  await signIn(page, "admin");
  const manifests = await page.request.get("/v1/apps", { headers: { authorization: "Bearer admin" } });
  expect(manifests.ok(), await manifests.text()).toBeTruthy();
  const titles = new Map((await manifests.json() as { id: string; title: string }[]).map(entry => [entry.id, entry.title]));
  expect(titles.get("build")).toBe("Files");
  expect(titles.get("flow")).toBe("Automate");
  expect(titles.get("platform")).toBe("Control Panel");
  expect([...titles.values()]).not.toContain("Builder");
  expect([...titles.values()]).not.toContain("Settings");
  expect([...titles.values()]).not.toContain("Flows");

  await page.getByRole("button", { name: "Applications", exact: true }).first().click();
  const portal = page.getByRole("region", { name: "Applications", exact: true });
  await expect(portal.getByRole("button", { name: /^Control Panel\b/ })).toHaveCount(1);
  await expect(portal.getByRole("button", { name: /^Automate\b/ })).toHaveCount(1);
  await expect(portal.getByRole("button", { name: /^(Settings|Flows|Builder)\b/ })).toHaveCount(0);

  await portal.getByRole("button", { name: /^Automate\b/ }).click();
  await expect(page).toHaveURL(/#\/automation$/);
  await expect(page.getByRole("main").getByRole("heading", { name: "Automations", level: 1 })).toBeVisible();

  await page.getByRole("button", { name: "Files", exact: true }).first().click();
  await expect(page).toHaveURL(/#\/files$/);
  await expect(page.getByRole("main").getByRole("heading", { name: "Files", level: 1 })).toBeVisible();

  await page.goto("/#/control-panel");
  await expect(page.getByRole("main").getByRole("heading", { name: "Control Panel", level: 1 })).toBeVisible();
});
