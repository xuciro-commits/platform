import type { Api } from "@platform/kernel";
import { useHost } from "@platform/app";
import { ContextHelp, Button, Checkbox, Input, InputDraftProvider, Panel, Select, t, type EntityInfo } from "@platform/ui";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../editor/ExpressionEditor";
import { authoringObject, useAuthoringModel } from "./authoring-model";
import { RuleCollectionsEditor } from "./RuleCollectionsEditor";
import { nameOf, type Process } from "./object-model";

const kindOf = (type: string) => ({ longtext: "text", number: "decimal", string: "text" } as Record<string, string>)[type] ?? type;

type Source = Pick<Api.RecordSet, "from" | "direction" | "object" | "binding"> & {title:string};

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
  const model=useAuthoringModel();
  const subject=authoringObject({...process,name:parent.slice("build.".length),title:model.entity(parent)?.title??parent});
  const lookup=(type:string)=>type===parent?subject:entities.find(entity=>entity.type===type)??model.entity(type);
  const choicesOf=(object:string):ExpressionChoice[]=>[{value:"id",label:t("Record ID"),type:"text"},...(lookup(object)?.fields??[]).map(field=>({value:field.name,label:field.title,type:kindOf(field.type),ref:field.ref}))];
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
        <fieldset className="grid gap-2 sm:col-span-2"><legend className="text-xs font-medium">{t("Element predicates")}</legend>
          {(set.where??[]).map((predicate,index)=><div key={index} className="grid gap-2 rounded border border-border p-2">
            <ExpressionEditor draftKey={`set:${at}:where:${index}`} value={predicate} choices={choicesOf(set.object)} lookup={lookup} onChange={next=>patch(at,{where:set.where!.map((entry,i)=>i===index?next:entry)})}/>
            <Button size="sm" variant="ghost" className="w-fit" onClick={()=>patch(at,{where:set.where!.length>1?set.where!.filter((_,i)=>i!==index):undefined})}>{t("Remove element predicate")}</Button>
          </div>)}
          <Button size="sm" className="w-fit" onClick={()=>patch(at,{where:[...(set.where??[]),literalExpression("boolean")]})}>{t("Add element predicate")}</Button>
        </fieldset>
        <fieldset className="grid gap-2 sm:col-span-2"><legend className="text-xs font-medium">{t("Stable order")}</legend>
          {set.order.map((sort,index)=>{
            const choices=choicesOf(set.object),simple=sort.expr.op===""&&sort.expr.ref!==undefined&&!sort.expr.lit,change=(next:Partial<Api.RecordSetSort>)=>patch(at,{order:set.order.map((entry,i)=>i===index?{...entry,...next}:entry)});
            return <div key={index} className="grid gap-2 rounded border border-border p-2">
              {simple?<Select aria-label={t("Sort by")} value={sort.expr.ref} onChange={event=>{const choice=choices.find(item=>item.value===event.target.value);if(choice)change({expr:{op:"",kind:kindOf(choice.type),ref:choice.value}});}}>
                {!choices.some(choice=>choice.value===sort.expr.ref)&&<option value={sort.expr.ref}>{sort.expr.ref}</option>}
                {choices.map(choice=><option key={choice.value} value={choice.value}>{choice.label}</option>)}
              </Select>:<ExpressionEditor draftKey={`set:${at}:order:${index}`} value={sort.expr} kind={sort.expr.kind} choices={choices} lookup={lookup} onChange={expr=>change({expr})}/>}
              <div className="flex flex-wrap items-center gap-3">
                <Checkbox checked={!!sort.desc} onChange={desc=>change({desc:desc||undefined})}>{t("Descending")}</Checkbox>
                <Checkbox checked={!simple} onChange={calculated=>change({expr:calculated?literalExpression(sort.expr.kind):{op:"",kind:kindOf(choices.find(item=>kindOf(item.type)===sort.expr.kind)?.type??"text"),ref:choices.find(item=>kindOf(item.type)===sort.expr.kind)?.value??"id"}})}>{t("Calculated sort key")}</Checkbox>
                <Button size="sm" variant="ghost" disabled={set.order.length<2} onClick={()=>patch(at,{order:set.order.filter((_,i)=>i!==index)})}>{t("Remove sort key")}</Button>
              </div>
            </div>;
          })}
          <Button size="sm" className="w-fit" disabled={set.order.length>=8} onClick={()=>patch(at,{order:[...set.order,{expr:{op:"",kind:"text",ref:"id"}}]})}>{t("Add sort key")}</Button>
        </fieldset>
        <Button size="sm" variant="ghost" onClick={()=>onChange({...process,sets:sets.filter((_,index)=>index!==at)})}>{t("Remove record set")}</Button>
      </div>
    </Panel></InputDraftProvider>)}
    <RuleCollectionsEditor value={process.reads} subject={subject} onChange={reads=>onChange({...process,reads:reads&&(reads.object||reads.inputs||reads.collections)?reads:undefined})}/>
  </div>;
}
