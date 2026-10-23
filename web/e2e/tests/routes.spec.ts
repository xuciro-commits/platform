// Browser smoke: canonical build/use paths. Visual layout is reviewed manually.
import { Builder, pageUIProfile, expect, test } from "./kit";
import type { Page } from "@playwright/test";
import { decide, fresh, open } from "./host";

const value = (page: Page, text: string) =>
  page
    .getByRole("definition")
    .filter({ hasText: new RegExp(`^${text}$`, "i") });

// These are editable declarations, so the shared form checks have no native industry dependency.
async function routeRecords(builder: Builder) {
  const account = await builder.object({ title: "Account", fields: [{ name: "name", title: "Name", type: "text", required: true }, { name: "kind", title: "Kind", type: "text" }], access: [{ role: "user", read: "all", create: true, edit: true }] });
  const task = await builder.object({ title: "Opportunity", fields: [
    { name: "account", title: "Account", type: "reference", ref: account.type, required: true },
    { name: "title", title: "What is being sold", type: "text", required: true },
    { name: "margin", title: "Expected margin", type: "decimal", read: ["builder"], write: ["builder"] },
  ], access: [{ role: "user", read: "all", create: true, edit: true }], states: [{ name: "open", title: "Open" }, { name: "won", title: "won" }], actions: [
    { name: "close", title: "Close opportunity", from: ["open"], to: "won", inputs: [{ name: "outcome", title: "Outcome", type: "choice", choices: "won,lost", required: true }] },
    { name: "plan", title: "Plan group stay", from: ["open"], inputs: [
      { name: "rooms", title: "Rooms, 1 to 20", type: "integer", required: true, constraints: { min: 1, max: 20 } },
      { name: "roomtype", title: "The provider's room type", type: "choice", choices: "standard,suite", required: true },
      { name: "arrive", title: "First night", type: "date", required: true, range: { end: "depart" } },
      { name: "depart", title: "Departure", type: "date", required: true, constraints: { after: "arrive" } },
      { name: "cutoff", title: "The last day the rooms are held", type: "date", required: true, constraints: { before: "arrive" } },
    ] },
    { name: "book", title: "Book stay", from: ["open"] },
  ] });
  return { account, task };
}

test("route 17: every action has an entry, pages follow changes", async ({
  browser,
  page,
  request,
}) => {
  test.setTimeout(90_000);
  const model = await routeRecords(new Builder(request, "manager"));
  const account = fresh("ACC"),
    opp = fresh("OPP");
  await decide(
    request,
    "sales",
    "build",
    `${model.account.type}.create`,
    { type: model.account.type, id: account },
    { name: "Acme " + account, kind: "company" },
  );
  await open(page, "sales", `/page?app=build&kind=page&name=${model.task.name}`);
  await page
    .getByRole("button", { name: "Create opportunity", exact: true })
    .click();
  const opening = page.getByRole("dialog");
  await opening.getByRole("textbox", { name: "ID *", exact: true }).fill(opp);
  await opening.getByRole("combobox", { name: /^Account/ }).fill(account);
  await opening.getByRole("option").filter({ hasText: account }).click();
  await opening
    .getByRole("textbox", { name: /^What is being sold/ })
    .fill("Board offsite");
  await opening
    .getByRole("button", { name: "Create", exact: true })
    .click();
  await expect(opening).toHaveCount(0);
  await open(page, "sales", `/record?type=${model.task.type}&id=${opp}`);
  for (const action of ["Close opportunity", "Plan group stay", "Book stay"]) {
    await expect(page.getByRole("button", { name: action })).toBeVisible();
  }
  await page.getByRole("button", { name: "Plan group stay", exact: true }).click();
  const plan = page.getByRole("dialog");
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1.5");
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).blur();
  await plan
    .getByLabel("The provider's room type", { exact: false })
    .selectOption("standard");
  await expect(plan.getByText("Enter a valid value.")).toBeVisible();
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1");
  await plan.getByRole("button", { name: /^First night \*/ }).click();
  await plan
    .getByRole("textbox", { name: "First night", exact: true })
    .fill("2030-01-30");
  await plan
    .getByRole("textbox", { name: "Departure", exact: true })
    .fill("2030-02-02");
  await plan
    .getByRole("group", { name: "Choose a date range" })
    .getByRole("button", { name: "Close", exact: true })
    .click();
  await expect(plan.getByText("3 nights", { exact: true })).toBeVisible();
  await plan.getByRole("button", { name: /^First night \*/ }).click();
  const first = plan.getByRole("button", { name: "2030-01-30", exact: true }),
    last = plan.getByRole("button", { name: "2030-02-02", exact: true });
  await first.hover();
  await page.mouse.down();
  await last.hover();
  await page.mouse.up();
  await expect(
    plan.getByRole("group", { name: "Choose a date range" }),
  ).toHaveCount(0);
  await expect(plan.getByText("3 nights", { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 480, height: 800 });
  await plan.getByRole("button", { name: /^First night \*/ }).click();
  const calendar = plan.getByRole("group", { name: "Choose a date range" });
  await expect(
    calendar.getByRole("button", { name: "2030-02-02", exact: true }),
  ).toHaveCount(0);
  const keyboardStart = calendar.getByRole("button", {
    name: "2030-01-30",
    exact: true,
  });
  await keyboardStart.focus();
  await keyboardStart.press("Enter");
  await keyboardStart.press("ArrowRight");
  await calendar
    .getByRole("button", { name: "2030-01-31", exact: true })
    .press("Enter");
  await expect(plan.getByText("1 night", { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 1280, height: 720 });
  await plan
    .getByLabel("The last day the rooms are held", { exact: false })
    .fill("2030-02-02");
  await plan.getByRole("button", { name: "Plan group stay", exact: true }).click();
  await expect(plan.getByRole("alert")).toContainText("before First night");
  await plan
    .getByLabel("The last day the rooms are held", { exact: false })
    .fill("2030-01-29");
  const keys: string[] = [];
  await page.route("**/v1/submissions", async (route) => {
    const submission = route.request().postDataJSON();
    if (submission.schema?.name !== `${model.task.type}.plan`)
      return route.continue();
    keys.push(submission.idempotencyKey);
    if (keys.length === 1) return route.abort();
    await route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({
        error: {
          code: "ERROR_CODE_INVALID_ARGUMENT",
          message: "The rooms are unavailable",
          issues: [
            {
              code: "availability",
              path: ["roomtype"],
              message: "Choose another room type",
            },
          ],
        },
      }),
    });
  });
  await plan.getByRole("button", { name: "Plan group stay", exact: true }).click();
  await expect(
    plan.getByRole("button", { name: "Retry confirmation" }),
  ).toBeVisible();
  await expect(
    plan.getByLabel("The provider's room type", { exact: false }),
  ).toHaveValue("standard");
  await plan.getByRole("button", { name: "Retry confirmation" }).click();
  await expect(plan.getByText("Choose another room type")).toBeVisible();
  expect(keys).toHaveLength(2);
  expect(keys[1]).toBe(keys[0]);
  await expect(
    plan.getByRole("button", { name: /^First night \*/ }),
  ).toContainText("2030-01-30");
  await expect(
    plan.getByLabel("The provider's room type", { exact: false }),
  ).toHaveValue("standard");
  await page.unroute("**/v1/submissions");
  await plan.getByRole("button", { name: "Cancel" }).click();
  await page.getByRole("button", { name: "Close opportunity", exact: true }).click();
  await page
    .getByRole("dialog")
    .getByRole("combobox")
    .first()
    .selectOption("won");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Close opportunity", exact: true })
    .click();
  await expect(value(page, "won")).toBeVisible();
  // Bound creation uses the shared capability route. A lost reply retains its
  // request key; an explicit refusal permits a corrected proposal.
  const builder = new Builder(request, "manager");
  const boundObject = await builder.object({
    title: "Bound input records",
    fields: [
      { name: "note", title: "Note", type: "text", required: true },
      { name: "quantity", title: "Quantity", type: "integer", required: true },
    ],
    states: [{ name: "open", title: "Open" }],
    actions: [
      {
        name: "schedule",
        title: "Schedule input",
        from: ["open"],
        inputs: [
          {
            name: "account",
            title: "Account",
            type: "reference",
            ref: model.account.type,
            required: true,
          },
          {
            name: "arrive",
            title: "First night",
            type: "date",
            required: true,
            range: { end: "depart" },
          },
          {
            name: "depart",
            title: "Departure",
            type: "date",
            required: true,
            constraints: { after: "arrive" },
          },
        ],
      },
    ],
  });
  const builderTab = await browser.newPage({
    baseURL: "http://127.0.0.1:18496",
    locale: "en-US",
  });
  try {
    // The builder configures the same references and date contract used by business actions.
    await builder.open(
      builderTab,
      `/action-type?id=${boundObject.id}&action=schedule`,
    );
    const structure = builderTab.getByRole("region", {
      name: "Action structure",
      exact: true,
    });
    const inspector = builderTab.getByRole("region", {
      name: "Action inspector",
      exact: true,
    });
    await structure.getByRole("button", { name: "Form", exact: true }).click();
    await builderTab.getByRole("button", { name: "Visual", exact: true }).click();
    await builderTab
      .getByRole("button", { name: "Account", exact: true })
      .click();
    await inspector
      .getByRole("combobox", { name: "Reference object", exact: true })
      .selectOption(model.account.type);
    await builderTab
      .getByRole("button", { name: "First night", exact: true })
      .click();
    await expect(
      inspector.getByRole("combobox", { name: "Date range end", exact: true }),
    ).toHaveValue("depart");
    await builderTab.getByRole("button", { name: /^First night \*/ }).click();
    const previewRange = builderTab.getByRole("group", {
      name: "Choose a date range",
      exact: true,
    });
    await previewRange
      .getByRole("textbox", { name: "First night", exact: true })
      .fill("2030-01-30");
    await previewRange
      .getByRole("textbox", { name: "Departure", exact: true })
      .fill("2030-02-30");
    await expect(
      previewRange.getByRole("textbox", { name: "Departure", exact: true }),
    ).toHaveValue("2030-02-30");
    await expect(
      previewRange.getByRole("textbox", { name: "Departure", exact: true }),
    ).toHaveAttribute("aria-invalid", "true");
    await previewRange
      .getByRole("textbox", { name: "Departure", exact: true })
      .fill("2030-02-02");
    await previewRange
      .getByRole("button", { name: "Close", exact: true })
      .click();
    await expect(
      builderTab.getByText("3 nights", { exact: true }),
    ).toBeVisible();
  } finally {
    await builderTab.close();
  }
  const boundPage = await builder.page({
    title: "Bound input form",
    object: boundObject.type,
    sections: [
      {
        id: "create",
        widget: "form",
        configVersion: 1,
        fields: ["note"],
        inputs: { quantity: { source: "literal", value: 3 } },
      },
    ],
    document: {
      formatVersion: 2,
      uiProfile: pageUIProfile,
      root: "root",
      nodes: {
        root: { kind: "rows", children: ["create"] },
        create: { kind: "widget", section: "create" },
      },
    },
  });
  await builder.decide(
    "build.page.publish",
    { type: "build.page", id: boundPage.id },
    {},
  );
  const bound = await browser.newPage({
    baseURL: "http://127.0.0.1:18496",
    locale: "en-US",
  });
  try {
    await builder.open(
      bound,
      `/page?app=build&kind=page&name=${boundPage.name}`,
    );
    const note = bound.getByRole("textbox", { name: /^Note/ });
    await note.fill("Retain this note");
    const invocations: { key: string; target: string }[] = [];
    await bound.route("**/v1/capabilities/invoke", async (route) => {
      invocations.push(route.request().postDataJSON());
      if (invocations.length === 1) return route.abort();
      return route.fulfill({
        status: 400,
        contentType: "application/json",
        body: JSON.stringify({
          error: {
            code: "ERROR_CODE_INVALID_ARGUMENT",
            message: "Bound form refused",
          },
        }),
      });
    });
    await bound.getByRole("button", { name: "Create", exact: true }).click();
    await expect(note).toHaveValue("Retain this note");
    await expect(
      bound.getByRole("button", { name: "Create", exact: true }),
    ).toBeEnabled();
    await bound.getByRole("button", { name: "Create", exact: true }).click();
    await expect(
      bound.getByText("Bound form refused", { exact: true }),
    ).toBeVisible();
    expect(invocations).toHaveLength(2);
    expect(invocations[1]).toMatchObject({
      key: invocations[0]!.key,
      target: invocations[0]!.target,
    });
    await bound.unroute("**/v1/capabilities/invoke");
    await note.fill("Corrected note");
    await bound.getByRole("button", { name: "Create", exact: true }).click();
    await expect(note).toHaveValue("");
    expect(
      await builder.record(boundObject.type, invocations[0]!.target),
    ).toMatchObject({ note: "Corrected note", quantity: 3 });
  } finally {
    await bound.close();
  }
});

test("route 4: an approval reaches the requester's page", async ({ page, builder, operator }) => {
  test.setTimeout(60_000);
  const object=await builder.object({
    title:"Review document",fields:[{name:"title",title:"Title",type:"text",required:true}],
    states:[{name:"draft",title:"Draft"},{name:"pending",title:"Pending approval"},{name:"approved",title:"Approved"},{name:"rejected",title:"Rejected"}],
    access:[{role:"user",read:"own",create:true}],
    actions:[{name:"request",title:"Request review",from:["draft"],to:"approved",roles:["user"],approval:{pending:"pending",rejected:"rejected",levels:[{title:"Manager",role:"builder"}]}}],
  });
  const id=fresh("REVIEW");
  await operator.decide(`${object.type}.create`,{type:object.type,id},{title:"Browser approval"});
  await operator.open(page,`/record?type=${object.type}&id=${id}`);
  await page.getByRole("button",{name:"Request review",exact:true}).click();
  await expect(page.locator('li[aria-current="step"]')).toHaveText("Pending approval");
  const chain=page.getByRole("figure",{name:"Approvals",exact:true});
  await expect(chain.getByText("Manager",{exact:true})).toBeVisible();
  const managerPage=await page.context().newPage();
  try {
    await builder.open(managerPage,"/inbox");
    await managerPage.getByRole("button",{name:`Approve: Request review ${object.type}/${id}`,exact:true}).click();
    await managerPage.getByRole("button",{name:"Approve",exact:true}).click();
    await managerPage.getByRole("dialog").getByRole("button",{name:"Approve",exact:true}).click();
    await managerPage.getByRole("button",{name:"Back to inbox",exact:true}).click();
    await expect(page.locator('li[aria-current="step"]')).toHaveText("Approved");
  } finally {await managerPage.close();}
});

test("route 28: preview cannot submit an action", async ({ page, builder }) => {
  const model = await routeRecords(builder);
  await open(page, "sales", "/definitions");
  await page
    .getByRole("textbox", { name: "Filter rows", exact: true })
    .fill(`build/page/${model.task.name}`);
  await page
    .getByRole("row")
    .filter({ has: page.getByRole("cell", { name: `build/page/${model.task.name}`, exact: true }) })
    .click();
  await page.getByRole("button", { name: "Preview page" }).click();
  await expect(
    page.getByText("Preview uses sample data. Actions do not run."),
  ).toBeVisible();
  const writes: string[] = [];
  page.on("request", (r) => {
    if (r.method() !== "GET" && r.url().includes("/v1/")) writes.push(r.url());
  });
  await page.getByRole("button", { name: "Plan group stay", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Preview only")).toBeVisible();
  await expect(
    dialog.getByRole("button", { name: "Plan group stay", exact: true }),
  ).toBeDisabled();
  expect(writes).toEqual([]);
});

test("route 20: field security", async ({ page, request }) => {
  const model = await routeRecords(new Builder(request, "manager"));
  const account = fresh("ACC"),
    opp = fresh("OPP");
  await decide(
    request,
    "sales",
    "build",
    `${model.account.type}.create`,
    { type: model.account.type, id: account },
    { name: "Margin " + account, kind: "company" },
  );
  await decide(
    request,
    "sales",
    "build",
    `${model.task.type}.create`,
    { type: model.task.type, id: opp },
    { account, title: "Retreat" },
  );
  await decide(
    request,
    "manager",
    "build",
    `${model.task.type}.edit`,
    { type: model.task.type, id: opp },
    { margin: 31.5 },
  );
  await open(page, "manager", `/record?type=${model.task.type}&id=${opp}`);
  await expect(
    page.getByRole("term").filter({ hasText: "Expected margin" }),
  ).toBeVisible();
  await expect(value(page, "31.50")).toBeVisible();
  const salesPage = await page.context().newPage(); // another member: a page of its own
  await open(salesPage, "sales", `/record?type=${model.task.type}&id=${opp}`);
  await expect(salesPage.getByText("Retreat").first()).toBeVisible();
  await expect(salesPage.getByText("Expected margin")).toHaveCount(0);
});
