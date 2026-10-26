import type {AggregateData,AggregateQuery,RecordQuery} from "@platform/ui";
import type {VariableResult} from "./variables";

/** Complete predicates only: a source window never limits membership. */
export function aggregateQuery(query:RecordQuery,measure:string|readonly string[]="count"):AggregateQuery {
 const {domain,search,set,archived,traversal}=query;
 return {domain,search,set,archived,traversal,measures:typeof measure==="string"?[measure]:[...measure]};
}
export function aggregateValue(data:AggregateData,requested:string|readonly string[]="count"):VariableResult {
 if(typeof requested!=="string"){
  const field=requested[1]?.slice(4)??"",expected=["count",`min:${field}`,`avg:${field}`,`max:${field}`,`sum:${field}`];
  const bad=()=>({status:"error" as const,code:"Statistics result is invalid."});
  if(!/^[A-Za-z][A-Za-z0-9._:-]{0,79}$/.test(field)||JSON.stringify(requested)!==JSON.stringify(expected)||!data||!Array.isArray(data.columns)||data.columns.length!==5||!Array.isArray(data.rows)||data.rows.length>1||data.columns.some((c,i)=>c.name!==expected[i]||c.kind!=="measure"||c.type!=="quantitative"||c.money||i>0&&c.field!==field))return bad();
  if(!data.rows.length)return {status:"value",value:{kind:"statistics",count:"0"}};
  const row=data.rows[0];if(!row||typeof row!=="object"||Object.keys(row).length!==5||expected.some(name=>!Object.hasOwn(row,name))||typeof row.count!=="number"||!Number.isSafeInteger(row.count)||row.count<0||expected.slice(1).some(name=>row[name]!==null&&(typeof row[name]!=="number"||!Number.isFinite(row[name]))))return bad();
  return {status:"value",value:{kind:"statistics",count:String(row.count),min:row[expected[1]!]===null?undefined:row[expected[1]!] as number,mean:row[expected[2]!]===null?undefined:row[expected[2]!] as number,max:row[expected[3]!]===null?undefined:row[expected[3]!] as number,sum:row[expected[4]!]===null?undefined:row[expected[4]!] as number}};
 }
 const measure=requested;
 if(measure!=="count"){
  const field=measure.slice(measure.indexOf(":")+1);if(!/^(sum|avg|min|max):[A-Za-z][A-Za-z0-9._:-]{0,79}$/.test(measure)||!data||data.columns?.length!==1||data.columns[0]?.name!==measure||data.columns[0]?.kind!=="measure"||data.columns[0]?.type!=="quantitative"||data.columns[0]?.field!==field||data.columns[0]?.money||!Array.isArray(data.rows)||data.rows.length>1)return {status:"error",code:"Aggregate scalar result is invalid."};
  if(!data.rows.length)return {status:"empty"};const row=data.rows[0];if(!row||typeof row!=="object"||Object.keys(row).length!==1||!Object.hasOwn(row,measure))return {status:"error",code:"Aggregate scalar result is invalid."};const value=row[measure];return value===null?{status:"empty"}:typeof value==="number"&&Number.isFinite(value)?{status:"value",value:{kind:"number",value}}:{status:"error",code:"Aggregate scalar result is invalid."};
 }
 if(!data||typeof data!=="object"||data.columns?.length!==1||data.columns[0]?.name!=="count"||data.columns[0]?.kind!=="measure"||data.columns[0]?.type!=="quantitative"||!Array.isArray(data.rows)||data.rows.length>1)return {status:"error",code:"Count result is invalid."};
 if(data.rows.length&&(!data.rows[0]||typeof data.rows[0]!=="object"))return {status:"error",code:"Count result is invalid."};
 const count=data.rows.length?data.rows[0]?.count:0;
 if(typeof count!=="number"||!Number.isSafeInteger(count)||count<0||data.rows.length&&Object.keys(data.rows[0]!).length!==1)return {status:"error",code:"Count result is invalid."};
 return {status:"value",value:{kind:"decimal",value:String(count)}};
}

/** Preview uses the same declaration budget as saved pages, before any read. */
export function aggregateBudget(variables:Record<string,import("@platform/kernel").Api.PageVariable>,plans:Record<string,import("@platform/kernel").Api.PageQuery>,nodes:Record<string,import("@platform/kernel").Api.PageLayoutNode>,contract:{maxVariables:number;maxExpandedReads:number}) {
 const declarations=Object.values(variables).filter(v=>v.mode==="aggregate");if(declarations.length>contract.maxVariables)return false;
 const parents=new Map<string,string>();for(const [id,node] of Object.entries(nodes))for(const child of node.children??[]){if(parents.has(child))return false;parents.set(child,id);}
 let total=0;
 for(const key of new Set(declarations.map(v=>JSON.stringify([v.source?.query??"",["aggregate","statistics"].includes(v.source?.kind??"")?`${v.source?.kind}:${v.source?.measure}`:"count"])))){const [query]=JSON.parse(key) as [string,string];const plan=plans[query];if(!plan)return false;let id=plan.itemOwner,factor=1;const seen=new Set<string>();while(id){if(seen.has(id)||!nodes[id])return false;seen.add(id);const loop=nodes[id]?.loop;if(loop){if(!Number.isInteger(loop.limit)||loop.limit<1)return false;factor*=loop.limit;}id=parents.get(id);if(factor>contract.maxExpandedReads)return false;}total+=factor;}
 return total<=contract.maxExpandedReads;
}
