import type { Api } from "@platform/kernel";
import { useHost } from "@platform/app";
import { ContextHelp, Button, Input, InputDraftProvider, Panel, Select, Textarea, t, type EntityInfo } from "@platform/ui";
import { nameOf, type Process } from "./object-model";

type Source = Pick<Api.RecordSet, "from" | "direction" | "object" | "binding"> & {title:string};

// The object draft owns these declarations, buffers and publication. This
// editor selects original declarations and never evaluates a business rule.
export function RecordSetsEditor({process,parent,entities,onChange}:{process:Process;parent:string;entities:EntityInfo[];onChange:(next:Process)=>void}) {
  const {definitions}=useHost();
  const sets=process.sets??[];
  const sources:Source[]=[];
  for(const field of process.fields) if(field.type==="reference"&&field.ref) sources.push({from:field.name,direction:"reference",object:field.ref,title:`${field.title||field.name} → ${field.ref}`});
  for(const entity of entities) for(const field of entity.fields) if(field.type==="reference"&&field.ref===parent&&field.inverse) sources.push({from:field.inverse,direction:"inverse",object:entity.type,title:`${entity.title} · ${field.inverse}`});
  for(const definition of definitions) {
    const binding={ref:definition.ref,sourceVersion:definition.version};
    if(definition.linkType) {
      const link=definition.linkType;
      if(link.parent.name===parent) sources.push({from:link.name,direction:"forward",object:link.child.name,binding,title:`${link.title} → ${link.child.name}`});
      if(link.child.name===parent) sources.push({from:link.name,direction:"reverse",object:link.parent.name,binding,title:`${link.title} ← ${link.parent.name}`});
    }
    if(definition.query) {
      const query=definition.query;
      const target=entities.find(entity=>entity.type===query.object);
      if(!query.by||target?.fields.some(field=>field.name===query.by&&field.ref===parent)) sources.push({from:query.name,object:query.object,binding,title:query.title});
    }
  }
  const identity=(source:Source|Api.RecordSet)=>JSON.stringify([source.from,source.direction??"",source.object,source.binding??null]);
  const patch=(at:number,values:Partial<Api.RecordSet>)=>onChange({...process,sets:sets.map((set,index)=>index===at?{...set,...values}:set)});
  return <div className="grid max-w-4xl content-start gap-3 p-4">
    <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Record sets")}</h2>
      <Button size="sm" disabled={!sources.length||sets.length>=64} onClick={()=>{const {title,...source}=sources[0]!;onChange({...process,sets:[...sets,{...source,name:nameOf("set",sets.map(set=>set.name)),order:[{expr:{op:"",kind:"text",ref:"id"}}],limit:1000}]});}}>{t("Add a record set")}</Button>
    </div>
    <ContextHelp title={t("Record sets")}><p className="text-sm text-muted">{t("Declare a bounded, ordered collection once and use it in conditions, calculations, constraints and transaction iteration.")}</p></ContextHelp>
    {sets.map((set,at)=><InputDraftProvider key={at} scope={JSON.stringify({kind:"recordSet",at})}><Panel title={set.name}>
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="grid gap-1 text-xs">{t("Set name")}<Input value={set.name} onChange={event=>patch(at,{name:event.target.value})}/></label>
        <label className="grid gap-1 text-xs">{t("Declared source")}<Select value={identity(set)} onChange={event=>{const source=sources.find(source=>identity(source)===event.target.value);if(source){const {title,...values}=source;patch(at,{...values,binding:values.binding,direction:values.direction,path:undefined,where:undefined});}}}>
          {!sources.some(source=>identity(source)===identity(set))&&<option value={identity(set)}>{`${set.from} · ${set.binding?.sourceVersion??set.object}`}</option>}
          {sources.map(source=><option key={identity(source)} value={identity(source)}>{source.title}</option>)}
        </Select></label>
        <label className="grid gap-1 text-xs">{t("Record limit")}<Input type="number" step={1} min={1} max={10000} value={set.limit} onChange={event=>patch(at,{limit:Number(event.target.value)})}/></label>
        <label className="grid gap-1 text-xs">{t("Filter reference path")}<Input value={set.path?.join(".")??""} onChange={event=>patch(at,{path:event.target.value?event.target.value.split("."):undefined})}/></label>
        <label className="grid gap-1 text-xs sm:col-span-2">{t("Element predicates · JSON")}<Textarea aria-label={t("Element predicates · JSON")} parse="json" rows={5} value={set.where?JSON.stringify(set.where,null,2):""} onChange={event=>patch(at,{where:event.target.value.trim()?JSON.parse(event.target.value):undefined})}/></label>
        <label className="grid gap-1 text-xs sm:col-span-2">{t("Stable order · JSON")}<Textarea aria-label={t("Stable order · JSON")} parse="json" rows={5} value={JSON.stringify(set.order,null,2)} onChange={event=>patch(at,{order:JSON.parse(event.target.value)})}/></label>
        <Button size="sm" variant="ghost" onClick={()=>onChange({...process,sets:sets.filter((_,index)=>index!==at)})}>{t("Remove record set")}</Button>
      </div>
    </Panel></InputDraftProvider>)}
    <label className="grid gap-1 text-xs">{t("Collection rules · JSON")}<Textarea aria-label={t("Collection rules · JSON")} parse="json" rows={6} value={process.reads?JSON.stringify(process.reads,null,2):""} onChange={event=>onChange({...process,reads:event.target.value.trim()?JSON.parse(event.target.value):undefined})}/></label>
  </div>;
}
