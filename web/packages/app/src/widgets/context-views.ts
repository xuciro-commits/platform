import type {Api} from "@platform/kernel";
import type {PageSessionStore,PageSessionSnapshot} from "../runtime/Session";
import {recordResourceSlot,sectionOverlay} from "../runtime/resources";

export const currentContextRead=(hostScope:string|undefined,readScope:string|undefined)=>hostScope===readScope;

/** Resolve the actual original selection producer, never a resource copy or guessed ID. */
export function originalContextSlot(page:Api.Page,section:Api.Section,variable:string|undefined):string|undefined {
 const v=page.document?.variables?.[variable??""];
 if(!variable||v?.type!=="record"||v.mode!=="resource"||v.source?.kind!=="record")return;
 const producer=page.sections?.find(s=>s.id===v.source?.section);
 if(!producer||!["table","record-list","resource-list","graph-explorer","record-timeline","kanban","record-calendar","record-picker","record-leaderboard","record-scatter"].includes(producer.widget)||sectionOverlay(page,section.id??"")!==sectionOverlay(page,producer.id??""))return;
 return recordResourceSlot(page,variable);
}
export function confirmedContext(page:Api.Page,section:Api.Section,variable:string|undefined,session:PageSessionStore,snapshot:PageSessionSnapshot) {
 const slot=originalContextSlot(page,section,variable);
 if(!slot)return {status:"error" as const};
 const status=snapshot.records[slot]?.status??"empty",record=session.confirmedSelected(slot);
 return {slot,status:status==="value"&&!record?"pending" as const:status,record};
}
export function avatarCollectionVariable(config:Api.PageAvatarStack|undefined,status:"empty"|"pending"|"value"|"error"|undefined,allVariable:string|undefined):string|undefined {
 if(!config?.contextVariable)return allVariable;
 if(status==="empty")return allVariable;
 if(status==="value")return config.contextCollectionVariable;
 return;
}
