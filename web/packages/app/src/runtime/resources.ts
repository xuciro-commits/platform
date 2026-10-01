import type { Api } from "@platform/kernel";
import type { PageSessionSnapshot } from "./Session";
import { variablePlan } from "./query-plans";
import type { VariableResult } from "./variables";

export const recordSlot = (object: string, name?: string, overlay?:string) => `${overlay?`overlay:${overlay}/`:""}${name ? `selection:${name}` : "object"}/${object}`;

/** Presentation ownership comes from the one layout document. */
export function sectionOverlay(page:Api.Page,section:string):string|undefined {
 const document=page.document;if(!document)return undefined;
 const find=(id:string,seen=new Set<string>()):boolean=>{if(seen.has(id))return false;seen.add(id);const node=document.nodes[id];return node?.section===section||!!node?.children?.some((child)=>find(child,seen));};
 return Object.entries(document.overlays??{}).find(([,overlay])=>find(overlay.root))?.[0];
}
export function selectionSlot(page:Api.Page,section:Api.Section,parent=false):string {
 const object=parent?(section.parentSelection?page.selections?.find((s)=>s.name===section.parentSelection)?.object.name??"":page.object.name):section.object?.name||page.object.name;
 const name=parent?section.parentSelection:section.selection,owner=sectionOverlay(page,section.id??"");
 const scoped=Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??"")?.[1])>=11;
 const local=scoped&&owner&&(page.sections??[]).some((s)=>s.widget==="table"&&(s.object?.name||page.object.name)===object&&(s.selection??"")===(name??"")&&sectionOverlay(page,s.id??"")===owner);
 return recordSlot(object,name,local?owner:undefined);
}
export function overlaySessionScopes(page:Api.Page) {
 const document=page.document;
 return new Map(Object.entries(document?.overlays??{}).map(([owner,overlay])=>{
  const nodes=new Set<string>();const collect=(id:string)=>{if(nodes.has(id))return;nodes.add(id);document?.nodes[id]?.children?.forEach(collect);};collect(overlay.root);
  const sections=(page.sections??[]).filter((s)=>[...nodes].some((id)=>document?.nodes[id]?.section===s.id));
  return [owner,{
   queries:new Set([...sections.map((s)=>s.id??""),...Object.entries(document?.queries??{}).filter(([,q])=>q.owner===owner).map(([id])=>`plan/${id}`)]),
   selections:new Set(sections.map((s)=>selectionSlot(page,s)).filter((key)=>key.startsWith(`overlay:${owner}/`))),
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
    const object = section.object?.name || page.object.name;
    let value: VariableResult;
    if (source.kind === "record") {
      const state = snapshot.records[selectionSlot(page,section)];
      value = state?.status === "value" ? { status: "value", value: { kind: "record", reference: state.value } }
        : state?.status === "pending" ? { status: "pending" } : state?.status === "error" ? { status: "error", code: "Resource read failed" } : { status: "empty" };
    } else if (source.kind === "filter") {
      const fields = snapshot.filters[object] ?? {}, payload = { kind: "filter" as const, object, fields };
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
