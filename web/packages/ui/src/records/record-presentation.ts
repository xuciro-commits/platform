import type {EntityInfo,EntityRecord} from "./Records";

export type OriginalRecordTitle={object:string;record:EntityRecord;info:EntityInfo;labelField:string};
export const recordIdentity=(object:string,id:string)=>encodeURIComponent(JSON.stringify([object,id]));
export function originalRecordTitle({object,record,info,labelField}:OriginalRecordTitle):string|undefined {
 if(!record||typeof record.id!=="string"||!record.id||record.archived===true||!Number.isSafeInteger(record.revision)||record.revision<0||!info||info.type!==object||!Array.isArray(info.fields))return;
 const field=info.fields.find(field=>field.name===labelField);
 if(labelField!=="id"&&(!field||!["text","longtext","choice","reference"].includes(field.type)))return;
 const raw=labelField==="id"?record.id:record[labelField];
 return raw==null?record.id:typeof raw==="string"?raw:undefined;
}
