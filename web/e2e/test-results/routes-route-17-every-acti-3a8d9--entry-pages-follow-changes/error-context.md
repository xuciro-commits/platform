# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: routes.spec.ts >> route 17: every action has an entry, pages follow changes
- Location: tests/routes.spec.ts:32:1

# Error details

```
Error: expect(locator).toHaveValue(expected) failed

Locator: getByRole('dialog').getByLabel('The provider\'s room type')
Expected: "standard"
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toHaveValue" getByRole('dialog').getByLabel('The provider\'s room type') with timeout 5000ms
  - waiting for getByRole('dialog').getByLabel('The provider\'s room type')

```

```yaml
- status: OPP-MVTIKI1K329 restored
- alert
- region "Notifications alt+T"
- dialog "Plan group stay OPP-MVTIKI1K329":
  - heading "Plan group stay OPP-MVTIKI1K329" [level=2]
  - button "Close"
  - paragraph: Plan group stay OPP-MVTIKI1K329
  - status "Waiting for the server":
    - note "Waiting for the server":
      - strong: Waiting for the server
      - paragraph: Waiting for the server response. Retry sends the same request.
    - button "Technical details"
  - button "View outbox"
  - button "Open the original record"
  - button "Original request inputs"
  - button "Close"
  - button "Retry confirmation"
```

# Test source

```ts
  57  |   await opening
  58  |     .getByRole("textbox", { name: /^What is being sold/ })
  59  |     .fill("Board offsite");
  60  |   await opening
  61  |     .getByRole("button", { name: "Create", exact: true })
  62  |     .click();
  63  |   await expect(opening).toHaveCount(0);
  64  |   await open(page, "sales", `/record?type=${model.task.type}&id=${opp}`);
  65  |   for (const action of ["Close opportunity", "Plan group stay", "Book stay"]) {
  66  |     await expect(page.getByRole("button", { name: action })).toBeVisible();
  67  |   }
  68  |   await page.getByRole("button", { name: "Plan group stay", exact: true }).click();
  69  |   const plan = page.getByRole("dialog");
  70  |   await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1.5");
  71  |   await plan.getByLabel("Rooms, 1 to 20", { exact: false }).blur();
  72  |   await plan
  73  |     .getByLabel("The provider's room type", { exact: false })
  74  |     .selectOption("standard");
  75  |   await expect(plan.getByText("Enter a valid value.")).toBeVisible();
  76  |   await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1");
  77  |   await plan.getByRole("button", { name: /^First night \*/ }).click();
  78  |   await plan
  79  |     .getByRole("textbox", { name: "First night", exact: true })
  80  |     .fill("2030-01-30");
  81  |   await plan
  82  |     .getByRole("textbox", { name: "Departure", exact: true })
  83  |     .fill("2030-02-02");
  84  |   await plan
  85  |     .getByRole("group", { name: "Choose a date range" })
  86  |     .getByRole("button", { name: "Close", exact: true })
  87  |     .click();
  88  |   await expect(plan.getByText("3 nights", { exact: true })).toBeVisible();
  89  |   await plan.getByRole("button", { name: /^First night \*/ }).click();
  90  |   const first = plan.getByRole("button", { name: "2030-01-30", exact: true }),
  91  |     last = plan.getByRole("button", { name: "2030-02-02", exact: true });
  92  |   await first.hover();
  93  |   await page.mouse.down();
  94  |   await last.hover();
  95  |   await page.mouse.up();
  96  |   await expect(
  97  |     plan.getByRole("group", { name: "Choose a date range" }),
  98  |   ).toHaveCount(0);
  99  |   await expect(plan.getByText("3 nights", { exact: true })).toBeVisible();
  100 |   await page.setViewportSize({ width: 480, height: 800 });
  101 |   await plan.getByRole("button", { name: /^First night \*/ }).click();
  102 |   const calendar = plan.getByRole("group", { name: "Choose a date range" });
  103 |   await expect(
  104 |     calendar.getByRole("button", { name: "2030-02-02", exact: true }),
  105 |   ).toHaveCount(0);
  106 |   const keyboardStart = calendar.getByRole("button", {
  107 |     name: "2030-01-30",
  108 |     exact: true,
  109 |   });
  110 |   await keyboardStart.focus();
  111 |   await keyboardStart.press("Enter");
  112 |   await keyboardStart.press("ArrowRight");
  113 |   await calendar
  114 |     .getByRole("button", { name: "2030-01-31", exact: true })
  115 |     .press("Enter");
  116 |   await expect(plan.getByText("1 night", { exact: true })).toBeVisible();
  117 |   await page.setViewportSize({ width: 1280, height: 720 });
  118 |   await plan
  119 |     .getByLabel("The last day the rooms are held", { exact: false })
  120 |     .fill("2030-02-02");
  121 |   await plan.getByRole("button", { name: "Plan group stay", exact: true }).click();
  122 |   await expect(plan.getByRole("alert")).toContainText("before First night");
  123 |   await plan
  124 |     .getByLabel("The last day the rooms are held", { exact: false })
  125 |     .fill("2030-01-29");
  126 |   const keys: string[] = [];
  127 |   await page.route("**/v1/submissions", async (route) => {
  128 |     const submission = route.request().postDataJSON();
  129 |     if (submission.schema?.name !== `${model.task.type}.plan`)
  130 |       return route.continue();
  131 |     keys.push(submission.idempotencyKey);
  132 |     if (keys.length === 1) return route.abort();
  133 |     await route.fulfill({
  134 |       status: 400,
  135 |       contentType: "application/json",
  136 |       body: JSON.stringify({
  137 |         error: {
  138 |           code: "ERROR_CODE_INVALID_ARGUMENT",
  139 |           message: "The rooms are unavailable",
  140 |           issues: [
  141 |             {
  142 |               code: "availability",
  143 |               path: ["roomtype"],
  144 |               message: "Choose another room type",
  145 |             },
  146 |           ],
  147 |         },
  148 |       }),
  149 |     });
  150 |   });
  151 |   await plan.getByRole("button", { name: "Plan group stay", exact: true }).click();
  152 |   await expect(
  153 |     plan.getByRole("button", { name: "Retry confirmation" }),
  154 |   ).toBeVisible();
  155 |   await expect(
  156 |     plan.getByLabel("The provider's room type", { exact: false }),
> 157 |   ).toHaveValue("standard");
      |     ^ Error: expect(locator).toHaveValue(expected) failed
  158 |   await plan.getByRole("button", { name: "Retry confirmation" }).click();
  159 |   await expect(plan.getByText("Choose another room type")).toBeVisible();
  160 |   expect(keys).toHaveLength(2);
  161 |   expect(keys[1]).toBe(keys[0]);
  162 |   await expect(
  163 |     plan.getByRole("button", { name: /^First night \*/ }),
  164 |   ).toContainText("2030-01-30");
  165 |   await expect(
  166 |     plan.getByLabel("The provider's room type", { exact: false }),
  167 |   ).toHaveValue("standard");
  168 |   await page.unroute("**/v1/submissions");
  169 |   await plan.getByRole("button", { name: "Cancel" }).click();
  170 |   await page.getByRole("button", { name: "Close opportunity", exact: true }).click();
  171 |   await page
  172 |     .getByRole("dialog")
  173 |     .getByRole("combobox")
  174 |     .first()
  175 |     .selectOption("won");
  176 |   await page
  177 |     .getByRole("dialog")
  178 |     .getByRole("button", { name: "Close opportunity", exact: true })
  179 |     .click();
  180 |   await expect(value(page, "won")).toBeVisible();
  181 |   // Bound creation uses the shared capability route. A lost reply retains its
  182 |   // request key; an explicit refusal permits a corrected proposal.
  183 |   const builder = new Builder(request, "manager");
  184 |   const boundObject = await builder.object({
  185 |     title: "Bound input records",
  186 |     fields: [
  187 |       { name: "note", title: "Note", type: "text", required: true },
  188 |       { name: "quantity", title: "Quantity", type: "integer", required: true },
  189 |     ],
  190 |     states: [{ name: "open", title: "Open" }],
  191 |     actions: [
  192 |       {
  193 |         name: "schedule",
  194 |         title: "Schedule input",
  195 |         from: ["open"],
  196 |         inputs: [
  197 |           {
  198 |             name: "account",
  199 |             title: "Account",
  200 |             type: "reference",
  201 |             ref: model.account.type,
  202 |             required: true,
  203 |           },
  204 |           {
  205 |             name: "arrive",
  206 |             title: "First night",
  207 |             type: "date",
  208 |             required: true,
  209 |             range: { end: "depart" },
  210 |           },
  211 |           {
  212 |             name: "depart",
  213 |             title: "Departure",
  214 |             type: "date",
  215 |             required: true,
  216 |             constraints: { after: "arrive" },
  217 |           },
  218 |         ],
  219 |       },
  220 |     ],
  221 |   });
  222 |   const builderTab = await browser.newPage({
  223 |     baseURL: "http://127.0.0.1:18496",
  224 |     locale: "en-US",
  225 |   });
  226 |   try {
  227 |     // The builder configures the same references and date contract used by business actions.
  228 |     await builder.open(
  229 |       builderTab,
  230 |       `/action-type?id=${boundObject.id}&action=schedule`,
  231 |     );
  232 |     const structure = builderTab.getByRole("region", {
  233 |       name: "Action structure",
  234 |       exact: true,
  235 |     });
  236 |     const inspector = builderTab.getByRole("region", {
  237 |       name: "Action inspector",
  238 |       exact: true,
  239 |     });
  240 |     await structure.getByRole("button", { name: "Form", exact: true }).click();
  241 |     await builderTab.getByRole("button", { name: "Visual", exact: true }).click();
  242 |     await builderTab
  243 |       .getByRole("button", { name: "Account", exact: true })
  244 |       .click();
  245 |     await inspector
  246 |       .getByRole("combobox", { name: "Reference object", exact: true })
  247 |       .selectOption(model.account.type);
  248 |     await builderTab
  249 |       .getByRole("button", { name: "First night", exact: true })
  250 |       .click();
  251 |     await expect(
  252 |       inspector.getByRole("combobox", { name: "Date range end", exact: true }),
  253 |     ).toHaveValue("depart");
  254 |     await builderTab.getByRole("button", { name: /^First night \*/ }).click();
  255 |     const previewRange = builderTab.getByRole("group", {
  256 |       name: "Choose a date range",
  257 |       exact: true,
```