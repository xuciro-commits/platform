import {useEffect,useRef,useState} from "react";
import {FacetChoices,Input,Panel,t,type RecordList,type RecordSource} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {isStringSet,type ScalarValue} from "../runtime/decimal";
import type {VariableResult} from "../runtime/variables";

export function Facets({section,source,object,window,values,onChange,scope}:{section:Pick<Api.Section,'facets'|'filterSearchVariable'|'title'>;source:RecordSource;object:string;window?:NonNullable<Parameters<typeof RecordList>[0]["window"]>;values:Record<string,VariableResult>;onChange:(id:string,value:ScalarValue)=>void;scope:string}){
 const grouped=(section.facets??[]).filter(f=>["checkbox","histogram"].includes(f.kind));
 const query=window?.query,key=JSON.stringify([scope,source.scope,source.revision,object,query,grouped.map(f=>f.field)]),current=useRef(source);current.current=source;
 const [state,setState]=useState<{key:string;options?:Record<string,{value:string;count:number}[]>;error?:string}>();
 useEffect(()=>{let live=true;setState({key});if(!query||window?.error||!current.current.aggregate)return;Promise.all(grouped.map(async facet=>{const data=await current.current.aggregate!(object,{domain:query.domain,search:query.search,set:query.set,traversal:query.traversal,groups:[facet.field],measures:["count"],maxRows:64});return [facet.field,data.rows.map(row=>({value:String(row[facet.field]??""),count:Number(row.count)})).sort((a,b)=>a.value.localeCompare(b.value))] as const;})).then(rows=>{if(live)setState({key,options:Object.fromEntries(rows)});},error=>{if(live)setState({key,error:String(error)});});return()=>{live=false;};},[key]);
 const ready=state?.key===key?state:undefined;
 if(!window||window.error)return <Panel role="alert">{t(window?.error??"Query window is unavailable.")}</Panel>;
 if(ready?.error)return <Panel role="alert">{ready.error}</Panel>;
 const textValue=(id:string)=>values[id]?.status==="value"&&typeof values[id].value==="string"?values[id].value:"";
 return <div role="search" aria-label={section.title||t("Filter")} className="flex flex-wrap items-start gap-4">{(section.facets??[]).map(facet=>{
  const title=source.entity(object)?.fields.find(f=>f.name===facet.field)?.title??facet.field,value=values[facet.variable];
  if(facet.kind==="search"||facet.kind==="number")return <label key={facet.field} className="grid gap-1 text-xs text-muted">{title}<Input value={textValue(facet.variable)} onChange={e=>onChange(facet.variable,e.target.value)}/></label>;
  const options=ready?.options?.[facet.field];return options?<FacetChoices key={facet.field} title={title} options={options} value={value?.status==="value"&&isStringSet(value.value)?value.value.values:[]} histogram={facet.kind==="histogram"} onChange={values=>onChange(facet.variable,{kind:"string-set",values})}/>:<p key={facet.field} role="status">{t("Loading…")}</p>;
 })}{section.filterSearchVariable&&<label className="grid gap-1 text-xs text-muted">{t("Search")}<Input value={textValue(section.filterSearchVariable)} onChange={e=>onChange(section.filterSearchVariable!,e.target.value)}/></label>}</div>;
}
