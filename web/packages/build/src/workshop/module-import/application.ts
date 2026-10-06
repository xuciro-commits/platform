import type {Api} from '@platform/kernel';
import type {ImportTarget,SourceModule} from './compile';
export type ApplicationImportBinding={binding:Api.AssetBinding;ports:Record<string,{variable:string;writable:boolean}>;recordSets?:Record<string,{variable:string;writable:boolean}>};
export function originalImportApplication(binding:ApplicationImportBinding|undefined,target:ImportTarget):Api.Application|undefined {
 const b=binding?.binding;if(!b||b.ref?.kind!=='app')return;
 return target.definitions?.find(d=>d.ref.app===b.ref.app&&d.ref.kind==='app'&&d.ref.name===b.ref.name&&d.version===b.sourceVersion)?.application;
}
/** Original metadata defines a record interface object; the target still rechecks authorization. */
export function sourceInterfaceObject(module:SourceModule,sourceID:string):string|undefined{
 const variable=module.variables.find(v=>v.id===sourceID),producer=module.widgets[String(variable?.widgetId??'')],input=module.variables.find(v=>v.id===producer?.config.objectSetVarId);
 if(variable?.type!=='object'||variable.definitionKind!=='widgetOutput'||variable.widgetOutputKey!=='activeObject'||!producer||!['ObjectTable','ObjectList','KanbanBoard','Calendar','ObjectSelector','Leaderboard','ScatterPlot','ResourceList','MapTemplate','Map'].includes(producer.type))return;
 const set=input?.objectSet;if(set&&typeof set==='object'&&!Array.isArray(set)&&typeof (set as Record<string,unknown>).objectType==='string')return (set as {objectType:string}).objectType;
 return typeof input?.sourceObjectType==='string'?input.sourceObjectType:undefined;
}

/** A non-interface selection set requires an explicit original table producer. */
export function sourceRecordSetObject(module:SourceModule,sourceID:string):string|undefined {
 const v=module.variables.find(v=>v.id===sourceID),producer=module.widgets[String(v?.widgetId??'')],input=module.variables.find(v=>v.id===producer?.config.objectSetVarId);
 if(v?.type!=='array'||v.definitionKind!=='widgetOutput'||v.widgetOutputKey!=='selectedObjects'||producer?.type!=='ObjectTable'||producer.config.selectionMode!=='multiple'||producer.config.selectedVarId!==sourceID||producer.config.enableSelection===false||input?.type!=='objectSet'||input.definitionKind!=='objectSetDefinition')return;
 const set=input.objectSet;if(set&&typeof set==='object'&&!Array.isArray(set)&&typeof (set as Record<string,unknown>).objectType==='string')return (set as {objectType:string}).objectType;
 return typeof input.sourceObjectType==='string'?input.sourceObjectType:undefined;
}
