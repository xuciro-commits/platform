import {pageUIManifest} from "../pageui/manifest";
import type * as PageUI from "../pageui/types";

import type {Api} from '@platform/kernel';
import type {EntityInfo} from '@platform/ui';

export const modelBindingContract=pageUIManifest.runtime.modelBinding;
export const modelFieldKey=(field:Api.FieldRef)=>(field.path??[field.name??'']).join('.');
type Lookup=(type:string)=>EntityInfo|undefined;
type ModelSection={widget:string;composite?:PageUI.PageComposite;model?:Api.ModelBinding;object?:string|Api.AssetRef;fields?:string[];relation?:string;parentSelection?:string;query?:string|Api.AssetRef};
type Values=Record<string,unknown>;
export function modelPort(value:unknown,path:string):unknown {
 let current=value;for(const name of path.split('.')){if(!current||typeof current!=='object')return;current=(current as Values)[name];}return current;
}
function setPort(value:Values,path:string,next:unknown){const names=path.split('.');let current=value;for(const name of names.slice(0,-1)){if(!current[name]||typeof current[name]!=='object')current[name]={};current=current[name] as Values;}if(next===undefined)delete current[names.at(-1)!];else current[names.at(-1)!]=next;}
export function modelField(type:string,path:string[],lookup:Lookup):Api.FieldInfo|undefined {
 if(!path.length||path.length>modelBindingContract.maxPath)return;
 let field:Api.FieldInfo|undefined;
 for(const [i,name] of path.entries()){
  field=name==='id'?{name:'id',title:'ID',type:'text',readOnly:true}:lookup(type)?.fields.find(f=>f.name===name);
  if(!field)return;
  if(i<path.length-1){if(field.type!=='reference'||!field.ref)return;type=field.ref;}
 }
 return field;
}
export function modelEntity(info:EntityInfo|undefined,model:Api.ModelBinding|undefined,lookup:Lookup):EntityInfo|undefined {
 if(!info||!model)return info;
 const fields=[...info.fields];
 for(const ref of model.fields??[]){if((ref.path?.length??0)<2)continue;const field=modelField(model.type,ref.path!,lookup);if(field&&!fields.some(f=>f.name===modelFieldKey(ref)))fields.push({...field,name:modelFieldKey(ref),title:ref.path!.join(' → '),readOnly:true});}
 return {...info,fields};
}
export const primaryModel=(section:Pick<PageUI.Section,'widget'|'model'|'composite'>)=>section.model??section.composite?.bindings[section.widget==='model-matrix'?'rows':'nodes']?.model;
export const sectionModels=(section:Pick<PageUI.Section,'model'|'composite'>)=>[...section.model?[section.model]:[],...Object.values(section.composite?.bindings??{}).map(b=>b.model)];
export function materializeModelSection<T extends ModelSection>(section:T,lookup:Lookup,authoring=false):T {
 if(section.composite){const model=primaryModel(section);return model?{...section,object:authoring?model.type:{app:model.type.split('.')[0]??'',kind:'object',name:model.type}}:section;}
 const model=section.model;if(!model)return section;
 const copy=structuredClone(section),values=copy as unknown as Values,info=lookup(model.type);
 copy.object=authoring?model.type:{app:model.type.split('.')[0]??'',kind:'object',name:model.type};
 copy.parentSelection=model.relation?.parentSelection;copy.relation=model.relation?.name;
 const fields=(model.fields??[]).filter(f=>!f.slot||f.slot==='fields').map(modelFieldKey);
 if(!fields.length&&['table','detail','form','filter'].includes(section.widget))for(const field of info?.fields??[]){if(field.aside||section.widget==='form'&&field.readOnly||section.widget==='filter'&&!['choice','boolean','reference'].includes(field.type))continue;fields.push(field.name);}
 copy.fields=fields;
 for(const ref of model.fields??[]){if(!ref.slot||ref.slot==='fields')continue;const name=modelFieldKey(ref);setPort(values,ref.slot,ref.transform?ref.slot==='measure'?`${ref.transform}:${name}`:`${name}:${ref.transform}`:name);}
 if(section.widget==='record-picker'&&!modelPort(copy,'recordPicker.labelField'))setPort(values,'recordPicker.labelField',info?.display||'id');
 if(section.widget==='status-tracker'&&info?.lifecycle){if(!modelPort(copy,'statusTracker.field'))setPort(values,'statusTracker.field',info.lifecycle.field);if(!(modelPort(copy,'statusTracker.stages') as unknown[]|undefined)?.length)setPort(values,'statusTracker.stages',info.lifecycle.states.map(s=>s.name));}
 return copy;
}

export function canonicalModelSection<T extends ModelSection>(section:T):T {
 if(section.composite){const copy=structuredClone(section);delete copy.object;return copy;}
 if(!section.model)return section;
 const copy=structuredClone(section),values=copy as unknown as Values;
 for(const name of ['object','fields','relation','parentSelection','query'])delete values[name];
 for(const slot of modelBindingContract.fieldSlots)if(modelPort(copy,slot)!==undefined)setPort(values,slot,undefined);
 return copy;
}

/** Original widget inspectors edit a temporary projection. Only changed ports
 * become overrides; unchanged inferred defaults stay model defaults. */
export function foldModelPorts<T extends ModelSection>(section:T,lookup:Lookup):T {
 if(!section.model)return section;
 const canonical=canonicalModelSection(section),expected=materializeModelSection(canonical,lookup,typeof section.object==='string'),model=structuredClone(section.model);
 if(JSON.stringify(section.fields)!==JSON.stringify(expected.fields))model.fields=[...(model.fields??[]).filter(f=>!!f.slot&&f.slot!=='fields'),...(section.fields??[]).map(name=>name.includes('.')?{path:name.split('.')}:{name})];
 const type=typeof section.object==='string'?section.object:section.object?.name;if(type&&type!==model.type)model.type=type;
 for(const slot of modelBindingContract.fieldSlots){const current=modelPort(section,slot);if(current===modelPort(expected,slot))continue;model.fields=(model.fields??[]).filter(f=>f.slot!==slot);if(typeof current!=='string'||!current||current==='count')continue;const parts=current.split(':'),name=parts.length>1?(slot==='measure'?parts[1]!:parts[0]!):current,transform=parts.length>1?(slot==='measure'?parts[0]:parts[1]):undefined;model.fields.push({...name.includes('.')?{path:name.split('.')}:{name},slot,...transform?{transform}:{}});}
 canonical.model=model;return canonical;
}
export const modelWindowKey=(section:Pick<PageUI.Section,'id'>)=>`model/${section.id??''}`;
export const modelWindowWidget=(section:Pick<PageUI.Section,'model'|'widget'|'composite'>)=>(!!section.composite&&['layered-graph','model-matrix'].includes(section.widget))||!!section.model&&['table','detail','record-picker','record-card','record-list','record-chart','record-events','record-calendar','record-gantt','record-leaderboard','record-scatter','record-timeline','kanban','record-map','histogram','term-counts','tag-counts','sparkline-kpi'].includes(section.widget);

/** Follows only declared single-record references. Every hop is an original
 * source.get(), so row and field authorization remain with the host. */
export async function readModelPath(source:Pick<import('@platform/ui').RecordSource,'get'|'entity'>,type:string,record:import('@platform/ui').EntityRecord,path:string[],active:()=>boolean):Promise<unknown>{
 if(!modelField(type,path,source.entity))throw Error('Declared relationship field is unavailable.');
 let current=record;
 for(const [i,name] of path.entries()){
  if(!active())throw Error('Model context changed.');
  const value=current[name];
  if(i===path.length-1){if(!Object.hasOwn(current,name))throw Error('Relationship field could not be read.');return value;}
  if(value===undefined||value===null||value==='')return null;
  const field=source.entity(type)?.fields.find(f=>f.name===name);if(field?.type!=='reference'||!field.ref||typeof value!=='string')throw Error('Declared relationship field is unavailable.');
  type=field.ref;current=(await source.get(type,value)).record;
 }
 throw Error('Declared relationship field is unavailable.');
}
