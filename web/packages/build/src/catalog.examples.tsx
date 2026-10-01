import "./i18n";
import { CatalogFixture, sampleObject, samplePage } from "@platform/app/catalog/fixtures";
import { PageWorkspaceExample } from "@platform/app/catalog/examples";
import { ComposedPage } from "@platform/app";
import { t } from "@platform/ui";
import { ProcessEditor } from "./process";
import { WorkflowEditor } from "./workflow";
import { pageTemplates, recordHandlingDraft } from "./templates";

export const MasterDetailExample = PageWorkspaceExample;
export function RecordHandlingExample() {
  const draft = recordHandlingDraft(sampleObject, "sample", t("Record handling workspace"), ["title", "state", "quantity"], ["catalog.sample.review"], pageTemplates[0].summary);
  const sections = draft.sections.map((section) => ({ ...section, actions: section.actions?.map((name) => ({ app: "catalog", kind: "action", name })) }));
  return <CatalogFixture><ComposedPage live={false} page={{ ...samplePage.page, layout: "composed", sections }} /></CatalogFixture>;
}

const builderRole = { build: "builder", catalog: "reader" };
const objectReads = {
  "/v1/records/build.object/OBJECT-SAMPLE": { record: {
    id: "OBJECT-SAMPLE", revision: 1, name: "sample", title: "Sample record", state: "draft",
    fields: [{ name: "title", title: "Title", type: "text", required: true }, { name: "quantity", title: "Quantity", type: "integer" }],
    states: [{ name: "draft", title: "Draft", tone: "warning" }, { name: "ready", title: "Ready", tone: "success" }],
    actions: [{ name: "review", title: "Review", from: ["draft"], to: "ready", inputs: [], sets: [], conditions: [], roles: [] }], access: [],
  } },
};
const logicReads = {
  "/v1/records/build.process/FLOW-SAMPLE": { record: {
    id: "FLOW-SAMPLE", revision: 1, name: "sample", title: "Sample logic", object: "", when: "", version: 1, input: {},
    inputSchema: { type: "object", properties: { quantity: { type: "integer" } }, required: ["quantity"] },
    outputSchema: { type: "integer" }, steps: [{ name: "input", title: "Input", kind: "payload", next: "result" },
      { name: "result", title: "Result", kind: "end", value: { source: "input", path: ["quantity"] } }],
  } },
  "/v1/capabilities": [], "/v1/flows": [],
};
export const ObjectStudioExample = () => <CatalogFixture reads={objectReads} roles={builderRole}><ProcessEditor id="OBJECT-SAMPLE" /></CatalogFixture>;
export const LogicStudioExample = () => <CatalogFixture reads={logicReads} roles={builderRole}><WorkflowEditor id="FLOW-SAMPLE" /></CatalogFixture>;
