import type { Api } from "@platform/kernel";
import type { PageSessionSnapshot } from "./Session";
import { variablePlan } from "./query-plans";
import type { VariableResult } from "./variables";

export const recordSlot = (object: string, name?: string, overlay?:string) => `${overlay?`overlay:${overlay}/`:""}${name ? `selection:${name}` : "object"}/${object}`;

/** Presentation ownership comes from the one layout document. */
export function sectionOverlay(page:Api.Page,section:string):string|undefined {
 const document=page.document;if(!document)return undefined;
 const find=(id:string,seen=new Set<string>()):boolean=>{if(seen.has(id))return false;seen.add(id);const node=document.nodes[id];return node?.section===section||[...(node?.children??[]),...(document.unusedWidgets??[]).filter(entry=>entry.parent===id).map(entry=>entry.node)].some(child=>find(child,seen));};
 return Object.entries(document.overlays??{}).find(([,overlay])=>find(overlay.root))?.[0];
}
export function selectionSlot(page:Api.Page,section:Api.Section,parent=false):string {
 const object=parent?(section.parentSelection?page.selections?.find((s)=>s.name===section.parentSelection)?.object.name??"":page.object.name):section.object?.name||page.object.name;
 const name=parent?section.parentSelection:section.selection,owner=sectionOverlay(page,section.id??"");
 const scoped=Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??"")?.[1])>=11;
 const local=scoped&&owner&&(page.sections??[]).some((s)=>["table","record-timeline","kanban"].includes(s.widget)&&(s.object?.name||page.object.name)===object&&(s.selection??"")===(name??"")&&sectionOverlay(page,s.id??"")===owner);
 return recordSlot(object,name,local?owner:undefined);
}
export const filterSlot=(object:string,owner?:string)=>`${owner?`overlay:${owner}/`:""}${object}`;
export function filterOwner(page:Api.Page,section:Api.Section):string|undefined {
 return Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??"")?.[1])>=13?sectionOverlay(page,section.id??""):undefined;
}
export function filtersForOwner(filters:PageSessionSnapshot["filters"],owner?:string) {
 const prefix=owner?`overlay:${owner}/`:"";
 return Object.fromEntries(Object.entries(filters).filter(([key])=>owner?key.startsWith(prefix):!key.startsWith("overlay:")).map(([key,fields])=>[owner?key.slice(prefix.length):key,fields]));
}
export function filterSessionBindings(page:Api.Page) {
 const selections=new Map<string,Set<string>>(),queries=new Map<string,Set<string>>();
 for(const section of page.sections??[]){
  if(section.widget!=="table"||section.collectionVariable)continue;
  const key=filterSlot(section.object?.name||page.object.name,filterOwner(page,section));
  if(!selections.has(key))selections.set(key,new Set());selections.get(key)!.add(selectionSlot(page,section));
  if(!queries.has(key))queries.set(key,new Set());queries.get(key)!.add(section.id??`section:${page.sections!.indexOf(section)}`);
 }
 return {filterSelections:selections,filterQueries:queries};
}
export function overlaySessionScopes(page:Api.Page) {
 const document=page.document;
 return new Map(Object.entries(document?.overlays??{}).map(([owner,overlay])=>{
  const nodes=new Set<string>();const collect=(id:string)=>{if(nodes.has(id))return;nodes.add(id);document?.nodes[id]?.children?.forEach(collect);};collect(overlay.root);
  const sections=(page.sections??[]).filter((s)=>[...nodes].some((id)=>document?.nodes[id]?.section===s.id));
  return [owner,{
   queries:new Set([...sections.map((s)=>s.id??""),...Object.entries(document?.queries??{}).filter(([,q])=>q.owner===owner).map(([id])=>`plan/${id}`)]),
   selections:new Set(sections.map((s)=>selectionSlot(page,s)).filter((key)=>key.startsWith(`overlay:${owner}/`))),
   filters:new Set(sections.filter((s)=>s.widget==="filter").map((s)=>filterSlot(s.object?.name||page.object.name,filterOwner(page,s))).filter((key)=>key.startsWith(`overlay:${owner}/`))),
   loops:new Set([...nodes].filter((id)=>document?.nodes[id]?.kind==="loop")),
  }];
 }));
}

/** Resource outputs refer to the original section binding. No second query
 * definition, policy or mutable record object is stored in the document.
 */
export function resourceVariables(page: Api.Page, snapshot: PageSessionSnapshot): Record<string, VariableResult> {
  return Object.fromEntries(Object.entries(page.document?.variables ?? {}).flatMap(([id, variable]) => {
    const source = variable.source;
    if (variable.mode !== "resource" || !source || !["page","overlay"].includes(variable.scope) || variablePlan(page,id)!==undefined) return [];
    const section = page.sections?.find((section) => section.id === source.section);
    if (!section) return [[id, { status: "error", code: "Resource source is unavailable" } as VariableResult]];
    if(page.document?.unusedWidgets?.some(entry=>page.document?.nodes[entry.node]?.section===section.id))return [[id,{status:"empty"} as VariableResult]];
    const object = section.object?.name || page.object.name;
    let value: VariableResult;
    if (source.kind === "record") {
      const state = snapshot.records[selectionSlot(page,section)];
      value = state?.status === "value" ? { status: "value", value: { kind: "record", reference: state.value } }
        : state?.status === "pending" ? { status: "pending" } : state?.status === "error" ? { status: "error", code: "Resource read failed" } : { status: "empty" };
    } else if (source.kind === "filter") {
      const fields = snapshot.filters[filterSlot(object,filterOwner(page,section))] ?? {}, payload = { kind: "filter" as const, object, fields };
      value = Object.keys(fields).length ? { status: "value", value: payload } : { status: "empty", value: payload };
    } else {
      const state = snapshot.queries[source.section ?? ""];
      value = state?.status === "value" ? { status: "value", value: { kind: "object-set", window: state.value } }
        : state?.status === "empty" && state.value ? { status: "empty", value: { kind: "object-set", window: state.value } }
        : state?.status === "pending" ? { status: "pending" } : state?.status === "error" ? { status: "error", code: "Resource read failed" } : { status: "empty" };
    }
    return [[id, value]];
  }));
}
