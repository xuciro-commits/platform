import { addToLibrary, installInput, packageWizard } from "./package-inputs";
import { expect, test } from "@playwright/test";
import { open, signIn, submit } from "./support";

test("ERP composition: purchasing works before inventory, posts stock when connected and survives retirement", async ({ page, context }) => {
  test.setTimeout(900_000);
  await signIn(page, "admin-1 · blank");
  const grant = async (app: string, role: string) => {
    await submit(page, "platform.member.grant", "platform.member", "admin-1", { app, role });
  };
  await installInput(page, "geo");
  await installInput(page, "party-next");
  await installInput(page, "uom-next");
  await installInput(page, "item-master");
  await installInput(page, "stock-contract");
  await installInput(page, "purchasing");
  for (const [app, role] of [["party","user"], ["uom","user"], ["item","steward"], ["purchasing","manager"]]) await grant(app, role);
  await submit(page, "party.organization.create", "party.organization", "ERP-ORG", { name: "ERP supplier" });
  await submit(page, "purchasing.supplier.create", "purchasing.supplier", "ERP-S", { base: "ERP-ORG", supplierkind: "company" });
  await submit(page, "purchasing.supplier.qualify", "purchasing.supplier", "ERP-S", {});
  await submit(page, "item.master.create", "item.master", "ERP-I", { title: "ERP item", code: "ERP-I", kind: "material", unit: "pcs" });
  const receipt = async (id: string, connected: boolean) => {
    await submit(page, "purchasing.receipt.create", "purchasing.receipt", id, { title: id, supplier: "ERP-S", warehouse: "MAIN", receivedon: "2026-11-26" });
    await submit(page, "purchasing.receiptline.create", "purchasing.receiptline", id + "-LINE", { receipt: id, material: "ERP-I", quantity: 2, unitprice: 5 });
    await page.goto("/purchasing/purchasing.receipt/" + id);
    await expect(page.getByRole("heading", { name: id, exact: true }).first()).toBeVisible();
    if (!connected) await expect(page.getByText("Inventory not connected", { exact: true })).toBeVisible();
    else await expect(page.getByText("Inventory not connected", { exact: true })).toHaveCount(0);
    await submit(page, "purchasing.receipt.post", "purchasing.receipt", id, {});
    await page.reload();
    await expect(page.getByText("Posted", { exact: true }).first()).toBeVisible();
  };
  await receipt("ERP-BEFORE", false);
  const wizard = await packageWizard(page, "inventory", true);
  await addToLibrary(wizard);
  await expect(wizard.getByText("stock.post/1", { exact: true })).toBeVisible();
  await expect(wizard.getByText(/Used by.*purchasing/i)).toBeVisible();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await wizard.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(wizard).toBeHidden({ timeout: 120_000 });
  const access = page.getByRole("dialog", { name: "Who can use it" });
  await expect(access).toBeVisible();
  await access.getByRole("button", { name: "Skip for now", exact: true }).click();
  await expect(access).toHaveCount(0);
  await grant("inventory", "keeper");
  await submit(page, "platform.protocol.bind", "platform.protocol", "stock.post/1", { provider: "build" });
  await submit(page, "inventory.warehouse.create", "inventory.warehouse", "MAIN", { title: "Main warehouse", code: "MAIN" });
  await receipt("ERP-CONNECTED", true);
  await page.goto("/inventory/inventory.movement");
  const movement = page.getByRole("row").filter({ hasText: "ERP-CONNECTED" });
  await expect(movement).toBeVisible({ timeout: 30_000 });
  await expect(movement.getByRole("cell", { name: "2", exact: true })).toBeVisible();

  // Phase 4: the same declarations exercise the generic document grid.
  await submit(page, "purchasing.order.create", "purchasing.order", "ERP-ORDER", { title: "ERP order 50", supplier: "ERP-S", currency: "USD", orderedon: "2026-11-26", expectedon: "2026-12-01" });
  const pasteLines = async (route: string, tableName: string, rows: string, fix = false) => {
    await page.goto(route);
    const add = page.getByRole("heading", { name: tableName, exact: true }).locator("../..").getByRole("button", { name: "Add a row", exact: true });
    await expect(add).toBeVisible({ timeout: 30000 });
    await add.focus();
    await add.evaluate((element, text) => {
      const dt = new DataTransfer();
      dt.setData("text/plain", text);
      element.dispatchEvent(new ClipboardEvent("paste", { clipboardData: dt, bubbles: true, cancelable: true }));
    }, rows);
    const table = page.getByRole("table", { name: tableName });
    await expect(table.getByRole("button", { name: "Remove row" })).toHaveCount(50);
    const submitRows = page.getByRole("button", { name: "Submit rows", exact: true });
    if (fix) {
      await page.keyboard.press("Meta+Enter");
      await expect(page.getByText("2 cells need fixing", { exact: true })).toBeVisible({ timeout: 60000 });
      await expect(table.locator('[aria-invalid="true"]')).toHaveCount(2);
      await page.keyboard.type("2");
      await page.keyboard.press("Meta+Enter");
      await expect(page.getByText("1 cells need fixing", { exact: true })).toBeVisible({ timeout: 60000 });
      await page.keyboard.type("5");
      await page.keyboard.press("Meta+Enter");
    } else await page.keyboard.press("Meta+Enter");
    await expect(submitRows).toHaveCount(0, { timeout: 120000 });
    await page.reload();
    await expect(table.getByRole("button", { name: "Remove row" })).toHaveCount(0);
    await expect(table.getByRole("row").filter({hasText: "ERP item"})).toHaveCount(50);
  };
  const orderRows = Array.from({ length: 50 }, (_, i) => ["", "ERP-I", "", "", "", i === 9 ? "badqty" : "2", i === 34 ? "badprice" : "5", ""].join("\t")).join("\n");
  await pasteLines("/purchasing/purchasing.order/ERP-ORDER", "Purchase order lines", orderRows, true);
  await submit(page, "purchasing.order.approve", "purchasing.order", "ERP-ORDER", {});
  await submit(page, "purchasing.receipt.create", "purchasing.receipt", "ERP-BULK", { title: "ERP bulk receipt", order: "ERP-ORDER", supplier: "ERP-S", warehouse: "MAIN", receivedon: "2026-11-26" });
  const receiptRows = Array.from({ length: 50 }, () => ["", "ERP-I", "", "MAIN", "2", "5"].join("\t")).join("\n");
  await pasteLines("/purchasing/purchasing.receipt/ERP-BULK", "Purchase receipt lines", receiptRows);
  await submit(page, "purchasing.receipt.post", "purchasing.receipt", "ERP-BULK", {});
  const records = async (type: string) => page.evaluate(async type => {
    const s = JSON.parse(sessionStorage.getItem("platform:session")!);
    const r = await fetch("/v1/records/" + type + "?limit=200", { headers: { Authorization: "Bearer " + s.token, "Platform-Tenant": s.tenant } });
    if (!r.ok) throw new Error("records " + type + ": " + r.status);
    return await r.json() as { total: number; records: Record<string, unknown>[] };
  }, type);
  await expect.poll(async () => (await records("inventory.movement")).records.filter(r => r.sourceid === "ERP-BULK").length, { timeout: 120000 }).toBe(50);
  const inbound = (await records("inventory.movement")).records.filter(r => r.sourceid === "ERP-BULK");
  expect(inbound.reduce((sum, r) => sum + Number(r.quantity), 0)).toBe(100);
  const balance = async () => (await records("inventory.movement.stock")).records.find(r => r.material === "ERP-I" && r.warehouse === "MAIN");
  await expect.poll(async () => Number((await balance())?.quantity), { timeout: 30000 }).toBe(102);

  // Two different users compete for the last four of another item.
  await submit(page, "item.master.create", "item.master", "ERP-RACE", { title: "ERP race item", code: "ERP-RACE", kind: "material", unit: "pcs" });
  await submit(page, "inventory.movement.create", "inventory.movement", "ERP-RACE-IN", { title: "Race inbound", material: "ERP-RACE", warehouse: "MAIN", quantity: 4, on: "2026-11-26" });
  for (const member of ["user-1", "manager-1"]) {
    await submit(page, "platform.member.grant", "platform.member", member, { app: "inventory", role: "keeper" });
    await submit(page, "platform.member.grant", "platform.member", member, { app: "item", role: "user" });
  }
  const p1 = await context.newPage(), p2 = await context.newPage();
  try {
    await signIn(p1, "user-1 · blank");
    await signIn(p2, "manager-1 · blank");
    const results = await Promise.allSettled([p1,p2].map((p,i) => submit(p,"inventory.movement.create","inventory.movement","ERP-RACE-OUT-"+i,{title:"Race outbound "+i,material:"ERP-RACE",warehouse:"MAIN",quantity:-4,on:"2026-11-26"})));
    expect(results.filter(r=>r.status==="fulfilled")).toHaveLength(1);
    const rejected=results.find(r=>r.status==="rejected");
    expect(String(rejected && rejected.status==="rejected" && rejected.reason)).toContain("Insufficient stock");
  } finally { await p1.close(); await p2.close(); }
  await expect.poll(async () => {
    const facts = (await records("inventory.movement")).records.filter(r => r.material === "ERP-RACE");
    return { count: facts.length, quantity: facts.reduce((sum, r) => sum + Number(r.quantity), 0) };
  }).toEqual({count: 2, quantity: 0});
  const raceBalance = (await records("inventory.movement.stock")).records.find(r => r.material === "ERP-RACE");
  expect(Number(raceBalance?.quantity ?? 0)).toBe(0);

  // Reverse all 50, retain original rows, and read the original balance.
  await submit(page, "purchasing.receipt.void", "purchasing.receipt", "ERP-BULK", { on: "2026-11-27" });
  await expect.poll(async () => Number((await balance())?.quantity), { timeout: 120000 }).toBe(2);
  const afterReverse = (await records("inventory.movement")).records;
  for(const row of inbound) {
    expect(afterReverse.some(r=>r.reverses===row.id)).toBe(true);
    expect(afterReverse.find(r=>r.id===row.id)?.reversed).toBe(true);
  }

  // Period summary and links to actual posting records.
  await page.goto("/inventory/inventory.movement?view=" + encodeURIComponent(JSON.stringify({version:1, layout:"summary", group:"material", subGroup:"warehouse", measure:"sum:quantity", dates:"on", from:"2026-11-01", to:"2026-11-30"})));
  const summary = page.getByRole("table", { name: "Summary", exact: true });
  await expect(summary).toBeVisible({timeout:30000});
  const stock = summary.getByRole("row").filter({hasText:"ERP item"}).filter({hasText:"Main warehouse"});
  await expect(stock).toBeVisible();
  const numbers = stock.getByRole("link", {name:/^\d+$/});
  await expect(numbers).toHaveCount(4);
  await expect(numbers.nth(0)).toHaveText("0");
  await expect(numbers.nth(1)).toHaveText("102");
  await expect(numbers.nth(2)).toHaveText("100");
  await expect(numbers.nth(3)).toHaveText("2");
  await numbers.nth(1).click();
  await expect(page.getByRole("row").filter({hasText:"ERP-BULK"}).first()).toBeVisible({timeout:30000});

  await page.goto("/purchasing/purchasing.receipt/ERP-BULK");
  await page.getByRole("button",{name:"More actions",exact:true}).click();
  await page.getByRole("menuitem",{name:"Print",exact:true}).click();
  await expect(page).toHaveURL(/\/print$/);
  const printed = page.getByRole("table",{name:"Purchase receipt lines"});
  await expect(printed.getByRole("row")).toHaveCount(52);
  await page.emulateMedia({media:"print"});
  const header = printed.getByRole("rowgroup").first();
  expect(await header.evaluate(el=>getComputedStyle(el).display)).toBe("table-header-group");
  await page.emulateMedia({media:"screen"});
  await page.getByRole("link", {name:"Back to record",exact:true}).click();

  await open(page, "Delivery", "Installations");
  const row = page.getByRole("listitem").filter({ hasText: "inventory · " });
  await row.getByRole("button", { name: "Manage installation", exact: true }).click();
  const manage = page.getByRole("dialog", { name: /Manage installation/ });
  await manage.getByRole("button", { name: "Mark for uninstall", exact: true }).click();
  await expect(manage).toBeHidden();
  await expect(row).toContainText("draining");
  await row.getByRole("button", { name: "Manage installation", exact: true }).click();
  const retire = manage.getByRole("button", { name: "Retire package", exact: true });
  await expect(retire).toBeEnabled({ timeout: 30_000 });
  await retire.click();
  await expect(manage).toBeHidden();
  await expect(row).toContainText("retired");
  await receipt("ERP-AFTER", false);
});
