import type { CatalogEntry } from "@platform/catalog";

const asset = (id: string, name: string, summary: string, exports: string[], example: string, extra: Partial<CatalogEntry> = {}): CatalogEntry => ({
  id: `app/${id}`, owner: "@platform/app", name, summary, layer: 3, authority: "api", maturity: "recommended", scope: "platform",
  uses: ["code"], tags: ["record", "binding", "host"], source: "web/packages/app/src/index.tsx", exports, example,
  constraints: ["Connect through the workspace Host. The Catalog preview uses local fixtures and grants no permissions."], ...extra,
});

export const entries: CatalogEntry[] = [
 asset("observation-statistics-reader","Original window statistics reader","Read recent business-time window statistics through the original authorized aggregate owner.",["createObservationStatisticsReader"],"ObservationStatisticsReaderExample",{type:"ObservationStatisticsRequest",source:"web/packages/app/src/exploration/observation-reader.ts",tags:["observations","statistics","window","read"],dependencies:["ui/observation-statistics","ui/datetime-input"],states:["Pending","Refused","Retired"],constraints:["The original host owns matching membership, time order, numeric semantics and the atomic read. The reader validates exact request echoes and discards old scopes, revisions or attempts without downloading records or retrying another mode."]}),
  asset("record-row-actions","Original row decisions","Submit original typed row targets and revisions, preserving unanswered decision keys.",["createRecordActionSubmitter"],"RecordDetailExample",{source:"web/packages/app/src/record-actions.ts",dependencies:["app/decision-confirmation"],states:["Pending","Refused","Retired"],constraints:["The original host owns authorization, validation and approval. An unanswered row can only resend its original decision with identical payload and revision; retiring a window ends new submissions."]}),
  asset("record-exploration", "Original record exploration", "Traverse retained original relationships, confirm every record and preserve authorized window totals.", ["createRecordExploration"], "RecordDetailExample", {type:"RecordExplorationReader",source:"web/packages/app/src/exploration/reader.ts",dependencies:["app/record-detail","ui/graph"],states:["Confirming","Refused","Retired"],constraints:["The bounded path and original traversal reader grant no extra record access. Counts retain original totals; typed output ports keep heterogeneous records separate."]}),
  asset("work-views", "Caller work services", "Confirm offered approval requests and use the member's original approval and notification decisions.", ["createWorkViews"], "RecordDetailExample", {type:"WorkViewsHost",source:"web/packages/app/src/work/service.ts",dependencies:["app/decision-confirmation","ui/tasks","ui/notifications"],states:["Confirming","Pending","Refused","Retired"],constraints:["The original inbox offers tasks; request IDs and revisions authorize approval decisions. Retiring a view never cancels a queued decision."]}),
  asset("record-collaboration", "Record collaboration services", "Post comments and attach real file bytes through the original record's authorized service actions.", ["createRecordCollaboration"], "RecordDetailExample", {type:"CollaborationTarget",source:"web/packages/app/src/collaboration/service.ts",dependencies:["app/record-detail"],states:["Confirming","Retired","Refused"],constraints:["Capture the original record and a current lease. Confirmed metadata never substitutes for authorized byte reads; queued decisions remain durable after a view retires."]}),
  asset("decision-confirmation", "Decision confirmation", "Match one decision's confirmation to its original tenant and idempotency key.", ["confirmedDecision"], "RecordDetailExample", {source:"web/packages/app/src/collaboration/decision.ts",dependencies:["app/record-collaboration"],states:["Pending","Confirmed","Refused"]}),
  asset("application-sessions", "Application sessions", "Share declared presentation state across pages of one scoped application instance.", ["ApplicationPage", "ApplicationSessionsProvider"], "ApplicationSessionsExample", {
    source:"web/packages/app/src/runtime/ApplicationRuntime.tsx", tags:["application","scope","instance","state"], dependencies:["app/composed-page","pattern/workspace"], states:["Shared","Isolated","Closed"],
    constraints:["Application owns scalar declarations and bounded queries. Pages use typed shared bindings; windows remain read-only, ephemeral and member-scoped."],
  }),
  asset("generated-form", "Generated record form", "Edit fields from the original object declaration; submit through the owning action.", ["GeneratedForm"], "GeneratedFormExample", {
    dependencies: ["ui/record-form"], uses: ["code", "widget"], widgets: ["form"], states: ["dirty", "pending", "validation"],
    snippet: 'import { GeneratedForm } from "@platform/app";\n<GeneratedForm type="your.object" submitLabel="Create" onSubmit={submit} onCancel={cancel} />',
  }),
  asset("records", "Authorized record list", "Browse records, choose a record and save a list view through the member’s host.", ["Records"], "RecordsExample", {
    dependencies: ["ui/record-list", "app/record-actions"], states: ["loading", "empty", "error"],
    snippet: 'import { Records } from "@platform/app";\n<Records type="your.object" />',
  }),
  asset("record-detail", "Authorized record detail", "Read a record with history, tasks and the actions offered to the current member.", ["RecordDetail", "OpenIn"], "RecordDetailExample", {
    dependencies: ["ui/record-page", "app/record-actions"], uses: ["code", "widget"], widgets: ["detail", "timeline", "tasks"],
    states: ["loading", "error", "pending", "conflict"], snippet: 'import { RecordDetail } from "@platform/app";\n<RecordDetail type="your.object" id={selectedID} />',
  }),
  asset("flow-instance", "Authorized workflow run", "Inspect a workflow's recorded version and startup release through the member's original read permissions.", ["FlowInstanceView"], "FlowInstanceExample", {
    source: "web/packages/app/src/flows.tsx", uses: ["code", "reference"], tags: ["flow", "run", "version", "release"],
    dependencies: ["ui/flow", "app/agent-run"], states: ["loading", "error", "running", "waiting", "done"],
    snippet: 'import { FlowInstanceView } from "@platform/app";\n<FlowInstanceView id={runID} />',
  }),
  asset("dashboard", "Bound dashboard", "Render the existing chart specification over the member’s permitted aggregates.", ["DashboardView"], "DashboardExample", {
    tags: ["aggregate", "chart", "metric"], uses: ["code", "widget"], widgets: ["chart", "metric"], source: "web/packages/app/src/index.tsx",
    snippet: 'import { DashboardView } from "@platform/app";\n<DashboardView dashboard={dashboard} />',
  }),
  asset("record-actions", "Declared action controls", "Render original action payloads and offered transitions without duplicating role rules.", ["NewActions", "RecordActions", "PayloadFields", "InlineActionForm"], "ActionsExample", {
    uses: ["code", "widget"], widgets: ["actions", "inline-action"], dependencies: ["ui/button", "app/generated-form"], source: "web/packages/app/src/actions.tsx",
    states: ["pending", "refused", "conflict"], snippet: 'import { RecordActions } from "@platform/app";\n<RecordActions type="your.object" record={record} steps />',
  }),
  asset("page-workspace", "Page workspace and preview", "The same list-detail descriptor serves a live workspace and a read-only sample preview.", ["PageWorkspace", "PagePreview"], "PageWorkspaceExample", {
    dependencies: ["ui/record-workspace", "app/record-detail"], uses: ["code", "reference"], source: "web/packages/app/src/pages.tsx",
    snippet: 'import { PageWorkspace } from "@platform/app";\n<PageWorkspace definition={installedPage} />',
  }),
  asset("composed-page", "Controlled page composition", "Render original Page sections with shared selection and filters; composition disables writes.", ["ComposedPage", "tableEditableFields", "SectionView"], "ComposedPageExample", {
    tags: ["page", "selection", "relation", "binding"],
    constraints: ["Named record selections keep same-object lists independent; parent bindings scope related sections.",
      "Form inputs read declared record paths. The original action checks writes; unavailable sources do not become manual inputs."],
    uses: ["code", "widget"], widgets: ["table", "detail", "actions", "chart", "metric", "pivot", "text", "filter", "form", "timeline", "tasks", "function", "compute"],
    source: "web/packages/app/src/sections.tsx", dependencies: ["app/generated-form", "ui/record-page", "app/record-actions", "app/compute-call", "ui/pivot"],
    snippet: 'import { ComposedPage } from "@platform/app";\n<ComposedPage page={installedPage.page} live={false} />',
  }),
  asset("agent-assistant", "Member-scoped assistant", "Give an installed agent a goal; review its proposed actions before confirming them.", ["Assistant"], "AssistantExample", {
    tags: ["agent", "goal", "human-review"], source: "web/packages/app/src/agents.tsx", dependencies: ["app/agent-run"],
    snippet: 'import { Assistant } from "@platform/app";\n<Assistant about="your.object/record-id" />',
  }),
  asset("agent-run", "Agent run and execution chain", "Inspect the original run trace, structured drafts and cross-application execution links.", ["RunView", "ChainGraph"], "AgentRunExample", {
    tags: ["agent", "trace", "chain"], source: "web/packages/app/src/agents.tsx", states: ["running", "waiting", "done", "stopped", "withheld"],
    snippet: 'import { RunView } from "@platform/app";\n<RunView id={runID} />',
  }),
  asset("knowledge-search", "Authorized cross-application search", "Search records and knowledge through the current member’s original read endpoints.", ["Search"], "SearchExample", {
    tags: ["search", "knowledge", "records"], source: "web/packages/app/src/agents.tsx", states: ["empty"],
    snippet: 'import { Search } from "@platform/app";\n<Search initial="order" />',
    constraints: ["Results come from the current member’s host. The offline example searches synthetic records only.", "This search currently has no dedicated error or pending feedback; callers must not claim otherwise."],
  }),
  asset("compute-call", "Published code function call", "Bind a published Go/Wasm operation to a page using its original owner contract.", ["ComputeCall"], "ComputeExample", {
    tags: ["wasm", "compute", "binding"], uses: ["code", "widget"], widgets: ["compute"], source: "web/packages/app/src/capability.tsx",
    states: ["pending", "error", "unavailable"], snippet: 'import { ComputeCall } from "@platform/app";\n<ComputeCall binding={publishedOperation} bindings={inputs} record={selected} recordType="your.object" />',
    constraints: ["A runtime call needs the actual host, a published owner revision and the current member’s permissions.", "The offline preview shows the binding only and never executes Wasm or reads tenant data."],
  }),
  asset("semantic-selection", "Typed semantic selection", "Choose an authorized object or property from the existing registry without writing internal identifiers.", ["SemanticObjectSelect", "SemanticPropertySelect", "SemanticPropertyTypeSelect"], "SemanticSelectionExample", {
    tags: ["semantic", "object", "property", "binding"], source: "web/packages/app/src/semantic/Selector.tsx", dependencies: ["ui/input"],
    constraints: ["The selector returns typed references. Host validation and original permissions remain authoritative."],
  }),
];

/** Nonvisual public API, attached to the existing public owner rather than visual cards. */
export const api = ["AppEntry", "AssetRef", "Definition", "Me", "Decision", "Host", "HostContext", "useHost", "useRecordArchive", "useReadQuery", "useRead", "useRecordInventory",
  "useDefinitions", "useCapabilities", "useInvokeCapability", "assetKey", "findDefinition", "useOpenRecord", "Dashboard", "AppUI", "defineApp", "PlatformAppCategory", "categoryOf", "SavedView", "newId",
  "runStates", "AgentInfo", "AgentRun", "Citation", "Memory", "Passage", "RunDraft", "RunSignal", "RunStep", "isPageDefinition", "isComposed", "pageDocumentFromSections", "createWidgetRegistry", "createWidgetDefinitions", "WidgetRegistry", "widgetContracts", "widgetContract", "pageUIProfile", "supportsPageUIProfile", "searchInputObjects", "parsePageDecimal", "isPageDecimal", "isPageDecimalDraft", "PageDecimalValue", "pageVariableContract", "pageVariableValues", "pageVariableDiagnostics", "pageLayoutDiagnostics", "PageVariableValue", "WidgetImplementation", "WidgetContract", "WidgetID", "semanticModelView", "semanticPropertyTypes", "assetBindingKey", "SemanticPropertyType", "propertyKey", "relationKey", "PropertyRef", "ReferenceRelationRef", "SemanticRelation", "SemanticModelView"];
