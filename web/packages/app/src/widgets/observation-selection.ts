import type {Api} from '@platform/kernel';
import type {EntityRecord} from '@platform/ui';
import type {PageSessionStore,RecordReference} from '../runtime/Session';
import {recordOutputSlot} from '../runtime/resources';

/** The row and referenced asset each need original authorization before publication. */
export async function confirmObservationRow(page:Api.Page,section:Api.Section,record:EntityRecord,session:PageSessionStore,query:string,active:()=>boolean,begin?:()=>((reference:RecordReference|undefined)=>void)|undefined):Promise<boolean>{
 const c=section.observation;if(c?.kind!=='table'||!c.rowOutput||!active())return false;
 const complete=begin?.(),rowSlot=recordOutputSlot(page,section,'row'),assetSlot=recordOutputSlot(page,section,'asset');
 let published=false;
 try{
  const confirmed=await session.confirmSelection(rowSlot,record,query);if(!confirmed||!active())return false;
  if(!c.assetOutput||!c.assetObject)return true;
  const lease=session.recordBindingEpoch(rowSlot),id=confirmed[c.assetField??''];session.select(assetSlot,undefined);
  if(typeof id!=='string'||!id)return false;
  const pending=session.selectReference(assetSlot,{object:c.assetObject.name,id}),assetLease=session.recordBindingEpoch(assetSlot),asset=await pending;
  if(!active()||session.recordBindingEpoch(rowSlot)!==lease){if(session.recordBindingEpoch(assetSlot)===assetLease)session.select(assetSlot,undefined);return false;}
  if(!asset||session.confirmedSelected(assetSlot)?.id!==asset.id)return false;
  if(complete){complete({object:c.assetObject.name,id:asset.id});published=true;}
  return true;
 }finally{if(complete&&!published)complete(undefined);}
}
