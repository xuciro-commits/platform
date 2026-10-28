import {test, expect, shots} from "./kit";

test("a host administrator reaches static maintenance while dynamic declarations are unavailable", async ({page, builder}, info) => {
  await page.route("**/v1/declarations", route => route.fulfill({status:503, contentType:"application/json", body:JSON.stringify({error:{code:"DEFINITIONS_UNAVAILABLE"}})}));
  await builder.open(page, "/apps");
  await expect(page.getByText("The host's declarations could not load, so the workspace cannot tell what you may see and do.", {exact:true})).toBeVisible();
  await page.getByRole("button", {name:"Open host maintenance", exact:true}).click();
  await expect(page.getByRole("heading", {name:"Host Console", exact:true})).toBeVisible();
  await expect(page).toHaveURL(/#\/host-overview$/);
  const overview=await builder.read<{tenants:{id:string}[]}>("host/overview");
  expect(overview.tenants.length).toBeGreaterThan(0);
  await expect(page.getByRole("heading", {name:"Host Console", exact:true})).toBeVisible();
  await shots(page, page.locator("body"), "static-maintenance", info);
});
