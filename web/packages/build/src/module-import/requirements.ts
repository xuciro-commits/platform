import type {SourceModule} from "./compile";

const text=(v:unknown)=>typeof v==="string"?v:"";
const object=(v:unknown)=>v&&typeof v==="object"&&!Array.isArray(v)?v as Record<string,unknown>:undefined;
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
  if(w.type==="ChartPie")property(external(text(w.config.objectSetVarId)),text(w.config.groupBy));
  if(w.type==="PivotTable")for(const key of ["rows","cols"])property(external(text(w.config.objectSetVarId)),text(w.config[key]));
  if(w.type==="KanbanBoard")property(external(text(w.config.objectSetVarId)),text(w.config.groupBy));
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
