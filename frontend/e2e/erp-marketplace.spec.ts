import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { addToLibrary, installInput, packageWizard } from "./package-inputs";
import { expect, test } from "@playwright/test";
import { open, property, restartHost, signIn, submit } from "./support";

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
  await installInput(page, "gl-contract");
  await installInput(page, "plan-contract");
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

test("ERP ledger: ten thousand accepted stock facts, posting/read latency and full replay", { tag: "@performance" }, async ({ page }) => {
  test.setTimeout(2_400_000);
  await signIn(page, "admin-1 · blank");
  for (const name of ["geo", "party-next", "uom-next", "item-master", "stock-contract", "gl-contract", "inventory"]) await installInput(page, name);
  for (const [app, role] of [["party","user"], ["uom","user"], ["item","steward"], ["inventory","keeper"]]) {
    await submit(page, "platform.member.grant", "platform.member", "admin-1", { app, role });
  }
  await submit(page, "item.master.create", "item.master", "LOAD-I", { title: "Ledger load item", code: "LOAD-I", kind: "material", unit: "pcs" });
  await submit(page, "inventory.warehouse.create", "inventory.warehouse", "LOAD-W", { title: "Ledger load warehouse", code: "LOAD-W" });
  const session = await page.evaluate(() => JSON.parse(sessionStorage.getItem("platform:session")!));
  const host = "http://127.0.0.1:" + process.env.PLATFORM_TRUNK_HOST_PORT;
  const started = performance.now();
  const rss = () => {
    const pid = execFileSync("lsof", ["-t", "-iTCP:" + process.env.PLATFORM_TRUNK_HOST_PORT, "-sTCP:LISTEN"], {encoding:"utf8"}).trim().split("\n")[0];
    return Number(execFileSync("ps", ["-o", "rss=", "-p", pid], {encoding:"utf8"}).trim()) / 1024;
  };
  let peakRSS = rss();
  let imported = 0;
  for (let batch=0; batch<10; batch++) {
    const csv = ["id,title,material,warehouse,quantity,on", ...Array.from({length: 1000}, (_, i) => "LOAD-"+String(batch*1000+i).padStart(5,"0")+",Fact "+(batch*1000+i)+",LOAD-I,LOAD-W,1,2026-11-26")].join("\n")+"\n";
    const response = await page.request.post(host+"/v1/import/inventory.movement", {
      headers: {Authorization:"Bearer "+session.token,"Platform-Tenant":session.tenant,"Content-Type":"text/csv"},
      data: csv, timeout: 600_000
    });
    const data = await response.json();
    if(!response.ok() || !Array.isArray(data)) throw Error(JSON.stringify(data).slice(0,500));
    expect(data).toHaveLength(1000);
    expect(data.filter(r=>r.outcome!=="ok"), JSON.stringify(data.find(r=>r.outcome!=="ok"))).toHaveLength(0);
    imported+=data.length;
    peakRSS=Math.max(peakRSS,rss());
    console.log("LEDGER IMPORT", imported, "elapsed ms", (performance.now()-started).toFixed(1), "RSS MiB", peakRSS);
  }
  expect(imported).toBe(10000);
  const importMs=performance.now()-started;
  const balances = async () => page.evaluate(async () => {
    const s = JSON.parse(sessionStorage.getItem("platform:session")!);
    const started = performance.now();
    const r = await fetch("/v1/records/inventory.movement.stock?limit=100",{headers:{Authorization:"Bearer "+s.token,"Platform-Tenant":s.tenant}});
    if(!r.ok) throw Error("balance status "+r.status);
    const data=await r.json();
    return {elapsedMs:performance.now()-started, records:data.records as Record<string,unknown>[]};
  });
  expect((await balances()).records.find(r=>r.material==="LOAD-I" && r.warehouse==="LOAD-W")?.quantity).toBe(10000);
  const postMs:number[]=[],readMs:number[]=[];
  for(let i=0;i<20;i++){
    const started=performance.now();
    await submit(page,"inventory.movement.create","inventory.movement","LOAD-PROBE-"+i,{title:"Posting probe",material:"LOAD-I",warehouse:"LOAD-W",quantity:1,on:"2026-11-27"});
    postMs.push(performance.now()-started);
    readMs.push((await balances()).elapsedMs);
  }
  const before=(await balances()).records;
  expect(before.find(r=>r.material==="LOAD-I")?.quantity).toBe(10020);
  const replayStarted=performance.now();
  await restartHost("replay");
  await signIn(page,"admin-1 · blank");
  const after=(await balances()).records;
  expect(JSON.stringify(after)).toBe(JSON.stringify(before));
  const p95=(values:number[])=>values.slice().sort((a,b)=>a-b)[Math.ceil(values.length*.95)-1];
  console.log("LEDGER BASELINE",JSON.stringify({facts:10020,importMs,peakRSSMiB:peakRSS,postingP95Ms:p95(postMs),balanceReadP95Ms:p95(readMs),replayMs:performance.now()-replayStarted,replayByteEqual:true}));
});

test("ERP stage 4 step 7: sales order through MRP, procurement, production, delivery, invoicing, GL reconciliation and period close", async ({ page }) => {
  test.setTimeout(600_000);
  const gateStarted = performance.now();
  const videoOut = process.env.STEP7_VIDEO_OUT;
  const frameDir = videoOut ? mkdtempSync(join(tmpdir(), "erp-step7-frames-")) : "";
  let frameCount = 0;
  const cdp = videoOut ? await page.context().newCDPSession(page) : null;
  if (cdp && frameDir) {
    await page.setViewportSize({ width: 1280, height: 800 });
    mkdirSync(frameDir, { recursive: true });
    cdp.on("Page.screencastFrame", ({ data, sessionId }) => {
      frameCount += 1;
      writeFileSync(join(frameDir, `frame-${String(frameCount).padStart(5, "0")}.jpg`), Buffer.from(data, "base64"));
      void cdp.send("Page.screencastFrameAck", { sessionId }).catch(() => {});
    });
    await cdp.send("Page.startScreencast", { format: "jpeg", quality: 80, maxWidth: 1280, maxHeight: 800, everyNthFrame: 1 });
  }
  try {
  await signIn(page, "admin-1 · blank");

  // 1. Install the complete ERP package suite into an empty PostgreSQL tenant via Marketplace UI.
  for (const name of [
    "geo",
    "party-next",
    "uom-next",
    "item-master",
    "stock-contract",
    "gl-contract",
    "plan-contract",
    "inventory",
    "purchasing",
    "sales",
    "accounting",
    "manufacturing",
  ]) {
    await installInput(page, name);
  }
  for (const [app, role] of [
    ["geo", "user"],
    ["party", "user"],
    ["uom", "user"],
    ["item", "steward"],
    ["inventory", "keeper"],
    ["purchasing", "manager"],
    ["sales", "manager"],
    ["accounting", "manager"],
    ["manufacturing", "manager"],
  ]) {
    await submit(page, "platform.member.grant", "platform.member", "admin-1", { app, role });
  }
  for (const contract of [
    "stock.post/1",
    "stock.reserve/1",
    "stock.close/1",
    "stock.balance/1",
    "gl.post/1",
    "demand.plan/1",
    "supply.order/1",
    "supply.requisition/1",
  ]) {
    await submit(page, "platform.protocol.bind", "platform.protocol", contract, { provider: "build" });
  }

  await page.goto("/delivery/installations");
  for (const pkg of ["sales · 0.2.2", "accounting · 0.2.0", "manufacturing · 0.2.0"]) {
    await expect(page.getByRole("listitem").filter({ hasText: pkg })).toBeVisible({ timeout: 30_000 });
  }

  const followReference = async (link: ReturnType<typeof page.getByRole>, recordTitle: string, idSuffix: RegExp) => {
    await link.click();
    const peek = page.getByRole("dialog", { name: recordTitle });
    await expect(peek).toBeVisible({ timeout: 15_000 });
    await peek.getByRole("link", { name: "Open full page", exact: true }).click();
    await expect(page).toHaveURL(idSuffix);
  };

  const readRecord = async (type: string, id: string) => page.evaluate(async ({ type, id }) => {
    const s = JSON.parse(sessionStorage.getItem("platform:session")!);
    const r = await fetch(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`, {
      headers: { Authorization: "Bearer " + s.token, "Platform-Tenant": s.tenant },
    });
    if (!r.ok) return null;
    const data = await r.json() as { record?: Record<string, unknown> };
    return data.record ?? null;
  }, { type, id });

  // 2. Master data & multi-level BOM hierarchy: FG-BOT -> 2 x SUB-ARM -> 3 x MAT-ROD (10% scrap => 3.3/arm).
  await submit(page, "party.organization.create", "party.organization", "ORG-SUP-S7", { name: "Suzhou Servo Supply" });
  await submit(page, "purchasing.supplier.create", "purchasing.supplier", "SUP-S7", { base: "ORG-SUP-S7", supplierkind: "company" });
  await submit(page, "purchasing.supplier.qualify", "purchasing.supplier", "SUP-S7", {});

  await submit(page, "party.organization.create", "party.organization", "ORG-CUST-S7", { name: "Apex Automation" });
  await submit(page, "sales.customergroup.create", "sales.customergroup", "CG-ENT", { title: "Enterprise accounts", code: "ENT" });
  await submit(page, "sales.territory.create", "sales.territory", "TER-CN", { title: "East China", code: "CN-EAST" });
  await submit(page, "sales.customer.create", "sales.customer", "CUST-S7", {
    base: "ORG-CUST-S7", customerkind: "company", group: "CG-ENT", territory: "TER-CN", creditlimit: 500000,
  });
  await submit(page, "sales.customer.activate", "sales.customer", "CUST-S7", {});

  await submit(page, "inventory.warehouse.create", "inventory.warehouse", "MAIN", { title: "Main warehouse", code: "MAIN" });
  await submit(page, "item.category.create", "item.category", "CAT-MA", { title: "Moving average items", code: "CAT-MA", valuation: "moving_average" });
  await submit(page, "item.master.create", "item.master", "FG-BOT", { title: "Collaborative robot", code: "FG-BOT", kind: "goods", unit: "pcs", category: "CAT-MA", leadtimedays: 2, safetystock: 0 });
  await submit(page, "item.master.create", "item.master", "SUB-ARM", { title: "Robot arm module", code: "SUB-ARM", kind: "goods", unit: "pcs", category: "CAT-MA", leadtimedays: 3, safetystock: 0 });
  await submit(page, "item.master.create", "item.master", "MAT-ROD", { title: "Alloy shaft", code: "MAT-ROD", kind: "material", unit: "pcs", category: "CAT-MA", leadtimedays: 5, safetystock: 0 });

  await submit(page, "manufacturing.bom.create", "manufacturing.bom", "BOM-FG", { title: "BOM-FG", product: "FG-BOT", versioncode: "1.0" });
  await submit(page, "manufacturing.bomline.create", "manufacturing.bomline", "BL-FG-1", { bom: "BOM-FG", component: "SUB-ARM", quantity: 2, scraprate: 0 });
  await submit(page, "manufacturing.bom.activate", "manufacturing.bom", "BOM-FG", {});

  await submit(page, "manufacturing.bom.create", "manufacturing.bom", "BOM-ARM", { title: "BOM-ARM", product: "SUB-ARM", versioncode: "1.0" });
  await submit(page, "manufacturing.bomline.create", "manufacturing.bomline", "BL-ARM-1", { bom: "BOM-ARM", component: "MAT-ROD", quantity: 3, scraprate: 0.1 });
  await submit(page, "manufacturing.bom.activate", "manufacturing.bom", "BOM-ARM", {});

  // 3. Sales Order SO-S7 (5 x FG-BOT @ 200 = 1,000) -> shortage holds order in Pending confirmation.
  await submit(page, "sales.order.create", "sales.order", "SO-S7", {
    title: "SO-S7", customer: "CUST-S7", orderedon: "2026-10-10", deliverydate: "2026-10-25", currency: "CNY", warehouse: "MAIN",
  });
  await submit(page, "sales.orderline.create", "sales.orderline", "SOL-S7", {
    order: "SO-S7", material: "FG-BOT", quantity: 5, unitprice: 200,
  });
  await submit(page, "sales.order.confirm", "sales.order", "SO-S7", {});
  await expect.poll(async () => (await readRecord("sales.order", "SO-S7"))?.shortage, { timeout: 30_000 })
    .toBe("Insufficient available stock: requested 5, available 0, short by 5.");
  await page.goto("/sales/sales.order/SO-S7");
  await expect(page.getByText("Pending confirmation", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("Insufficient available stock: requested 5, available 0, short by 5.").first()).toBeVisible();

  // 4. MRP backward scheduling -> 2 planned work orders (FG-BOT=5, SUB-ARM=10) + 1 planned requisition (MAT-ROD=33) with provenance drilldown.
  await submit(page, "manufacturing.mrprun.create", "manufacturing.mrprun", "MRP-S7", { title: "October MRP Run" });
  await submit(page, "manufacturing.mrprun.execute", "manufacturing.mrprun", "MRP-S7", {});
  await page.goto("/manufacturing/manufacturing.mrprun/MRP-S7");
  await expect(page.getByText("Completed", { exact: true }).first()).toBeVisible({ timeout: 30_000 });

  await page.goto("/manufacturing/manufacturing.plannedrequisition/PREQ-00001");
  await expect(page.getByText("sales_order:SO-S7 -> FG-BOT -> SUB-ARM").first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText("gross 33 - supply 0 = net 33").first()).toBeVisible();
  await followReference(property(page, "Source document").getByRole("link", { name: "SO-S7", exact: true }), "SO-S7", /\/SO-S7$/);

  await submit(page, "manufacturing.plannedrequisition.confirm", "manufacturing.plannedrequisition", "PREQ-00001", {});
  await expect.poll(async () => (await readRecord("manufacturing.plannedrequisition", "PREQ-00001"))?.generatedid, { timeout: 30_000 })
    .toBe("REQ-MRP-PREQ-00001");
  await page.goto("/purchasing/purchasing.requisition/REQ-MRP-PREQ-00001");
  await expect(page.getByText("Alloy shaft").first()).toBeVisible({ timeout: 30_000 });
  await expect(property(page, "Base quantity")).toContainText("33");

  // 5. Purchase receipt + non-atomic GL reconciliation: hold gl.post/1, inspect 330 diff & click RCV-S7 drilldown, verify close guard, then resume.
  await submit(page, "purchasing.order.create", "purchasing.order", "PO-S7", {
    title: "PO-S7", supplier: "SUP-S7", sourcekind: "manual", currency: "CNY", orderedon: "2026-10-11", expectedon: "2026-10-15",
  });
  await submit(page, "purchasing.orderline.create", "purchasing.orderline", "POL-S7", {
    order: "PO-S7", material: "MAT-ROD", quantity: 33, unitprice: 10, expectedon: "2026-10-15",
  });
  await submit(page, "purchasing.order.approve", "purchasing.order", "PO-S7", {});

  await submit(page, "platform.protocol.bind", "platform.protocol", "gl.post/1", { provider: "build", paused: true });
  await submit(page, "purchasing.receipt.create", "purchasing.receipt", "RCV-S7", {
    title: "RCV-S7", order: "PO-S7", supplier: "SUP-S7", warehouse: "MAIN", receivedon: "2026-10-12",
  });
  await submit(page, "purchasing.receiptline.create", "purchasing.receiptline", "RL-S7", {
    receipt: "RCV-S7", orderline: "POL-S7", material: "MAT-ROD", warehouse: "MAIN", quantity: 33, unitprice: 10,
  });
  await submit(page, "purchasing.receipt.post", "purchasing.receipt", "RCV-S7", {});

  await expect.poll(async () => {
    const book = await readRecord("accounting.book", "DEFAULT");
    return { sub: Number(book?.subledgervalue), gl: Number(book?.glinventorybalance), diff: Number(book?.reconciliationdiff), inflight: Number(book?.inflightcount) };
  }, { timeout: 30_000 }).toEqual({ sub: 330, gl: 0, diff: 330, inflight: 1 });

  await page.goto("/accounting/accounting.book/DEFAULT");
  await expect(property(page, "Subledger stock value")).toContainText("330", { timeout: 30_000 });
  await expect(property(page, "GL inventory balance")).toContainText("0");
  await expect(property(page, "Reconciliation difference")).toContainText("330");
  await expect(property(page, "Undelivered vouchers")).toContainText("1");
  const inflightTable = page.getByRole("table", { name: "Journal voucher In-Flights" });
  await expect(inflightTable).toBeVisible();
  await expect(inflightTable.getByRole("row").filter({ hasText: "Purchase receipt inventory posting" })).toBeVisible();
  await followReference(inflightTable.locator('a[href$="/RCV-S7"]').first(), "RCV-S7", /\/RCV-S7$/);

  await expect(submit(page, "accounting.period.closeperiod", "accounting.period", "2026-10", {}))
    .rejects.toThrow(/reconciliation=330, inflight=1/);

  await submit(page, "platform.protocol.bind", "platform.protocol", "gl.post/1", { provider: "build", paused: false });
  await expect.poll(async () => {
    const book = await readRecord("accounting.book", "DEFAULT");
    return { sub: Number(book?.subledgervalue), gl: Number(book?.glinventorybalance), diff: Number(book?.reconciliationdiff), inflight: Number(book?.inflightcount) };
  }, { timeout: 30_000 }).toEqual({ sub: 330, gl: 330, diff: 0, inflight: 0 });
  await page.goto("/accounting/accounting.book/DEFAULT");
  await expect(property(page, "GL inventory balance")).toContainText("330", { timeout: 30_000 });
  await expect(property(page, "Reconciliation difference")).toContainText("0");

  // 6. Multi-stage production from MRP work order suggestions: SUB-ARM (10 units @ 33 = 330) -> FG-BOT (5 units @ 66 = 330).
  await submit(page, "manufacturing.plannedworkorder.confirm", "manufacturing.plannedworkorder", "PWO-00002", { bom: "BOM-ARM", warehouse: "MAIN" });
  const armWoId = String((await readRecord("manufacturing.plannedworkorder", "PWO-00002"))?.generatedworkorder);
  expect(armWoId).toBeTruthy();
  await submit(page, "manufacturing.workorder.release", "manufacturing.workorder", armWoId, {});
  await submit(page, "manufacturing.workorder.issue", "manufacturing.workorder", armWoId, { on: "2026-10-14" });
  await expect.poll(async () => Number((await readRecord("manufacturing.workorder", armWoId))?.totalcost), { timeout: 30_000 }).toBe(330);
  await submit(page, "manufacturing.workorder.complete", "manufacturing.workorder", armWoId, { on: "2026-10-16" });

  await submit(page, "manufacturing.plannedworkorder.confirm", "manufacturing.plannedworkorder", "PWO-00001", { bom: "BOM-FG", warehouse: "MAIN" });
  const fgWoId = String((await readRecord("manufacturing.plannedworkorder", "PWO-00001"))?.generatedworkorder);
  expect(fgWoId).toBeTruthy();
  await submit(page, "manufacturing.workorder.release", "manufacturing.workorder", fgWoId, {});
  await submit(page, "manufacturing.workorder.issue", "manufacturing.workorder", fgWoId, { on: "2026-10-17" });
  await expect.poll(async () => Number((await readRecord("manufacturing.workorder", fgWoId))?.totalcost), { timeout: 30_000 }).toBe(330);
  await submit(page, "manufacturing.workorder.complete", "manufacturing.workorder", fgWoId, { on: "2026-10-19" });
  await expect.poll(async () => {
    const book = await readRecord("accounting.book", "DEFAULT");
    return { sub: Number(book?.subledgervalue), gl: Number(book?.glinventorybalance), diff: Number(book?.reconciliationdiff) };
  }, { timeout: 30_000 }).toEqual({ sub: 330, gl: 330, diff: 0 });
  await page.goto(`/manufacturing/manufacturing.workorder/${fgWoId}`);
  await expect(page.getByText("Completed", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await expect(property(page, "Total material cost")).toContainText("330");

  // 7. Re-confirm SO-S7 -> outbound delivery DN-S7 (COGS = 330) -> customer invoice SINV-S7 (1,000 + 130 tax = 1,130).
  await submit(page, "sales.order.confirm", "sales.order", "SO-S7", {});
  await expect.poll(async () => (await readRecord("sales.order", "SO-S7"))?.state, { timeout: 30_000 }).toBe("confirmed");

  await submit(page, "sales.delivery.create", "sales.delivery", "DN-S7", {
    title: "DN-S7", order: "SO-S7", customer: "CUST-S7", warehouse: "MAIN", postedon: "2026-10-20", currency: "CNY",
  });
  await submit(page, "sales.deliveryline.create", "sales.deliveryline", "DNL-S7", {
    delivery: "DN-S7", orderline: "SOL-S7", material: "FG-BOT", quantity: 5, unitprice: 200,
  });
  await submit(page, "sales.delivery.post", "sales.delivery", "DN-S7", {});
  await expect.poll(async () => Number((await readRecord("sales.delivery", "DN-S7"))?.totalcost), { timeout: 30_000 }).toBe(330);
  await submit(page, "sales.delivery.queuegl", "sales.delivery", "DN-S7", {});
  await page.goto("/sales/sales.delivery/DN-S7");
  await expect(page.getByText("Posted", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await expect(property(page, "Cost of goods sold")).toContainText("330");

  await submit(page, "sales.invoice.create", "sales.invoice", "SINV-S7", {
    title: "SINV-S7", order: "SO-S7", delivery: "DN-S7", customer: "CUST-S7", postedon: "2026-10-21", duedate: "2026-11-21", currency: "CNY",
  });
  await submit(page, "sales.invoiceline.create", "sales.invoiceline", "SIL-S7", {
    invoice: "SINV-S7", orderline: "SOL-S7", deliveryline: "DNL-S7", material: "FG-BOT", quantity: 5, unitprice: 200, taxrate: 13,
  });
  await submit(page, "sales.invoice.post", "sales.invoice", "SINV-S7", {});
  await page.goto("/sales/sales.invoice/SINV-S7");
  await expect(page.getByText("Posted", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await expect(property(page, "Receivable amount")).toContainText(/1,?130/);

  await page.goto("/sales/sales.order/SO-S7");
  await expect(page.getByRole("heading", { name: "Downstream documents", exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(page.getByRole("link", { name: /DN-S7/ }).first()).toBeVisible();
  await expect(page.getByRole("link", { name: /SINV-S7/ }).first()).toBeVisible();

  // 8. Reconcile book, close period 2026-10, verify Trial Balance summary + drilldown to journal line and source invoice, and reject backdated receipt.
  await expect.poll(async () => {
    const book = await readRecord("accounting.book", "DEFAULT");
    return {
      sub: Number(book?.subledgervalue),
      gl: Number(book?.glinventorybalance),
      diff: Number(book?.reconciliationdiff),
      inflight: Number(book?.inflightcount),
      tb: Number(book?.trialbalancediff),
    };
  }, { timeout: 30_000 }).toEqual({ sub: 0, gl: 0, diff: 0, inflight: 0, tb: 0 });

  await submit(page, "accounting.period.closeperiod", "accounting.period", "2026-10", {});
  await page.goto("/accounting/accounting.period/2026-10");
  await expect(page.getByText("Closed", { exact: true }).first()).toBeVisible({ timeout: 30_000 });

  await page.goto("/accounting/accounting.journalline?view=" + encodeURIComponent(JSON.stringify({
    version: 1, layout: "summary", columns: ["on", "title", "account", "debit", "credit", "net", "journal", "sourceid"], group: "account", measure: "sum:net", dates: "on", from: "2026-10-01", to: "2026-10-31",
  })));
  const tb = page.getByRole("table", { name: "Summary", exact: true });
  await expect(tb).toBeVisible({ timeout: 30_000 });
  await expect(tb.getByRole("row").filter({ hasText: "Accounts Receivable" })).toContainText(/1,?130/);
  await expect(tb.getByRole("row").filter({ hasText: "Inventory" })).toContainText("990");
  await expect(tb.getByRole("row").filter({ hasText: "Cost of Goods Sold" })).toContainText("330");
  await expect(tb.getByRole("row").filter({ hasText: "Sales Revenue" })).toContainText(/-1,?000/);
  await expect(tb.getByRole("row").filter({ hasText: "Output Tax Payable" })).toContainText("-130");
  await expect(tb.getByRole("row").filter({ hasText: "Goods Received Not Invoiced" })).toContainText("-330");
  const totalsRow = tb.locator("tfoot").getByRole("row").first();
  await expect(totalsRow).toContainText(/3,?110/);
  await expect(totalsRow.getByRole("link").last()).toHaveText("0");

  await tb.getByRole("row").filter({ hasText: "Accounts Receivable" }).getByRole("link", { name: /1,?130/ }).first().click();
  const arJournalRow = page.getByRole("row").filter({ hasText: "Accounts Receivable" }).filter({ hasText: "SINV-S7" }).first();
  await expect(arJournalRow).toBeVisible({ timeout: 30_000 });
  await followReference(arJournalRow.locator('a[href$="/SINV-S7"]').first(), "SINV-S7", /\/SINV-S7$/);

  await submit(page, "purchasing.receipt.create", "purchasing.receipt", "RCV-BACK-S7", {
    title: "RCV-BACK-S7", supplier: "SUP-S7", warehouse: "MAIN", receivedon: "2026-10-28",
  });
  await submit(page, "purchasing.receiptline.create", "purchasing.receiptline", "RL-BACK-S7", {
    receipt: "RCV-BACK-S7", material: "MAT-ROD", warehouse: "MAIN", quantity: 2, unitprice: 10,
  });
  await submit(page, "purchasing.receipt.post", "purchasing.receipt", "RCV-BACK-S7", {});
  await expect.poll(async () => (await readRecord("purchasing.receipt", "RCV-BACK-S7"))?.state, { timeout: 30_000 }).toBe("rejected");

  await page.goto("/purchasing/purchasing.receipt/RCV-BACK-S7");
  await expect(page.getByText("Rejected", { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await expect(page.getByText(/ledger is closed before 2026-11-01/).first()).toBeVisible();

  const elapsedSec = (performance.now() - gateStarted) / 1000;
  expect(elapsedSec).toBeLessThan(600);
  console.log("ERP_STEP7_GATE", JSON.stringify({ elapsedSec: Number(elapsedSec.toFixed(3)), armWoId, fgWoId, frames: frameCount }));
  } finally {
    if (cdp && videoOut && frameDir) {
      await cdp.send("Page.stopScreencast").catch(() => {});
      if (frameCount > 0) {
        execFileSync("ffmpeg", ["-y", "-framerate", "10", "-i", join(frameDir, "frame-%05d.jpg"), "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-movflags", "+faststart", videoOut], { stdio: "ignore" });
      }
      rmSync(frameDir, { recursive: true, force: true });
    }
  }
});
