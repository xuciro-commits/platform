import {renderingLimits} from "../gen/presentation-limits";
import {Boxes} from "lucide-react";
import { List, ListItem } from "@astryxdesign/core/List";
import { Text } from "@astryxdesign/core/Text";
import {Tag,type Tone} from "../components/StatusTag";
import {t} from "../i18n";
import {entityFrom,type EntityInfo,type EntityRecord} from "./Records";
import {originalRecordTitle} from "./record-presentation";

export type ResourceStatusTone={value:string;tone:Tone};
export type RecordResourceListProps={records:readonly EntityRecord[];info:EntityInfo;labelField:string;statusField:string;statusTones?:readonly ResourceStatusTone[];total:number;selected?:string;onSelect?:(record:EntityRecord)=>void;enabled?:boolean;label?:string};

export function RecordResourceList({records,info,labelField,statusField,statusTones=[],total,selected,onSelect,enabled=true,label}:RecordResourceListProps) {
 const field=info?.fields.find(field=>field.name===statusField),titles=Array.isArray(records)?records.map(record=>originalRecordTitle({object:info.type,record,info,labelField})):[],tones=["neutral","info","success","warning","danger"];
 if(!Array.isArray(records)||records.length>renderingLimits.runtime.exploration.maxResourceWindow||!Number.isSafeInteger(total)||total<records.length||new Set(records.map(record=>record?.id)).size!==records.length||titles.some(title=>title===undefined)||!field||!["text","choice"].includes(field.type)||records.some(record=>record[statusField]!=null&&typeof record[statusField]!=="string")||statusTones.length>renderingLimits.runtime.recordEvents.maxTones||new Set(statusTones.map(item=>item.value)).size!==statusTones.length||statusTones.some(item=>!item||typeof item.value!=="string"||!tones.includes(item.tone)))return <Text as="p" role="alert">{t("Resource records, title, status or total are unavailable or incompatible.")}</Text>;
 const entity=entityFrom(info),originalRecords:readonly EntityRecord[]=records;
 return <section aria-label={label??t("Resource list")} className="grid min-w-0 gap-2"><Text type="supporting" role="status">{t("Showing {shown} of {total} resources.",{shown:records.length,total})}</Text>{!records.length?<Text type="supporting">{t("No resources in this window.")}</Text>:<List aria-label={label??t("Resource list")} density="compact" hasDividers>{originalRecords.map((record,index)=>{const status=record[statusField] as string|null|undefined;return <ListItem key={record.id} itemKey={record.id} startContent={<Boxes className="size-3"/>} label={titles[index]} description={<span className="break-all font-mono text-xs text-muted">{record.id}</span>} endContent={status==null?<Text type="supporting">{t("No status provided")}</Text>:<Tag label={entity.fields[statusField]!.text(status)} tone={statusTones.find(item=>item.value===status)?.tone??"neutral"}/>} isSelected={selected===record.id} isDisabled={!enabled} onClick={onSelect?()=>{if(enabled)onSelect(record);}:undefined} />;})}</List>}</section>;
}
