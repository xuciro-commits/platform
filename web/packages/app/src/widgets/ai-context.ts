import type {Api} from '@platform/kernel';
import {recordResourceSlot} from '../runtime/resources';
/** Only the explicit AI profile admits an original application record input. */
export function aiRecordSlot(page:Api.Page,variable:string|undefined):string|undefined {
 const v=page.document?.variables?.[variable??''];
 if(variable&&Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??'')?.[1])>=91&&v?.type==='record'&&v.mode==='shared'&&v.scope==='application'&&v.source?.kind==='application'&&v.source.object?.kind==='object')return `input/${variable}`;
 return recordResourceSlot(page,variable);
}
