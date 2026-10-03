import type {SourceModule,ImportBindings} from "./compile";

const text=(v:unknown)=>typeof v==="string"?v:"";
const object=(v:unknown)=>v&&typeof v==="object"&&!Array.isArray(v)?v as Record<string,unknown>:undefined;
/** Offer only active-record producers actually present in this widget's owner. */
export function collaborationRecordChoices(module:SourceModule|undefined,pageID:string,widgetID:string):{id:string;title:string;object:string}[] {
 if(!module)return [];
 const owners=new Map<string,string|undefined>(),walk=(root:string,owner?:string,seen=new Set<string>())=>{if(seen.has(root))return;seen.add(root);for(const child of module.sections[root]?.children??[]){if(child.kind==="widget")owners.set(child.id,owner);else walk(child.id,owner,seen);}};
 const page=module.pages.find(p=>p.id===pageID);if(!page)return [];walk(page.rootSectionId);module.overlays.forEach(o=>walk(o.rootSectionId,o.id));module.unusedWidgetIds.forEach(id=>owners.set(id,undefined));if(!owners.has(widgetID))return [];
 return module.variables.flatMap(v=>{if(v.type!=="object"||v.definitionKind!=="widgetOutput"||v.widgetOutputKey!=="activeObject"||v.isInterface)return [];
  const producers=Object.values(module.widgets).filter(w=>owners.has(w.id)&&!module.unusedWidgetIds.includes(w.id)&&owners.get(w.id)===owners.get(widgetID)&&["ObjectTable","ObjectList","KanbanBoard","Calendar","ObjectSelector","Leaderboard","ScatterPlot","ResourceList"].includes(w.type)&&w.config.activeVarId===v.id&&(w.type!=="ObjectTable"||v.widgetId===w.id));if(producers.length!==1)return [];
  const set=module.variables.find(value=>value.id===producers[0]!.config.objectSetVarId),external=text(object(set?.objectSet)?.objectType||set?.sourceObjectType);return external?[{id:v.id,title:v.name,object:external}]:[];
 });
}
/** Discover the explicit source metadata that the import mapping dialog must expose. */
export function workshopRequirements(module:SourceModule|undefined){
 const objects=new Set<string>(),fields:Record<string,Set<string>>=Object.create(null),actions=new Set<string>();
 const external=(id:string)=>{const v=module?.variables.find(v=>v.id===id);return text(object(v?.objectSet)?.objectType||v?.sourceObjectType);};
 const property=(type:string,p:string)=>{if(!type||!p||p==="*")return;objects.add(type);(fields[type]??=new Set()).add(p);};
 for(const v of module?.variables??[]){const set=object(v.objectSet),type=text(set?.objectType||v.sourceObjectType);if(type)objects.add(type);for(const step of Array.isArray(set?.steps)?set.steps:[])for(const clause of Array.isArray(object(step)?.clauses)?object(step)!.clauses as unknown[]:[])property(type,text(object(clause)?.property));}
 for(const w of Object.values(module?.widgets??{})){
  if(w.type==="FilterList")for(const f of Array.isArray(w.config.facets)?w.config.facets:[])property(external(text(w.config.objectSetVarId)),text(object(f)?.property));
  if(w.type==="ObjectTable")for(const c of Array.isArray(w.config.columns)?w.config.columns:[])property(external(text(w.config.objectSetVarId)),text(object(c)?.key));
  if(w.type==="ChartXY")for(const key of ["xProperty","yProperty"])property(external(text(w.config.objectSetVarId)),text(w.config[key]));
  if(["ChartVega","ChartWaterfall"].includes(w.type))property(external(text(w.config.objectSetVarId)),"status");
  if(w.type==="DerivedSeries")property(external(text(w.config.objectSetVarId)),"pressure");
  if(w.type==="FreeFormAnalysis")for(const key of ["pressure","temperature","availability","revenueImpact"])property(external(text(w.config.objectSetVarId)),key);
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
  if(w.type==="StatusTracker"){const variable=module?.variables.find(v=>v.id===w.config.objectVarId),producer=module?.widgets[text(variable?.widgetId)];property(external(text(producer?.config.objectSetVarId)),text(w.config.activeProp));}
  if(w.type==="PropertyList"){const variable=module?.variables.find(v=>v.id===w.config.objectVarId),producer=module?.widgets[text(variable?.widgetId)],type=external(text(producer?.config.objectSetVarId));for(const p of Array.isArray(w.config.properties)?w.config.properties:[])property(type,text(p));}
  if(w.type==="InlineAction"&&text(w.config.actionId))actions.add(text(w.config.actionId));
 }
 return {objects:[...objects],fields,actions:[...actions]};
}

/** Each explicit graph mapping control edits one part of the same reviewed binding. */
export function patchGraphImportBinding(current:NonNullable<ImportBindings["graphs"]>[string]|undefined,patch:Partial<NonNullable<ImportBindings["graphs"]>[string]>):NonNullable<ImportBindings["graphs"]>[string] {return {relations:current?.relations??[],labelFields:current?.labelFields??{},...current,...patch};}
