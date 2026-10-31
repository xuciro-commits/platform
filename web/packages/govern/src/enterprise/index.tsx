import { useState } from "react";
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, DataTable, Sheet, Disclosure, Form, Input, Notice, PageHeader, Panel, PropertyList, Select, Tag, t, type ColumnDef } from "@platform/ui";
import { LifecycleDialog, MoveElementDialog, Usage, type LifecycleOperation } from "./lifecycle";
import { ELEMENT, RELATIONSHIP, ORGANIZATION, POST, PERSON, MEMBERSHIP, FILLS_POST, RESPONSIBLE_FOR, today, live, elementStatus, allowedRelationships, sourceRelationship, type Element, type Model, type Metamodel } from "./model";

const types = [ORGANIZATION, POST, PERSON];
const labels: Record<string,string> = { [ORGANIZATION]:"Organisation", [POST]:"Post", [PERSON]:"Person", [MEMBERSHIP]:"Membership", [FILLS_POST]:"Fills post", [RESPONSIBLE_FOR]:"Responsible for" };

export function Enterprise({ element }: { element?: string; section?: string }) {
 const host = useHost();
 const query = useRead<Model>("/v1/enterprise");
 const meta = useRead<Metamodel>("/v1/enterprise-metamodel");
 const [selected,setSelected] = useState(element ?? "");
 const [day,setDay] = useState(today());
 const [operation,setOperation] = useState<LifecycleOperation | "move">();
 const [draft,setDraft] = useState<{id:string;stereotype:string;name:string;kind:string;shortName:string}>();
 const [relationship,setRelationship] = useState<{id:string;stereotype:string;source:string;target:string;role:string}>();
 const [structure,setStructure] = useState("");
 const [newStructure,setNewStructure] = useState("");
 const model=query.data;
 if(query.isError) return <Panel role="alert"><Notice tone="danger" message={t("Organisation could not be loaded.")} /><Button onClick={()=>void query.refetch()}>{t("Try again")}</Button></Panel>;
 if(!model) return <p role="status">{t("Loading…")}</p>;
 const chosen=model.elements.find(row=>row.id===selected);
 const directory=model.elements.filter(row=>types.includes(row.stereotype));
 const historical=model.elements.filter(row=>!types.includes(row.stereotype));
 const kind=structure || model.kinds[0]?.id || "";
 const columns:ColumnDef<Element,any>[]=[
  {accessorKey:"name",header:t("Name"),cell:({row:{original:row}})=><Button variant="ghost" onClick={()=>setSelected(row.id)}>{row.name}</Button>},
  {accessorKey:"stereotype",header:t("Type"),cell:({row:{original:row}})=>t(labels[row.stereotype]??row.stereotype)},
  {accessorKey:"kind",header:t("Kind")},
  {id:"status",header:t("Status"),cell:({row:{original:row}})=><Tag label={t(elementStatus(row,day))} />},
 ];
 const names=new Map(model.elements.map(row=>[row.id,row.name]));
 const assignmentSource=relationship?.source.startsWith("member:")?PERSON:model.elements.find(row=>row.id===relationship?.source)?.stereotype;
 const assignmentTargets=relationship&&assignmentSource&&meta.data?directory.filter(row=>live(row,day)&&allowedRelationships(meta.data,assignmentSource,row.stereotype).includes(relationship.stereotype)):[];
 return <>
  <PageHeader title={t("Organisation")} description={t("Units, posts, people and assignments. Historical enterprise content is read-only.")} actions={host.can("enterprise.element.add")&&<Button onClick={()=>setDraft({id:crypto.randomUUID(),stereotype:ORGANIZATION,name:"",kind:"",shortName:""})}>{t("Add element")}</Button>} />
  <div className="flex flex-wrap gap-3 mb-3"><label className="grid gap-1 text-sm">{t("Effective date")}<Input type="date" value={day} onChange={e=>setDay(e.target.value)}/></label><label className="grid gap-1 text-sm">{t("Structure")}<Select value={kind} onChange={e=>setStructure(e.target.value)}>{model.kinds.map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</Select></label></div>
  <div className="grid gap-4 lg:grid-cols-[minmax(0,3fr)_minmax(280px,2fr)]">
   <Panel title={t("Directory")}><DataTable data={directory} columns={columns} getRowId={row=>row.id} height={440}/></Panel>
   <Panel title={chosen?.name??t("Choose an element")}>
    {chosen&&<div className="grid gap-3">
     <PropertyList items={[[t("ID"),chosen.id],[t("Type"),t(labels[chosen.stereotype]??chosen.stereotype)],[t("Kind"),chosen.kind??""],[t("Status"),t(elementStatus(chosen,day))]]}/>
     {types.includes(chosen.stereotype)&&!chosen.owner&&<div className="flex flex-wrap gap-2">
      {host.can("enterprise.element.edit")&&<Button onClick={()=>setDraft({id:chosen.id,stereotype:chosen.stereotype,name:chosen.name,kind:chosen.kind??"",shortName:chosen.shortName??""})}>{t("Edit")}</Button>}
      {chosen.stereotype===ORGANIZATION&&meta.data&&kind&&host.can("enterprise.relationship.add")&&<Button onClick={()=>setOperation("move")}>{t("Move to")}</Button>}
      {(live(chosen,day)?["close","delete"]:["reopen","reinstate"]).map(action=>host.can(`enterprise.element.${action}`)&&<Button key={action} onClick={()=>setOperation(action as LifecycleOperation)}>{t(({close:"Close element",delete:"Delete unused element",reopen:"Reopen element",reinstate:"Reinstate element"}as Record<string,string>)[action]??action)}</Button>)}
     </div>}
     <Usage element={chosen.id}/>
     {chosen.properties&&<Disclosure summary={t("Historical properties")}><pre className="whitespace-pre-wrap text-xs">{JSON.stringify(chosen.properties,null,2)}</pre></Disclosure>}
    </div>}
   </Panel>
  </div>
  <Panel className="mt-4" title={t("Assignments")} actions={host.can("enterprise.relationship.add")&&<Button disabled={!meta.data} onClick={()=>setRelationship({id:crypto.randomUUID(),stereotype:MEMBERSHIP,source:"",target:"",role:"employee"})}>{t("Add assignment")}</Button>}>
   {meta.isError&&<><Notice tone="warning" message={t("Assignment rules could not be loaded.")}/><Button onClick={()=>void meta.refetch()}>{t("Try again")}</Button></>}
   <DataTable data={model.relationships.filter(row=>[MEMBERSHIP,FILLS_POST,RESPONSIBLE_FOR].includes(row.stereotype))} getRowId={row=>row.id} height={240} columns={[
    {accessorKey:"source",header:t("Source"),cell:({row:{original:row}})=>names.get(row.source)??row.source},
    {accessorKey:"target",header:t("Target"),cell:({row:{original:row}})=>names.get(row.target)??row.target},
    {accessorKey:"role",header:t("Role")},
    {id:"status",header:t("Status"),cell:({row:{original:row}})=>t(live(row,day)?"Active":"Inactive")},
    {id:"end",header:t("Actions"),cell:({row:{original:row}})=>host.can("enterprise.relationship.end")&&!sourceRelationship(row)&&[row.source,row.target].every(id=>id.startsWith("member:")||directory.some(element=>element.id===id))&&live(row,day)&&<Button size="sm" onClick={()=>void host.decide("enterprise.relationship.end",{type:RELATIONSHIP,id:row.id},{until:day})}>{t("End relationship")}</Button>},
   ]}/>
  </Panel>
  {host.can("enterprise.kind.add")&&<Panel className="mt-4" title={t("Structures")}><Form className="flex gap-2" onSubmit={async()=>{if(await host.decide("enterprise.kind.add",{type:"enterprise.model",id:crypto.randomUUID()},{name:newStructure,kind:"custom"}))setNewStructure("");}}><Input aria-label={t("Name")} value={newStructure} onChange={e=>setNewStructure(e.target.value)} required/><Button type="submit">{t("Add structure")}</Button></Form></Panel>}
  {(historical.length>0||model.views.length>0)&&<Panel className="mt-4" title={t("Historical enterprise content")}><DataTable data={historical} columns={columns} getRowId={row=>row.id} height={200}/>{model.views.map(view=><Disclosure key={view.id} summary={view.name}><pre className="whitespace-pre-wrap text-xs">{JSON.stringify(view,null,2)}</pre></Disclosure>)}</Panel>}
  {draft&&<Sheet isolated open title={t(model.elements.some(row=>row.id===draft.id)?"Edit element":"Add element")} onOpenChange={open=>!open&&setDraft(undefined)}><Form className="grid gap-3" onSubmit={async()=>{const editing=model.elements.some(row=>row.id===draft.id);if(await host.decide(editing?"enterprise.element.edit":"enterprise.element.add",{type:ELEMENT,id:draft.id},{name:draft.name,kind:draft.kind,shortName:draft.shortName,...(!editing?{stereotype:draft.stereotype,from:day}:{})}))setDraft(undefined);}}>
   <label className="grid gap-1">{t("Type")}<Select value={draft.stereotype} disabled={model.elements.some(row=>row.id===draft.id)} onChange={e=>setDraft({...draft,stereotype:e.target.value})}>{types.map(type=><option key={type} value={type}>{t(labels[type]??type)}</option>)}</Select></label>
   <label className="grid gap-1">{t("Name")}<Input value={draft.name} onChange={e=>setDraft({...draft,name:e.target.value})} required/></label>
   <label className="grid gap-1">{t("Kind")}<Input value={draft.kind} onChange={e=>setDraft({...draft,kind:e.target.value})}/></label>
   <label className="grid gap-1">{t("Short name")}<Input value={draft.shortName} onChange={e=>setDraft({...draft,shortName:e.target.value})}/></label>
   <Button type="submit">{t("Save")}</Button>
  </Form></Sheet>}
  {relationship&&<Sheet isolated open title={t("Add assignment")} onOpenChange={open=>!open&&setRelationship(undefined)}><Form className="grid gap-3" onSubmit={async()=>{if(await host.decide("enterprise.relationship.add",{type:RELATIONSHIP,id:relationship.id},{stereotype:relationship.stereotype,source:relationship.source,target:relationship.target,...(relationship.stereotype===MEMBERSHIP?{role:relationship.role}:{}),from:day}))setRelationship(undefined);}}>
   <label>{t("Type")}<Select value={relationship.stereotype} onChange={e=>setRelationship({...relationship,stereotype:e.target.value,target:""})}>{[MEMBERSHIP,FILLS_POST,RESPONSIBLE_FOR].map(type=><option key={type} value={type}>{t(labels[type]??type)}</option>)}</Select></label>
   <label>{t("Source")}<Input value={relationship.source} onChange={e=>setRelationship({...relationship,source:e.target.value,target:""})} placeholder={t("Element ID or member ID with member: prefix")} required/></label>
   <label>{t("Target")}<Select value={relationship.target} onChange={e=>setRelationship({...relationship,target:e.target.value})} required><option value="">{t("Choose an element")}</option>{assignmentTargets.map(row=><option key={row.id} value={row.id}>{row.name}</option>)}</Select></label>
   {relationship.stereotype===MEMBERSHIP&&<label>{t("Role")}<Input value={relationship.role} onChange={e=>setRelationship({...relationship,role:e.target.value})} required/></label>}
   <Button type="submit">{t("Save")}</Button>
  </Form></Sheet>}
  {chosen&&operation&&operation!=="move"&&<LifecycleDialog element={chosen} operation={operation} day={day} onClose={()=>setOperation(undefined)}/>}
  {chosen&&operation==="move"&&meta.data&&<MoveElementDialog element={chosen} model={model} meta={meta.data} kind={kind} day={day} onClose={()=>setOperation(undefined)}/>}
 </>;
}
