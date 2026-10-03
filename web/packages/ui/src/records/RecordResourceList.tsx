import {pageUIManifest} from "@platform/kernel";
import {Boxes} from "lucide-react";
import {Button} from "../primitives/button";
import {Tag,type Tone} from "../components/StatusTag";
import {t} from "../i18n";
import {entityFrom,type EntityInfo,type EntityRecord} from "./Records";
import {originalRecordTitle} from "./record-presentation";

export type ResourceStatusTone={value:string;tone:Tone};
export type RecordResourceListProps={records:readonly EntityRecord[];info:EntityInfo;labelField:string;statusField:string;statusTones?:readonly ResourceStatusTone[];total:number;selected?:string;onSelect?:(record:EntityRecord)=>void;enabled?:boolean;label?:string};

/** The original window and its original status; no secondary reader, filter or inferred membership. */
export function RecordResourceList({records,info,labelField,statusField,statusTones=[],total,selected,onSelect,enabled=true,label}:RecordResourceListProps) {
 const field=info?.fields.find(field=>field.name===statusField),titles=Array.isArray(records)?records.map(record=>originalRecordTitle({object:info.type,record,info,labelField})):[],tones=["neutral","info","success","warning","danger"];
 if(!Array.isArray(records)||records.length>pageUIManifest.runtime.exploration.maxResourceWindow||!Number.isSafeInteger(total)||total<records.length||new Set(records.map(record=>record?.id)).size!==records.length||titles.some(title=>title===undefined)||!field||!["text","choice"].includes(field.type)||records.some(record=>record[statusField]!=null&&typeof record[statusField]!=="string")||statusTones.length>pageUIManifest.runtime.recordEvents.maxTones||new Set(statusTones.map(item=>item.value)).size!==statusTones.length||statusTones.some(item=>!item||typeof item.value!=="string"||!tones.includes(item.tone)))return <p role="alert">{t("Resource records, title, status or total are unavailable or incompatible.")}</p>;
 const entity=entityFrom(info),originalRecords:readonly EntityRecord[]=records;
 return <section aria-label={label??t("Resource list")} className="grid min-w-0 gap-2"><p role="status" className="text-xs text-muted">{t("Showing {shown} of {total} resources.",{shown:records.length,total})}</p>{!records.length?<p className="text-xs text-muted">{t("No resources in this window.")}</p>:<ul className="grid min-w-0 gap-1">{originalRecords.map((record,index)=>{
  const status=record[statusField] as string|null|undefined,content=<><Boxes aria-hidden className="size-3 shrink-0"/><span className="min-w-0 flex-1"><span className="block break-words font-medium">{titles[index]}</span><span className="block break-all font-mono text-[11px] text-muted">{record.id}</span></span>{status==null?<span className="text-xs text-muted">{t("No status provided")}</span>:<Tag label={entity.fields[statusField]!.text(status)} tone={statusTones.find(item=>item.value===status)?.tone??"neutral"}/>}</>;
  return <li key={record.id}>{onSelect?<Button variant={selected===record.id?"primary":"row"} disabled={!enabled} aria-pressed={selected===record.id} className="flex h-auto w-full min-w-0 gap-2 whitespace-normal border-b border-border py-1.5 text-left text-xs" onClick={()=>{if(enabled)onSelect(record);}}>{content}</Button>:<div className="flex min-w-0 items-center gap-2 border-b border-border py-1.5 text-xs">{content}</div>}</li>;
 })}</ul>}</section>;
}
