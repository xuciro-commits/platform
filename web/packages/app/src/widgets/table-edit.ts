import {pageUIManifest,type ActionDeclaration} from "@platform/kernel";
import type {EntityInfo} from "@platform/ui";
const tableEditing=pageUIManifest.runtime.tableEditing;

/** Project the original catalog's field patch inputs; this is no authorization. */
export function tableEditableFields(info:EntityInfo|undefined,action:ActionDeclaration|undefined,fields:readonly string[]):string[]{
 if(!info||!action||action.schema!==`${info.type}.edit`||action.target!==info.type||action.new||action.needsApproval||!info.standard.includes(action.schema)||action.payload.some(f=>f.required))return [];
 return fields.filter(name=>{const f=info.fields.find(f=>f.name===name);return !!f&&!f.readOnly&&!f.aside&&info.lifecycle?.field!==name&&(tableEditing.fieldTypes as readonly string[]).includes(f.type)&&action.payload.some(p=>p.name===name);});
}
