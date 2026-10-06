import type {SourceModule} from './compile';
export type UnusedImportBinding={migration:'complete-unused-configuration'|'';collection:string;columns?:string[];xProperty?:string;yProperty?:string;colorBy?:string;labelField?:string};
/** An explicit completion of two incomplete source forms, never an arbitrary override. */
export function unusedConfigurationKind(w:SourceModule['widgets'][string]):'table'|'scatter'|undefined{
 const keys=Object.keys(w.config);
 return w.type==='ObjectTable'&&!keys.length?'table':w.type==='ChartXY'&&keys.length===1&&w.config.chartKind==='scatter'?'scatter':undefined;
}
export function completedUnusedConfiguration(module:SourceModule,w:SourceModule['widgets'][string],b:UnusedImportBinding):Record<string,unknown>|undefined{
 const kind=unusedConfigurationKind(w),v=module.variables.find(v=>v.id===b.collection);
 if(!module.unusedWidgetIds.includes(w.id)||!kind||b.migration!=='complete-unused-configuration'||typeof b.collection!=='string'||v?.type!=='objectSet'||v.definitionKind!=='objectSetDefinition')return;
 const keys=kind==='table'?['migration','collection','columns']:['migration','collection','xProperty','yProperty','colorBy','labelField'];
 if(Object.keys(b).some(k=>!keys.includes(k)))return;
 if(kind==='table')return Array.isArray(b.columns)&&b.columns.length>0&&b.columns.length<=24&&b.columns.every(c=>typeof c==='string'&&!!c)&&new Set(b.columns).size===b.columns.length?{objectSetVarId:b.collection,columns:b.columns.map(key=>({key}))}:undefined;
 return ['xProperty','yProperty','colorBy','labelField'].every(k=>typeof b[k as keyof UnusedImportBinding]==='string'&&!!b[k as keyof UnusedImportBinding])?{chartKind:'scatter',objectSetVarId:b.collection,xProperty:b.xProperty,yProperty:b.yProperty,colorBy:b.colorBy}:undefined;
}
