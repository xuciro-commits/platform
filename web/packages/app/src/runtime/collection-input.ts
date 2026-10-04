import {pageUIManifest} from "@platform/kernel/host-contract";
import type {Api} from "@platform/kernel";
import type {EntityInfo,RecordQuery} from "@platform/ui";
import {isDecimal} from "./decimal";

const object=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==="object"&&!Array.isArray(value);
const id=/^[A-Za-z][A-Za-z0-9._:-]{0,79}$/;
const bytes=(v:string)=>new TextEncoder().encode(v).length;
export type CollectionInput=Omit<Api.PageCollectionInput,"kind">&{kind:"object-set-input"};
const scalar=(v:unknown):boolean=>typeof v==="boolean"||typeof v==="number"&&Number.isFinite(v)||typeof v==="string"&&bytes(v)<=4096||isDecimal(v);
const ref=(v:unknown,kind?:string):v is Api.AssetRef=>object(v)&&Object.keys(v).every(k=>["app","kind","name"].includes(k))&&typeof v.app==="string"&&id.test(v.app)&&typeof v.name==="string"&&id.test(v.name)&&(!kind||v.kind===kind);
export function validCollectionInput(value:unknown):value is CollectionInput {
 if(!object(value)||Object.keys(value).some(k=>!["kind","object","predicate","sort","sortLocked","traversal","bindings"].includes(k))||value.kind!=="object-set-input"||!ref(value.object,"object")||!Array.isArray(value.sort)||value.sort.length<1||value.sort.length>pageUIManifest.runtime.query.maxSort||value.sort.some(k=>typeof k!=="string"||!id.test(k.replace(/^-/,"")))||value.sortLocked!==undefined&&typeof value.sortLocked!=="boolean")return false;
 let nodes=0;
 const predicate=(p:unknown,depth:number):boolean=>{
  if(!object(p)||++nodes>pageUIManifest.runtime.query.set.maxNodes||depth>pageUIManifest.runtime.query.set.maxDepth||Object.keys(p).some(k=>!["domain","search","set"].includes(k))||p.search!==undefined&&(typeof p.search!=="string"||bytes(p.search)>4096))return false;
  if(p.domain!==undefined&&(!Array.isArray(p.domain)||p.domain.length>pageUIManifest.runtime.query.maxConditions||p.domain.some(t=>typeof t==="string"?!["&","|","!"].includes(t):!Array.isArray(t)||t.length!==3||typeof t[0]!=="string"||!id.test(t[0])||!(pageUIManifest.runtime.query.operators as readonly unknown[]).includes(t[1])||(Array.isArray(t[2])?t[2].length>64||t[2].some(v=>!scalar(v)):!scalar(t[2])))))return false;
  if(p.set!==undefined&&(!object(p.set)||Object.keys(p.set).some(k=>!["op","inputs"].includes(k))||!["union","intersect","subtract"].includes(String(p.set.op))||!Array.isArray(p.set.inputs)||p.set.inputs.length!==2||!p.set.inputs.every(child=>predicate(child,depth+1))))return false;
  return true;
 };
 if(!predicate(value.predicate,0))return false;
 const binding=(b:unknown):b is Api.AssetBinding=>object(b)&&Object.keys(b).every(k=>["ref","sourceVersion"].includes(k))&&ref(b.ref)&&["query","link-type"].includes(String(b.ref.kind))&&typeof b.sourceVersion==="string"&&b.sourceVersion.length>0&&bytes(b.sourceVersion)<=256;
 if(value.bindings!==undefined&&(!Array.isArray(value.bindings)||value.bindings.length>31||!value.bindings.every(binding)||new Set(value.bindings.map(b=>JSON.stringify([b.ref.app,b.ref.kind,b.ref.name]))).size!==value.bindings.length))return false;
 if(value.traversal!==undefined&&(!object(value.traversal)||Object.keys(value.traversal).some(k=>!["binding","direction","id"].includes(k))||!binding(value.traversal.binding)||value.traversal.binding.ref.kind!=="link-type"||!["forward","reverse"].includes(String(value.traversal.direction))||typeof value.traversal.id!=="string"||!value.traversal.id||bytes(value.traversal.id)>1024))return false;
 try{return bytes(JSON.stringify(value))<=65536;}catch{return false;}
}

/** Recheck inherited fields against the receiving member's current schema. */
export function collectionFieldsVisible(input:Api.PageCollectionInput,info:EntityInfo|undefined):boolean{
 if(!info||info.type!==input.object.name)return false;
 const field=(name:string)=>["id","created","changed"].includes(name)||info.fields.some(f=>f.name===name);
 const walk=(p:Api.RecordSetPredicate):boolean=>(!Array.isArray(p.domain)||p.domain.every(t=>!Array.isArray(t)||field(String(t[0]))))&&(!p.set||p.set.inputs.every(child=>!!child&&walk(child)));
 return input.sort.every(k=>field(k.replace(/^-/,"")))&&walk(input.predicate);
}

export function collectionInput(object:Api.AssetRef,query:RecordQuery,bindings:Api.AssetBinding[]=[],sortLocked=false):CollectionInput {
 return {kind:"object-set-input",object,predicate:{domain:query.domain??[],...query.search?{search:query.search}:{},...query.set?{set:query.set}:{}},sort:query.sort??["id"],sortLocked,...query.traversal?{traversal:query.traversal}:{},bindings};
}

/** The child owns its window. All inherited membership constraints remain. */
export function collectionQuery(input:Api.PageCollectionInput,local:RecordQuery,explicitSort?:string[]):RecordQuery|undefined{
 if(input.sortLocked&&explicitSort?.length&&JSON.stringify(explicitSort)!==JSON.stringify(input.sort))return;
 const query:RecordQuery={...local,...input.traversal?{traversal:input.traversal}:{},sort:explicitSort?.length?explicitSort:input.sort};
 const own={domain:local.domain??[],...local.search?{search:local.search}:{},...local.set?{set:local.set}:{}};
 if(local.search&&input.predicate.search){query.domain=[];delete query.search;query.set={op:"intersect",inputs:[input.predicate,own]};}
 else{query.domain=[...(Array.isArray(input.predicate.domain)?input.predicate.domain:[]),...(local.domain??[])];query.search=input.predicate.search||local.search;query.set=input.predicate.set??local.set;}
 if(input.predicate.set&&local.set){query.domain=[];delete query.search;query.set={op:"intersect",inputs:[input.predicate,own]};}
 return validCollectionInput(collectionInput(input.object,query,input.bindings,input.sortLocked))?query:undefined;
}
