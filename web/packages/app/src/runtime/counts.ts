import type {AggregateData,AggregateQuery,RecordQuery} from "@platform/ui";
import type {VariableResult} from "./variables";

/** Complete predicates only: a source window never limits membership. */
export function countQuery(query:RecordQuery):AggregateQuery {
 const {domain,search,set,archived}=query;
 return {domain,search,set,archived,measures:["count"]};
}
export function countValue(data:AggregateData):VariableResult {
 if(!data||typeof data!=="object"||data.columns?.length!==1||data.columns[0]?.name!=="count"||data.columns[0]?.kind!=="measure"||data.columns[0]?.type!=="quantitative"||!Array.isArray(data.rows)||data.rows.length>1)return {status:"error",code:"Count result is invalid."};
 if(data.rows.length&&(!data.rows[0]||typeof data.rows[0]!=="object"))return {status:"error",code:"Count result is invalid."};
 const count=data.rows.length?data.rows[0]?.count:0;
 if(typeof count!=="number"||!Number.isSafeInteger(count)||count<0||data.rows.length&&Object.keys(data.rows[0]!).length!==1)return {status:"error",code:"Count result is invalid."};
 return {status:"value",value:{kind:"decimal",value:String(count)}};
}

/** Preview uses the same declaration budget as saved pages, before any read. */
export function countBudget(variables:Record<string,import("@platform/kernel").Api.PageVariable>,plans:Record<string,import("@platform/kernel").Api.PageQuery>,nodes:Record<string,import("@platform/kernel").Api.PageLayoutNode>,contract:{maxVariables:number;maxExpandedReads:number}) {
 const declarations=Object.values(variables).filter(v=>v.mode==="aggregate");if(declarations.length>contract.maxVariables)return false;
 const parents=new Map<string,string>();for(const [id,node] of Object.entries(nodes))for(const child of node.children??[]){if(parents.has(child))return false;parents.set(child,id);}
 let total=0;
 for(const query of new Set(declarations.map(v=>v.source?.query??""))){const plan=plans[query];if(!plan)return false;let id=plan.itemOwner,factor=1;const seen=new Set<string>();while(id){if(seen.has(id)||!nodes[id])return false;seen.add(id);const loop=nodes[id]?.loop;if(loop){if(!Number.isInteger(loop.limit)||loop.limit<1)return false;factor*=loop.limit;}id=parents.get(id);if(factor>contract.maxExpandedReads)return false;}total+=factor;}
 return total<=contract.maxExpandedReads;
}
