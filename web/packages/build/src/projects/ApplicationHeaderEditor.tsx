import type {Api} from "@platform/kernel";
import {Button,Checkbox,Input,Select,Textarea,t} from "@platform/ui";

export function ApplicationHeaderEditor({value,pages,onChange}:{value?:Api.ApplicationHeader;pages:string[];onChange:(header:Api.ApplicationHeader|undefined)=>void}){
 const update=(patch:Partial<Api.ApplicationHeader>)=>value&&onChange({...value,...patch});
 return <fieldset className="grid gap-2"><legend>{t("Application header")}</legend><Checkbox checked={!!value} onChange={enabled=>onChange(enabled?{variant:"horizontal",title:"",items:[{kind:"logo"},{kind:"title"},{kind:"tabs",pages:[...pages]}]}:undefined)}>{t("Show application header")}</Checkbox>{value&&<>
  <label className="grid gap-1 text-xs">{t("Header title")}<Input value={value.title} onChange={e=>update({title:e.target.value})}/></label>
  <label className="grid gap-1 text-xs">{t("Header layout")}<Select value={value.variant} onChange={e=>update({variant:e.target.value,collapsed:false})}><option value="horizontal">{t("Horizontal")}</option><option value="vertical">{t("Vertical")}</option></Select></label>
  <label className="grid gap-1 text-xs">{t("Header logo URL")}<Input value={value.logo??""} onChange={e=>update({logo:e.target.value||undefined})}/></label>
  {value.variant==="vertical"&&<Checkbox checked={!!value.collapsed} onChange={collapsed=>update({collapsed})}>{t("Initially collapse application navigation")}</Checkbox>}
  {value.items.map((item,index)=><div key={index} className="grid gap-1 rounded border border-border p-2"><strong className="text-xs">{item.kind}</strong>
   {item.kind==="button"&&<><Input aria-label={t("Header button label")} value={item.label??""} onChange={e=>update({items:value.items.map((it,i)=>i===index?{...it,label:e.target.value}:it)})}/><Select aria-label={t("Header button action")} value={item.action??"refresh"} onChange={e=>update({items:value.items.map((it,i)=>i===index?{...it,action:e.target.value}:it)})}><option value="refresh">{t("Refresh data")}</option><option value="theme">{t("Toggle theme")}</option></Select></>}
   {item.kind==="text"&&<Textarea aria-label={t("Header text")} value={item.text??""} onChange={e=>update({items:value.items.map((it,i)=>i===index?{...it,text:e.target.value}:it)})}/>}
   {item.kind==="tabs"&&pages.map(page=><Checkbox key={page} checked={item.pages?.includes(page)??false} onChange={checked=>update({items:value.items.map((it,i)=>i===index?{...it,pages:checked?[...it.pages??[],page]:(it.pages??[]).filter(p=>p!==page)}:it)})}>{page}</Checkbox>)}
   <div className="flex gap-1"><Button size="sm" disabled={index===0} onClick={()=>{const items=[...value.items];[items[index-1],items[index]]=[items[index]!,items[index-1]!];update({items});}}>{t("Move up")}</Button><Button size="sm" onClick={()=>update({items:value.items.filter((_,i)=>i!==index)})}>{t("Remove")}</Button></div>
  </div>)}
  <Select aria-label={t("Add header item")} value="" disabled={value.items.length>=24} onChange={e=>{const kind=e.target.value;if(kind)update({items:[...value.items,kind==="button"?{kind,label:t("Refresh"),action:"refresh"}:kind==="tabs"?{kind,pages:[...pages]}:kind==="text"?{kind,text:""}:{kind}]});}}><option value="">{t("Add header item")}</option>{["logo","title","tabs","spacer","button","text"].map(kind=><option key={kind} value={kind}>{kind}</option>)}</Select>
 </>}</fieldset>;
}
