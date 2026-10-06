import type {Api} from "@platform/kernel";

type Producer=Pick<Api.Section,"id"|"widget"|"collectionVariable"|"graphExplorer"|"observation">&{object?:string|Api.AssetRef};
/** Resolve the original typed record declaration, without reading or casting records. */
export function originalRecordObject(document:Api.PageDocument,sections:Producer[],variable:string,root?:Api.AssetRef):Api.AssetRef|undefined {
 const v=document.variables?.[variable];if(v?.type!=="record")return;
 if(v.source?.object)return v.source.object;
 const input=Object.values(document.interface?.inputs??{}).find(p=>p.variable===variable);if(input?.object)return input.object;
 const object=(id?:string)=>{const value=sections.find(s=>s.id===id)?.object;return typeof value==="string"?{app:value.split(".")[0]!,kind:"object",name:value}:value??root;};
 if(v.source?.kind==="item"){const set=document.variables?.[document.nodes[v.owner??""]?.loop?.collection??""];return set?.source?.kind==="plan"?document.queries?.[set.source.query??""]?.object:set?.source?.object??object(set?.source?.section);}
 if(v.mode!=="resource"||v.source?.kind!=="record")return;
 const producer=sections.find(s=>s.id===v.source?.section),port=v.source.port;if(!producer)return;
 if(producer.widget==="graph-explorer")return producer.graphExplorer?.outputs?.find(o=>o.id===port&&o.variable===variable)?.object;
 if(producer.widget==="observation"){const c=producer.observation;if(c?.kind!=="table")return;if(port==="asset"&&c.assetOutput===variable)return c.assetObject;if(port==="row"&&c.rowOutput===variable){const set=document.variables?.[producer.collectionVariable??""];return set?.source?.kind==="plan"?document.queries?.[set.source.query??""]?.object:undefined;}return;}
 return port?undefined:object(producer.id);
}
