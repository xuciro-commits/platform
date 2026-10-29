# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: workflow.spec.ts >> primary >> a tested native flow binds its release and only its operator answers
- Location: tests/workflow.spec.ts:11:5

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: getByRole('status').filter({ hasText: 'Release active for operators.' })
Expected: visible
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" getByRole('status').filter({ hasText: 'Release active for operators.' }) with timeout 5000ms
  - waiting for getByRole('status').filter({ hasText: 'Release active for operators.' })

```

```yaml
- complementary "Platform":
  - button "Home"
  - button "Search"
  - button "Notifications": "6"
  - button "Applications"
  - button "Recent"
  - button "Favorites"
  - button "Assistant"
  - button "manager-1 · browser-primary": M
- banner:
  - button "Toggle navigation" [expanded]
  - button "Switch application": Builder
  - button "Add to favorites"
  - menubar:
    - menuitem "View"
    - menuitem "Appearance"
    - menuitem "Window"
  - button "Search and commands": Search and commands ⌘K
  - button "Offline applications"
  - text: browser-primary
  - button "Last activated release":
    - text: "Last activated release:"
    - code: 0bb58f758a
- navigation "Main":
  - text: Applications
  - button "All applications"
  - text: Model
  - button "Object types"
  - button "Link types"
  - button "Shared properties"
  - button "Queries"
  - text: Behavior
  - button "Object behavior"
  - button "Logic flows"
  - button "Automations"
  - button "AI functions"
  - button "Agents"
  - button "Code functions"
  - button "Decision tables"
  - button "Alert rules"
  - text: Interface
  - button "Pages"
  - button "Templates"
  - text: Connections
  - button "Connections"
  - button "Data sources"
  - button "Datasets"
  - button "Pipelines"
  - button "Writebacks"
  - button "Matching rules"
  - button "Observation sources"
  - button "Integration health"
  - text: Release
  - button "Sample sets"
  - button "Evaluation sets"
  - button "Business test plans"
  - button "Changes"
  - button "Release review"
  - button "Release history"
- region "Flows · Reviewed native flow":
  - tablist:
    - tab "Flows · Reviewed native flow" [selected]:
      - text: Flows · Reviewed native flow
      - button "Close tab":
        - img
  - tabpanel "Flows · Reviewed native flow"
- status: Flows opened
- alert
- region "Flows":
  - group:
    - button "Toggle structure panel" [expanded] [pressed]
    - navigation "Breadcrumbs":
      - button "Automate"
      - button "Flows"
      - heading "Reviewed native flow" [level=1]
    - text: Draft
    - button "Undo"
    - button "Redo" [disabled]
    - status: Saved
    - group:
      - button "Validate"
      - button "Run" [disabled]
      - button "More flow commands"
      - button "Publish"
      - button "Publish options"
    - button "Toggle dock" [expanded] [pressed]
    - button "Toggle inspector panel" [expanded] [pressed]
    - group:
      - region "Flow structure":
        - tablist "Flow structure":
          - tab "Steps 3" [selected]
          - tab "Blocks"
          - tab "Data"
        - tabpanel "Steps 3":
          - button "Reviewed native flow" [pressed]
          - button "Review sample Entry"
          - button "Close sample"
          - button "Reject sample"
      - separator "Resize structure panel"
      - region "Editor":
        - group "Editing mode":
          - button "Visual" [pressed]
          - button "Source editor"
        - group:
          - region "Flow map":
            - application:
              - img:
                - group "Edge from ask to close"
              - img:
                - group "Edge from ask to reject"
              - text: approve reject
              - button "Insert block on connection"
              - button "Insert block on connection"
              - button "Review sample":
                - text: Human task Review sample
                - button "Collapse block ports"
                - text: Entry block In
                - code: flow
                - text: Continue
                - code: flow
                - text: approve
                - code: flow
                - text: reject
                - code: flow
                - text: Error
                - code: flow
                - text: Result
                - code: object
                - text: answer
                - code: string
              - button "Close sample":
                - text: Action Close sample
                - button "Collapse block ports"
                - text: Platform control In
                - code: flow
                - text: Record ID
                - code: string
                - text: Continue
                - code: flow
                - text: Error
                - code: flow
              - button "Reject sample":
                - text: Action Reject sample
                - button "Collapse block ports"
                - text: Platform control In
                - code: flow
                - text: Record ID
                - code: string
                - text: Continue
                - code: flow
                - text: Error
                - code: flow
              - img
              - button "Zoom In":
                - img
              - button "Zoom Out":
                - img
              - button "Fit View":
                - img
              - link "React Flow attribution":
                - /url: https://reactflow.dev?utm_source=attribution
                - text: React Flow
            - toolbar "Canvas tools":
              - button "Add block"
              - button "Tidy up workflow"
              - button "Flow top to bottom"
              - button "Collapse all blocks"
              - button "Fit canvas"
              - button "Undo"
              - button "Redo" [disabled]
              - button "Duplicate selection"
              - button "Delete selection"
              - button "How to work on this canvas"
            - button "Expand canvas"
        - button "Check inputs"
        - button "Try operation"
        - separator "Resize dock"
        - tablist "Flow dock":
          - tab "Problems"
          - tab "Run"
          - tab "Runs"
          - tab "Test"
          - tab "Release" [selected]
        - button "Close dock"
        - tabpanel "Release":
          - heading "Saved releases" [level=2]
          - button "Refresh releases" [disabled]
          - paragraph:
            - text: "Current active release:"
            - code: sha256-v1:0bb58f758acd8a0aba01c429cabd7c43b6c4cdb409485aad85d226b0458e766d
          - text: Saved candidate
          - combobox "Saved candidate" [disabled]:
            - option "Choose a saved candidate"
            - option "Scoped item · 3 assets · 2417f3ce"
            - option "Original publisher sample · 5 assets · a98ebc1c"
            - option "Review sample · 4 assets · 458e766d · Active" [selected]
            - option "Country · 4 assets · 9f69420d"
            - option "Task · 5 assets · 467b3746"
            - option "Protocol provider · 6 assets · 2201c7be"
            - option "Change discovery app · 2 assets · da47648c"
            - option "Country · 4 assets · 8664f03b"
            - option "Runtime case application · 3 assets · df6fc1d0"
            - option "Observed equipment · 4 assets · 698c281a"
            - option "service.plan · 3 assets · 1abdf4d2"
            - option "Master data extension · 45 assets · a6f94a6b"
            - option "Versioned order · 6 assets · 32c74ee3"
            - option "Instruction records · 2 assets · 634e74af"
            - option "Business route · 5 assets · bf9a68f9"
            - option "Batch · 11 assets · cd572e80"
            - option "Service visits · 13 assets · 31b649d2"
            - option "Service application · 8 assets · 621b9a77"
            - option "Batch · 11 assets · 4ce77ccc"
            - option "Shared context application · 4 assets · a1e9ec0f"
          - text: Showing 20 of 38 candidates
          - button "Previous" [disabled]
          - button "Next" [disabled]
          - button "Review active release" [disabled]
          - button "Checking…" [disabled]
          - heading "Deliver drafts together" [level=2]
          - button "About Deliver drafts together": View help
          - button "Add selected draft" [disabled]
          - button "Add the drafts it depends on" [disabled]
          - button "Clear selection" [disabled]
          - list "Joint selection":
            - listitem:
              - text: Logic flows ·
              - code: FLOW-MVTILCU351
              - button "Remove" [disabled]
          - button "Checking…" [disabled]
          - text: Saved candidate review Active
          - button "Candidate identity"
          - button "Promote to another environment…" [disabled]
          - button "About Promote to another environment…": View help
          - button "Sealed definitions · 4"
          - status: Running definitions differ from this saved release.
          - paragraph: Activation installs the candidate's included objects, pages, applications, workflows, AI functions and code functions together.
          - heading "Added · 1" [level=3]
          - list:
            - listitem: build/flow/build.reviewmvtilcu352
          - heading "Changed · 0" [level=3]
          - list
          - heading "Removed · 0" [level=3]
          - list
          - heading "Included assets · 4" [level=3]
          - list:
            - listitem: build/action/build.objmvtilboi47.close
            - listitem: build/action/build.objmvtilboi47.reject
            - listitem: build/flow/build.reviewmvtilcu352
            - listitem: build/object/build.objmvtilboi47
          - button "Activate release" [disabled]
          - status:
            - text: Active release differs from running definitions.
            - code: sha256-v1:0bb58f758acd8a0aba01c429cabd7c43b6c4cdb409485aad85d226b0458e766d
          - paragraph: Verify the release from the business application in the operations zone.
      - separator "Resize inspector panel"
      - region "Flow inspector":
        - heading "Workflow settings" [level=2]
        - button "Close inspector panel"
        - region "Workflow properties":
          - text: Workflow name
          - textbox "Workflow name":
            - /placeholder: stockallocation
            - text: reviewmvtilcu352
          - text: Workflow title
          - textbox "Workflow title": Reviewed native flow
          - checkbox "Manual or API start"
          - text: Manual or API start Repeat every
          - combobox "Repeat every":
            - option "Not scheduled" [selected]
            - option "every 15 minutes"
            - option "every hour"
            - option "every day"
            - option "every week"
          - text: Source object
          - combobox "Source object":
            - option "No source record"
            - option "Agent memory"
            - option "Access request"
            - option "Agent"
            - option "Alert rule"
            - option "Application"
            - option "Batch"
            - option "Card records"
            - option "Code function"
            - option "Connection"
            - option "Dependency source"
            - option "Device dispatch requests"
            - option "Business disposition"
            - option "Journey entry"
            - option "Integration receipt"
            - option "AI function"
            - option "Inspection review"
            - option "Link type"
            - option "Matching rule"
            - option "Object"
            - option "Declaration model"
            - option "Equipment"
            - option "Observed equipment"
            - option "Task"
            - option "Resource"
            - option "Opportunity"
            - option "Review document"
            - option "Opportunity"
            - option "Opportunity"
            - option "Review sample" [selected]
            - option "Observation source"
            - option "Page"
            - option "Pipeline"
            - option "Process"
            - option "Property type"
            - option "Protocol provider"
            - option "Query"
            - option "Receiving confirmation"
            - option "Receiving request"
            - option "Changed after sealing"
            - option "Sample set"
            - option "Service case"
            - option "Data source"
            - option "Decision table"
            - option "Visit"
            - option "Writeback"
            - option "Journal entry"
            - option "Fiscal period"
            - option "Approval request"
            - option "Task"
            - option "AI source model"
            - option "Saved receiving model"
            - option "Trial task"
            - option "Trial work order"
            - option "Template entry"
          - text: Start state
          - combobox "Start state":
            - option "Choose a state"
            - option "Open" [selected]
            - option "Done"
            - option "Rejected"
          - button "Lanes"
          - button "Workflow input schema" [expanded]
          - combobox "Schema type":
            - option "object" [selected]
            - option "array"
            - option "string"
            - option "integer"
            - option "number"
            - option "boolean"
            - option "variant"
          - checkbox "Allow an explicit null value"
          - text: Allow an explicit null value
          - textbox "New schema field":
            - /placeholder: Field name
          - button "Add field" [disabled]
          - textbox "Schema description":
            - /placeholder: Description
          - text: Default workflow input
          - textbox "Default workflow input": "{}"
          - text: Continuous source intake
          - textbox "Continuous source intake": "null"
          - button "About Entry": View help
- region "Notifications alt+T":
  - list:
    - listitem: Release activated.
```

# Test source

```ts
  2   | 
  3   | test.use({ actionTimeout: 15_000 });
  4   | 
  5   | for (const fixture of [
  6   |   { industry: "primary", baseURL: "http://127.0.0.1:18496", builder: "manager", operator: "desk", operatorID: "desk-1", answer: "approve", state: "Done" },
  7   |   { industry: "secondary", baseURL: "http://127.0.0.1:18497", builder: "supervisor", operator: "operator-l1", operatorID: "op-l1", answer: "reject", state: "Rejected" },
  8   | ]) {
  9   |   test.describe(fixture.industry, () => {
  10  |     test.use({ baseURL: fixture.baseURL });
  11  |     test("a tested native flow binds its release and only its operator answers", async ({ browser, page, request }, testInfo) => {
  12  |       test.setTimeout(90_000);
  13  |       const builder = new Builder(request, fixture.builder);
  14  |       const operator = new Member(request, fixture.operator);
  15  |       const { type } = await builder.object({ title: "Review sample", fields: [{ name: "note", title: "Note", type: "text" }],
  16  |         states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }, { name: "rejected", title: "Rejected" }],
  17  |         actions: [{ name: "close", title: "Close", from: ["open"], to: "done" }, { name: "reject", title: "Reject", from: ["open"], to: "rejected" }],
  18  |       });
  19  |       const id = fresh("FLOW"), name = fresh("review").replace(/[^a-z0-9]/gi, "").toLowerCase();
  20  |       await builder.decide("build.process.create", { type: "build.process", id }, { name, title: "Native review", object: type, when: "open", steps: [
  21  |         { name: "ask", title: "Review sample", kind: "ask", ask: "user", answers: ["approve", "reject"], cases: { approve: "close", reject: "reject" } },
  22  |         { name: "close", title: "Close sample", kind: "action", act: "close" },
  23  |         { name: "reject", title: "Reject sample", kind: "action", act: "reject" },
  24  |       ] });
  25  | 
  26  |       // Edit and save through the current flow workbench, then test synthetic data.
  27  |       await builder.open(page, `/flow?id=${id}`);
  28  |       await page.getByRole("region", { name: "Editor", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  29  |       const properties = page.getByRole("region", { name: "Workflow properties", exact: true });
  30  |       await properties.getByLabel("Default workflow input", { exact: false }).fill("{");
  31  |       const structure = page.getByRole("region", { name: "Flow structure", exact: true });
  32  |       await structure.getByRole("button", { name: /^Review sample/ }).click();
  33  |       await expect(page.getByRole("button", { name: "Validate", exact: true })).toBeDisabled();
  34  |       await properties.getByRole("tab",{name:"Settings",exact:true}).click();
  35  |       const history=page.locator("header").filter({has:page.getByRole("heading",{name:"Native review",exact:true})});
  36  |       const timeout = properties.getByRole("spinbutton", {name:"Timeout (seconds)",exact:true});
  37  |       await timeout.fill("-");
  38  |       await history.getByRole("button",{name:"Undo",exact:true}).click();
  39  |       await expect(timeout).toHaveValue("0");
  40  |       await history.getByRole("button",{name:"Redo",exact:true}).click();
  41  |       await expect(timeout).toHaveValue("-");
  42  |       // The shared frame closes without deselecting the block or its input draft.
  43  |       await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
  44  |       await expect(properties).not.toBeVisible();
  45  |       await page.getByRole("button", { name: "Toggle inspector panel", exact: true }).click();
  46  |       await expect(properties.getByRole("tab", { name: "Settings", exact: true })).toHaveAttribute("aria-selected", "true");
  47  |       await expect(timeout).toHaveValue("-");
  48  |       await properties.getByRole("tab", { name: "Settings", exact: true }).focus();
  49  |       await page.keyboard.press("ArrowRight");
  50  |       await expect(properties.getByRole("tab", { name: "Input", exact: true })).toBeFocused();
  51  |       await page.keyboard.press("ArrowLeft");
  52  |       await expect(timeout).toHaveValue("-");
  53  |       await structure.getByRole("button", { name: "Native review", exact: true }).click();
  54  |       await expect(properties.getByLabel("Default workflow input", { exact: false })).toHaveValue("{");
  55  |       await properties.getByLabel("Default workflow input", { exact: false }).fill("{}");
  56  |       await expect(page.getByRole("button", {name:"Validate",exact:true})).toBeDisabled();
  57  |       await structure.getByRole("button",{name:/^Review sample/}).click();
  58  |       await properties.getByRole("tab",{name:"Settings",exact:true}).click();
  59  |       await expect(properties.getByRole("spinbutton",{name:"Timeout (seconds)",exact:true})).toHaveValue("-");
  60  |       await properties.getByRole("spinbutton",{name:"Timeout (seconds)",exact:true}).fill("0");
  61  |       await structure.getByRole("button",{name:"Native review",exact:true}).click();
  62  |       await expect(page.getByRole("button", { name: "Validate", exact: true })).toBeEnabled();
  63  |       await properties.getByRole("textbox", { name: "Workflow title", exact: true }).fill("Reviewed native flow");
  64  |       await page.keyboard.press("ControlOrMeta+s");
  65  |       await expect.poll(async () => (await builder.record("build.process", id)).title).toBe("Reviewed native flow");
  66  |       await page.getByRole("tab", { name: "Test", exact: true }).click();
  67  |       await page.getByRole("tabpanel", { name: "Test", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  68  |       await page.getByRole("textbox", { name: "Member ID (empty: you)", exact: true }).fill(fixture.operatorID);
  69  |       await page.getByRole("group", { name: "Test step 1", exact: true }).getByRole("textbox", { name: "Note", exact: true }).fill("SYNTHETIC");
  70  |       await page.getByRole("group", { name: "Test step 2", exact: true }).getByRole("combobox", { name: "Human answer", exact: true }).selectOption(fixture.answer);
  71  |       // Active tabs select; only an explicit close collapses the dock. Neither
  72  |       // tab changes nor collapse may destroy the isolated test setup.
  73  |       const testTab = page.getByRole("tab", { name: "Test", exact: true });
  74  |       const note = page.getByRole("group", { name: "Test step 1", exact: true }).getByRole("textbox", { name: "Note", exact: true });
  75  |       await testTab.click();
  76  |       await expect(note).toHaveValue("SYNTHETIC");
  77  |       await page.getByRole("tab", { name: /^Problems(?: \d+)?$/ }).click();
  78  |       await testTab.click();
  79  |       await expect(note).toHaveValue("SYNTHETIC");
  80  |       await page.getByRole("button", { name: "Close dock", exact: true }).click();
  81  |       await expect(note).not.toBeVisible();
  82  |       await testTab.focus();
  83  |       await page.keyboard.press("ControlOrMeta+j");
  84  |       await expect(note).toHaveValue("SYNTHETIC");
  85  |       await page.getByRole("button", { name: "Run isolated test", exact: true }).click();
  86  |       await expect(page.getByRole("status").filter({ hasText: "All expected outcomes matched." })).toBeVisible();
  87  |       await expect(page.getByRole("status").filter({ hasText: "Test state recovered exactly." })).toBeVisible();
  88  |       await page.getByRole("button", { name: "Test details", exact: true }).click();
  89  |       const candidate = await page.getByText("Tested candidate", { exact: true }).locator("..").locator("code").innerText();
  90  |       expect((await request.get(`/v1/records/${type}/TEST-1`, { headers: operator.headers })).status()).toBe(404);
  91  | 
  92  |       // Release the tested bytes; actual work must retain that candidate binding.
  93  |       await page.getByRole("button", { name: "Publish", exact: true }).click();
  94  |       await expect(page.getByRole("button", { name: "Check draft and dependencies", exact: true })).toBeVisible();
  95  |       await page.getByRole("button", { name: "Close dock", exact: true }).click();
  96  |       await page.getByRole("button", { name: "Publish", exact: true }).click();
  97  |       await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  98  |       await page.getByRole("button", { name: "Candidate identity", exact: true }).click();
  99  |       await expect(page.getByText("Draft candidate", { exact: true }).locator("..").locator("code")).toHaveText(candidate);
  100 |       await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  101 |       await page.getByRole("button", { name: "Activate release", exact: true }).click();
> 102 |       await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
      |                                                                                                   ^ Error: expect(locator).toBeVisible() failed
  103 |       if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("flow-workbench.png"), fullPage: true });
  104 |       // The same editor remains usable at a narrow container: no stacked,
  105 |       // unreachable inspector beneath a clipped canvas.
  106 |       await page.setViewportSize({ width: 390, height: 844 });
  107 |       const inspectorToggle = page.getByRole("button", { name: "Toggle inspector panel", exact: true });
  108 |       await expect(inspectorToggle).toHaveAttribute("aria-expanded", "false");
  109 |       await inspectorToggle.click();
  110 |       await expect(properties.getByRole("textbox", { name: "Workflow title", exact: true })).toHaveValue("Reviewed native flow");
  111 |       await expect(structure).not.toBeVisible();
  112 |       if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("flow-inspector-narrow.png"), fullPage: true });
  113 |       await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
  114 |       await expect(properties).not.toBeVisible();
  115 |       await expect(inspectorToggle).toBeFocused();
  116 |       await page.getByRole("button", { name: "Toggle structure panel", exact: true }).click();
  117 |       await expect(structure.getByRole("button", { name: /^Review sample/ })).toBeVisible();
  118 |       await page.getByRole("button", { name: "Close structure panel", exact: true }).click();
  119 |       await page.setViewportSize({ width: 1280, height: 720 });
  120 |       const real = fresh("REAL");
  121 |       await operator.decide(`${type}.create`, { type, id: real }, { note: "OPERATOR RECORD" });
  122 |       const context = await browser.newContext({ baseURL: fixture.baseURL });
  123 |       try {
  124 |         const inbox = await context.newPage();
  125 |         await operator.open(inbox, "/inbox");
  126 |         const task = inbox.getByRole("listitem").filter({ hasText: real });
  127 |         await expect(task).toBeVisible();
  128 |         const binding = await builder.record("flow.instance", `build.${name}:${real}`);
  129 |         expect(binding.dependencies).toMatch(/^sha256-v1:/);
  130 |         expect(binding.release).toBe(candidate);
  131 |         await task.getByRole("button", { name: fixture.answer, exact: true }).click();
  132 |         await operator.open(inbox, `/record?type=${type}&id=${real}`);
  133 |         await expect(inbox.getByRole("definition").filter({ hasText: new RegExp(`^${fixture.state}$`) })).toBeVisible();
  134 |         expect([403, 404]).toContain((await request.get(`/v1/records/build.process/${id}`, { headers: operator.headers })).status());
  135 |       } finally { await context.close(); }
  136 |     });
  137 |   });
  138 | }
  139 | 
  140 | test.describe("flow condition editing", () => {
  141 |   test.use({ baseURL: "http://127.0.0.1:18496" });
  142 |   test("nested condition connections and problem locations survive source edits and undo", async ({ page, request }) => {
  143 |     const builder = new Builder(request, "manager");
  144 |     const id = fresh("FLOW"), name = fresh("conditions").replace(/[^a-z0-9]/gi, "").toLowerCase();
  145 |     const condition = { op: "all", terms: [
  146 |       { op: "eq", left: { source: "step", step: "calculate", path: ["score"] }, right: { source: "literal", value: 3 } },
  147 |       { op: "not", terms: [{ op: "exists", left: { source: "step", step: "calculate", path: ["blocked"] } }] },
  148 |     ] };
  149 |     await builder.decide("build.process.create", { type: "build.process", id }, {
  150 |       name, title: "Condition references", manual: true,
  151 |       inputSchema: { type: "object", properties: { score: { type: "integer" }, blocked: { type: "boolean" } }, required: ["score", "blocked"] },
  152 |       input: { score: 3, blocked: false },
  153 |       steps: [
  154 |         { name: "calculate", title: "Calculate score", kind: "transform", next: "eligible", inputs: { score: { source: "input", path: ["score"] }, blocked: { source: "input", path: ["blocked"] } } },
  155 |         { name: "eligible", title: "Eligible score", kind: "branch", condition, cases: { true: "done", false: "done" } },
  156 |         { name: "done", title: "Review complete", kind: "end", value: { source: "literal", value: "done" } },
  157 |       ],
  158 |     });
  159 |     await builder.open(page, `/flow?id=${id}`);
  160 |     const editor = page.getByRole("region", { name: "Editor", exact: true });
  161 |     await editor.getByRole("button", { name: "Source editor", exact: true }).click();
  162 |     const source = editor.getByRole("textbox", { name: "Configuration source", exact: true });
  163 |     const original = JSON.parse(await source.inputValue());
  164 |     await editor.getByRole("button", { name: "Visual", exact: true }).click();
  165 |     const canvas = page.getByRole("region", { name: "Flow map", exact: true });
  166 |     const firstEdge = canvas.getByTestId("rf__edge-eligible:binding:condition.terms.0.left");
  167 |     const nestedEdge = canvas.getByTestId("rf__edge-eligible:binding:condition.terms.1.terms.0.left");
  168 |     const expectNestedEdgeDrawn = async () => {
  169 |       // A horizontal SVG edge has zero geometric height, so Playwright calls
  170 |       // its group hidden even though its nonzero-width stroke is rendered.
  171 |       await expect.poll(() => nestedEdge.locator("path.react-flow__edge-path").evaluate((node) => {
  172 |         const path = node as SVGPathElement, box = path.getBoundingClientRect(), style = getComputedStyle(path);
  173 |         return path.getTotalLength() > 0 && Math.max(box.width, box.height) > 0 && style.visibility === "visible" &&
  174 |           style.stroke !== "none" && Number.parseFloat(style.strokeWidth) > 0;
  175 |       })).toBe(true);
  176 |     };
  177 |     await expect(firstEdge).toBeVisible();
  178 |     await expectNestedEdgeDrawn();
  179 |     await expect(canvas.getByLabel("Input port: Condition 1 · Left value, integer", { exact: true })).toBeVisible();
  180 |     await expect(canvas.getByLabel("Input port: Condition 2.1 · Left value, boolean", { exact: true })).toBeVisible();
  181 |     await expect(canvas.getByLabel("Output port: score, integer", { exact: true })).toBeVisible();
  182 |     await expect(canvas.getByLabel("Output port: blocked, boolean", { exact: true })).toBeVisible();
  183 | 
  184 |     // Keep the original nested references while introducing a control error in this draft.
  185 |     await editor.getByRole("button", { name: "Source editor", exact: true }).click();
  186 |     const changed = structuredClone(original);
  187 |     changed.steps[1].cases.false = "missing";
  188 |     await source.fill(JSON.stringify(changed, null, 2));
  189 |     await editor.getByRole("button", { name: "Visual", exact: true }).click();
  190 |     const structure = page.getByRole("region", { name: "Flow structure", exact: true });
  191 |     await structure.getByRole("button", { name: "Review complete", exact: true }).click();
  192 |     await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
  193 |     await page.getByRole("tab", { name: /^Problems(?: \d+)?$/ }).click();
  194 |     const problems = page.getByRole("tabpanel", { name: /^Problems(?: \d+)?$/ });
  195 |     await problems.getByRole("button").filter({ hasText: "A connected step is missing." }).click();
  196 |     const properties = page.getByRole("region", { name: "Workflow properties", exact: true });
  197 |     await properties.getByRole("tab", { name: "Settings", exact: true }).click();
  198 |     await expect(properties.getByRole("textbox", { name: "Step name", exact: true })).toHaveValue("eligible");
  199 | 
  200 |     // The native compiler's node-qualified error locates the same original step.
  201 |     await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
  202 |     await page.getByRole("button", { name: "Validate", exact: true }).click();
```