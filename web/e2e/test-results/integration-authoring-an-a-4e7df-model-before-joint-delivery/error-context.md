# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: integration-authoring.spec.ts >> an application assembles integration drafts over a saved model before joint delivery
- Location: tests/integration-authoring.spec.ts:45:1

# Error details

```
Error: expect(locator).toBeVisible() failed

Locator: getByRole('alert').filter({ hasText: 'The connection is not ready' })
Expected: visible
Timeout: 5000ms
Error: element(s) not found

Call log:
  - Expect "toBeVisible" getByRole('alert').filter({ hasText: 'The connection is not ready' }) with timeout 5000ms
  - waiting for getByRole('alert').filter({ hasText: 'The connection is not ready' })

```

```yaml
- complementary "Platform":
  - button "Home"
  - button "Search"
  - button "Notifications": "5"
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
    - code: 7adb5bdc5e
- navigation "Main":
  - region "Application assets":
    - heading "Integration drafting application" [level=2]
    - navigation "Application workspaces":
      - button "Model"
      - button "Behavior"
      - button "Interface"
      - button "Connections"
      - button "Release"
    - searchbox "Filter assets in this workspace"
    - paragraph: 3 assets in this workspace
    - button "Connections and data 3" [expanded]
    - button "Saved external connection draft"
    - button "Saved input rows"
    - button "Saved receiving feed draft"
    - button "Organize business concepts"
  - text: Applications
  - button "All applications"
- region "Data sources":
  - tablist:
    - tab "Application Studio · Integration drafting application"
    - tab "Datasets"
    - tab "Connections"
    - tab "Data sources" [selected]:
      - text: Data sources
      - button "Close tab":
        - img
  - tabpanel "Data sources"
- status: Data sources opened
- alert
- region "Data sources":
  - group:
    - navigation "Application context":
      - button "Integration drafting application"
    - heading "Saved receiving feed" [level=1]
    - button "About Saved receiving feed": View help
    - button "Data sources"
    - button "Discard" [disabled]
    - button "Save data source"
    - button "Review and publish…"
    - button "Publish"
    - alert: The target object build.integrationmvtiblef652 is not installed
    - status:
      - paragraph: This logical connection is not configured for use. You can publish the declaration; configure and check it before running the integration.
      - button "Open connection"
    - group "Editing mode":
      - button "Visual" [pressed]
      - button "Source editor"
    - group:
      - heading "Endpoint" [level=2]
      - text: Source name
      - textbox "Source name":
        - /placeholder: erpitems
        - text: integrationmvtiblef652source
      - text: Source title
      - textbox "Source title": Saved receiving feed
      - text: Connection
      - combobox "Connection Choose a saved logical connection. Its address and credentials can be configured after publication.":
        - 'option "— none: a bare URL"'
        - option "Saved external connection · http" [selected]
        - option "Confirmation receiver · http"
        - option "External warehouse · http"
        - option "External device controller · http"
      - text: Choose a saved logical connection. Its address and credentials can be configured after publication. Profile
      - combobox "Profile":
        - option "JSON" [selected]
        - option "CSV"
      - text: URL
      - textbox "URL Relative to the connection's address, or absolute.":
        - /placeholder: items.csv
        - text: rows
      - text: Relative to the connection's address, or absolute. Rows at
      - textbox "Rows at Dotted path to the array inside the answer; empty when the answer is the array.":
        - /placeholder: data.items
      - text: Dotted path to the array inside the answer; empty when the answer is the array. Pull every
      - combobox "Pull every":
        - option "Only when asked" [selected]
        - option "every 15 minutes"
        - option "every hour"
        - option "every day"
        - option "every week"
      - heading "Target and mapping" [level=2]
      - text: Rows go to
      - combobox "Rows go to":
        - option "An object, mapped here" [selected]
        - option "A dataset, as they came"
        - option "A continuous Flow" [disabled]
      - text: Target object
      - combobox "Target object":
        - option "Choose an object type"
        - option "Agent evaluation"
        - option "Agent memory"
        - option "Agent run"
        - option "Agent switch"
        - option "Access request"
        - option "Agent"
        - option "Alert rule"
        - option "Application"
        - option "Code function"
        - option "Business confirmation"
        - option "Connection"
        - option "Customer"
        - option "Dataset"
        - option "Dataset version"
        - option "Dependency source"
        - option "Device dispatch"
        - option "Device dispatch requests"
        - option "Business disposition"
        - option "Function evaluation"
        - option "AI function"
        - option "AI function call"
        - option "Link type"
        - option "Matching rule"
        - option "Object"
        - option "Opportunity"
        - option "Empty page records"
        - option "Preview entry"
        - option "Context record"
        - option "Change discovery object updated"
        - option "Retained published object"
        - option "Runtime case object"
        - option "Evaluation object"
        - option "Experience record"
        - option "Heating zone"
        - option "Disposition evidence"
        - option "Transaction balance"
        - option "Mapping reference"
        - option "Mapping target"
        - option "Definition editor source"
        - option "Application reference model"
        - option "Source supplier"
        - option "Guided source item"
        - option "Declaration model"
        - option "Observation source"
        - option "Versioned order"
        - option "Page"
        - option "Pipeline"
        - option "Process"
        - option "Property type"
        - option "Query"
        - option "Receiving confirmation"
        - option "Receiving request"
        - option "Business route"
        - option "Sample set"
        - option "Service case"
        - option "Data source"
        - option "Source operation item"
        - option "Decision table"
        - option "Test plan"
        - option "Trainable record"
        - option "Visit"
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
        - option "Related draft item"
        - option "AI source model"
        - option "Saved receiving model" [selected]
        - option "Trial task"
        - option "Service case"
        - option "Trial work order"
      - paragraph: Saved model draft
      - button "Edit target object"
      - text: Row id field
      - textbox "Row id field The row field whose value becomes the record id; the same id edits the existing record.":
        - /placeholder: id
        - text: id
      - text: The row field whose value becomes the record id; the same id edits the existing record. Row field Object field Convert
      - group "Field mapping 1":
        - textbox "Row field":
          - /placeholder: sku
          - text: note
        - combobox "Object field":
          - option "Choose a field"
          - option "Imported note" [selected]
          - option "External receipt"
        - combobox "Convert":
          - option "as is" [selected]
          - option "string"
          - option "number"
          - option "boolean"
          - option "date"
        - button "Remove"
        - term: Field type
        - definition: text
        - term: Required
        - definition: "No"
        - term: Reference object
        - definition: None
        - button "Edit field definition"
      - button "Add mapping"
      - heading "Last pull" [level=2]
      - paragraph: Not pulled yet.
    - button "Check inputs"
    - button "Try operation"
- region "Notifications alt+T"
```

# Test source

```ts
  34  |   await expect(trial.getByRole("region", { name: "Trial output rows", exact: true })).toContainText("ROW-1");
  35  |   await expect(trial.getByRole("region", { name: "Trial quarantined rows", exact: true })).toContainText("sku");
  36  |   await shots(page, page.locator("body"), "pipeline-draft-trial", info);
  37  |   await page.getByPlaceholder("column", { exact: true }).fill("qty");
  38  |   await expect(trial).toHaveCount(0);
  39  |   expect(await builder.record("build.dataset", input)).toEqual(original.input);
  40  |   expect(await builder.record("build.dataset", output)).toEqual(original.output);
  41  |   expect(await builder.record("build.pipeline", pipeline)).toEqual(original.pipeline);
  42  |   expect(await operator.definitions()).toEqual(original.definitions);
  43  | });
  44  | 
  45  | test("an application assembles integration drafts over a saved model before joint delivery", async ({ page, builder, operator }, info) => {
  46  |   test.setTimeout(150_000);
  47  |   const name = slug("integration"), objectID = fresh("MODEL"), type = `build.${name}`;
  48  |   let pulls = 0, deliveries = 0;
  49  |   const external = createServer(async (request, response) => {
  50  |     response.setHeader("Content-Type", "application/json");
  51  |     if (request.method === "POST") {
  52  |       for await (const _chunk of request) { /* consume the original writeback body */ }
  53  |       deliveries++;
  54  |       response.writeHead(201).end(JSON.stringify({ receipt: "EXTERNAL-RECEIPT" }));
  55  |     } else if (request.url === "/rows") {
  56  |       pulls++;
  57  |       response.end(JSON.stringify([{ id: "FROM-SOURCE", note: "Imported note" }]));
  58  |     } else response.end("{}");
  59  |   });
  60  |   external.listen(0, "127.0.0.1");
  61  |   await once(external, "listening");
  62  |   try {
  63  |     const address = external.address();
  64  |     if (!address || typeof address === "string") throw Error("External system did not listen");
  65  |     await builder.decide("build.object.create", { type: "build.object", id: objectID }, {
  66  |       name, title: "Saved receiving model", fields: [
  67  |         { name: "note", title: "Imported note", type: "text" },
  68  |         { name: "receipt", title: "External receipt", type: "text" },
  69  |       ], states: [{ name: "open", title: "Open" }],
  70  |     });
  71  |     const application = await builder.application({ title: "Integration drafting application", resources: [{ app: "build", kind: "object", name: type }] });
  72  |     const original = await operator.definitions();
  73  |     await builder.open(page, `/project?id=${application.id}&area=integration`);
  74  |     const create = async (label: string) => {
  75  |       await page.getByRole("button", { name: "New", exact: true }).click();
  76  |       await page.getByRole("menuitem", { name: label, exact: true }).click();
  77  |       await expect(page.getByRole("dialog")).toHaveCount(0);
  78  |       await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  79  |     };
  80  |     const back = async () => {
  81  |       await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Integration drafting application", exact: true }).click();
  82  |       await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Connections", exact: true }).click();
  83  |     };
  84  |     const savedID = async (save: string) => {
  85  |       await page.getByRole("button", { name: save, exact: true }).click();
  86  |       await expect(page).not.toHaveURL(/[?&]id=new(?:&|$)/);
  87  |       return new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
  88  |     };
  89  | 
  90  |     await create("Dataset");
  91  |     await page.getByRole("textbox", { name: "Dataset name", exact: true }).fill(`${name}raw`);
  92  |     await page.getByRole("textbox", { name: "Dataset title", exact: true }).fill("Saved input rows");
  93  |     // Saving the definition and joining an application are separate writes.
  94  |     // A refused join must retry that same definition rather than create it again.
  95  |     let refuseJoin = true;
  96  |     await page.route("**/v1/submissions", async route => {
  97  |       const input = route.request().postDataJSON();
  98  |       if (refuseJoin && input.schema?.name === "build.app.edit" && input.target?.id === application.id) {
  99  |         refuseJoin = false;
  100 |         return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_CONFLICT", message: "Application join refused" } }) });
  101 |       }
  102 |       await route.continue();
  103 |     });
  104 |     await page.getByRole("button", { name: "Save dataset", exact: true }).click();
  105 |     await expect(page.getByRole("alert").filter({ hasText: "Application join refused" })).toBeVisible();
  106 |     const datasets = await builder.read<{ records: { id: string; name: string; revision: number }[] }>("records/build.dataset?limit=1000");
  107 |     const dataset = datasets.records.find(record => record.name === `${name}raw`)!;
  108 |     expect(dataset).toBeTruthy();
  109 |     expect(await savedID("Save dataset")).toBe(dataset.id);
  110 |     expect((await builder.record("build.dataset", dataset.id)).revision).toBe(dataset.revision);
  111 |     await page.unroute("**/v1/submissions");
  112 | 
  113 |     await back();
  114 |     await create("Connection");
  115 |     await page.getByRole("textbox", { name: "Connection name", exact: true }).fill(`${name}connection`);
  116 |     await page.getByRole("textbox", { name: "Connection title", exact: true }).fill("Saved external connection");
  117 |     await page.getByRole("textbox", { name: "Address", exact: true }).fill(`http://127.0.0.1:${address.port}/`);
  118 |     await page.getByRole("checkbox", { name: "Allow http and private addresses (on-premise systems)", exact: true }).check();
  119 |     const connection = await savedID("Save connection");
  120 |     expect((await builder.record("build.connection", connection)).state).toBe("draft");
  121 | 
  122 |     await back();
  123 |     await create("Data source");
  124 |     await page.getByRole("textbox", { name: "Source name", exact: true }).fill(`${name}source`);
  125 |     await page.getByRole("textbox", { name: "Source title", exact: true }).fill("Saved receiving feed");
  126 |     await page.getByRole("combobox", { name: /^Connection/ }).selectOption(connection);
  127 |     await expect(page.getByRole("status").filter({ hasText: "This logical connection is not configured for use. You can publish the declaration; configure and check it before running the integration." })).toBeVisible();
  128 |     await page.getByRole("textbox", { name: /^URL/ }).fill("rows");
  129 |     await page.getByRole("combobox", { name: "Target object", exact: true }).selectOption(type);
  130 |     await page.getByRole("button", { name: "Add mapping", exact: true }).click();
  131 |     await page.getByRole("textbox", { name: "Row field", exact: true }).fill("note");
  132 |     await page.getByRole("combobox", { name: "Object field", exact: true }).selectOption("note");
  133 |     await page.getByRole("button", { name: "Publish", exact: true }).click();
> 134 |     await expect(page.getByRole("alert").filter({ hasText: "The connection is not ready" })).toBeVisible();
      |                                                                                              ^ Error: expect(locator).toBeVisible() failed
  135 |     await expect(page).toHaveURL(/[?&]id=new(?:&|$)/);
  136 |     const sources = await builder.read<{ records: { id: string; name: string; revision: number }[] }>("records/build.source?limit=1000");
  137 |     const sourceDraft = sources.records.find(record => record.name === `${name}source`)!;
  138 |     const source = await savedID("Save data source");
  139 |     expect(source).toBe(sourceDraft.id);
  140 |     expect((await builder.record("build.source", source)).revision).toBe(sourceDraft.revision);
  141 | 
  142 |     await back();
  143 |     await create("Pipeline");
  144 |     await page.getByRole("textbox", { name: "Pipeline name", exact: true }).fill(`${name}pipeline`);
  145 |     await page.getByRole("textbox", { name: "Pipeline title", exact: true }).fill("Saved receiving pipeline");
  146 |     await page.getByRole("combobox", { name: /^Input dataset/ }).selectOption(dataset.id);
  147 |     await page.getByRole("combobox", { name: "Write to", exact: true }).selectOption("object");
  148 |     await page.getByRole("combobox", { name: /^Output object/ }).selectOption(type);
  149 |     await page.getByRole("textbox", { name: /^Record id column/ }).fill("id");
  150 |     await savedID("Save pipeline");
  151 | 
  152 |     await back();
  153 |     await create("Writeback");
  154 |     await page.getByRole("textbox", { name: "Writeback name", exact: true }).fill(`${name}writeback`);
  155 |     await page.getByRole("textbox", { name: "Writeback title", exact: true }).fill("Saved receipt delivery");
  156 |     await page.getByRole("combobox", { name: "Object", exact: true }).selectOption(type);
  157 |     await page.getByRole("combobox", { name: /^Connection/ }).selectOption(connection);
  158 |     await page.getByRole("textbox", { name: /^Path/ }).fill("receipts");
  159 |     await page.getByRole("button", { name: "Add body field", exact: true }).click();
  160 |     await page.getByRole("combobox", { name: "Payload field", exact: true }).selectOption("note");
  161 |     await page.getByRole("textbox", { name: "Body key", exact: true }).fill("note");
  162 |     await page.getByRole("button", { name: "Add answer field", exact: true }).click();
  163 |     await page.getByRole("textbox", { name: "Answer key", exact: true }).fill("receipt");
  164 |     await page.getByRole("combobox", { name: "Record field", exact: true }).selectOption("receipt");
  165 |     await savedID("Save writeback");
  166 | 
  167 |     await back();
  168 |     await create("Matching rule");
  169 |     await page.getByRole("textbox", { name: "Rule name", exact: true }).fill(`${name}matching`);
  170 |     await page.getByRole("textbox", { name: "Rule title", exact: true }).fill("Saved receiving identity");
  171 |     await page.getByRole("combobox", { name: "Object", exact: true }).selectOption(type);
  172 |     await page.getByRole("button", { name: "Add key", exact: true }).click();
  173 |     await page.getByLabel("Fields", { exact: true }).fill("note");
  174 |     await savedID("Save matching rule");
  175 |     await shots(page, page.locator("body"), "application-integration-drafts", info);
  176 | 
  177 |     const members = (await builder.record("build.app", application.id)).resources;
  178 |     for (const kind of ["object", "dataset", "connection", "source", "pipeline", "writeback", "matching"]) {
  179 |       expect(members.some((member: { kind: string }) => member.kind === kind)).toBe(true);
  180 |     }
  181 |     expect(pulls).toBe(0);
  182 |     expect(deliveries).toBe(0);
  183 |     expect(await operator.definitions()).toEqual(original);
  184 |     await builder.open(page, `/connection?id=${connection}&application=${application.id}`);
  185 |     await page.getByRole("button", { name: "Check", exact: true }).click();
  186 |     await expect.poll(async () => (await builder.record("build.connection", connection)).state, { timeout: 15_000 }).toBe("ready");
  187 |     await builder.open(page, `/project?id=${application.id}&area=release`);
  188 |     const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  189 |     await verify.getByRole("button", { name: "Add the drafts of Integration drafting application", exact: true }).click();
  190 |     await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  191 |     await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  192 |     expect((await builder.record("build.object", objectID)).published ?? "").toBe("");
  193 |     expect((await builder.record("build.source", source)).state).toBe("draft");
  194 |     expect(await operator.definitions()).toEqual(original);
  195 |     await verify.getByRole("button", { name: "Activate release", exact: true }).click();
  196 |     await expect(verify.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
  197 |     await expect.poll(async () => (await builder.record(type, "FROM-SOURCE"))?.receipt, { timeout: 20_000 }).toBe("EXTERNAL-RECEIPT");
  198 |     expect(pulls).toBeGreaterThan(0);
  199 |     expect(deliveries).toBe(1);
  200 |     expect((await builder.record(type, "FROM-SOURCE")).note).toBe("Imported note");
  201 |     await expect(operator.decide("build.source.edit", { type: "build.source", id: source }, { title: "Unauthorized" })).rejects.toThrow(/DENIED/);
  202 |   } finally {
  203 |     await new Promise<void>((resolve, reject) => external.close(error => error ? reject(error) : resolve()));
  204 |   }
  205 | });
  206 | 
```