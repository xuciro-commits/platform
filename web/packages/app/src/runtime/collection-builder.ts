import {collectionInput,collectionQuery,validCollectionInput,type CollectionInput} from './collection-input';
import type {Api} from '@platform/kernel';
import type {EntityInfo,Filter} from '@platform/ui';
import {parseDecimal} from './decimal';

export const builderOperators=(type:string)=>['integer','decimal'].includes(type)?['is','isNot','gte','lte']:['is','isNot',...type==='text'?['contains']:[]];
const operators:Record<string,string>={is:'=',isNot:'!=',gte:'>=',lte:'<=',contains:'like'};
/** Explicit literal clauses are bounded and checked against current original fields. */
export function builderConditions(filters:Filter[],fields:string[],info:EntityInfo):Api.PageQueryCondition[]|undefined {
 if(filters.length>16)return;
 const result:Api.PageQueryCondition[]=[];
 for(const f of filters){
  const field=info.fields.find(v=>v.name===f.field);
  if(!fields.includes(f.field)||!field||!builderOperators(field.type).includes(f.operator)||!['text','choice','integer','decimal'].includes(field.type))return;
  let value:unknown=f.arg;
  if(['integer','decimal'].includes(field.type)){
   if(typeof value!=='string')return;
   const decimal=parseDecimal(value);if(!decimal||field.type==='integer'&&decimal.value.includes('.'))return;
   value=decimal;
  }else if(typeof value!=='string'||new TextEncoder().encode(value).length>4096||field.type==='choice'&&!field.choices?.includes(value))return;
  result.push({field:f.field,op:operators[f.operator]!,value:{literal:value as Api.PageValue['literal']}});
 }
 return result;
}
export function refineCollection(input:CollectionInput,conditions:Api.PageQueryCondition[]):CollectionInput|undefined {
 if(conditions.length>16||conditions.some(c=>!['=','!=','>=','<=','like'].includes(c.op)||!c.value||c.value.variable||c.value.literal===undefined))return;
 const query=collectionQuery(input,{domain:conditions.map(c=>[c.field,c.op,c.value.literal]),sort:input.sort,limit:1,offset:0});
 if(!query)return;
 const output=collectionInput(input.object,query,input.bindings,input.sortLocked);
 return validCollectionInput(output)?output:undefined;
}
export function builderSection(input:string,variables:Record<string,Api.PageVariable>,sections:Api.Section[],owner?:string):Api.Section|undefined {
 const v=variables[input];if(v?.writable||v?.source&&Object.keys(v.source).some(key=>!['kind','section'].includes(key))||v?.type!=='object-set'||v.mode!=='resource'||v.source?.kind!=='query'||v.owner!==owner||v.scope!==(owner?'overlay':'page'))return;
 return sections.find(s=>s.widget==='collection-builder'&&s.id===v.source!.section&&s.collectionOutputVariable===input&&s.collectionBuilder);
}
