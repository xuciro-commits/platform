import type { CatalogEntry } from "@platform/catalog";
import { pageTemplates } from "./templates";

const template = pageTemplates[0];
export const entries: CatalogEntry[] = [
  {
    id: "scenario/query-studio", owner: "@pkg/build", name: "Reusable query workbench", summary: "Author fixed record conditions and retain explicit query versions for shared page plans.",
    layer: 5, authority: "example", maturity: "recommended", scope: "platform", uses: ["reference"], tags: ["query", "version", "resources", "studio"],
    source: "web/packages/build/src/query.tsx", example: "QueryStudioExample", dependencies: ["ui/panels", "app/record-actions"],
    constraints: ["The local example uses synthetic query and object descriptors. Save and publication cannot reach a host.", "Queries use the original record reader and member permissions. Pages explicitly choose a retained source version; later drafts and publications do not replace it."],
  },

  {
    id: "pattern/master-detail", owner: "@pkg/build", name: "Master-detail interaction", layer: 4, authority: "recommendation", maturity: "recommended", scope: "platform",
    summary: "Keep selection and detail together using the existing record workspace or controlled table/detail sections.",
    uses: ["code", "widget", "reference"], widgets: ["table", "detail"], tags: ["list", "selection", "inspector", "record"], source: "web/packages/app/src/pages.tsx", example: "MasterDetailExample",
    dependencies: ["ui/record-workspace", "app/page-workspace", "app/composed-page"],
    constraints: ["List selection is shared by details and actions. The object contract and current member’s read scope remain authoritative.", "This pattern recommends a composition; it does not grant widget eligibility or permissions."],
    snippet: 'import { PageWorkspace } from "@platform/app";\n<PageWorkspace definition={installedListDetailPage} />',
  },
  {
    id: "scenario/record-handling", owner: "@pkg/build", name: template.title, summary: template.summary, layer: 5, authority: "example", maturity: "recommended", scope: "platform",
    uses: ["template", "reference"], tags: ["record", "actions", "history", "tasks", "studio"], source: "web/packages/build/src/templates.ts", template: template.id, example: "RecordHandlingExample",
    dependencies: ["pattern/master-detail", "app/composed-page", "app/record-actions", "app/record-detail"], widgets: ["filter", "table", "detail", "actions", "timeline", "tasks"],
    constraints: ["Studio requires an object and visible fields; actions are chosen from the current member’s original action catalog.", "The output is a build.page draft. It never publishes, generates tenant TSX or bypasses release validation.", "Template identity and revision are recorded in the editable page description as informative provenance."],
  },
  {
    id: "scenario/object-studio", owner: "@pkg/build", name: "Object design workbench", summary: "Inspect and edit fields, lifecycle states, transitions and access through the existing object designer.",
    layer: 5, authority: "example", maturity: "recommended", scope: "platform", uses: ["reference"], tags: ["object", "lifecycle", "fields", "access", "studio"],
    source: "web/packages/build/src/process.tsx", example: "ObjectStudioExample", dependencies: ["ui/block-canvas", "app/record-actions"],
    constraints: ["This is the existing ProcessEditor with synthetic local data. Save and publish are refused in Catalog.", "A tenant object must be created, tested and published in the connected Application Studio.", "Action conditions compare declared record paths or inputs with a fixed value or another compatible field; related reads and approval retries use the original action and current requester permissions."],
  },
  {
    id: "scenario/application-studio", owner: "@pkg/build", name: "Application release workbench", summary: "Organize navigation and shared published objects, workflows and functions with the original application editor.",
    layer: 5, authority: "example", maturity: "recommended", scope: "platform", uses: ["reference"], tags: ["application", "resources", "release", "navigation", "studio"],
    source: "web/packages/build/src/application.tsx", example: "ApplicationStudioExample", dependencies: ["ui/panels", "app/record-actions"],
    constraints: ["The local example uses synthetic application membership. Save, release and navigation actions cannot reach a host.", "Application resources refer to original owners; membership does not grant access. Release review freezes published dependencies using the existing candidate path."],
  },
  {
    id: "scenario/logic-studio", owner: "@pkg/build", name: "Logic design workbench", summary: "Compose typed platform blocks with the existing native workflow editor and shared canvas.",
    layer: 5, authority: "example", maturity: "recommended", scope: "platform", uses: ["reference"], tags: ["workflow", "logic", "block", "canvas", "studio"],
    source: "web/packages/build/src/workflow.tsx", example: "LogicStudioExample", dependencies: ["ui/block-canvas", "app/compute-call", "app/agent-run"],
    constraints: ["The local example demonstrates a payload and result flow in the original WorkflowEditor; it does not execute a process.", "Installed blocks, versions, permissions, testing and activation come from the connected owner APIs."],
  },
];
export const api = ["default"];
