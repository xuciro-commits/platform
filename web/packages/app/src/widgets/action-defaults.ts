import type {Api} from '@platform/kernel';
type Field={name:string;type:string;ref?:string};type Parameter={name:string;type?:string;ref?:string;from?:string;choices?:string[]};
export function compatibleActionField(f:Field,p:Parameter):boolean {
 if(p.from)return false;
 return p.type==='string'?p.ref?f.type==='reference'&&f.ref===p.ref:['text','longtext','choice'].includes(f.type):p.type==='number'?['integer','decimal'].includes(f.type):['integer','boolean','date','datetime'].includes(p.type??'')&&f.type===p.type;
}
export function validActionDefaults(info:{type:string;fields:Field[]}|undefined,action:{target:string;new?:boolean;payload?:Parameter[]}|undefined,mappings:Api.PageActionParameter[]):boolean {
 return !!info&&!!action&&action.target===info.type&&!action.new&&mappings.length<=16&&mappings.every(m=>!!m&&typeof m.parameter==='string'&&typeof m.field==='string')&&new Set(mappings.map(m=>m.parameter)).size===mappings.length&&mappings.every(m=>{const f=info.fields.find(f=>f.name===m.field),p=action.payload?.find(p=>p.name===m.parameter);return !!f&&!!p&&!['id','revision','created','changed','archived','__proto__','constructor','prototype'].includes(m.parameter)&&compatibleActionField(f,p);});
}
/** Seed one opened baseline; missing optional values remain absent. No live draft synchronization. */
export function originalActionDefaults(info:{type:string;fields:Field[]}|undefined,action:{target:string;new?:boolean;payload:Parameter[]},mappings:Api.PageActionParameter[],record:Record<string,unknown>|undefined):Record<string,unknown>|undefined {
 if(!validActionDefaults(info,action,mappings))return;
 const values:Record<string,unknown>={};for(const m of mappings){const value=record?.[m.field],p=action.payload.find(p=>p.name===m.parameter)!;if(value===undefined||value===null)continue;if((['string','date','datetime'].includes(p.type??'')&&typeof value!=='string'||p.type==='boolean'&&typeof value!=='boolean'||['number','integer'].includes(p.type??'')&&typeof value!=='number')||!['string','boolean','number'].includes(typeof value)||typeof value==='number'&&(!Number.isFinite(value)||p.type==='integer'&&!Number.isSafeInteger(value))||p.choices?.length&&!p.choices.includes(String(value)))return;values[m.parameter]=value;}
 return values;
}
