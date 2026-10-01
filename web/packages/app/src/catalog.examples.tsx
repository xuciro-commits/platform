import "./i18n";
import { useState } from "react";
import { Panel, t } from "@platform/ui";
import { ApplicationPage, ApplicationSessionsProvider, Assistant, ComposedPage, ComputeCall, DashboardView, FlowInstanceView, GeneratedForm, PagePreview, PayloadFields, RecordActions, RecordDetail, Records, RunView, Search, SemanticObjectSelect, SemanticPropertySelect, type Definition, type AssetRef } from "./index";
import { CatalogFixture, sampleActions, sampleDefinitions, sampleObject, samplePage, sampleRecords } from "./catalog.fixtures";

export function GeneratedFormExample() {
  const [values, setValues] = useState<object>();
  return <CatalogFixture><GeneratedForm type={sampleObject.type} record={sampleRecords[0]} submitLabel={t("Try local form")} onCancel={() => setValues(undefined)} onSubmit={setValues} />
    {values && <Panel role="status" className="text-xs"><pre className="overflow-auto">{JSON.stringify(values, null, 2)}</pre></Panel>}</CatalogFixture>;
}
export const RecordsExample = () => <CatalogFixture><Records type={sampleObject.type} /></CatalogFixture>;
export const RecordDetailExample = () => <CatalogFixture><RecordDetail type={sampleObject.type} id="SAMPLE-001" /></CatalogFixture>;
export const FlowInstanceExample = () => <CatalogFixture reads={{
  "/v1/records/flow.instance/FLOW-SAMPLE": { record: { id: "FLOW-SAMPLE", flow: "catalog.review", title: "Sample review", version: 1,
    key: "sample", state: "waiting", dependencies: "fixture-release-v1", release: "fixture-release-v1",
    tokens: [{ id: 1, step: "review", waits: "ask" }], undo: [], trace: [] } },
  "/v1/flows": [{ id: "catalog.review", app: "catalog", title: "Sample review", version: 1, start: [],
    steps: [{ name: "review", title: "Review", kind: "ask", next: [] }] }],
  "/v1/chain/flow.instance/FLOW-SAMPLE": { nodes: [{ ref: "flow.instance/FLOW-SAMPLE", title: "Sample review", kind: "flow", state: "waiting" }], edges: [] },
}}><FlowInstanceView id="FLOW-SAMPLE" /></CatalogFixture>;
export const PageWorkspaceExample = () => <CatalogFixture><PagePreview definition={samplePage} definitions={sampleDefinitions} /></CatalogFixture>;
export function ActionsExample() {
  const [values, setValues] = useState<Record<string, unknown>>({});
  return <CatalogFixture><div className="grid gap-3"><RecordActions type={sampleObject.type} record={sampleRecords[0]!} />
    <PayloadFields fields={sampleActions[0]!.payload} values={values} onChange={setValues} preview /></div></CatalogFixture>;
}
export const DashboardExample = () => <CatalogFixture><DashboardView dashboard={{ id: "catalog-fixture", title: t("Sample dashboard"), charts: [
  { title: t("Records by state"), data: { entity: sampleObject.type }, mark: "bar", encoding: { x: { field: "state", type: "nominal" }, y: { field: "count", type: "quantitative", aggregate: "count" } } },
] }} /></CatalogFixture>;
export const ComposedPageExample = () => <CatalogFixture><ComposedPage live={false} page={{ ...samplePage.page, layout: "composed",
  selections: [{ name: "first", object: samplePage.page.object }, { name: "second", object: samplePage.page.object }], sections: [
  { widget: "filter", title: t("Filter"), fields: ["state"], width: "full" },
  { widget: "table", title: t("First selection"), selection: "first", fields: ["title", "state", "quantity"], width: "half" },
  { widget: "table", title: t("Second selection"), selection: "second", fields: ["title", "state", "quantity"], width: "half" },
  { widget: "detail", title: t("First record"), selection: "first", fields: ["title", "state", "quantity"], width: "half" },
  { widget: "detail", title: t("Second record"), selection: "second", fields: ["title", "state", "quantity"], width: "half" },
  { widget: "actions", title: t("Actions"), selection: "first", actions: samplePage.page.actions, width: "half" },
] }} /></CatalogFixture>;
export const AssistantExample = () => <CatalogFixture><Assistant about="catalog.sample/SAMPLE-001" /></CatalogFixture>;
export const AgentRunExample = () => <CatalogFixture><RunView id="RUN-SAMPLE" /></CatalogFixture>;
export const SearchExample = () => <CatalogFixture><Search initial="order" /></CatalogFixture>;
export const ComputeExample = () => <CatalogFixture><ComputeCall recordType={sampleObject.type} live={false}
  binding={{ ref: { app: "build", kind: "compute", name: "sample" }, sourceVersion: "fixture.compute-1" }} /></CatalogFixture>;

function SemanticChoices() {
  const [object, setObject] = useState<AssetRef>(), [property, setProperty] = useState<string>();
  return <div className="grid gap-3"><SemanticObjectSelect label={t("Object")} value={object?.name} onChange={(ref) => { setObject(ref); setProperty(undefined); }} />
    {object && <SemanticPropertySelect object={object} value={property} label={t("Property")} onChange={(ref) => setProperty(ref?.field)} />}</div>;
}
export const SemanticSelectionExample = () => <CatalogFixture><SemanticChoices /></CatalogFixture>;

export function ApplicationSessionsExample() {
  const object={app:"catalog",kind:"object" as const,name:sampleObject.type};
  const variables = { draft: { scope:"application",type:"string",mode:"state",initial:"" },window:{scope:"application",type:"object-set",mode:"resource",source:{kind:"plan",query:"read"}} };
  const queries={read:{object,limit:2,sort:["id"],search:{variable:"draft"}}};
  const definition: typeof samplePage = { ...samplePage, ref:{app:"catalog",kind:"page",name:"shared"}, page:{...samplePage.page,name:"shared",layout:"composed",sections:[{id:"input",widget:"input",configVersion:1,title:t("Application draft")},{id:"table",widget:"table",configVersion:1,title:t("Shared query window"),collectionVariable:"window",fields:["title","state"]}], document:{formatVersion:2,uiProfile:"platform.page.v2.12",root:"root",nodes:{root:{kind:"rows",children:["input","table"]},input:{kind:"widget",section:"input",valueVariable:"draft"},table:{kind:"widget",section:"table"}},variables:{draft:{scope:"application",type:"string",mode:"shared",writable:true,source:{kind:"application",variable:"draft"}},window:{scope:"application",type:"object-set",mode:"shared",source:{kind:"application",variable:"window",object}}}}} };
  const definitions: Definition[] = [...sampleDefinitions, definition, {ref:{app:"catalog",kind:"app",name:"shared"},source:"code",version:"fixture-1",contractVersion:1,requires:[],application:{name:"shared",title:t("Shared application"),pages:["shared"],uiProfile:"platform.page.v2.12",variables,queries}}];
  return <CatalogFixture definitions={definitions}><ApplicationSessionsProvider><div className="grid gap-3 md:grid-cols-2">{["first","second"].map((view) => <ApplicationPage key={view} pageRef={definition.ref} route={{view,params:{application:"catalog:shared",instance:"example"}}}><ComposedPage page={definition.page} live={false} /></ApplicationPage>)}</div></ApplicationSessionsProvider></CatalogFixture>;
}
