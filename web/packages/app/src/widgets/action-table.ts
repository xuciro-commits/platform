import type {ActionDeclaration,Api} from "@platform/kernel";
import type {EntityInfo} from "@platform/ui";
export function actionTableParameters(info:EntityInfo|undefined,action:ActionDeclaration|undefined,parameters:Api.PageActionParameter[]):Api.PageActionParameter[]|undefined {
 if(!info||!action||action.target!==info.type||action.new||action.automation||parameters.length<1||parameters.length>16||parameters.length!==action.payload.length||new Set(parameters.map(p=>p.parameter)).size!==parameters.length)return;
 if(parameters.some(m=>{const f=info.fields.find(f=>f.name===m.field),p=action.payload.find(p=>p.name===m.parameter);if(!f||!p||p.from||["id","revision","created","changed","archived","__proto__","constructor","prototype"].includes(m.parameter))return true;return p.type==="string"?p.ref?f.type!=="reference"||f.ref!==p.ref:!["text","longtext","choice"].includes(f.type):p.type==="number"?!["decimal","integer"].includes(f.type):!["integer","boolean","date","datetime"].includes(p.type)||f.type!==p.type;}))return;
 return parameters;
}
