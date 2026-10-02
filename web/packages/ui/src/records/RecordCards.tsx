import {EntityCard} from "../components/EntityCard";
import {Button} from "../primitives/button";
import {t} from "../i18n";
import {entityFrom,type EntityInfo,type EntityRecord} from "./Records";

/** Presentation of the caller's permission-filtered window; no reader or writer. */
export function RecordCards({records,info,fields=[],labelField,layout,selected,onSelect,onNavigate}:{records:EntityRecord[];info:EntityInfo;fields?:string[];labelField:string;layout:"grid"|"list";selected?:string;onSelect?:(record:EntityRecord)=>void;onNavigate?:(record:EntityRecord)=>void}){
 const entity=entityFrom(info),status=info.lifecycle?.field;
 return <div className={layout==="grid"?"grid grid-cols-[repeat(auto-fit,minmax(min(100%,170px),1fr))] gap-2":"grid grid-cols-1 gap-2"}>{records.map(record=>{
  const title=labelField==="id"?record.id:info.fields.some(f=>f.name===labelField)?String(record[labelField]??record.id):record.id;
  return <div key={record.id} className={selected===record.id?"rounded-md ring-2 ring-primary":""}><EntityCard title={onSelect?<Button variant="ghost" className="h-auto max-w-full justify-start whitespace-normal p-0 text-left" aria-label={title} aria-pressed={selected===record.id} onClick={()=>onSelect(record)}>{title}</Button>:title} subtitle={record.id} status={status&&entity.fields[status]?entity.fields[status]!.display(record[status],record):undefined} actions={onNavigate?<Button size="sm" variant="ghost" onClick={()=>onNavigate(record)}>{t("Open record")}</Button>:undefined} properties={[...new Set(fields)].flatMap(name=>{const field=info.fields.find(f=>f.name===name);return field&&entity.fields[name]?[[field.title,entity.fields[name]!.display(record[name],record)]]:[];})}/></div>;
 })}{!records.length&&<p className="text-sm text-muted">{t("No cards in this window.")}</p>}</div>;
}
