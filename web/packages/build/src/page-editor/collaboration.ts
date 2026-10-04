import {originalRecordObject} from "./record-binding";
import type {Api} from "@platform/kernel";
import {loopOwner,overlayOwner} from "../page-layout";
import type {AuthoringSection} from "./draft";

export const collaborationWidgets=["record-comments","record-uploader","media-preview","pdf-viewer","image-annotation","scene-3d"] as const;
export const isCollaborationWidget=(widget:string)=>collaborationWidgets.some(value=>value===widget);
export const requiresOriginalRecord=(section:AuthoringSection)=>isCollaborationWidget(section.widget)||["breadcrumb","record-card","graph-explorer","vertex-graph"].includes(section.widget)||section.widget==="timeline"&&(section.historyLimit??0)>0;

/** The shared record resource remains attached to its actual compatible producer. */
export function collaborationRecordSource(document:Api.PageDocument,sections:AuthoringSection[],variable:string,consumer:string,pageObject:string):{object:string;producer?:AuthoringSection}|undefined {
 const value=document.variables?.[variable],producer=sections.find(s=>s.id===value?.source?.section),leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===consumer)?.[0],origin=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===producer?.id)?.[0];
 const widget=sections.find(s=>s.id===consumer)?.widget;
 if(leaf&&!loopOwner(document,leaf)&&(['breadcrumb','graph-explorer','record-card'].includes(widget??'')||widget==='vertex-graph'&&Number(document.uiProfile.split('.').at(-1))>=102)&&Number(document.uiProfile.split('.').at(-1))>=98&&value?.type==='record'&&value.mode==='shared'&&value.scope==='application'&&value.source?.kind==='application'&&value.source.object)return {object:value.source.object.name};
 if(!leaf||!origin||value?.type!=="record"||value.mode!=="resource"||value.source?.kind!=="record"||!producer||!["table","record-list","kanban","record-calendar","record-picker","record-leaderboard","record-scatter","record-map","resource-list","graph-explorer","observation"].includes(producer.widget)||loopOwner(document,leaf)||loopOwner(document,origin))return undefined;
 const owner=overlayOwner(document,leaf);
 if(owner!==overlayOwner(document,origin)||value.scope!==(owner?"overlay":"page")||value.owner!==owner)return undefined;
 if(producer.widget==="observation"||producer.widget==="graph-explorer"){if(producer.id===consumer)return undefined;const original=originalRecordObject(document,sections,variable,{app:pageObject.split(".")[0]!,kind:"object",name:pageObject});return original?{object:original.name,producer}:undefined;}
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
