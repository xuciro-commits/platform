// Browser smoke: canonical build/use paths. Visual layout is reviewed manually.
import {Builder,pageUIProfile, expect, test } from "./kit";
import type { Page } from "@playwright/test";
import { decide, fresh, open } from "./host";

const value = (page: Page, text: string) => page.getByRole("definition").filter({ hasText: new RegExp(`^${text}$`, "i") });

test("route 17: every action has an entry, pages follow changes", async ({ browser, page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Acme " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Board offsite" });
  await open(page, "sales", `/record?type=crm.opportunity&id=${opp}`);
  for (const action of ["Close opportunity", "Plan group stay", "Book stay"]) {
    await expect(page.getByRole("button", { name: action })).toBeVisible();
  }
  await page.getByRole("button", { name: "Plan group stay" }).click();
  const plan = page.getByRole("dialog");
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1.5");
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).blur();
  await plan.getByLabel("The provider's room type", { exact: false }).selectOption("standard");
  await expect(plan.getByText("Enter a valid value.")).toBeVisible();
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1");
  await plan.getByRole("button",{name:/^First night \*/}).click();
  await plan.getByRole("textbox",{name:"First night",exact:true}).fill("2030-01-30");
  await plan.getByRole("textbox",{name:"Departure",exact:true}).fill("2030-02-02");
  await plan.getByRole("group",{name:"Choose a date range"}).getByRole("button",{name:"Close",exact:true}).click();
  await expect(plan.getByText("3 nights",{exact:true})).toBeVisible();
  await plan.getByRole("button",{name:/^First night \*/}).click();
  const first=plan.getByRole("button",{name:"2030-01-30",exact:true}),last=plan.getByRole("button",{name:"2030-02-02",exact:true});
  await first.hover();await page.mouse.down();await last.hover();await page.mouse.up();
  await expect(plan.getByRole("group",{name:"Choose a date range"})).toHaveCount(0);
  await expect(plan.getByText("3 nights",{exact:true})).toBeVisible();
  await page.setViewportSize({width:480,height:800});
  await plan.getByRole("button",{name:/^First night \*/}).click();
  const calendar=plan.getByRole("group",{name:"Choose a date range"});
  await expect(calendar.getByRole("button",{name:"2030-02-02",exact:true})).toHaveCount(0);
  const keyboardStart=calendar.getByRole("button",{name:"2030-01-30",exact:true});
  await keyboardStart.focus();await keyboardStart.press("Enter");await keyboardStart.press("ArrowRight");
  await calendar.getByRole("button",{name:"2030-01-31",exact:true}).press("Enter");
  await expect(plan.getByText("1 night",{exact:true})).toBeVisible();
  await page.setViewportSize({width:1280,height:720});
  await plan.getByLabel("The last day the rooms are held", { exact: false }).fill("2030-02-02");
  await plan.getByRole("button",{name:"Plan group stay"}).click();
  await expect(plan.getByRole("alert")).toContainText("before First night");
  await plan.getByLabel("The last day the rooms are held", { exact: false }).fill("2030-01-29");
  const keys: string[] = [];
  await page.route("**/v1/submissions", async route => {
    const submission = route.request().postDataJSON();
    if (submission.schema?.name !== "crm.opportunity.plan") return route.continue();
    keys.push(submission.idempotencyKey);
    if (keys.length === 1) return route.abort();
    await route.fulfill({status:400,contentType:"application/json",body:JSON.stringify({error:{code:"ERROR_CODE_INVALID_ARGUMENT",message:"The rooms are unavailable",issues:[{code:"availability",path:["roomType"],message:"Choose another room type"}]}})});
  });
  await plan.getByRole("button", { name: "Plan group stay" }).click();
  await expect(plan.getByRole("button", { name: "Retry confirmation" })).toBeVisible();
  await expect(plan.getByLabel("The provider's room type", { exact: false })).toHaveValue("standard");
  await plan.getByRole("button", { name: "Retry confirmation" }).click();
  await expect(plan.getByText("Choose another room type")).toBeVisible();
  expect(keys).toHaveLength(2);expect(keys[1]).toBe(keys[0]);
  await expect(plan.getByRole("button",{name:/^First night \*/})).toContainText("2030-01-30");
  await expect(plan.getByLabel("The provider's room type", { exact: false })).toHaveValue("standard");
  await page.unroute("**/v1/submissions");
  await plan.getByRole("button", { name: "Cancel" }).click();
  await page.getByRole("button", { name: "Close opportunity" }).click();
  await page.getByRole("dialog").getByRole("combobox").first().selectOption("won");
  await page.getByRole("dialog").getByRole("button", { name: "Close opportunity" }).click();
  await expect(value(page, "won")).toBeVisible();
  // Bound creation uses the shared capability route. A lost reply retains its
  // request key; an explicit refusal permits a corrected proposal.
  const builder=new Builder(request,"manager");
  const boundObject=await builder.object({title:"Bound input records",fields:[{name:"note",title:"Note",type:"text",required:true},{name:"quantity",title:"Quantity",type:"integer",required:true}]});
  const boundPage=await builder.page({title:"Bound input form",object:boundObject.type,
    sections:[{id:"create",widget:"form",configVersion:1,fields:["note"],inputs:{quantity:{source:"literal",value:3}}}],
    document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:{root:{kind:"rows",children:["create"]},create:{kind:"widget",section:"create"}}},
  });
  await builder.decide("build.page.publish",{type:"build.page",id:boundPage.id},{});
  const bound=await browser.newPage({baseURL:"http://127.0.0.1:18496",locale:"en-US"});
  try{
  await builder.open(bound,`/page?app=build&kind=page&name=${boundPage.name}`);
  const note=bound.getByRole("textbox",{name:/^Note/});
  await note.fill("Retain this note");
  const invocations:{key:string;target:string}[]=[];
  await bound.route("**/v1/capabilities/invoke",async route=>{
    invocations.push(route.request().postDataJSON());
    if(invocations.length===1)return route.abort();
    return route.fulfill({status:400,contentType:"application/json",body:JSON.stringify({error:{code:"ERROR_CODE_INVALID_ARGUMENT",message:"Bound form refused"}})});
  });
  await bound.getByRole("button",{name:"Create",exact:true}).click();
  await expect(note).toHaveValue("Retain this note");
  await expect(bound.getByRole("button",{name:"Create",exact:true})).toBeEnabled();
  await bound.getByRole("button",{name:"Create",exact:true}).click();
  await expect(bound.getByText("Bound form refused",{exact:true})).toBeVisible();
  expect(invocations).toHaveLength(2);expect(invocations[1]).toMatchObject({key:invocations[0]!.key,target:invocations[0]!.target});
  await bound.unroute("**/v1/capabilities/invoke");
  await note.fill("Corrected note");
  await bound.getByRole("button",{name:"Create",exact:true}).click();
  await expect(note).toHaveValue("");
  expect(await builder.record(boundObject.type,invocations[0]!.target)).toMatchObject({note:"Corrected note",quantity:3});

  }finally{await bound.close();}
});

test("route 4: an approval reaches the requester's page", async ({ page, request }) => {
  const leave = fresh("LEA");
  const day = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString().slice(0, 10);
  await open(page, "sales", "/definitions");
  await page.getByRole("textbox", { name: "Filter rows", exact: true }).fill("hcm/page/leaves");
  await page.getByRole("row").filter({ hasText: "hcm/page/leaves" }).click();
  await page.getByRole("button", { name: "Open page" }).click();
  await page.getByRole("button", { name: /Draft leave request/ }).click();
  const draft = page.getByRole("dialog");
  await draft.getByRole("textbox").first().fill(leave);
  await draft.getByRole("combobox").first().selectOption("vacation");
  await draft.getByRole("button",{name:/^First day \*/}).click();
  await draft.getByRole("textbox",{name:"First day",exact:true}).fill(day(30));
  await draft.getByRole("textbox",{name:"Last day",exact:true}).fill(day(32));
  await draft.getByRole("group",{name:"Choose a date range"}).getByRole("button",{name:"Close",exact:true}).click();
  await expect(draft.getByText("3 days",{exact:true})).toBeVisible();
  await draft.getByRole("button", { name: "Create" }).click();
  await page.getByRole("textbox", { name: "Search" }).fill(leave);
  const leaveRow = page.getByRole("row").filter({ hasText: leave });
  await expect(leaveRow).toBeVisible();
  await leaveRow.click();
  await page.getByRole("button", { name: "Submit for approval" }).click();
  await expect(value(page, "Pending approval")).toBeVisible();
  await expect(page.getByText(/^waiting for Manager:/)).toBeVisible();
  const chain = page.getByRole("figure", { name: "Approvals" }); // the chain drawn (#122)
  await expect(chain.getByText("Manager", { exact: true })).toBeVisible();
  await expect(chain.getByText("sales-1", { exact: true })).toBeVisible();
  const managerPage = await page.context().newPage();
  await open(managerPage, "manager", "/inbox");
  const task = managerPage.getByRole("listitem").filter({ hasText: leave });
  await expect(task).toBeVisible();
  await task.getByRole("button").first().click();
  await managerPage.getByRole("button", { name: "Open related record" }).click();
  await managerPage.getByRole("region", { name: "Approvals", exact: true }).getByRole("button", { name: "Submit for approval", exact: true }).click();
  const approvalID = new URLSearchParams(new URL(managerPage.url()).hash.split("?")[1]).get("id")!;
  await managerPage.getByRole("region", { name: approvalID, exact: true }).getByRole("button", { name: "Approve", exact: true }).click();
  await managerPage.getByRole("dialog").getByRole("button", { name: "Approve", exact: true }).click();
  await managerPage.getByRole("region", { name: approvalID, exact: true }).getByRole("button", { name: "Back to inbox", exact: true }).click();
  await expect(value(page, "Approved")).toBeVisible();
  await managerPage.close();
});

test("route 28: preview cannot submit an action", async ({ page }) => {
  await open(page, "sales", "/definitions");
  await page.getByRole("textbox", { name: "Filter rows", exact: true }).fill("crm/page/opportunities");
  await page.getByRole("row").filter({ hasText: "crm/page/opportunities" }).click();
  await page.getByRole("button", { name: "Preview page" }).click();
  await expect(page.getByText("Preview uses sample data. Actions do not run.")).toBeVisible();
  const writes: string[] = [];
  page.on("request", (r) => { if (r.method() !== "GET" && r.url().includes("/v1/")) writes.push(r.url()); });
  await page.getByRole("button", { name: "Plan group stay" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Preview only")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Plan group stay" })).toBeDisabled();
  expect(writes).toEqual([]);
});

test("route 20: field security", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Margin " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Retreat" });
  await decide(request, "manager", "crm", "crm.opportunity.edit", { type: "crm.opportunity", id: opp }, { margin: 31.5 });
  await open(page, "manager", `/record?type=crm.opportunity&id=${opp}`);
  await expect(page.getByRole("term").filter({ hasText: "Expected margin" })).toBeVisible();
  await expect(value(page, "31.50")).toBeVisible();
  const salesPage = await page.context().newPage(); // another member: a page of its own
  await open(salesPage, "sales", `/record?type=crm.opportunity&id=${opp}`);
  await expect(salesPage.getByText("Retreat").first()).toBeVisible();
  await expect(salesPage.getByText("Expected margin")).toHaveCount(0);
});
