import {pageUIManifest} from "@platform/kernel";
import type {RelationNode,RelationEdge} from "../graph/relation/model";
import {RelationCanvas} from "../graph/relation/RelationCanvas";
import {neighborhoodPositions} from "../graph/relation/layouts";
import type {Tone} from "../components/StatusTag";
import {t} from "../i18n";
import type {EntityInfo,EntityRecord} from "./Records";
import {originalRecordTitle,recordIdentity,type OriginalRecordTitle} from "./record-presentation";

export type RecordNeighborhoodGroup={id:string;records:readonly EntityRecord[];info:EntityInfo;bindingTitle:string;badge:string;tone:Tone;total:number;limit:number;labelField?:string};
export type RecordNeighborhoodProps={root:OriginalRecordTitle;groups:readonly RecordNeighborhoodGroup[];label?:string;onOpen?:(object:string,record:EntityRecord)=>void;enabled?:boolean};

/** The caller already traversed the original relationships; this component only lays out their bounded windows. */
export function RecordNeighborhood({root,groups,label,onOpen,enabled=true}:RecordNeighborhoodProps) {
 const title=originalRecordTitle(root),tones=["neutral","info","success","warning","danger"],limits:{S:number;A:number}={S:4,A:3};
 if(title===undefined||!Array.isArray(groups)||groups.length>2||new Set(groups.map(group=>group?.id)).size!==groups.length||new Set(groups.map(group=>group?.badge)).size!==groups.length||groups.some(group=>!group||typeof group.id!=="string"||!group.id||!group.info||typeof group.bindingTitle!=="string"||typeof group.badge!=="string"||!["S","A"].includes(group.badge)||!tones.includes(group.tone)||group.limit!==limits[group.badge as "S"|"A"]||!Array.isArray(group.records)||group.records.length>group.limit||!Number.isSafeInteger(group.total)||group.total<group.records.length||new Set(group.records.map((record:EntityRecord)=>record?.id)).size!==group.records.length||group.records.some((record:EntityRecord)=>originalRecordTitle({object:group.info.type,record,info:group.info,labelField:group.labelField??"id"})===undefined))||groups.reduce((sum,group)=>sum+group.records.length,0)>pageUIManifest.runtime.exploration.maxNeighborhoodRecords)return <p role="alert">{t("The original neighborhood root, relations or windows are unavailable or incompatible.")}</p>;
 const rootID=recordIdentity(root.object,root.record.id),nodes:RelationNode[]=[{id:rootID,label:`${title} · ${root.object}/${root.record.id}`,detail:`${root.object}/${root.record.id}`,tone:"info",badge:{label:root.record.id,size:44}}],edges:RelationEdge[]=[],records=new Map([[rootID,{object:root.object,record:root.record}]]),originalGroups:readonly RecordNeighborhoodGroup[]=groups;
 const layout=originalGroups.map(group=>({side:group.badge==="S"?"right" as const:"left" as const,nodes:group.records.map(record=>{
  const id=recordIdentity(group.info.type,record.id);if(!records.has(id)){records.set(id,{object:group.info.type,record});nodes.push({id,label:`${originalRecordTitle({object:group.info.type,record,info:group.info,labelField:group.labelField??"id"})!} · ${group.info.type}/${record.id}`,detail:`${group.info.type}/${record.id}`,tone:group.tone,badge:{label:group.badge,size:24}});}
  edges.push({id:encodeURIComponent(JSON.stringify([group.id,rootID,id])),source:rootID,target:id,label:group.bindingTitle,tone:group.tone,directed:false});return id;
 })}));
 return <section aria-label={label??t("Record neighborhood")} className="grid min-w-0 gap-2"><RelationCanvas nodes={nodes} edges={edges} positions={neighborhoodPositions(rootID,layout)} selected={rootID} height={220} label={label??t("Record neighborhood")} storeKey={`neighborhood:${rootID}`} onSelect={onOpen&&enabled?id=>{const original=id?records.get(id):undefined;if(original&&enabled)onOpen(original.object,original.record);}:undefined}/>{!groups.length&&<p role="status" className="text-xs text-muted">{t("No readable neighborhood relations.")}</p>}<ul className="grid gap-1 text-xs text-muted">{originalGroups.map(group=><li key={group.id}>{group.bindingTitle}{" · "}{t("Showing {shown} of {total} neighbors.",{shown:group.records.length,total:group.total})}</li>)}</ul></section>;
}
