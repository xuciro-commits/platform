import {PageEditor} from "./editor";
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

import sampleModule from "./module-import/sample.workshop.json";
import {compileWorkshopModule} from "./module-import/compile";
import {pageUIProfile} from "@platform/app";
import {Panel} from "@platform/ui";
export function WorkshopImportExample(){
 const report=compileWorkshopModule(JSON.stringify(sampleModule),"page",{objects:{Asset:sampleObject.type},fields:{Asset:{name:"title"}},actions:{finishAsset:"catalog.sample.review"},queries:{}},{object:sampleObject.type,profile:pageUIProfile,entities:[sampleObject],actions:[{schema:"catalog.sample.review",target:sampleObject.type}]});
 return <Panel><p>{t("All {count} source widget types have an owner and migration status.",{count:92})}</p><p>{t("Mapped {count} widgets into the original V2 page format.",{count:report.draft?.sections.length??0})}</p><pre className="overflow-auto text-xs">{JSON.stringify({diagnostics:report.diagnostics,sections:report.draft?.sections},null,2)}</pre></Panel>;
}

const canvasReads={
 "/v1/records/build.page/PAGE-CANVAS":{record:{id:"PAGE-CANVAS",revision:1,name:"canvas",title:"Canvas design",object:sampleObject.type,state:"draft",sections:[
  {id:"heading",widget:"heading",configVersion:1,title:"Heading",text:"Workspace",headingLevel:"h2"},
  {id:"left",widget:"text",configVersion:1,title:"Left content",text:"Edit, move and resize this region."},
  {id:"right",widget:"text",configVersion:1,title:"Right content",text:"Use the attached toolbar or layout tree."}],
  document:{formatVersion:2,uiProfile:pageUIProfile,root:"root",nodes:{root:{kind:"rows",children:["heading","columns"]},heading:{kind:"widget",section:"heading"},columns:{kind:"columns",children:["left","right"]},left:{kind:"widget",section:"left"},right:{kind:"widget",section:"right"}}}}},
 "/v1/records/build.function?limit=500&offset=0":{records:[],total:0},
 "/v1/records/build.code?limit=500&offset=0":{records:[],total:0},
};
export const PageStudioExample=()=> <CatalogFixture reads={canvasReads} roles={builderRole}><PageEditor id="PAGE-CANVAS"/></CatalogFixture>;
