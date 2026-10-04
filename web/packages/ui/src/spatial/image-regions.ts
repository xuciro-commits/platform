import {imageRegionLimits,type ImageRegion} from "@platform/kernel/host-contract";
export function validImageRegions(value:unknown):value is ImageRegion[]{
 if(!Array.isArray(value)||value.length>imageRegionLimits.maxRegions)return false;
 const seen=new Set<string>(),bytes=(text:string)=>new TextEncoder().encode(text).length;
 return value.every(r=>!!r&&typeof r==="object"&&!Object.keys(r).some(k=>!["id","label","x","y","width","height"].includes(k))&&typeof r.id==="string"&&!!r.id.trim()&&bytes(r.id)<=imageRegionLimits.maxIDBytes&&!seen.has(r.id)&&(seen.add(r.id),true)&&typeof r.label==="string"&&bytes(r.label)<=imageRegionLimits.maxLabelBytes&&[r.x,r.y,r.width,r.height].every(v=>typeof v==="number"&&Number.isFinite(v))&&r.x>=0&&r.y>=0&&r.width>0&&r.height>0&&r.x+r.width<=1&&r.y+r.height<=1);
}
