import type {Api,ActionDeclaration} from "@platform/kernel";
import type {EntityInfo} from "@platform/ui";
import {actionTableParameters} from "@platform/app/action-table";
import {loopOwner,overlayOwner} from "../page-layout";
import type {AuthoringSection} from "./draft";
export function recordWorkProblem(document:Api.PageDocument,s:AuthoringSection,pageObject:string,entity:(name:string)=>EntityInfo|undefined,catalog:ActionDeclaration[],definitions:Api.Definition[]):boolean {
 const tiles=s.recordList?.layout==="tiles";if(!["action-table","notepad"].includes(s.widget)&&!tiles)return !!s.actionTable||!!s.notepadVariable;
 const leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0],owner=leaf?overlayOwner(document,leaf):undefined;
 if(!leaf||loopOwner(document,leaf)||Number(document.uiProfile.split(".").at(-1))<81)return true;
 if(s.widget==="notepad"){const v=document.variables?.[s.notepadVariable??""];return v?.type!=="string"||v.mode!=="state"||v.scope!==(owner?"overlay":"page")||v.owner!==owner||!!s.collectionVariable||!!s.recordVariable||!!s.selection||!!s.actions?.length||!!s.fields?.length;}
 const v=document.variables?.[s.collectionVariable??""],q=document.queries?.[v?.source?.query??""],object=s.object||pageObject,info=entity(object),limit=tiles?8:50;if(v?.type!=="object-set"||v.mode!=="resource"||v.source?.kind!=="plan"||v.scope!==(owner?"overlay":"page")||v.owner!==owner||!q||q.owner!==owner||q.itemOwner||q.object.name!==object||q.object.app!==info?.app||q.limit!==limit||(q.offset??0)!==0||JSON.stringify(q.sort)!==JSON.stringify(["id"]))return true;
 if(q.query){const d=definitions.find(d=>d.ref.app===q.query!.ref.app&&d.ref.kind===q.query!.ref.kind&&d.ref.name===q.query!.ref.name),named=d?.queryVersions?.[q.query.sourceVersion]??(d?.version===q.query.sourceVersion?d.query:undefined);if(!named||named.sort?.length&&JSON.stringify(named.sort)!==JSON.stringify(["id"])||named.limit&&named.limit<limit)return true;}
 if(tiles)return s.widget!=="record-list"||!!s.fields?.length||!!s.actions?.length||s.cardLabel!=="id"&&!info?.fields.some(f=>f.name===s.cardLabel&&["text","longtext","choice","reference"].includes(f.type));
 return !s.actionTable||s.actions?.length!==1||!!s.selection||!!s.recordVariable||!!s.selectionVariable||!!s.selectionSetVariable||!!s.inlineEdit||!!s.function||!!s.operation||!!s.fields?.length||!!s.filterVariable||!!s.parentSelection||!!s.relation||!!s.query||!!Object.keys(s.inputs??{}).length||!actionTableParameters(info,catalog.find(a=>a.schema===s.actions?.[0]),s.actionTable.parameters);
}
