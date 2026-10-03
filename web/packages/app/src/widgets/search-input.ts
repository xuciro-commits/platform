import type {Api} from "@platform/kernel";
/** Scope comes only from declared original plans, without another reader. */
export function searchInputObjects(document:Api.PageDocument|undefined,variable:string|undefined):string[] {
 const v=document?.variables?.[variable??""];if(!variable||!v||v.type!=="string"||v.mode!=="state"||!["page","overlay"].includes(v.scope))return [];
 return [...new Set(Object.values(document?.queries??{}).filter(q=>q.search?.variable===variable&&(q.owner??"")===(v.owner??"")&&!q.itemOwner).map(q=>q.object.name))];
}
