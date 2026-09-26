// docs/Testing.md's routes 4, 5, 17 and 18, walked in a browser.
import { expect, test, type Page } from "@playwright/test";
import { decide, fresh, open } from "./host";

/** A field's value on the record page, not the status bar's steps or the history. */
const value = (page: Page, text: string) => page.getByRole("definition").filter({ hasText: new RegExp(`^${text}$`, "i") });

// Route 17 (#118): a record page offers the type's declared actions, asks a
// transition's payload in a form, and follows the change without a reload.
test("route 17: every action has an entry, pages follow changes", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Acme " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Board offsite" });
  await open(page, "sales", `/record?type=crm.opportunity&id=${opp}`);
  for (const action of ["Close opportunity", "Plan group stay", "Book stay"]) {
    await expect(page.getByRole("button", { name: action })).toBeVisible();
  }
  await page.getByRole("button", { name: "Close opportunity" }).click();
  await page.getByRole("dialog").getByRole("textbox").first().fill("won");
  await page.getByRole("dialog").getByRole("button", { name: "Close opportunity" }).click();
  await expect(value(page, "won")).toBeVisible();
});

// Route 5 (ADR-0026): a group's rooms are held at the hotel until a cutoff; won, they are confirmed.
test("route 5: a group block is held, then confirmed", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Group " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Retreat" });
  const day = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString().slice(0, 10);
  await decide(request, "sales", "crm", "crm.opportunity.plan", { type: "crm.opportunity", id: opp },
    { rooms: 1, roomType: "standard", arrive: day(200 + Math.floor(Math.random() * 100)), depart: day(310), cutoff: day(150) });
  await open(page, "sales", `/record?type=crm.opportunity&id=${opp}`);
  await expect(value(page, "held")).toBeVisible();
  await decide(request, "sales", "crm", "crm.opportunity.close", { type: "crm.opportunity", id: opp }, { outcome: "won" });
  await expect(value(page, "confirmed")).toBeVisible(); // the page follows without a reload
});

// Route 4 (#118): a leave request approved by the manager; the requester's open
// page turns approved by itself, and the requester may open the approval.
test("route 4: an approval reaches the requester's page", async ({ page, request }) => {
  const leave = fresh("LEA");
  const day = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString().slice(0, 10);
  await decide(request, "sales", "hcm", "hcm.leave.create", { type: "hcm.leave", id: leave }, { kind: "vacation", from: day(30), until: day(31) });
  const submitted = await decide(request, "sales", "hcm", "hcm.leave.submit", { type: "hcm.leave", id: leave }, {});
  await open(page, "sales", `/record?type=hcm.leave&id=${leave}`);
  await expect(value(page, "Draft")).toBeVisible();
  const approval = `hcm.${submitted.key}`;
  await decide(request, "manager", "work", "work.approval.approve", { type: "work.approval", id: approval }, {});
  await expect(value(page, "Approved")).toBeVisible();
  const opened = await request.get(`/v1/records/work.approval/${encodeURIComponent(approval)}`, { headers: { Authorization: "Bearer sales" } });
  expect(opened.status()).toBe(200);
});

// Route 18 (ADR-0027): the process answers /healthz; Settings → Automation shows the tenant's health.
test("route 18: health", async ({ page, request }) => {
  expect((await (await request.get("/healthz")).json()).status).toBe("ok");
  await open(page, "manager", "/automation");
  await expect(page.getByText(/^(Healthy|Needs attention)$/).first()).toBeVisible();
});
