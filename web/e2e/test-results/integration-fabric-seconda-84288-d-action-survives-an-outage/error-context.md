# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: integration-fabric.spec.ts >> secondary >> external rows become a released receiving application whose accepted action survives an outage
- Location: tests/integration-fabric.spec.ts:11:5

# Error details

```
Error: expect(received).toBe(expected) // Object.is equality

Expected: 1
Received: undefined

Call Log:
- Timeout 15000ms exceeded while waiting on the predicate
```

# Page snapshot

```yaml
- generic [ref=e2]:
  - generic [ref=e3]:
    - complementary "Platform" [ref=e4]:
      - button "Home" [ref=e5]
      - button "Search" [ref=e9]
      - button "Notifications" [ref=e13]
      - button "Applications" [ref=e17]
      - button "Recent" [ref=e21]
      - button "Favorites" [ref=e25]
      - generic [ref=e28]:
        - button "Assistant" [ref=e29]
        - button "sup-1 · browser-secondary" [ref=e33]:
          - generic [ref=e34]: S
    - banner [ref=e35]:
      - button "Toggle navigation" [expanded] [ref=e36]
      - button "Switch application" [ref=e39]:
        - generic [ref=e44]: Builder
      - button "Add to favorites" [ref=e47]
      - menubar [ref=e50]:
        - menuitem "View" [ref=e51]
        - menuitem "Appearance" [ref=e52]
        - menuitem "Window" [ref=e53]
      - button "Search and commands" [ref=e54]:
        - generic [ref=e59]: ⌘K
      - generic [ref=e62]:
        - button "Offline applications" [ref=e63]
        - generic [ref=e64]: browser-secondary
        - button "Last activated release" [ref=e65]:
          - text: "Last activated release:"
          - code [ref=e66]: f63713b3c5
    - generic [ref=e67]:
      - navigation "Main" [ref=e68]:
        - generic [ref=e69]:
          - generic [ref=e70]: Applications
          - button "All applications" [ref=e71]
        - generic [ref=e75]:
          - generic [ref=e76]: Model
          - button "Object types" [ref=e77]
          - button "Link types" [ref=e89]
          - button "Shared properties" [ref=e94]
          - button "Queries" [ref=e100]
        - generic [ref=e105]:
          - generic [ref=e106]: Behavior
          - button "Object behavior" [ref=e107]
          - button "Logic flows" [ref=e113]
          - button "Automations" [ref=e119]
          - button "AI functions" [ref=e125]
          - button "Agents" [ref=e130]
          - button "Code functions" [ref=e135]
          - button "Decision tables" [ref=e140]
          - button "Alert rules" [ref=e144]
        - generic [ref=e151]:
          - generic [ref=e152]: Interface
          - button "Pages" [ref=e153]
          - button "Templates" [ref=e159]
        - generic [ref=e165]:
          - generic [ref=e166]: Connections
          - button "Connections" [ref=e167]
          - button "Data sources" [ref=e172]
          - button "Datasets" [ref=e178]
          - button "Pipelines" [ref=e184]
          - button "Writebacks" [ref=e190]
          - button "Matching rules" [ref=e195]
          - button "Observation sources" [ref=e201]
          - button "Integration health" [ref=e207]
        - generic [ref=e213]:
          - generic [ref=e214]: Release
          - button "Sample sets" [ref=e215]
          - button "Evaluation sets" [ref=e217]
          - button "Business test plans" [ref=e222]
          - button "Changes" [ref=e228]
          - button "Release review" [ref=e234]
          - button "Release history" [ref=e240]
      - generic [ref=e246]:
        - generic [ref=e253]:
          - generic [ref=e261]:
            - paragraph [ref=e262]: No open tabs.
            - generic [ref=e263]:
              - button "Open home" [ref=e264]
              - button "Search and commands" [ref=e265]
          - status [ref=e266]
          - alert [ref=e267]
        - generic [ref=e270]:
          - generic [ref=e280]:
            - region "Pipelines" [ref=e287]:
              - tablist [ref=e290]:
                - tab "Datasets" [ref=e291] [cursor=pointer]
                - tab "Data sources" [ref=e296] [cursor=pointer]
                - tab "Pipelines" [ref=e301] [cursor=pointer]
                - tab "Datasets" [ref=e306] [cursor=pointer]
                - tab "Pipelines" [selected] [ref=e311] [cursor=pointer]:
                  - generic [ref=e314]:
                    - generic [ref=e315]: Pipelines
                    - button "Close tab" [ref=e316]
              - tabpanel "Pipelines" [ref=e320]
            - status [ref=e321]: Pipelines opened
            - alert [ref=e322]
          - region "Pipelines" [ref=e325]:
            - group [ref=e326]:
              - generic [ref=e327]:
                - generic [ref=e328]:
                  - generic [ref=e330]:
                    - heading "Land receiving records" [level=1] [ref=e331]
                    - button "About Land receiving records" [ref=e332]: View help
                  - generic [ref=e337]:
                    - button "Pipelines" [ref=e338]
                    - button "Discard" [disabled]
                    - button "Save pipeline" [disabled]
                    - button "Trial pipeline" [ref=e339]
                    - button "Publish" [ref=e340]
                    - button "Review and publish…" [ref=e341]
                    - button "Run now" [ref=e342]
                    - button "Pause" [ref=e343]
                - generic [ref=e344]:
                  - group "Editing mode" [ref=e346]:
                    - button "Visual" [pressed] [ref=e347]
                    - button "Source editor" [ref=e348]
                  - group [ref=e350]:
                    - generic [ref=e351]:
                      - heading "Input" [level=2] [ref=e353]
                      - generic [ref=e354]:
                        - text: Pipeline name
                        - textbox "Pipeline name" [disabled] [ref=e355]:
                          - /placeholder: cleanmaterials
                          - text: fabricmvticpip0land
                      - generic [ref=e356]:
                        - text: Pipeline title
                        - textbox "Pipeline title" [ref=e357]: Land receiving records
                      - generic [ref=e358]:
                        - text: Input dataset
                        - 'combobox "Input dataset Columns: id, qty, sku" [ref=e359]':
                          - option "Choose a dataset"
                          - option "Raw integration rows"
                          - option "Clean integration rows" [selected]
                        - generic [ref=e360]: "Columns: id, qty, sku"
                      - generic [ref=e361]:
                        - text: Run every
                        - combobox "Run every" [ref=e362]:
                          - option "When the input gains a version, or when asked" [selected]
                          - option "every 15 minutes"
                          - option "every hour"
                          - option "every day"
                          - option "every week"
                    - generic [ref=e363]:
                      - heading "Output" [level=2] [ref=e365]
                      - generic [ref=e366]:
                        - text: Write to
                        - combobox "Write to" [ref=e367]:
                          - option "A dataset (next version)"
                          - option "An object (one record per row)" [selected]
                          - option "The enterprise model (one element per row)"
                      - generic [ref=e368]:
                        - text: Output object
                        - 'combobox "Output object Columns are written to the object''s fields of the same name; unknown columns are dropped. Fields: item, qty, docno, code, name" [ref=e369]':
                          - option "Agent evaluation"
                          - option "Agent memory"
                          - option "Agent run"
                          - option "Agent switch"
                          - option "Agent"
                          - option "Alert rule"
                          - option "Application"
                          - option "Code function"
                          - option "Business confirmation"
                          - option "Connection"
                          - option "Dataset"
                          - option "Dataset version"
                          - option "Device dispatch"
                          - option "Local note"
                          - option "Function evaluation"
                          - option "Integration receipt" [selected]
                          - option "AI function"
                          - option "AI function call"
                          - option "Link type"
                          - option "Matching rule"
                          - option "Object"
                          - option "Observation source"
                          - option "Page"
                          - option "Pipeline"
                          - option "Process"
                          - option "Property type"
                          - option "Query"
                          - option "Sample set"
                          - option "Data source"
                          - option "Decision table"
                          - option "Test plan"
                          - option "Writeback"
                          - option "Account"
                          - option "Currency"
                          - option "Journal entry"
                          - option "Location"
                          - option "Material"
                          - option "Business partner"
                          - option "Fiscal period"
                          - option "Person"
                          - option "Site"
                          - option "Unit of measure"
                          - option "File"
                          - option "Flow dead letter"
                          - option "Flow instance"
                          - option "Flow output"
                          - option "Document"
                          - option "Term"
                          - option "Comment"
                          - option "Follow"
                          - option "Approval request"
                          - option "Approval policy"
                          - option "Delegation"
                          - option "Task"
                          - option "Saved view"
                        - generic [ref=e370]: "Columns are written to the object's fields of the same name; unknown columns are dropped. Fields: item, qty, docno, code, name"
                      - generic [ref=e371]:
                        - text: Record id column
                        - textbox "Record id column The same id edits the existing record." [ref=e372]:
                          - /placeholder: matnr
                          - text: id
                        - generic [ref=e373]: The same id edits the existing record.
                    - generic [ref=e374]:
                      - generic [ref=e375]:
                        - heading "Steps" [level=2] [ref=e376]
                        - button "About Steps" [ref=e377]: View help
                      - region "Steps" [ref=e381]:
                        - generic "Steps" [ref=e382]:
                          - application [ref=e383]:
                            - generic [ref=e385] [cursor=pointer]:
                              - generic:
                                - generic:
                                  - img:
                                    - group "Edge from in to s0"
                                  - img:
                                    - group "Edge from s0 to out"
                                - generic:
                                  - button "Insert block on connection" [ref=e386]
                                  - button "Insert block on connection" [ref=e388]
                                - generic:
                                  - button "Clean integration rows" [ref=e390]:
                                    - 'generic "Start event: Clean integration rows" [ref=e391]':
                                      - generic [ref=e393]:
                                        - generic [ref=e398]:
                                          - generic [ref=e399]: Dataset
                                          - generic "Clean integration rows" [ref=e400]
                                        - button "Collapse block ports" [ref=e401]
                                      - generic "The dataset this pipeline reads" [ref=e404]
                                      - 'generic "Output port: Rows out, rows" [ref=e405]'
                                      - generic "Rows out · rows" [ref=e406]:
                                        - generic [ref=e407]: Rows out
                                        - code [ref=e408]: rows
                                  - button "1. rename" [ref=e409]:
                                    - 'generic "Task: 1. rename" [ref=e410]':
                                      - generic [ref=e412]:
                                        - generic [ref=e417]:
                                          - generic [ref=e418]: rename
                                          - generic "1. rename" [ref=e419]
                                        - button "Collapse block ports" [ref=e420]
                                      - generic "sku → item" [ref=e423]
                                      - 'generic "Input port: Rows in, rows" [ref=e424]'
                                      - generic "Rows in · rows" [ref=e425]:
                                        - generic [ref=e426]: Rows in
                                        - code [ref=e427]: rows
                                      - 'generic "Output port: Rows out, rows" [ref=e428]'
                                      - generic "Rows out · rows" [ref=e429]:
                                        - generic [ref=e430]: Rows out
                                        - code [ref=e431]: rows
                                  - button "Integration receipt" [ref=e432]:
                                    - 'generic "End event: Integration receipt" [ref=e433]':
                                      - generic [ref=e435]:
                                        - generic [ref=e440]:
                                          - generic [ref=e441]: Output
                                          - generic "Integration receipt" [ref=e442]
                                        - button "Collapse block ports" [ref=e443]
                                      - generic "Record id column id" [ref=e446]
                                      - 'generic "Input port: Rows in, rows" [ref=e447]'
                                      - generic "Rows in · rows" [ref=e448]:
                                        - generic [ref=e449]: Rows in
                                        - code [ref=e450]: rows
                            - generic "Control Panel" [ref=e451]:
                              - button "Zoom In" [ref=e452] [cursor=pointer]
                              - button "Zoom Out" [ref=e455] [cursor=pointer]
                              - button "Fit View" [ref=e458] [cursor=pointer]
                            - link "React Flow attribution" [ref=e462] [cursor=pointer]:
                              - /url: https://reactflow.dev?utm_source=attribution
                              - text: React Flow
                          - toolbar "Canvas tools" [ref=e464]:
                            - button "Add block" [ref=e465]
                            - button "Tidy up workflow" [ref=e468]
                            - button "Flow top to bottom" [ref=e472]
                            - button "Collapse all blocks" [ref=e475]
                            - button "Fit canvas" [ref=e479]
                            - button "How to work on this canvas" [ref=e485]
                        - button "Expand canvas" [ref=e489]
                        - generic [aria-hidden]: ◢
                    - generic [ref=e495]:
                      - generic [ref=e496]:
                        - heading "Expectations" [level=2] [ref=e497]
                        - button "About Expectations" [ref=e498]: View help
                      - button "Add expectation" [ref=e503]
                    - generic [ref=e504]:
                      - heading "Last run" [level=2] [ref=e506]
                      - paragraph [ref=e507]: Not run yet.
                  - generic [ref=e508]:
                    - button "Check inputs" [ref=e509]
                    - button "Try operation" [ref=e510]
  - region "Notifications alt+T"
```

# Test source

```ts
  25  |       sink.listen(0, "127.0.0.1");
  26  |       await once(sink, "listening");
  27  |       try {
  28  |         const address = sink.address();
  29  |         if (!address || typeof address === "string") throw new Error("external fixture did not listen");
  30  |         const name = fresh("fabric").replace(/[^a-z0-9]/gi, "").toLowerCase();
  31  |         const headers = builder.headers;
  32  |         const record = builder.record.bind(builder);
  33  |         // Connect and clean real external rows through the Data Connection editors.
  34  |         await page.addInitScript(() => localStorage.setItem("platform.language", "en"));
  35  |         await builder.open(page, "/dataset?id=new");
  36  |         await page.locator('[data-structured-editor^="build.dataset:new:"]').getByRole("button", { name: "Visual", exact: true }).click();
  37  |         await page.getByLabel("Dataset name", { exact: true }).fill(name);
  38  |         await page.getByLabel("Dataset title", { exact: true }).fill("Raw integration rows");
  39  |         await page.getByRole("button", { name: "Save dataset", exact: true }).click();
  40  |         await expect.poll(() => page.url().includes("id=new")).toBe(false);
  41  |         const raw = new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
  42  |         const clean = fresh("CLEAN"), conn = fresh("CONN");
  43  |         await builder.decide("build.dataset.create", { type: "build.dataset", id: clean }, { name: `${name}clean`, title: "Clean integration rows" });
  44  |         await builder.decide("build.connection.create", { type: "build.connection", id: conn }, { name: `${name}conn`, title: "External fixture", kind: "http", address: `http://127.0.0.1:${address.port}/`, allowPrivate: true });
  45  |         await builder.decide("build.connection.check", { type: "build.connection", id: conn }, {});
  46  |         await expect.poll(async () => (await record("build.connection", conn))?.state, { timeout: 15_000 }).toBe("ready");
  47  |         await builder.open(page, "/data-source?id=new");
  48  |         await page.locator('[data-structured-editor^="build.source:new:"]').getByRole("button", { name: "Visual", exact: true }).click();
  49  |         await page.getByLabel("Source name", { exact: true }).fill(`${name}source`);
  50  |         await page.getByLabel("Source title", { exact: true }).fill("External rows");
  51  |         await page.getByRole("combobox", { name: /^Connection/ }).selectOption(conn);
  52  |         await page.getByRole("combobox", { name: "Rows go to", exact: true }).selectOption("dataset");
  53  |         await page.getByRole("combobox", { name: /^Target dataset/ }).selectOption(raw);
  54  |         await page.getByRole("button", { name: "Publish", exact: true }).click();
  55  |         await page.getByRole("button", { name: "Pull now", exact: true }).click();
  56  |         await expect.poll(async () => (await record("build.dataset", raw))?.version, { timeout: 15_000 }).toBe(1);
  57  |         await expect(page.getByRole("tab", { name: /^Data sources(?: ·|$)/ })).toHaveCount(1);
  58  |         await builder.open(page, "/pipeline?id=new");
  59  |         await page.locator('[data-structured-editor^="build.pipeline:new:"]').getByRole("button", { name: "Visual", exact: true }).click();
  60  |         await page.getByLabel("Pipeline name", { exact: true }).fill(`${name}pipe`);
  61  |         await page.getByLabel("Pipeline title", { exact: true }).fill("Clean external rows");
  62  |         await page.getByRole("combobox", { name: /^Input dataset/ }).selectOption(raw);
  63  |         await page.getByRole("combobox", { name: "Output dataset", exact: true }).selectOption(clean);
  64  |         await page.getByRole("button",{name:"Add block",exact:true}).click();
  65  |         await page.getByRole("option", { name: /^cast/ }).click();
  66  |         // Select the step by its available grammar, then fill its labelled fields.
  67  |         await page.locator("select").filter({ has: page.locator('option[value="cast"]') }).selectOption("cast");
  68  |         await page.getByLabel("Column", { exact: true }).fill("qty");
  69  |         await page.getByRole("combobox", { name: "Type", exact: true }).selectOption("number");
  70  |         await page.getByRole("button", { name: "Add expectation", exact: true }).click();
  71  |         await page.getByPlaceholder("column", { exact: true }).fill("sku");
  72  |         await page.getByRole("button", { name: "Add expectation", exact: true }).click();
  73  |         await page.getByPlaceholder("column", { exact: true }).nth(1).fill("qty");
  74  |         await page.locator("select").filter({ has: page.locator('option[value="range"]') }).nth(1).selectOption("range");
  75  |         await page.getByPlaceholder("0..100", { exact: true }).fill("0..10");
  76  |         await page.getByRole("button", { name: "Publish", exact: true }).click();
  77  |         await page.getByRole("button", { name: "Run now", exact: true }).click();
  78  |         await expect.poll(async () => (await record("build.dataset", clean))?.version, { timeout: 15_000 }).toBe(1);
  79  |         await expect(page.getByRole("tab", { name: /^Pipelines(?: ·|$)/ })).toHaveCount(1);
  80  |         const pipe = new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
  81  |         await page.locator(`[data-structured-editor^="build.pipeline:${pipe}:"]`).getByRole("button", { name: "Visual", exact: true }).click();
  82  |         await expect.poll(async () => (await record("build.pipeline", pipe)).last).toMatchObject({ rows: 3, written: 1, quarantined: 2 });
  83  |         expect((await record("build.datasetversion", `${clean}@1`)).data).toEqual([{ id: "IN-A", sku: "A", qty: 3 }]);
  84  |         const lastRun = page.locator(`[data-structured-editor^="build.pipeline:${pipe}:"] section`).filter({ has: page.getByRole("heading", { name: "Last run", exact: true }) });
  85  |         await expect(lastRun.getByRole("term").filter({ hasText: /^Rows$/ }).locator("..").getByRole("definition")).toHaveText("3");
  86  |         await expect(lastRun.getByRole("term").filter({ hasText: /^Written$/ }).locator("..").getByRole("definition")).toHaveText("1");
  87  |         await expect(lastRun.getByRole("term").filter({ hasText: /^Quarantined$/ }).locator("..").getByRole("definition")).toHaveText("2");
  88  |         if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: info.outputPath("pipeline.png"), fullPage: true });
  89  |         await builder.open(page, `/dataset?id=${clean}`);
  90  |         await page.locator(`[data-structured-editor^="build.dataset:${clean}:"]`).getByRole("button", { name: "Visual", exact: true }).click();
  91  |         await expect(page.getByText("A", { exact: true }).last()).toBeVisible();
  92  |         await expect(page.getByRole("group", { name: "Dependency diagram", exact: true })).toHaveAttribute("aria-busy", "false");
  93  |         await page.getByRole("region",{name:"Declared dependencies",exact:true}).getByText("Clean external rows",{exact:true}).dblclick();
  94  |         await page.locator(`[data-structured-editor^="build.pipeline:${pipe}:"]`).getByRole("button", { name: "Visual", exact: true }).click();
  95  |         await expect(page.getByRole("textbox", { name: "Pipeline name", exact: true })).toHaveValue(`${name}pipe`);
  96  | 
  97  |         // The object is the integration pipeline's target. The whole delivery
  98  |         // is frozen after its complete integration closure exists.
  99  |         const { id: object, type } = await builder.object({ name: `${name}receipt`, title: "Integration receipt", plural: "Integration receipts",
  100 |           fields: [{ name: "item", title: "Item", type: "text", search: true }, { name: "qty", title: "Quantity", type: "integer" }, { name: "docno", title: "Document", type: "text" },
  101 |             { name: "code", title: "Code", type: "text", search: true }, { name: "name", title: "Name", type: "text", search: true }],
  102 |           implements: ["core.coded"],
  103 |           states: [{ name: "open", title: "Open" }, { name: "received", title: "Received" }],
  104 |           actions: [{ name: "receive", title: "Receive", from: ["open"], to: "received", roles: ["user"],
  105 |             inputs: [{ name: "item", title: "Received item", type: "text", required: true }] }],
  106 |         });
  107 |         const receiving = await builder.page({ name: `${name}receiving`, title: "Receiving workspace", object: type,
  108 |           sections: [{ id: "rows", widget: "table", configVersion: 1, fields: ["item", "qty", "state", "docno"] },
  109 |             { id: "actions", widget: "actions", configVersion: 1, actions: [`${type}.receive`] }],
  110 |           document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: ["rows", "actions"] }, rows: { kind: "widget", section: "rows" }, actions: { kind: "widget", section: "actions" } } },
  111 |         });
  112 |         const flowID = fresh("FLOW"), flowName = `${name}received`, appID = fresh("APP");
  113 |         await builder.decide("build.process.create", { type: "build.process", id: flowID }, { name: flowName, title: "Received delivery", object: type, when: "received", steps: [{ name: "done", kind: "end" }] });
  114 |         const matching = fresh("MATCH");
  115 |         await builder.decide("build.match.create", { type: "build.match", id: matching }, { name: `${name}match`, title: "Match receiving items", object: type, keys: [{ fields: ["item"], normalize: "folded" }] });
  116 |         await builder.decide("build.match.publish", { type: "build.match", id: matching }, {});
  117 |         const land = fresh("PIPE");
  118 |         await builder.decide("build.pipeline.create", { type: "build.pipeline", id: land }, { name: `${name}land`, title: "Land receiving records", input: clean, steps: [{ kind: "rename", from: "sku", to: "item" }] });
  119 |         await builder.open(page, `/pipeline?id=${land}`);
  120 |         await page.locator(`[data-structured-editor^="build.pipeline:${land}:"]`).getByRole("button", { name: "Visual", exact: true }).click();
  121 |         await page.getByRole("combobox", { name: "Write to", exact: true }).selectOption("object");
  122 |         await page.getByRole("combobox", { name: /^Output object/ }).selectOption(type);
  123 |         await page.getByRole("textbox", { name: /^Record id column/ }).fill("id");
  124 |         await page.getByRole("button", { name: "Publish", exact: true }).click();
> 125 |         await expect.poll(async () => (await record("build.pipeline", land)).last?.written, { timeout: 15_000 }).toBe(1);
      |                                                                                                                  ^ Error: expect(received).toBe(expected) // Object.is equality
  126 |         const wb = fresh("WB"), receipt = "IN-A";
  127 |         expect(await record(type, receipt)).toMatchObject({ item: "A", qty: 3, state: "open" });
  128 |         await builder.decide("build.writeback.create", { type: "build.writeback", id: wb }, { name: `${name}wb`, title: "Receipt delivery", object: type, on: "receive", connection: conn, path: "receipts", result: [{ from: "MaterialDocument", to: "docno" }] });
  129 |         await builder.open(page, `/writeback?id=${wb}`);
  130 |         await page.locator(`[data-structured-editor^="build.writeback:${wb}:"]`).getByRole("button", { name: "Visual", exact: true }).click();
  131 |         await page.getByRole("button", { name: "Publish", exact: true }).click();
  132 |         await expect.poll(async () => (await record("build.writeback", wb))?.state).toBe("published");
  133 | 
  134 |         await builder.decide("build.app.create", { type: "build.app", id: appID }, { name: `${name}app`, title: "Receiving application", pages: [receiving.name],
  135 |           resources: [
  136 |             { app: "build", kind: "flow", name: `build.${flowName}` },
  137 |             { app: "build", kind: "connection", name: `${name}conn` },
  138 |             { app: "build", kind: "source", name: `${name}source` },
  139 |             { app: "build", kind: "dataset", name },
  140 |             { app: "build", kind: "dataset", name: `${name}clean` },
  141 |             { app: "build", kind: "pipeline", name: `${name}pipe` },
  142 |             { app: "build", kind: "pipeline", name: `${name}land` },
  143 |             { app: "build", kind: "writeback", name: `${name}wb` },
  144 |             { app: "build", kind: "matching", name: `${name}match` },
  145 |           ], groups: [{ title: "Empty receiving group", pages: [] }],
  146 |         });
  147 |         await builder.open(page, `/module?id=${appID}`);
  148 |         await page.getByRole("button", { name: "Publish", exact: true }).click();
  149 |         await page.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  150 |         await page.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  151 |         await expect(page.getByRole("alert").filter({ hasText: "holds no page" })).toBeVisible();
  152 |         await page.getByRole("button", { name: "Open the module's navigation", exact: true }).click();
  153 |         await expect(page.getByRole("tab", { name: "Navigation", exact: true })).toHaveAttribute("aria-selected", "true");
  154 |         await page.getByRole("button", { name: "Remove group", exact: true }).click();
  155 |         await expect.poll(async () => (await record("build.app", appID)).groups ?? []).toEqual([]);
  156 |         let candidate = "";
  157 |         await editor.release(async () => {
  158 |           await expect(page.getByRole("list", { name: "Joint selection", exact: true })).toContainText(receiving.id);
  159 |           await expect(page.getByRole("list", { name: "Joint selection", exact: true })).toContainText(flowID);
  160 |           const selectedCandidate = page.getByRole("combobox", { name: "Saved candidate", exact: true });
  161 |           await expect(selectedCandidate).not.toHaveValue("");
  162 |           candidate = await selectedCandidate.inputValue();
  163 |           const saved = await request.get(`/v1/releases/candidates/${candidate}`, { headers });
  164 |           expect(saved.ok()).toBe(true);
  165 |           const review = await saved.json();
  166 |           const kinds = new Set(review.assets.map((asset: { ref: { kind: string } }) => asset.ref.kind));
  167 |           for (const kind of ["connection", "source", "dataset", "pipeline", "writeback", "matching"]) expect(kinds.has(kind)).toBe(true);
  168 |         }, true);
  169 |         await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
  170 |         await builder.open(page, `/writeback?id=${wb}`);
  171 |         await page.locator(`[data-structured-editor^="build.writeback:${wb}:"]`).getByRole("button", { name: "Visual", exact: true }).click();
  172 | 
  173 |         const context = await browser.newContext({ baseURL: fixture.baseURL, locale: "en-US" });
  174 |         const member = await (await request.get("/v1/me", { headers: operator.headers })).json();
  175 |         const hadIntegrator = member.profile.roles.build === "integrator" || member.profile.grants?.some((grant: { app: string; role: string }) => grant.app === "build" && grant.role === "integrator");
  176 |         let addedIntegrator = false;
  177 |         try {
  178 |           // An ordinary member receives the imported row in the released application, without builder access.
  179 |           const business = await context.newPage();
  180 |           await operator.open(business, "/home");
  181 |           await business.getByRole("button", { name: "Receiving application", exact: true }).click();
  182 |           await expect(business.getByRole("heading", { name: "Receiving workspace", exact: true })).toBeVisible();
  183 |           await business.getByRole("row").filter({ hasText: receipt }).click();
  184 |           await business.getByRole("button", { name: "Receive", exact: true }).click();
  185 |           const confirmation = business.getByRole("dialog");
  186 |           await confirmation.getByRole("textbox", { name: /^Received item/ }).fill("A");
  187 |           await confirmation.getByRole("button", { name: "Receive", exact: true }).click();
  188 |           await expect(confirmation).toHaveCount(0);
  189 |           await expect.poll(async () => (await record(type, receipt)).state).toBe("received");
  190 |           await expect.poll(async () => (await record("flow.instance", `build.${flowName}:${receipt}`))?.release).toBe(candidate);
  191 |           await expect(operator.decide("build.object.edit", { type: "build.object", id: object }, { title: "Unauthorized" })).rejects.toThrow(/POLICY_DENIED/);
  192 |           await expect.poll(async () => (await (await request.get("/v1/integration-effects", { headers })).json()).find((e: { event: string }) => e.event === `writeback/${name}wb`)?.state, { timeout: 15_000 }).toBe("retrying");
  193 |           await expect(page.getByText("1 queued", { exact: true })).toBeVisible();
  194 |           const first = attempts[0];
  195 |           if (!first) throw new Error("writeback did not reach the external fixture");
  196 |           expect(first.status).toBe(503);
  197 |           expect(JSON.parse(first.body)).toMatchObject({ id: receipt, item: "A" });
  198 |           state.down = false;
  199 |           await expect.poll(async () => (await record(type, receipt))?.docno, { timeout: 20_000 }).toBe("DOC-1");
  200 |           await expect(page.getByText("1 delivered", { exact: true })).toBeVisible();
  201 |           await expect(business.getByRole("row").filter({ hasText: receipt })).toContainText("DOC-1");
  202 |           await business.reload();
  203 |           await expect(business.getByRole("row").filter({ hasText: receipt })).toContainText("DOC-1");
  204 |           if (process.env.PLATFORM_SCREENSHOTS) await business.screenshot({ path: info.outputPath("receiving-application.png"), fullPage: true });
  205 |           expect(attempts.filter((a) => a.status === 201)).toHaveLength(1);
  206 |           for (const attempt of attempts) expect(attempt).toMatchObject({ key: first.key, body: first.body });
  207 |           const health = await (await request.get("/v1/health", { headers })).json();
  208 |           expect(health.status).not.toBe("quarantined");
  209 |           await builder.open(page, "/integration-health");
  210 |           await expect(page.getByRole("button", { name: /Writeback · Receipt delivery/ })).toBeVisible();
  211 |           if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: info.outputPath("integration-health.png"), fullPage: true });
  212 | 
  213 |           // The interface selector must distinguish equal IDs and open the actual object.
  214 |           const reference = await builder.object({ name: `${name}reference`, title: "Reference receipt", implements: ["core.coded"],
  215 |             fields: [{ name: "code", title: "Code", type: "text", search: true }, { name: "name", title: "Name", type: "text", search: true }],
  216 |           });
  217 |           await builder.decide(`${reference.type}.create`, { type: reference.type, id: receipt }, { code: "REF", name: "Duplicate identity" });
  218 |           await builder.open(page, "/query?id=new");
  219 |           await page.locator('[data-structured-editor^="build.query:new:"]').getByRole("button", { name: "Visual", exact: true }).click();
  220 |           await page.getByRole("textbox", { name: "Query name", exact: true }).fill(`${name}lookup`);
  221 |           await page.getByRole("textbox", { name: "Query title", exact: true }).fill("Receiving lookup");
  222 |           await page.getByRole("textbox", { name: "Query description", exact: true }).fill("Typed interface selection");
  223 |           await page.getByRole("combobox", { name: "Query source kind", exact: true }).selectOption("interface");
  224 |           await page.getByRole("combobox", { name: "Source interface", exact: true }).selectOption("core.coded");
  225 |           await page.getByRole("button", { name: "Save query", exact: true }).click();
```