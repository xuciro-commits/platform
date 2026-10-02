import {Select,t} from "@platform/ui";
import {widgetContract} from "@platform/app";
import {variableAccessible} from "../../page-layout";
import type {TableInspectorPorts} from "./TableInspector";

export function AggregateSource({section,document,object,overlay,onChange,widget}:{widget:"chart"|"pivot"}&TableInspectorPorts) {
 const type=widgetContract(widget)!.inputPorts.find(p=>p.bindingField==="collectionVariable")!.type;
 return <label className="grid gap-1 text-xs">{t("Aggregate query set")}<Select value={section.collectionVariable??""} onChange={event=>{
  const v=document.variables?.[event.target.value],target=v?.source?.query?document.queries?.[v.source.query]?.object:v?.source?.object;
  onChange({collectionVariable:event.target.value||undefined,filterVariable:undefined,query:undefined,parentSelection:undefined,relation:undefined,...(target?{object:target.name===object?undefined:target.name}:{})});
 }}><option value="">{t("Use the widget's own aggregate")}</option>{Object.entries(document.variables??{}).filter(([,v])=>variableAccessible(v,undefined,overlay)&&v.type===type&&(v.source?.kind==="plan"||v.mode==="shared"&&!!v.source?.object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>;
}
