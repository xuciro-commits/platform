import type {RecordPageData} from '@platform/ui';
type Window={query:unknown;page?:RecordPageData;error?:string};
export type RetainedWindowView={key:string;page:RecordPageData};
/** This is presentation retention only. No unavailable window becomes actionable. */
export function retainedWindowView(previous:RetainedWindowView|undefined,identity:string|undefined,window:Window|undefined):RetainedWindowView|undefined{
 if(identity===undefined||!window||window.error)return;
 const key=JSON.stringify([identity,window.query]);
 return window.page?{key,page:window.page}:previous?.key===key?previous:undefined;
}
