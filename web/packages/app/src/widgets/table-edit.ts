import {pageUIManifest,type ActionDeclaration} from "@platform/kernel";
import type {EntityInfo} from "@platform/ui";
const tableEditing=pageUIManifest.runtime.tableEditing;

/** Project the original catalog's field patch inputs; this is no authorization. */
export function tableEditableFields(info:EntityInfo|undefined,action:ActionDeclaration|undefined,fields:readonly string[]):string[]{
 if(!info||!action||action.schema!==`${info.type}.edit`||action.target!==info.type||action.new||action.needsApproval||!info.standard.includes(action.schema)||action.payload.some(f=>f.required))return [];
 return fields.filter(name=>{const f=info.fields.find(f=>f.name===name);return !!f&&!f.readOnly&&!f.aside&&info.lifecycle?.field!==name&&(tableEditing.fieldTypes as readonly string[]).includes(f.type)&&action.payload.some(p=>p.name===name);});
}

/** The actions a section may offer: the page's declared set, narrowed by an
 * explicit section allow-list; an empty page set or an explicit empty section
 * list forbids writes instead of opening the member's catalog. */
export function sectionWriteActions(page:{actions:readonly {name:string}[]},section:{actions?:readonly {name:string}[]|null}):string[]{
 const pageNames=page.actions.map(ref=>ref.name);
 if(pageNames.length===0)return [];
 const sectionNames=section.actions?.map(ref=>ref.name);
 if(sectionNames===undefined)return pageNames;
 return sectionNames.filter(name=>pageNames.includes(name));
}

/** The default standard patch for a list without authored inline editing:
 * the type's own edit action from the member's catalog, within the declared
 * action set, over the displayed fields, under the original profile gates.
 * This derives what may be offered — it is not authorization. */
export function defaultTableEdit(info:EntityInfo|undefined,catalog:readonly ActionDeclaration[]|undefined,allowed:readonly string[],displayed?:readonly string[]):ActionDeclaration|undefined{
 if(!info)return undefined;
 return catalog?.find(a=>a.schema===`${info.type}.edit`&&allowed.includes(a.schema)&&tableEditableFields(info,a,displayed??info.fields.filter(f=>f.type!=="lines").map(f=>f.name)).length>0)||undefined;
}
