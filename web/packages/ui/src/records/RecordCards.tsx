import {EntityCard} from "../components/EntityCard";
import {Grid} from "@astryxdesign/core/Grid";
import {Text} from "@astryxdesign/core/Text";
import {Button} from "../primitives/button";
import {t} from "../i18n";
import {entityFrom,type EntityInfo,type EntityRecord,type RecordSource} from "./Records";

export function RecordCards({records,info,fields=[],labelField,layout,selected,onSelect,onNavigate,label,source,onOpenReference}:{records:EntityRecord[];info:EntityInfo;fields?:string[];labelField:string;layout:"grid"|"list"|"tiles";selected?:string;onSelect?:(record:EntityRecord)=>void;onNavigate?:(record:EntityRecord)=>void;label?:string;source?:RecordSource;onOpenReference?:(type:string,id:string)=>void}){
 const entity=entityFrom(info,{},source,onOpenReference),status=info.lifecycle?.field;
 return <Grid role={label?"group":undefined} aria-label={label} columns={layout==="tiles"?4:layout==="grid"?{minWidth:170,repeat:"fit"}:1} gap={layout==="tiles"?1:2}>{records.map(record=>{
  const title=labelField==="id"?record.id:info.fields.some(f=>f.name===labelField)?String(record[labelField]??record.id):record.id;
  return <div key={record.id} className={selected===record.id?"rounded-md ring-2 ring-primary":""}><EntityCard title={onSelect?<Button variant="ghost" className="h-auto max-w-full justify-start whitespace-normal p-0 text-left" aria-label={title} aria-pressed={selected===record.id} onClick={()=>onSelect(record)}>{title}</Button>:title} subtitle={record.id} status={status&&entity.fields[status]?entity.fields[status]!.display(record[status],record):undefined} actions={onNavigate?<Button size="sm" variant="ghost" onClick={()=>onNavigate(record)}>{t("Open record")}</Button>:undefined} properties={[...new Set(fields)].flatMap(name=>{const field=info.fields.find(f=>f.name===name);return field&&entity.fields[name]?[[field.title,entity.fields[name]!.display(record[name],record)]]:[];})}/></div>;
 })}{!records.length&&<Text type="supporting">{t("No cards in this window.")}</Text>}</Grid>;
}
