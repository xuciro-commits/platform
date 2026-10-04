import {sourceInterfaceObject,sourceRecordSetObject} from "./application";
import type {SourceModule,ImportBindings} from "./compile";

const text=(v:unknown)=>typeof v==="string"?v:"";
const object=(v:unknown)=>v&&typeof v==="object"&&!Array.isArray(v)?v as Record<string,unknown>:undefined;
/** Regions presented by the selected page and retained overlays, excluding other pages. */
export function workshopRegionSections(module:SourceModule|undefined,pageID:string){
 if(!module)return [];
 const visited=new Set<string>(),walk=(id:string)=>{if(visited.has(id))return;visited.add(id);for(const c of module.sections[id]?.children??[])if(c.kind==='section')walk(c.id);};
 const page=module.pages.find(p=>p.id===pageID);if(!page)return [];walk(page.rootSectionId);module.overlays.forEach(o=>walk(o.rootSectionId));return [...visited].flatMap(id=>module.sections[id]?[module.sections[id]!]:[]);
}
/** Mapping controls only. Compilation always receives the complete original source. */
export function workshopBindingView(module:SourceModule,pageID:string):SourceModule{
 const regions=workshopRegionSections(module,pageID),widgets=new Set([...module.unusedWidgetIds,...regions.flatMap(s=>s.children.filter(c=>c.kind==='widget').map(c=>c.id))]),variables=new Set<string>(),known=new Map(module.variables.map(v=>[v.id,v]));
 const scan=(value:unknown):void=>{if(typeof value==='string'&&known.has(value)){if(variables.has(value))return;variables.add(value);scan(known.get(value));}else if(Array.isArray(value))value.forEach(scan);else if(value&&typeof value==='object')Object.values(value).forEach(scan);};
 regions.forEach(scan);[...widgets].forEach(id=>scan(module.widgets[id]));scan(module.moduleInterface);scan(module.overlays);
 return {...module,sections:Object.fromEntries(regions.map(s=>[s.id,s])),widgets:Object.fromEntries([...widgets].flatMap(id=>module.widgets[id]?[[id,module.widgets[id]!]]:[])),variables:module.variables.filter(v=>variables.has(v.id))};
}
/** Offer only active-record producers actually present in this widget's owner. */
export function collaborationRecordChoices(module:SourceModule|undefined,pageID:string,widgetID:string):{id:string;title:string;object:string}[] {
 if(!module)return [];
 const owners=new Map<string,string|undefined>(),walk=(root:string,owner?:string,seen=new Set<string>())=>{if(seen.has(root))return;seen.add(root);for(const child of module.sections[root]?.children??[]){if(child.kind==="widget")owners.set(child.id,owner);else walk(child.id,owner,seen);}};
 const page=module.pages.find(p=>p.id===pageID);if(!page)return [];walk(page.rootSectionId);module.overlays.forEach(o=>walk(o.rootSectionId,o.id));module.unusedWidgetIds.forEach(id=>owners.set(id,undefined));if(!owners.has(widgetID))return [];
 const shared=["VertexGraph","Scene3D"].includes(module.widgets[widgetID]?.type??"")?aiImportInterfaceRecords(module):[];
 return [...shared,...module.variables.flatMap(v=>{if(v.type!=="object"||v.definitionKind!=="widgetOutput"||v.widgetOutputKey!=="activeObject"||v.isInterface)return [];
  const producers=Object.values(module.widgets).filter(w=>owners.has(w.id)&&!module.unusedWidgetIds.includes(w.id)&&owners.get(w.id)===owners.get(widgetID)&&["ObjectTable","ObjectList","KanbanBoard","Calendar","ObjectSelector","Leaderboard","ScatterPlot","ResourceList","MapTemplate","Map"].includes(w.type)&&w.config.activeVarId===v.id&&(w.type!=="ObjectTable"||v.widgetId===w.id));if(producers.length!==1)return [];
  const set=module.variables.find(value=>value.id===producers[0]!.config.objectSetVarId),external=text(object(set?.objectSet)?.objectType||set?.sourceObjectType);return external?[{id:v.id,title:v.name,object:external}]:[];
 })];
}
/** Discover the explicit source metadata that the import mapping dialog must expose. */
export function workshopRequirements(module:SourceModule|undefined,origin:SourceModule|undefined=module){
 const objects=new Set<string>(),fields:Record<string,Set<string>>=Object.create(null),actions=new Set<string>();
 const external=(id:string,seen=new Set<string>()):string=>{if(seen.has(id))return "";seen.add(id);const producer=Object.values(module?.widgets??{}).find(w=>w.type==="ObjectSetBuilder"&&w.config.outputVarId===id);if(producer)return external(text(producer.config.objectSetVarId),seen);const v=module?.variables.find(v=>v.id===id);return text(object(v?.objectSet)?.objectType||v?.sourceObjectType);};
 const property=(type:string,p:string)=>{if(!type||!p||p==="*")return;objects.add(type);(fields[type]??=new Set()).add(p);};
 for(const v of module?.variables??[]){const set=object(v.objectSet),type=text(set?.objectType||v.sourceObjectType);if(type)objects.add(type);for(const step of Array.isArray(set?.steps)?set.steps:[])for(const clause of Array.isArray(object(step)?.clauses)?object(step)!.clauses as unknown[]:[])property(type,text(object(clause)?.property));}
 for(const w of Object.values(module?.widgets??{})){
  if(w.type==="ObjectComparison"&&origin){const type=sourceRecordSetObject(origin,text(w.config.objectsVarId));if(type)objects.add(type);}
  if(w.type==="FilterList")for(const f of Array.isArray(w.config.facets)?w.config.facets:[])property(external(text(w.config.objectSetVarId)),text(object(f)?.property));
  if(w.type==="ObjectTable")for(const c of Array.isArray(w.config.columns)?w.config.columns:[])property(external(text(w.config.objectSetVarId)),text(object(c)?.key));
  if(w.type==="Map"&&w.config.colorBy!==undefined)property(external(text(w.config.objectSetVarId)),text(w.config.colorBy));
  if(w.type==="ChartXY")for(const key of ["xProperty","yProperty"])property(external(text(w.config.objectSetVarId)),text(w.config[key]));
  if(["ChartVega","ChartWaterfall"].includes(w.type))property(external(text(w.config.objectSetVarId)),"status");
  if(w.type==="ObservabilityChart")property(external(text(w.config.objectSetVarId)),"availability");
  if(w.type==="DerivedSeries")property(external(text(w.config.objectSetVarId)),"pressure");
  if(w.type==="FreeFormAnalysis")for(const key of ["pressure","temperature","availability","revenueImpact"])property(external(text(w.config.objectSetVarId)),key);
  if(w.type==="MapTemplate")property(external(text(w.config.objectSetVarId)),"name");
  if(w.type==="ActionTable"&&text(w.config.actionId))actions.add(text(w.config.actionId));
  if(w.type==="ChartPie")property(external(text(w.config.objectSetVarId)),text(w.config.groupBy));
  if(w.type==="PivotTable")for(const key of ["rows","cols"])property(external(text(w.config.objectSetVarId)),text(w.config[key]));
  if(w.type==="KanbanBoard")property(external(text(w.config.objectSetVarId)),text(w.config.groupBy));
  if(w.type==="ResourceList")for(const key of ["name","status"])property(external(text(w.config.objectSetVarId)),key);
  if(w.type==="TagList")property(external(text(w.config.objectSetVarId)),text(w.config.property));
  if(w.type==="Leaderboard")property(external(text(w.config.objectSetVarId)),text(w.config.property));
  if(w.type==="SparklineKpi"&&w.config.seriesSetVarId!==undefined)property(external(text(w.config.seriesSetVarId)),w.config.seriesProperty===undefined?"availability":text(w.config.seriesProperty));
  if(w.type==="Treemap")property(external(text(w.config.objectSetVarId)),text(w.config.groupBy));
  if(w.type==="Heatmap")for(const key of ["rows","cols"])property(external(text(w.config.objectSetVarId)),text(w.config[key]));
  if(w.type==="ScatterPlot"){for(const key of ["x","y"])property(external(text(w.config.objectSetVarId)),text(w.config[key]));property(external(text(w.config.objectSetVarId)),w.config.colorBy==null?"status":text(w.config.colorBy));}
  if(w.type==="Histogram")property(external(text(w.config.objectSetVarId)),text(w.config.property));
  if(w.type==="ProminentTerms")property(external(text(w.config.objectSetVarId)),w.config.property==null?"status":text(w.config.property));
  if(w.type==="SummaryStats")property(external(text(w.config.objectSetVarId)),text(w.config.property));
  if(w.type==="Calendar")property(external(text(w.config.objectSetVarId)),text(w.config.dateProperty));
  if(w.type==="StatusTracker"){property(origin?sourceInterfaceObject(origin,text(w.config.objectVarId))??"":"",text(w.config.activeProp));}
  if(w.type==="PropertyList"){const type=origin?sourceInterfaceObject(origin,text(w.config.objectVarId))??"":"";for(const p of Array.isArray(w.config.properties)?w.config.properties:[])property(type,text(p));}
  if(w.type==="InlineAction"&&text(w.config.actionId))actions.add(text(w.config.actionId));
 }
 return {objects:[...objects],fields,actions:[...actions]};
}

/** Each explicit graph mapping control edits one part of the same reviewed binding. */
export function patchGraphImportBinding(current:NonNullable<ImportBindings["graphs"]>[string]|undefined,patch:Partial<NonNullable<ImportBindings["graphs"]>[string]>):NonNullable<ImportBindings["graphs"]>[string] {return {relations:current?.relations??[],labelFields:current?.labelFields??{},...current,...patch};}

function aiImportInterfaceRecords(module:SourceModule){
 const ports=Array.isArray(module.moduleInterface)?module.moduleInterface as {variableId?:string}[]:[];
 return ports.flatMap(port=>{const variable=module.variables.find(v=>v.id===port.variableId),objectType=sourceInterfaceObject(module,port.variableId??'');return variable?.type==='object'&&objectType?[{id:variable.id,title:variable.name,object:objectType}]:[];});
}
export function aiImportRecordChoices(module:SourceModule,page:string,widget:string){
 const choices=collaborationRecordChoices(module,page,widget),ids=new Set(choices.map(c=>c.id));
 return [...choices,...aiImportInterfaceRecords(module).filter(c=>!ids.has(c.id))];
}
