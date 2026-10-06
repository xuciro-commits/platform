import {useState} from "react";
import type {Api} from "@platform/kernel";
import {useHost,useReadQuery} from "@platform/app";
import {Button,Card,Dialog,Input,Select,Tag,t} from "@platform/ui";
import {Canvas} from "./canvas";
import {autoLayout,live,MODEL,ORGANIZATION,PLACEMENT,today,type Model} from "./model";

export function EnterpriseExamples({model,admin,onClose,onApplied}:{model:Model;admin:boolean;onClose:()=>void;onApplied:(view:string)=>void}) {
 const host=useHost();
 const mayApply=admin&&host.can("enterprise.model.apply-example");
 const examples=useReadQuery<Api.ModelExample[]>("/v1/enterprise-examples");
 const [chosen,setChosen]=useState<string>();
 const [name,setName]=useState<string>();
 const [parent,setParent]=useState("");
 const [selected,setSelected]=useState<string>();
 const [viewId,setViewId]=useState<string>();
 const [applying,setApplying]=useState(false);
 const example=examples.data?.find(e=>e.id===chosen)??examples.data?.[0];
 const view=example?.model.views.find(v=>v.id===viewId)??example?.model.views[0];
 const day=today();
 const kind=example?.model.kinds.find(k=>k.kind==="management")?.id??"";
 const shown=view?.elements??[];
 const positions=example?autoLayout(example.model,shown,kind,day,view?.layout):{};
 const orgs=model.elements.filter(e=>e.stereotype===ORGANIZATION&&!e.owner&&live(e,day));
 const selectedElement=example?.model.elements.find(e=>e.id===selected);
 const apply=async()=>{
  if(!example||applying)return;
  const prefix=`example-${crypto.randomUUID()}`;
  setApplying(true);
  try {
   if(await host.decide("enterprise.model.apply-example",{type:MODEL,id:prefix},{example:example.id,name:name??example.model.elements[0]?.name,parent:parent||undefined})) {
    onApplied(`${prefix}-${example.model.views[0]!.id}`);
    onClose();
   }
  } finally {setApplying(false);}
 };
 return <Dialog open wide width={1100} title={t("Enterprise examples")} onOpenChange={open=>{if(!open&&!applying)onClose();}}>
  <p className="mb-3 text-sm text-muted">{t("Preview an example, then add an editable copy to this tenant's enterprise model. Existing elements and views are preserved.")}</p>
  {examples.error&&<p role="alert" className="text-sm text-[var(--tone-danger)]">{String(examples.error)}</p>}
  <div className="grid gap-2 md:grid-cols-4">
   {examples.data?.map(e=><Card key={e.id} className="grid content-start gap-2 p-3">
    <Button variant={example?.id===e.id?"default":"ghost"} aria-pressed={example?.id===e.id} onClick={()=>{setChosen(e.id);setName(undefined);setSelected(undefined);setViewId(undefined);}}>{t(e.title)}</Button>
    <p className="text-xs text-muted">{t(e.description)}</p>
    <Tag label={e.model.scale??""}/>
   </Card>)}
  </div>
  {example&&<>
   <div className="my-3 flex flex-wrap items-center gap-3">
    <Select aria-label={t("Preview view")} value={view?.id??""} onChange={e=>{setViewId(e.target.value);setSelected(undefined);}}>{example.model.views.map(v=><option key={v.id} value={v.id}>{v.name} · {v.grid}</option>)}</Select>
    <span className="text-xs text-muted">{t("{elements} elements · {relationships} relationships · {views} views",{elements:example.model.elements.length,relationships:example.model.relationships.length,views:example.model.views.length})}</span>
   </div>
   <div className="h-[400px] overflow-auto rounded-md border border-border">
    <Canvas elements={example.model.elements.filter(e=>shown.includes(e.id))} relationships={example.model.relationships.filter(r=>shown.includes(r.source)&&shown.includes(r.target)&&(r.stereotype!==PLACEMENT||r.kind===kind))} positions={positions} selected={selected} linking={false} readOnly
     onSelect={setSelected} onMove={()=>{}} onDrop={()=>{}} onLink={()=>{}} label={r=>r.role||r.relation||r.stereotype}/>
   </div>
   {selectedElement&&<p className="mt-2 text-sm">{selectedElement.name} · {selectedElement.stereotype} · {selectedElement.kind}</p>}
   <div className="mt-4 grid gap-3 md:grid-cols-2">
    <label className="grid gap-1 text-xs text-muted">{t("Name for the applied example")}<Input value={name??example.model.elements[0]?.name??""} disabled={!mayApply||applying} onChange={e=>setName(e.target.value)}/></label>
    <label className="grid gap-1 text-xs text-muted">{t("Attach to an existing organisation")}<Select value={parent} disabled={!mayApply||applying} onChange={e=>setParent(e.target.value)}><option value="">{t("Add as a separate enterprise root")}</option>{orgs.map(e=><option key={e.id} value={e.id}>{e.name}</option>)}</Select></label>
   </div>
   <div className="mt-4 flex items-center justify-end gap-2">
    {!mayApply&&<p className="text-xs text-muted">{t("An enterprise administrator applies examples.")}</p>}
    <Button variant="ghost" disabled={applying} onClick={onClose}>{t("Cancel")}</Button>
    <Button disabled={!mayApply||applying||!(name??example.model.elements[0]?.name)?.trim()} onClick={()=>void apply()}>{applying?t("Applying…"):t("Apply example")}</Button>
   </div>
  </>}
 </Dialog>;
}
