import {useEffect,useMemo,useRef,useState} from "react";
import {AssetDirectory,Panel,RecordNeighborhood,RecordResourceList,SearchAround,t,type EntityInfo,type EntityRecord,type RecordSource,type SearchAroundPathEntry} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {createRecordExploration,originalDirectoryItems,type ExplorationWindow} from "../exploration/reader";
import type {QueryWindow} from "./QueryWindowFrame";
import type {VariableResult} from "../runtime/variables";
type Status="empty"|"pending"|"value"|"error";

export function ResourceListRenderer({config,window,collection,info,selected,onSelect,enabled,label,readCurrent=true}:{config?:Api.PageResourceList;window?:QueryWindow;collection?:VariableResult;info?:EntityInfo;selected?:EntityRecord;onSelect?:(record:EntityRecord)=>void;enabled?:boolean;label:string;readCurrent?:boolean}) {
 if(!readCurrent||collection?.status==="pending")return <p role="status">{t("Loading…")}</p>;
 if(!config||!info||!window)return <Panel role="alert">{t("The original resource list is unavailable.")}</Panel>;
 if(collection?.status==="error"||window.error)return <Panel role="alert">{t("The original resource query could not be read.")}</Panel>;
 if(!window.page)return <p role="status">{t("Loading…")}</p>;
 if(window.query.limit!==12||!!window.query.offset||JSON.stringify(window.query.sort)!==JSON.stringify(["id"]))return <Panel role="alert">{t("The original resource window bounds or ordering changed.")}</Panel>;
 return <RecordResourceList records={window.page.records} total={window.page.total} info={info} labelField={config.labelField} statusField={config.statusField} statusTones={config.statusTones as Parameters<typeof RecordResourceList>[0]["statusTones"]} selected={selected?.id} onSelect={onSelect} enabled={enabled} label={label}/>;
}
export function AssetDirectoryRenderer({config,definitions,onOpen,canOpen,enabled,label}:{config?:Api.PageAssetDirectory;definitions:readonly Api.Definition[];onOpen?:(id:string)=>void;canOpen?:(id:string)=>boolean;enabled?:boolean;label:string}) {
 if(!config)return <Panel role="alert">{t("The original asset directory is unavailable.")}</Panel>;
 const items=originalDirectoryItems(config,definitions);
 return <AssetDirectory items={items} onOpen={onOpen?item=>onOpen(item.id):undefined} canOpen={canOpen?item=>canOpen(item.id):undefined} enabled={enabled} label={label}/>;
}

type Props={kind:"graph-explorer"|"vertex-graph";config?:Api.PageGraphExplorer;vertex?:Api.PageVertexGraph;root?:EntityRecord;rootObject:Api.AssetRef;status?:Status;source:RecordSource;definitions:readonly Api.Definition[];identity:string;isActive:()=>boolean;onOutput?:(object:string,record:EntityRecord)=>Promise<boolean>;selected?:{object:string;id:string};enabled?:boolean;label:string;readCurrent?:boolean};
/** The widget owns only a bounded path; records, relations and output confirmation retain their original owners. */
export function ExplorationRenderer(props:Props) {
 const {kind,config,vertex,root,rootObject,status,source,definitions,identity,isActive,onOutput,selected,enabled,label,readCurrent=true}=props;
 const rootKey=JSON.stringify([identity,source.scope,source.revision,root?.id,root?.revision,config,vertex]),life=useRef({active:true,rootKey,frame:""});life.current.rootKey=rootKey;
 useEffect(()=>{life.current.active=true;return()=>{life.current.active=false;};},[]);
 const [path,setPath]=useState<{key:string;items:SearchAroundPathEntry[]}>(),[result,setResult]=useState<{key:string;frame?:ExplorationWindow[];neighborhood?:Awaited<ReturnType<ReturnType<typeof createRecordExploration>["neighborhood"]>>;error?:string}>(),[operation,setOperation]=useState<{key:string;pending?:boolean;error?:string}>();
 const rootInfo=source.entity(rootObject.name),rootLabel=config?.objects.find(o=>o.object.app===rootObject.app&&o.object.name===rootObject.name)?.labelField??"id";
 const initial=root&&rootInfo?{object:rootObject.name,record:root,info:rootInfo,labelField:rootLabel}:undefined;
 const currentPath=path?.key===rootKey?path.items:initial?[initial]:[],current=currentPath.at(-1),currentObject=config?.objects.find(o=>o.object.name===current?.object&&o.object.app===current?.info.app)?.object??rootObject;
 const frameKey=JSON.stringify([rootKey,currentPath.map(p=>[p.object,p.record.id,p.record.revision])]);life.current.frame=frameKey;
 const active=()=>life.current.active&&life.current.rootKey===rootKey&&life.current.frame===frameKey&&readCurrent&&isActive();
 const reader=useMemo(()=>createRecordExploration({source,definitions,active}),[source,definitions,frameKey,readCurrent]);
 useEffect(()=>{
  if(!readCurrent||status!=="value"||!current||!active())return;
  const reference={object:currentObject,id:current.record.id};
  if(kind==="graph-explorer"&&config)void reader.around(config,reference).then(frame=>{if(active())setResult({key:frameKey,frame});},failure=>{if(active())setResult({key:frameKey,error:failure instanceof Error?failure.message:"The original exploration could not be read."});});
  else if(kind==="vertex-graph"&&vertex)void reader.neighborhood(vertex,reference).then(neighborhood=>{if(active())setResult({key:frameKey,neighborhood});},failure=>{if(active())setResult({key:frameKey,error:failure instanceof Error?failure.message:"The original neighborhood could not be read."});});
 },[reader,frameKey,status,readCurrent]);
 const [relation,setRelation]=useState<{key:string;id:string}>();
 if(!readCurrent||status==="pending")return <p role="status">{t("Confirming record access…")}</p>;
 if(status==="error")return <Panel role="alert">{t("The original exploration root could not be confirmed.")}</Panel>;
 if(status!=="value"||!root||!current)return <p role="status">{t("Select an original record to explore its relationships.")}</p>;
 const frame=result?.key===frameKey?result:undefined;
 if(frame?.error)return <Panel role="alert">{t(frame.error)}</Panel>;
 if(kind==="vertex-graph"){
  if(!frame?.neighborhood)return <p role="status">{t("Loading original neighborhood…")}</p>;
  return <RecordNeighborhood root={currentPath[0]!} groups={frame.neighborhood.map(group=>({id:group.id,records:group.page.records,info:group.info,bindingTitle:group.bindingTitle,badge:group.badge,tone:group.tone as Parameters<typeof RecordNeighborhood>[0]["groups"][number]["tone"],total:group.page.total,limit:group.limit}))} label={label}/>;
 }
 if(!config)return <Panel role="alert">{t("The original exploration graph is unavailable.")}</Panel>;
 const rows=frame?.frame??[],choice=rows.find(r=>relation?.key===frameKey&&r.id===relation.id)??rows[0],info=choice?source.entity(choice.object.name):undefined,window=choice?.page&&info?{records:choice.page.records,info,labelField:choice.labelField,total:choice.page.total}:undefined;
 const busy=operation?.key===frameKey&&operation.pending===true,problem=operation?.key===frameKey?operation.error:undefined;
 const offered=(object:string,record:EntityRecord)=>active()&&enabled!==false&&choice?.object.name===object&&!!choice.page?.records.some(r=>r.id===record.id&&r.revision===record.revision);
 const run=async(object:string,record:EntityRecord,select:boolean)=>{
  if(busy||!offered(object,record)||!choice||!select&&currentPath.length>=8)return;
  setOperation({key:frameKey,pending:true});
  try{const confirmed=await reader.confirm({object:choice.object,id:record.id});if(!offered(object,record))return;
   if(select){const accepted=onOutput&&await onOutput(object,confirmed);if(active())setOperation(accepted?{key:frameKey}:{key:frameKey,error:"The original graph output could not be confirmed."});}
   else if(active()){setPath({key:rootKey,items:[...currentPath,{object,record:confirmed,info:reader.info(choice.object,choice.labelField),labelField:choice.labelField}]});setOperation({key:frameKey});}
  }catch(failure){if(active())setOperation({key:frameKey,error:failure instanceof Error?failure.message:"The original exploration record could not be confirmed."});}
  finally{if(active())setOperation(previous=>previous?.key===frameKey?{...previous,pending:false}:previous);}
 };
 return <div className="grid min-w-0 gap-2"><SearchAround path={currentPath} relations={rows.map(row=>({id:row.id,label:row.label,total:row.page?.total,error:row.error?t(row.error):undefined}))} activeRelation={choice?.id} window={window} pending={!frame} error={choice?.error?t(choice.error):undefined} onBack={index=>{if(active()&&enabled!==false&&!busy)setPath({key:rootKey,items:currentPath.slice(0,index+1)});}} onRelation={id=>{if(active()&&enabled!==false&&!busy)setRelation({key:frameKey,id});}} onTraverse={(object,record)=>void run(object,record,false)} onSelect={onOutput?(object,record)=>void run(object,record,true):undefined} selectionEnabled={!!choice&&!!config.outputs?.some(o=>o.object.app===choice.object.app&&o.object.name===choice.object.name)} selected={selected} selectionPending={busy} selectionError={problem?t(problem):undefined} enabled={enabled!==false&&!busy} label={label}/>{currentPath.length>=8&&<p role="status">{t("The exploration path reached its eight-record limit. Return to an earlier record to continue.")}</p>}</div>;
}
