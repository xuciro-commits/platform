import "./i18n";
import { CatalogFixture, sampleObject, samplePage } from "@platform/app/catalog/fixtures";
import { PageWorkspaceExample } from "@platform/app/catalog/examples";
import { QueryEditor } from "./query";
import { ApplicationEditor } from "./application";
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

const applicationDefinitions = [{ ...samplePage, ref: { app: "build", kind: "page" as const, name: "samples" } },
  { ref: { app: "catalog", kind: "object" as const, name: sampleObject.type }, source: "code", version: "fixture-v1", contractVersion: 1, requires: [], entity: sampleObject }];
const applicationReads = {
  "/v1/records/build.app/APP-SAMPLE": { record: { id: "APP-SAMPLE", revision: 1, name: "sample", title: "Sample application", state: "draft", icon: "boxes", pages: ["samples"], resources: [applicationDefinitions[1]!.ref], groups: [] } },
  "/v1/records/build.process?limit=500&offset=0": { records: [], total: 0 },
};
export const ApplicationStudioExample = () => <CatalogFixture reads={applicationReads} roles={builderRole} definitions={applicationDefinitions}><ApplicationEditor id="APP-SAMPLE" /></CatalogFixture>;

const querySource = {...sampleObject,app:"build",type:"build.sample"};
const queryDeclaration = {name:"shared",title:"Shared query",description:"Read ready records.",object:querySource.type,domain:[["state","=","ready"]],sort:["id"],limit:20};
const queryDefinitions = [
 {ref:{app:"build",kind:"object" as const,name:querySource.type},source:"tenant",version:"1",contractVersion:1,requires:[],entity:querySource},
 {ref:{app:"build",kind:"query" as const,name:"shared"},source:"tenant",version:"1.query-1",contractVersion:1,requires:[],query:queryDeclaration,queryVersions:{"1.query-1":queryDeclaration}},
];
const queryReads = {"/v1/records/build.query/QUERY-SAMPLE":{record:{...queryDeclaration,id:"QUERY-SAMPLE",revision:1,state:"published",version:1,published:JSON.stringify(queryDeclaration)}}};
export const QueryStudioExample = () => <CatalogFixture reads={queryReads} roles={builderRole} definitions={queryDefinitions}><QueryEditor id="QUERY-SAMPLE" /></CatalogFixture>;
