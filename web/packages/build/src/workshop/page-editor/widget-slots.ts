import type {Api} from "@platform/kernel";
import {widgetContract} from "@platform/app";
import {layoutID} from "../page-layout";
type Document=Api.PageDocument;

export function addWidgetSlot(document:Document,section:string,widget:string,slotID:string):{document:Document;id?:string}{
 const leaf=Object.keys(document.nodes).find(id=>document.nodes[id]?.kind==="widget"&&document.nodes[id]?.section===section),contract=widgetContract(widget),slot=contract&&"slots" in contract?contract.slots.find(s=>s.id===slotID):undefined;
 if(!leaf||!slot||Number(document.uiProfile.split('.').at(-1))<Number(slot.requiredUIProfile.split('.').at(-1)))return {document};
 const existing=document.nodes[leaf]?.children?.find(id=>document.nodes[id]?.slot===slotID);if(existing)return {document,id:existing};
 const next=structuredClone(document),id=layoutID("slot");next.nodes[id]={kind:slotID==="toolbar"?"toolbar":"flow",slot:slotID,children:[]};next.nodes[leaf]!.children=[...(next.nodes[leaf]!.children??[]),id];return {document:next,id};
}
