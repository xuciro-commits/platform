import type {Api} from "@platform/kernel";
import type {EntityInfo,EntityRecord,RecordPageData,RecordSource} from "@platform/ui";

export type RecordExplorationReader={source:RecordSource;definitions:readonly Api.Definition[];active:()=>boolean};
export type ExplorationReference={object:Api.AssetRef;id:string};
export type ExplorationRelation={id:string;label:string;binding:Api.AssetBinding;direction:"forward"|"reverse";object:Api.AssetRef;labelField:string};
export type ExplorationWindow=ExplorationRelation&{page?:RecordPageData;error?:string};
const same=(a:Api.AssetRef,b:Api.AssetRef)=>a.app===b.app&&a.kind===b.kind&&a.name===b.name;
const error=(failure:unknown)=>failure instanceof Error?failure.message:"The original relation could not be read.";
function originalRecord(record:EntityRecord,id?:string):EntityRecord {
 if(!record||typeof record.id!=="string"||!record.id||id!==undefined&&record.id!==id||record.archived===true||!Number.isSafeInteger(record.revision)||record.revision<0)throw Error("The original exploration record is unavailable or incompatible.");
 return record;
}
function originalWindow(page:RecordPageData,limit:number):RecordPageData {
 if(!page||!Array.isArray(page.records)||page.records.length>limit||!Number.isSafeInteger(page.total)||page.total<page.records.length||new Set(page.records.map(r=>r?.id)).size!==page.records.length)throw Error("The original relation window is unavailable or incompatible.");
 page.records.forEach(r=>originalRecord(r));return page;
}
function originalLink(definitions:readonly Api.Definition[],binding:Api.AssetBinding):Api.LinkType {
 const d=definitions.find(d=>same(d.ref,binding.ref)),link=d?.version===binding.sourceVersion?d.linkType:d?.linkVersions?.[binding.sourceVersion];
 if(binding.ref.kind!=="link-type"||!binding.sourceVersion||!link)throw Error("The original relationship version is unavailable.");
 return link;
}
function originalInfo(source:RecordSource,object:Api.AssetRef,labelField="id"):EntityInfo {
 const info=source.entity(object.name),title=info?.fields.find(f=>f.name===labelField);
 if(object.kind!=="object"||!info||info.type!==object.name||info.app!==object.app||labelField!=="id"&&(!title||!["text","longtext","choice","reference"].includes(title.type)))throw Error("The original exploration object or title is unavailable.");
 return info;
}
function originalLinkFields(source:RecordSource,link:Api.LinkType) {
 originalInfo(source,link.parent);const child=originalInfo(source,link.child),via=child.fields.find(f=>f.name===link.via);
 if(!via||via.type!=="reference"||via.ref!==link.parent.name)throw Error("The original relationship field is unavailable.");
}
export function explorationRelations(source:RecordSource,definitions:readonly Api.Definition[],config:Api.PageGraphExplorer,current:ExplorationReference):ExplorationRelation[] {
 if(config.objects.length>8||config.relations.length>12||!config.objects.some(o=>same(o.object,current.object)))throw Error("The original exploration graph is unavailable or exceeds its budget.");
 originalInfo(source,current.object,config.objects.find(o=>same(o.object,current.object))!.labelField);
 const result:ExplorationRelation[]=[];
 for(const relation of config.relations){
  const link=originalLink(definitions,relation.binding);originalLinkFields(source,link);
  for(const direction of ["forward","reverse"] as const){const start=direction==="forward"?link.parent:link.child,target=direction==="forward"?link.child:link.parent;if(!same(start,current.object))continue;
   const mapped=config.objects.find(o=>same(o.object,target));if(!mapped)throw Error("The original relationship target is outside the declared graph.");originalInfo(source,target,mapped.labelField);
   result.push({id:JSON.stringify([relation.id,relation.binding.ref,relation.binding.sourceVersion,direction,target]),label:direction==="forward"?link.forward:link.reverse,binding:relation.binding,direction,object:target,labelField:mapped.labelField});
  }
 }
 if(new Set(result.map(r=>r.id)).size!==result.length)throw Error("The original exploration relation identities are duplicated.");
 return result;
}

/** A bounded facade over the original authorized record/traversal readers, with no business joins. */
export function createRecordExploration(reader:RecordExplorationReader) {
 const scope=reader.source.scope,requireActive=()=>{if(!reader.active()||reader.source.scope!==scope)throw Error("The original exploration scope has ended.");};
 const windows=new Map<string,Promise<RecordPageData>>();let revision=reader.source.revision;
 const read=async(relation:ExplorationRelation,current:ExplorationReference,limit:number)=>{
  requireActive();if(revision!==reader.source.revision){windows.clear();revision=reader.source.revision;}
  const readRevision=reader.source.revision,key=JSON.stringify([relation.binding,relation.direction,current,relation.object,limit]);let promise=windows.get(key);
  if(!promise){promise=reader.source.list(relation.object.name,{traversal:{binding:relation.binding,direction:relation.direction,id:current.id},sort:["id"],offset:0,limit}).then(page=>{requireActive();if(reader.source.revision!==readRevision)throw Error("The original relation changed. Read it again.");return originalWindow(page,limit);}).catch(failure=>{if(windows.get(key)===promise)windows.delete(key);throw failure;});windows.set(key,promise);}
  const page=await promise;requireActive();if(reader.source.revision!==readRevision)throw Error("The original relation changed. Read it again.");return page;
 };
 return {
  info:(object:Api.AssetRef,labelField="id")=>originalInfo(reader.source,object,labelField),
  async confirm(reference:ExplorationReference){requireActive();originalInfo(reader.source,reference.object);const view=await reader.source.get(reference.object.name,reference.id);requireActive();return originalRecord(view.record,reference.id);},
  async around(config:Api.PageGraphExplorer,current:ExplorationReference):Promise<ExplorationWindow[]>{
   requireActive();const relations=explorationRelations(reader.source,reader.definitions,config,current);
   return await Promise.all(relations.map(async relation=>{try{return {...relation,page:await read(relation,current,20)};}catch(failure){requireActive();return {...relation,error:error(failure)};}}));
  },
  async neighborhood(config:Api.PageVertexGraph,current:ExplorationReference){
   requireActive();if(config.groups.length>2||new Set(config.groups.map(g=>g.badge)).size!==config.groups.length||config.groups.some(g=>g.direction!=="forward"||!(g.badge==="S"&&g.limit===4||g.badge==="A"&&g.limit===3)))throw Error("The original neighborhood exceeds its declared budget.");
   return await Promise.all(config.groups.map(async group=>{const link=originalLink(reader.definitions,group.binding);originalLinkFields(reader.source,link);const start=group.direction==="forward"?link.parent:link.child,target=group.direction==="forward"?link.child:link.parent;if(!same(start,current.object))throw Error("The neighborhood relationship has a different original root.");
    const relation:ExplorationRelation={id:group.id,label:group.direction==="forward"?link.forward:link.reverse,binding:group.binding,direction:group.direction as "forward"|"reverse",object:target,labelField:"id"};
    return {...group,bindingTitle:relation.label,object:target,info:originalInfo(reader.source,target),page:await read(relation,current,group.limit)};
   }));
  },
 };
}

/** Resolve fixed metadata only; discovery never implies execution or navigation authority. */
export function originalDirectoryItems(config:Api.PageAssetDirectory,definitions:readonly Api.Definition[]) {
 return config.items.flatMap(item=>{const d=definitions.find(d=>same(d.ref,item.asset.ref));if(!d)return [];
  const retained=item.asset.ref.kind==="query"?d.queryVersions?.[item.asset.sourceVersion]:item.asset.ref.kind==="link-type"?d.linkVersions?.[item.asset.sourceVersion]:item.asset.ref.kind==="property-type"?d.propertyVersions?.[item.asset.sourceVersion]:undefined;
  if(d.version!==item.asset.sourceVersion&&!retained)return [];
  const title=retained?.title??d.entity?.title??d.page?.title??d.query?.title??d.linkType?.title??d.action?.title??d.application?.title??d.function?.title??d.operation?.title??d.propertyType?.title??d.ref.name;
  return [{id:item.id,label:item.label,asset:item.asset,title}];
 });
}
