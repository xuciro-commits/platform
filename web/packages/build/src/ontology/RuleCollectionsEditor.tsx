import type {Api} from "@platform/kernel";
import {useHost,useRecordInventory} from "@platform/app";
import {Button,Disclosure,Input,Select,t,type EntityInfo} from "@platform/ui";
import {nameOf,type Input_} from "./object-model";

type QueryDraft=Api.NamedQuery&{id:string;version?:number;state:string;published?:string;archived?:boolean};
const labelClass="grid min-w-0 gap-1 text-xs";
const ordinal=(version:string)=>Number(version.match(/\.query-(\d+)$/)?.[1]??0);

export function RuleCollectionsEditor({value,onChange,subject,inputs=[]}:{value?:Api.RuleReadSpec;onChange:(next:Api.RuleReadSpec|undefined)=>void;subject:EntityInfo;inputs?:Input_[]}){
 const host=useHost(),builder=host.holds("build", "builder");
 const drafts=useRecordInventory<QueryDraft>("build.query",1000,builder);
 const published=host.definitions.filter(d=>d.ref.kind==="query"&&d.query?.object).map(d=>({app:d.ref.app,name:d.ref.name,title:d.query!.title,object:d.query!.object,by:d.query!.by,version:ordinal(d.version),draft:false}));
 const available=[...published,...(builder?drafts.data?.records??[]:[]).filter(q=>!q.archived&&q.object&&(!q.published||q.state==="draft")).map(q=>({app:"build",name:q.name,title:q.title,object:q.object,by:q.by,version:(q.version??0)+1,draft:true}))];
 const collections=value?.collections??{};
 const save=(next:Record<string,Api.RuleCollection>)=>onChange(Object.keys(next).length?{...value,collections:next}:{...value,collections:undefined});
 const patch=(name:string,next:Partial<Api.RuleCollection>)=>save({...collections,[name]:{...collections[name]!,...next}});
 const entries=Object.entries(collections);
 const inputsOf=inputs.filter(input=>["text","longtext","choice","reference"].includes(input.type));
 const fieldsOf=subject.fields.filter(field=>["text","longtext","choice","reference"].includes(field.type));
 return <Disclosure summary={t("Rule collection inputs")}><div className="grid gap-3">
  <p className="text-xs text-muted">{t("Complete reads reject hidden or truncated inputs. Visible reads use only the caller's permitted records.")}</p>
  {entries.map(([name,collection],index)=>{
   const selected=available.find(q=>q.app===collection.app&&q.name===collection.query&&q.version===collection.version),key=`${collection.app}/${collection.query}/${collection.version}`,current=collection.set?"set":"query";
   return <fieldset key={index} className="grid gap-2 rounded border border-border p-3"><legend className="text-xs font-medium">{name}</legend>
    <label className={labelClass}>{t("Collection name")}<Input value={name} onChange={event=>{const after=event.target.value;if(!after||after===name||collections[after])return;save(Object.fromEntries(entries.map(([key,item])=>[key===name?after:key,item])));}}/></label>
    <p className="text-xs text-muted">{t("When renaming a collection, update the expressions that use its name.")}</p>
    <label className={labelClass}>{t("Collection source")}<Select value={current} onChange={event=>{const set=subject.sets?.[0];save({...collections,[name]:event.target.value==="set"?{app:subject.app,object:set?.object??"",set:set?.name??"",query:"",version:0,policy:"complete"}:{app:subject.app,object:"",query:"",version:1,policy:collection.policy}});}}><option value="set">{t("Declared record set")}</option><option value="query">{t("Retained query")}</option></Select></label>
    {current==="set"?<label className={labelClass}>{t("Declared collection")}<Select value={collection.set??""} onChange={event=>{const set=subject.sets?.find(s=>s.name===event.target.value);if(set)patch(name,{set:set.name,object:set.object});}}><option value="">{t("Choose a collection")}</option>{subject.sets?.map(set=><option key={set.name} value={set.name}>{set.name} · {set.object}</option>)}</Select></label>:<>
     <label className={labelClass}>{t("Retained query")}<Select value={key} onChange={event=>{const query=available.find(q=>`${q.app}/${q.name}/${q.version}`===event.target.value);if(query)patch(name,{app:query.app,object:query.object,query:query.name,version:query.version,set:undefined,of:query.by?"$subject":undefined});}}>{!selected&&<option value={key}>{collection.query?`${collection.query} @ ${collection.version}`:t("Choose a query")}</option>}{available.map(q=><option key={`${q.app}/${q.name}/${q.version}`} value={`${q.app}/${q.name}/${q.version}`}>{q.title} · {q.name} @ {q.version}{q.draft?` · ${t("Saved draft")}`:""}</option>)}</Select></label>
     <label className={labelClass}>{t("Exact query version")}<Input type="number" min={collection.version===0?0:1} max={64} value={collection.version} onChange={event=>patch(name,{version:event.target.valueAsNumber})}/></label>
     {(selected?.by||collection.of)&&<label className={labelClass}>{t("Query record key")}<Select value={collection.of??"$subject"} onChange={event=>patch(name,{of:event.target.value})}><option value="$subject">{t("Subject record ID")}</option>{fieldsOf.map(field=><option key={field.name} value={`$subject.${field.name}`}>{t("This record: {name}",{name:field.title})}</option>)}{inputsOf.map(input=><option key={input.name} value={input.name}>{t("Input: {name}",{name:input.title})}</option>)}</Select></label>}
    </>}
    <label className={labelClass}>{t("Collection scope policy")}<Select value={collection.policy} onChange={event=>patch(name,{policy:event.target.value})}><option value="complete">{t("Complete collection")}</option><option value="visible">{t("Visible records only")}</option></Select></label>
    <Button size="sm" onClick={()=>save(Object.fromEntries(entries.filter(([key])=>key!==name)))}>{t("Remove collection input")}</Button>
   </fieldset>;
  })}
  <Button size="sm" disabled={entries.length>=64} onClick={()=>{const name=nameOf("collection",Object.keys(collections)),set=subject.sets?.[0];save({...collections,[name]:{app:subject.app,object:set?.object??subject.type,set:set?.name,query:"",version:set?0:1,policy:"complete"}});}}>{t("Add collection input")}</Button>
 </div></Disclosure>;
}
