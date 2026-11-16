import {renderingLimits} from "../gen/presentation-limits";
import {BreadcrumbItem, Breadcrumbs} from "@astryxdesign/core/Breadcrumbs";
import {List, ListItem} from "@astryxdesign/core/List";
import {HStack, VStack} from "@astryxdesign/core/Stack";
import {Text} from "@astryxdesign/core/Text";
import {Button} from "../primitives/button";
import {Select} from "../primitives/input";
import {t} from "../i18n";
import type {EntityInfo,EntityRecord} from "./Records";
import {originalRecordTitle,recordIdentity,type OriginalRecordTitle} from "./record-presentation";

export type SearchAroundPathEntry=OriginalRecordTitle;
export type SearchAroundRelation={id:string;label:string;total?:number;pending?:boolean;error?:string};
export type SearchAroundWindow={records:readonly EntityRecord[];info:EntityInfo;labelField:string;total:number};
export type SearchAroundProps={path:readonly SearchAroundPathEntry[];relations:readonly SearchAroundRelation[];activeRelation?:string;window?:SearchAroundWindow;onBack?:(index:number)=>void;onRelation?:(id:string)=>void;onTraverse?:(object:string,record:EntityRecord)=>void;onSelect?:(object:string,record:EntityRecord)=>void;selectionEnabled?:boolean;selected?:{object:string;id:string};selectionPending?:boolean;selectionError?:string;enabled?:boolean;pending?:boolean;error?:string;label?:string};

export function SearchAround({path,relations,activeRelation,window,onBack,onRelation,onTraverse,onSelect,selectionEnabled=true,selected,selectionPending,selectionError,enabled=true,pending,error,label}:SearchAroundProps) {
 const limits=renderingLimits.runtime.exploration,titles=Array.isArray(path)?path.map(originalRecordTitle):[],results=window?.records;
 if(!Array.isArray(path)||path.length>limits.maxPath||titles.some(title=>title===undefined)||!Array.isArray(relations)||relations.length>2*limits.maxRelations||new Set(relations.map(relation=>relation?.id)).size!==relations.length||relations.some(relation=>!relation||typeof relation.id!=="string"||!relation.id||typeof relation.label!=="string"||relation.total!==undefined&&(!Number.isSafeInteger(relation.total)||relation.total<0))||window&&(!Array.isArray(results)||results.length>limits.maxWindow||!Number.isSafeInteger(window.total)||window.total<results.length||new Set(results.map(record=>record?.id)).size!==results.length||results.some(record=>originalRecordTitle({object:window.info.type,record,info:window.info,labelField:window.labelField})===undefined)))return <Text as="p" role="alert">{t("The original exploration path, relations or window is unavailable or incompatible.")}</Text>;
 if(!path.length)return <Text as="p" role="status">{t("Select a confirmed record to start Search Around.")}</Text>;
 const originalPath:readonly SearchAroundPathEntry[]=path,originalRelations:readonly SearchAroundRelation[]=relations,current=path.at(-1)!,active=relations.find(relation=>relation.id===activeRelation),ready=enabled&&!pending&&!error&&!!active&&!active.pending&&!active.error;
 return <section aria-label={label??t("Search Around")} className="grid min-w-0 gap-2">
  <Breadcrumbs label={t("Exploration path")}>
   {originalPath.map((entry,index)=>{const identity=recordIdentity(entry.object,entry.record.id);return <BreadcrumbItem key={JSON.stringify([identity,index])} isCurrent={index===path.length-1} onClick={onBack&&index<path.length-1&&enabled?()=>onBack(index):undefined}><VStack gap={0}><Text type="body">{titles[index]}</Text><Text type="code">{entry.object}/{entry.record.id}</Text></VStack></BreadcrumbItem>;})}
  </Breadcrumbs>
  <Select aria-label={t("Original relations")} value={activeRelation??""} disabled={!enabled||!onRelation} onChange={event=>{if(enabled)onRelation?.(event.target.value);}}><option value="" disabled>{t("Choose a relation")}</option>{originalRelations.map(relation=><option key={relation.id} value={relation.id}>{relation.label} {relation.error?t("Unavailable"):relation.pending?t("Loading…"):relation.total===undefined?t("Count unavailable"):t("({count})",{count:relation.total})}</option>)}</Select>
  {!relations.length&&<Text type="supporting">{t("No readable relations from {object}.",{object:current.object})}</Text>}
  {error?<Text as="p" role="alert" className="text-danger">{error}</Text>:pending?<Text type="supporting" role="status">{t("Confirming exploration access…")}</Text>:active?.error?<Text as="p" role="alert" className="text-danger">{active.error}</Text>:active?.pending?<Text type="supporting" role="status">{t("Loading original related records…")}</Text>:activeRelation&&!active?<Text as="p" role="alert">{t("The selected original relation is unavailable.")}</Text>:active&&!window?<Text type="supporting" role="status">{t("Loading original related records…")}</Text>:active&&window?<><Text type="supporting" role="status">{t("Showing {shown} of {total} related records.",{shown:window.records.length,total:window.total})}</Text>{!window.records.length?<Text type="supporting">{t("No linked records in this window.")}</Text>:<List aria-label={t("Related records")} density="compact" hasDividers>{window.records.map(record=>{const object=window.info.type,title=originalRecordTitle({object,record,info:window.info,labelField:window.labelField})!,chosen=selected?.object===object&&selected.id===record.id;return <ListItem key={recordIdentity(object,record.id)} itemKey={recordIdentity(object,record.id)} label={<VStack gap={0}><Text type="body" weight="medium">{title}</Text><Text type="code">{object}/{record.id}</Text></VStack>} endContent={<HStack gap={1} wrap="wrap">{onTraverse&&<Button size="sm" variant="ghost" disabled={!ready} aria-label={t("Traverse from {id}",{id:`${object}/${record.id}`})} onClick={()=>{if(ready)onTraverse(object,record);}}>{t("Traverse")}</Button>}{onSelect&&<Button size="sm" variant={chosen?"primary":"ghost"} aria-pressed={chosen} disabled={!ready||!selectionEnabled||!!selectionPending} aria-label={t("Select output {id}",{id:`${object}/${record.id}`})} onClick={()=>{if(ready&&selectionEnabled&&!selectionPending)onSelect(object,record);}}>{t("Select")}</Button>}</HStack>} />;})}</List>}</>:null}
  {selectionPending&&<Text type="supporting" role="status">{t("Confirming original output record…")}</Text>}
  {selectionError&&<Text as="p" role="alert" className="text-danger">{selectionError}</Text>}
 </section>;
}
