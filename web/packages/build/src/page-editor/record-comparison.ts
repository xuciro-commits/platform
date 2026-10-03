import type {Api} from "@platform/kernel";
import {loopOwner,overlayOwner} from "../page-layout";
import type {AuthoringSection} from "./draft";

/** Resolve the actual table output and its owner, never infer an object from labels. */
export function recordComparisonSource(document:Api.PageDocument,sections:AuthoringSection[],variable:string,consumer:string,pageObject:string):{object:string;producer:AuthoringSection}|undefined {
 const value=document.variables?.[variable],producer=sections.find(s=>s.id===value?.source?.section),leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===consumer)?.[0],origin=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===producer?.id)?.[0];
 if(!leaf||!origin||!value||value.type!=="record-set"||value.mode!=="resource"||value.source?.kind!=="records"||producer?.widget!=="table"||producer.selectionSetVariable!==variable||loopOwner(document,leaf)||loopOwner(document,origin))return undefined;
 const owner=overlayOwner(document,leaf),producerOwner=overlayOwner(document,origin);
 if(owner!==producerOwner||value.scope!==(owner?"overlay":"page")||value.owner!==owner)return undefined;
 const object=producer.object||pageObject,collection=document.variables?.[producer.collectionVariable??""],query=document.queries?.[collection?.source?.query??""];
 if(collection&&(collection.mode!=="resource"||collection.source?.kind!=="plan"||!query||query.object.name!==object))return undefined;
 return {object,producer};
}
