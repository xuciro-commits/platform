import type {Api} from "@platform/kernel";
import type {EntityInfo,EntityRecord} from "../records/Records";

export type SceneInput={record:EntityRecord;info:EntityInfo};
export type SceneMapping=Api.PageSceneMapping;
export type SceneLayer=Api.PageSceneLayer;
export type SceneConfig=Api.PageSceneConfig;
export const sceneLimits={maxBytes:16<<20,maxJSONBytes:1<<20,maxNodes:512,maxMeshes:256,maxMaterials:128,maxVertices:250000,maxIndices:1500000,maxTexturePixels:16000000,maxLayers:32,maxMappings:64} as const;
const hex=(color?:string)=>color===undefined||/^#[\da-fA-F]{6}$/.test(color);
export function validSceneConfig(c:SceneConfig):boolean{
 if(!c||!["light","dark"].includes(c.background)||!["performance","balanced","high"].includes(c.quality)||typeof c.showGrid!=="boolean"||!Array.isArray(c.layers)||!Array.isArray(c.mappings)||c.layers.length>sceneLimits.maxLayers||c.mappings.length>sceneLimits.maxMappings)return false;
 const ids=new Set<string>(),drives=new Set<string>();
 const bytes=(v:string)=>new TextEncoder().encode(v).length;
 const id=(v:string)=>typeof v==="string"&&!!v.trim()&&bytes(v)<=128&&!ids.has(v)&&(ids.add(v),true),name=(v:string)=>typeof v==="string"&&!!v.trim()&&bytes(v)<=256;
 return c.layers.every(l=>!!l&&id(l.id)&&name(l.name)&&Array.isArray(l.nodes)&&l.nodes.length>0&&l.nodes.length<=sceneLimits.maxNodes&&l.nodes.every(name)&&new Set(l.nodes).size===l.nodes.length&&typeof l.visible==="boolean"&&typeof l.wireframe==="boolean"&&Number.isFinite(l.opacity)&&l.opacity>=0&&l.opacity<=1&&hex(l.color))&&c.mappings.every(m=>{
  if(!m)return false;
  const drive=JSON.stringify([m.node,m.mode,["color","visibility"].includes(m.mode)?undefined:m.axis]);
  if(!m||!id(m.id)||!name(m.node)||!name(m.field)||!["asset","sample"].includes(m.source)||!["rotation","position","scale","color","visibility"].includes(m.mode)||!["x","y","z"].includes(m.axis)||![m.inputMin,m.inputMax,m.outputMin,m.outputMax].every(n=>Number.isFinite(n)&&Math.abs(n)<=1e9)||m.inputMin>=m.inputMax||drives.has(drive)||typeof m.enabled!=="boolean"||!hex(m.colorLow)||!hex(m.colorHigh)||m.smooth!==undefined&&(!Number.isFinite(m.smooth)||m.smooth<.02||m.smooth>1.5)||m.threshold!==undefined&&!Number.isFinite(m.threshold))return false;
  drives.add(drive);return true;
 });
}
/** Numeric original fields only. A sample must actually reference the selected asset. */
export function sceneMappingValue(mapping:SceneMapping,asset:SceneInput,sample?:SceneInput,sampleAssetField?:string){
 let input=asset;
 if(mapping.source==="sample"){
  const field=sample?.info.fields.find(f=>f.name===sampleAssetField);
  if(!sample||!field||field.type!=="reference"||field.ref!==asset.info.type||sample.record[field.name]!==asset.record.id)return;
  input=sample;
 }
 const field=input.info.fields.find(f=>f.name===mapping.field),raw=input.record[mapping.field];
 if(!field||!["integer","decimal"].includes(field.type)||typeof raw!=="number"||!Number.isFinite(raw)||field.type==="integer"&&!Number.isSafeInteger(raw))return;
 const normalised=Math.max(0,Math.min(1,(raw-mapping.inputMin)/(mapping.inputMax-mapping.inputMin)));
 return {raw,normalised,value:mapping.outputMin+normalised*(mapping.outputMax-mapping.outputMin)};
}
