import { useState } from "react";
import type { Api } from "@platform/kernel";
import { SemanticObjectSelect, SemanticPropertySelect, pageUIProfile, pageVariableContract, useHost, type PageVariableValue } from "@platform/app";
import { Button, Card, Checkbox, Input, PropertyList, Select, t } from "@platform/ui";
import { layoutID } from "../page-layout";

export function QueriesPanel({ document, object, values, onChange }: { document: Api.PageDocument; object: Api.AssetRef; values: Record<string,PageVariableValue>; onChange:(document:Api.PageDocument)=>void }) {
  const { definitions } = useHost(), [chosen,choose] = useState("");
  const namedQueries=definitions.flatMap((d)=>{
    if(d.ref.kind!=="query")return [];
    const versions={...d.queryVersions,...(d.query?{[d.version]:d.query}:{})};
    return Object.entries(versions).map(([version,query])=>({ref:d.ref,version,query,key:`${d.ref.app}/${d.ref.name}@${version}`}));
  });
  const plans = document.queries ?? {}, id = plans[chosen] ? chosen : Object.keys(plans)[0] ?? "", plan = plans[id];
  const update = (queries:Record<string,Api.PageQuery>, variables=document.variables) => onChange({...document,uiProfile:pageUIProfile,queries,variables});
  const patch = (change:Partial<Api.PageQuery>) => update({...plans,[id]:{...plan!,...change}});
  const aliases = Object.entries(document.variables ?? {}).filter(([,v])=>v.source?.kind==="plan"&&v.source.query===id), result = values[aliases[0]?.[0] ?? ""];
  const window = result && (result.status==="value"||result.status==="empty") && typeof result.value==="object" && result.value.kind==="object-set" ? result.value.window : undefined;
  return <Card className="grid content-start gap-3 p-3"><h3 className="text-sm font-semibold">{t("Query plans")}</h3>
    <p className="text-xs text-muted">{t("Queries use the member's original record read and run without a table widget.")}</p>
    <Select aria-label={t("Choose query plan")} value={id} onChange={(event)=>choose(event.target.value)}><option value="">{t("Choose query plan")}</option>{Object.entries(plans).map(([id,p])=><option key={id} value={id}>{p.title||id}</option>)}</Select>
    <Button disabled={Object.keys(plans).length>=pageVariableContract.query.maxPlans} onClick={()=>{const query=layoutID("query"),variable=layoutID("window"),title=t("Query {n}",{n:Object.keys(plans).length+1});update({...plans,[query]:{title,object,limit:50,sort:["id"]}},{...document.variables,[variable]:{title,scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query}}});choose(query);}}>{t("Add query plan")}</Button>
    {plan && <>
      <label className="grid gap-1 text-xs">{t("Query title")}<Input value={plan.title??""} onChange={(event)=>patch({title:event.target.value})}/></label>
      <label className="grid gap-1 text-xs">{t("Query object")}<SemanticObjectSelect value={plan.object.name} label={t("Query object")} onChange={(object)=>{if(object)patch({object,conditions:[],sort:["id"],query:undefined,for:undefined})}}/></label>
      <label className="grid gap-1 text-xs">{t("Named query binding")}<Select value={plan.query?`${plan.query.ref.app}/${plan.query.ref.name}@${plan.query.sourceVersion}`:""} onChange={(event)=>{const named=namedQueries.find((d)=>d.key===event.target.value);patch({query:named?{ref:named.ref,sourceVersion:named.version}:undefined,for:named?.query?.by?{literal:""}:undefined})}}><option value="">{t("Read the object directly")}</option>{namedQueries.filter((d)=>d.query.object===plan.object.name).map((d)=><option key={d.key} value={d.key}>{d.query?.title||d.ref.name} · {d.version}</option>)}</Select></label>
      {plan.query && <p className="break-all text-xs text-muted">{t("Named query conditions and version remain fixed.")} {plan.query.sourceVersion}</p>}
      {plan.for && <QueryValue label="Query parent input" value={plan.for} document={document} onChange={(value)=>patch({for:value})}/>}
      <label className="grid gap-1 text-xs">{t("Query window limit")}<Input type="number" min={1} max={pageVariableContract.query.maxLimit} value={plan.limit} onChange={(event)=>patch({limit:Number(event.target.value)})}/></label>
      <label className="grid gap-1 text-xs">{t("Query window offset")}<Input type="number" min={0} max={pageVariableContract.query.maxOffset} value={plan.offset??0} onChange={(event)=>patch({offset:Number(event.target.value)})}/></label>
      <label className="grid gap-1 text-xs">{t("Query sort")}<Select value={plan.sort?.[0]??"id"} onChange={(event)=>patch({sort:[event.target.value]})}><option value="id">{t("ID")}</option>{(definitions.find((d)=>d.ref.kind==="object"&&d.ref.name===plan.object.name)?.entity?.fields??[]).filter((f)=>!["references","tags","lines","json"].includes(f.type)).flatMap((f)=>[f.name,`-${f.name}`].map((name)=><option key={name} value={name}>{f.title} {name.startsWith("-")?"↓":"↑"}</option>))}</Select></label>
      <Checkbox checked={!!plan.search} onChange={(checked)=>patch({search:checked?{literal:""}:undefined})}>{t("Query search parameter")}</Checkbox>
      {plan.search && <QueryValue label="Query search input" value={plan.search} document={document} onChange={(value)=>patch({search:value})}/>}
      {(plan.conditions??[]).map((condition,at)=><fieldset key={at} className="grid min-w-0 gap-2 border-t border-border pt-2"><legend className="text-xs">{t("Condition {n}",{n:at+1})}</legend>
        <SemanticPropertySelect object={plan.object} value={condition.field} label={t("Query condition field")} filter={(field)=>["text","choice","boolean","reference","date","datetime","integer","decimal"].includes(field.type)} onChange={(ref)=>{if(ref)patch({conditions:plan.conditions!.map((c,i)=>i===at?{...c,field:ref.field}:c)})}}/>
        <Select aria-label={t("Query condition operator")} value={condition.op} onChange={(event)=>patch({conditions:plan.conditions!.map((c,i)=>i===at?{...c,op:event.target.value}:c)})}>{pageVariableContract.query.operators.map((op)=><option key={op} value={op}>{op}</option>)}</Select>
        <QueryValue label="Query condition value" value={condition.value} document={document} onChange={(value)=>patch({conditions:plan.conditions!.map((c,i)=>i===at?{...c,value}:c)})}/>
        <Button onClick={()=>patch({conditions:plan.conditions!.filter((_,i)=>i!==at)})}>{t("Remove query condition")}</Button>
      </fieldset>)}
      <Button disabled={(plan.conditions?.length??0)>=pageVariableContract.query.maxConditions} onClick={()=>patch({conditions:[...(plan.conditions??[]),{field:"",op:"=",value:{literal:""}}]})}>{t("Add query condition")}</Button>
      <div role="region" aria-label={t("Query result window")} className="grid gap-2 border-t border-border pt-2 text-xs"><strong>{t("Query result window")}</strong><span role="status">{t(result?.status==="error"?"Value error":result?.status==="pending"?"Loading value":window?"Value available":"No value")}</span>{result?.status==="error"&&<p role="alert">{t(result.code)}</p>}{window&&<PropertyList items={[[t("Records in window"),String(window.records.length)],[t("Total matching records"),String(window.total)],[t("Offset"),String(window.query.offset??0)],[t("Window coverage"),t(window.complete?"Complete for this read":"Partial window")]]}/>}</div>
      <Button onClick={()=>{const next={...plans};delete next[id];const variables=Object.fromEntries(Object.entries(document.variables??{}).filter(([,v])=>!(v.source?.kind==="plan"&&v.source.query===id)));update(next,variables);choose("");}}>{t("Remove query plan")}</Button>
    </>}
  </Card>;
}

function QueryValue({label,value,document,onChange}:{label:string;value:Api.PageValue;document:Api.PageDocument;onChange:(value:Api.PageValue)=>void}) {
  const type=typeof value.literal;
  const available=Object.entries(document.variables??{}).filter(([,v])=>["page","application"].includes(v.scope)&&["string","boolean","record"].includes(v.type));
  return <div className="grid gap-2"><label className="grid gap-1 text-xs">{t(label)}<Select value={value.variable??""} onChange={(event)=>onChange(event.target.value?{variable:event.target.value}:{literal:""})}><option value="">{t("Literal")}</option>{available.map(([id,v])=><option key={id} value={id}>{v.title||id} · {t(v.type)}</option>)}</Select></label>
    {!value.variable&&<><Select aria-label={t("Query literal type")} value={type==="boolean"?"boolean":type==="number"?"number":"string"} onChange={(event)=>onChange({literal:event.target.value==="boolean"?false:event.target.value==="number"?0:""})}><option value="string">{t("Text")}</option><option value="boolean">{t("Boolean")}</option><option value="number">{t("Number")}</option></Select>{type==="boolean"?<Checkbox checked={value.literal===true} onChange={(value)=>onChange({literal:value})}>{t("Query literal value")}</Checkbox>:<Input aria-label={t("Query literal value")} type={type==="number"?"number":"text"} value={String(value.literal??"")} onChange={(event)=>onChange({literal:type==="number"?Number(event.target.value):event.target.value})}/>}</>}
  </div>;
}
