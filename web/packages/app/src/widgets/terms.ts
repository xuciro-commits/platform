import {pageUIManifest} from "@platform/kernel";
import type {AggregateData,TermCount} from "@platform/ui";
/** Refuse malformed or duplicate groups; ties retain original host group order. */
export function termCounts(data:AggregateData,field:string,preserveOrder=false):TermCount[]|undefined {
 if(field==="count"||!data||data.columns?.length!==2||data.columns[0]?.name!==field||data.columns[0]?.kind!=="group"||data.columns[0]?.type!=="nominal"||data.columns[1]?.name!=="count"||data.columns[1]?.kind!=="measure"||data.columns[1]?.type!=="quantitative"||data.columns.some(c=>c.money)||!Array.isArray(data.rows)||data.rows.length>pageUIManifest.runtime.terms.maxGroups)return;
 const seen=new Set<string>(),terms:TermCount[]=[];
 for(const row of data.rows){if(!row||typeof row!=="object"||Object.keys(row).length!==2||!Object.hasOwn(row,field)||!Object.hasOwn(row,"count"))return;const value=row[field],count=row.count,key=JSON.stringify(value);if(value!==null&&typeof value!=="string"||typeof count!=="number"||!Number.isSafeInteger(count)||count<1||seen.has(key))return;seen.add(key);terms.push({value:value as string|null,count});}
 return preserveOrder?terms:terms.sort((a,b)=>b.count-a.count);
}
