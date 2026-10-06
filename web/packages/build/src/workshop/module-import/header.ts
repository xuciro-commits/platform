import type {Api} from "@platform/kernel";
import {validStaticImage} from "@platform/ui/static-image";

/** Header authoring stays in the application; source event code is never loaded. */
export function importApplicationHeader(value:unknown,destinations:{sourcePage:string;name:string}[]):Api.ApplicationHeader|undefined{
 const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==="object"&&!Array.isArray(v),keys=(v:object,allowed:string[])=>Object.keys(v).every(k=>allowed.includes(k)),text=(v:unknown)=>typeof v==="string"&&new TextEncoder().encode(v).length<=1024;
 if(!object(value)||!keys(value,["variant","title","logo","collapsed","items"])||!["horizontal","vertical"].includes(String(value.variant))||!text(value.title)||value.logo!==undefined&&(typeof value.logo!=="string"||value.logo&&!validStaticImage({url:value.logo}))||value.collapsed!==undefined&&typeof value.collapsed!=="boolean"||value.collapsed===true&&value.variant!=="vertical"||!Array.isArray(value.items)||!value.items.length||value.items.length>24)return;
 const items:Api.ApplicationHeaderItem[]=[],used=new Set<string>();
 for(const raw of value.items){
  if(!object(raw)||typeof raw.kind!=="string")return;
  if(["logo","title","spacer"].includes(raw.kind)){if(!keys(raw,["kind"]))return;items.push({kind:raw.kind});}
  else if(raw.kind==="text"){if(!keys(raw,["kind","text"])||!text(raw.text))return;items.push({kind:"text",text:raw.text as string});}
  else if(raw.kind==="tabs"){
   if(!keys(raw,["kind","pageIds"])||!Array.isArray(raw.pageIds)||!raw.pageIds.length||raw.pageIds.length>32)return;
   const pages:string[]=[];for(const id of raw.pageIds){const destination=destinations.find(d=>d.sourcePage===id);if(!destination||used.has(destination.name))return;used.add(destination.name);pages.push(destination.name);}items.push({kind:"tabs",pages});
  }else if(raw.kind==="button"){
   if(!keys(raw,["kind","label","eventActions"])||!text(raw.label)||!raw.label||!Array.isArray(raw.eventActions)||raw.eventActions.length!==1)return;
   const event=raw.eventActions[0];if(!object(event)||!keys(event,["kind"])||!["refreshData","toggleTheme"].includes(String(event.kind)))return;
   items.push({kind:"button",label:raw.label as string,action:event.kind==="refreshData"?"refresh":"theme"});
  }else return;
 }
 return {variant:String(value.variant),title:value.title as string,logo:value.logo as string|undefined,collapsed:value.collapsed as boolean|undefined,items};
}
