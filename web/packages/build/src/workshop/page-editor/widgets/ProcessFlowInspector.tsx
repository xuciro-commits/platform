import {useHost} from "@platform/app";
import {Select,t,type EntityInfo} from "@platform/ui";
import type {Api} from "@platform/kernel";
import type {AuthoringSection} from "../draft";

type Binding = Api.PageProcessFlow;
type Option = {value: string; title: string};

/** Binds a process flow to declared fields only: the record's routing, its pinned version and step, and the steps' order and version (ADR-0098 D4). */
export function ProcessFlowInspector({section,info,onChange}:{section:AuthoringSection;info?:EntityInfo;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const {entities}=useHost();
 const pf:Partial<Binding>=section.processFlow??{};
 const set=(patch:Partial<Binding>)=>onChange({processFlow:{...pf,...patch} as Binding});
 const fields=info?.fields??[];
 const options=(list:{name:string;title:string}[]):Option[]=>list.map(f=>({value:f.name,title:f.title||f.name}));
 const routing=fields.find(f=>f.name===pf.routingField&&f.type==="reference"&&f.ref);
 const step=entities.find(e=>e.type===pf.stepObject);
 const stepOptions=routing?entities.filter(e=>e.type!==info?.type&&e.fields.some(f=>f.type==="reference"&&f.ref===routing.ref)):[];
 const pick=(label:string,value:string|undefined,list:Option[],onPick:(v:string)=>void,optional=false)=><label className="grid gap-1 text-xs">{t(label)}<Select value={value??""} onChange={e=>onPick(e.target.value)}><option value="">{optional?t("None"):t("Choose a field")}</option>{list.map(o=><option key={o.value} value={o.value}>{o.title}</option>)}</Select></label>;
 if(!info)return <p role="alert" className="text-xs text-danger">{t("Choose an object to read its steps.")}</p>;
 return <div className="grid gap-2">
  {pick("Routing reference",pf.routingField,options(fields.filter(f=>f.type==="reference"&&f.ref)),v=>set({routingField:v,stepObject:undefined,stepVersionField:undefined,stepField:undefined,orderField:undefined}))}
  {pick("Pinned version field",pf.versionField,options(fields.filter(f=>f.type==="integer")),v=>set({versionField:v}))}
  {pick("Current step reference",pf.stepField,options(fields.filter(f=>f.type==="reference"&&f.ref)),v=>set({stepField:v}))}
  <label className="grid gap-1 text-xs">{t("Step object")}<Select value={pf.stepObject??""} onChange={e=>set({stepObject:e.target.value||undefined,stepVersionField:undefined,orderField:undefined,titleField:undefined,laneField:undefined})}><option value="">{t("Choose a field")}</option>{stepOptions.map(e=><option key={e.type} value={e.type}>{e.title}</option>)}</Select></label>
  {step&&pick("Step version field",pf.stepVersionField,options(step.fields.filter(f=>f.type==="integer")),v=>set({stepVersionField:v}))}
  {step&&pick("Step order",pf.orderField,options(step.fields.filter(f=>f.type==="integer"||f.type==="decimal")),v=>set({orderField:v}))}
  {step&&pick("Step title",pf.titleField,options(step.fields),v=>set({titleField:v||undefined}),true)}
  {step&&pick("Lane",pf.laneField,options(step.fields.filter(f=>f.type==="reference")),v=>set({laneField:v||undefined}),true)}
  {pick("Notes",pf.notesField,options(fields.filter(f=>f.type==="lines")),v=>set({notesField:v||undefined}),true)}
  <p className="text-xs text-muted">{t("Steps show in the order of their order field, only for the version the record pins. A note belongs to the step its step column names.")}</p>
 </div>;
}
