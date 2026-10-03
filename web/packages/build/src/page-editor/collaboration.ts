import type {Api} from "@platform/kernel";
import {loopOwner,overlayOwner} from "../page-layout";
import type {AuthoringSection} from "./draft";

export const collaborationWidgets=["record-comments","record-uploader","media-preview","pdf-viewer"] as const;
export const isCollaborationWidget=(widget:string)=>collaborationWidgets.some(value=>value===widget);
export const requiresOriginalRecord=(section:AuthoringSection)=>isCollaborationWidget(section.widget)||section.widget==="breadcrumb"||section.widget==="timeline"&&(section.historyLimit??0)>0;

/** The shared record resource remains attached to its actual compatible producer. */
export function collaborationRecordSource(document:Api.PageDocument,sections:AuthoringSection[],variable:string,consumer:string,pageObject:string):{object:string;producer:AuthoringSection}|undefined {
 const value=document.variables?.[variable],producer=sections.find(s=>s.id===value?.source?.section),leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===consumer)?.[0],origin=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===producer?.id)?.[0];
 if(!leaf||!origin||value?.type!=="record"||value.mode!=="resource"||value.source?.kind!=="record"||!producer||!["table","record-list","kanban","record-calendar","record-picker","record-leaderboard","record-scatter"].includes(producer.widget)||loopOwner(document,leaf)||loopOwner(document,origin))return undefined;
 const owner=overlayOwner(document,leaf);
 if(owner!==overlayOwner(document,origin)||value.scope!==(owner?"overlay":"page")||value.owner!==owner)return undefined;
 const object=producer.object||pageObject,collection=document.variables?.[producer.collectionVariable??""],query=document.queries?.[collection?.source?.query??""];
 if(collection){const target=query?.object||collection.source?.object;if(!(collection.mode==="resource"&&collection.source?.kind==="plan"||collection.mode==="shared"&&collection.type==="object-set")||!target||target.name!==object)return undefined;}
 return {object,producer};
}

/** An explicitly edited window must stay bounded; clearing it preserves legacy history. */
export function historyViewProblem(document:Api.PageDocument,sections:AuthoringSection[],section:AuthoringSection,pageObject:string):boolean {
 if(section.historyLimit===undefined)return false;
 if(section.widget!=="timeline"||!Number.isSafeInteger(section.historyLimit)||section.historyLimit<1||section.historyLimit>100)return true;
 const original=collaborationRecordSource(document,sections,section.recordVariable??"",section.id??"",pageObject);
 return !original||(section.object||pageObject)!==original.object||!!section.fields?.length||!!section.actions?.length||!!section.selection||!!section.collectionVariable||!!section.selectionVariable||!!section.selectionSetVariable||!!section.filterVariable||!!section.recordSetVariable||!!section.parentSelection||!!section.relation||!!section.query||!!section.inlineEdit;
}
