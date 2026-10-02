import {useMemo,useState} from "react";
import {SemanticObjectSelect,useHost,pageLayoutDiagnostics,pageVariableDiagnostics} from "@platform/app";
import {Button,Checkbox,Dialog,Input,Panel,Select,Textarea,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import type {PageDraft} from "../page-editor/draft";
import {compileWorkshopModule,parseWorkshopModule,type ImportBindings,type ImportReport,type SourceModule} from "./compile";
import {workshopMigrationCatalog} from "./catalog";
import {diagnosticMessages,unsupportedProfileMessage} from "./diagnostics";

const text=(v:unknown)=>typeof v==="string"?v:"";
const object=(v:unknown)=>v&&typeof v==="object"&&!Array.isArray(v)?v as Record<string,unknown>:undefined;
function requirements(module:SourceModule|undefined){
 const objects=new Set<string>(),fields:Record<string,Set<string>>=Object.create(null),actions=new Set<string>();
 const external=(id:string)=>{const v=module?.variables.find(v=>v.id===id);return text(object(v?.objectSet)?.objectType||v?.sourceObjectType);};
 const property=(type:string,p:string)=>{if(!type||!p||p==="*")return;objects.add(type);(fields[type]??=new Set()).add(p);};
 for(const v of module?.variables??[]){const set=object(v.objectSet),type=text(set?.objectType||v.sourceObjectType);if(type)objects.add(type);for(const step of Array.isArray(set?.steps)?set.steps:[])for(const clause of Array.isArray(object(step)?.clauses)?object(step)!.clauses as unknown[]:[])property(type,text(object(clause)?.property));}
 for(const w of Object.values(module?.widgets??{})){
  if(w.type==="FilterList")for(const f of Array.isArray(w.config.facets)?w.config.facets:[])property(external(text(w.config.objectSetVarId)),text(object(f)?.property));
  if(w.type==="ObjectTable")for(const c of Array.isArray(w.config.columns)?w.config.columns:[])property(external(text(w.config.objectSetVarId)),text(object(c)?.key));
  if(w.type==="PropertyList"){const variable=module?.variables.find(v=>v.id===w.config.objectVarId),producer=module?.widgets[text(variable?.widgetId)],type=external(text(producer?.config.objectSetVarId));for(const p of Array.isArray(w.config.properties)?w.config.properties:[])property(type,text(p));}
  if(w.type==="InlineAction"&&text(w.config.actionId))actions.add(text(w.config.actionId));
 }
 return {objects:[...objects],fields,actions:[...actions]};
}
const download=(name:string,contents:string)=>{const url=URL.createObjectURL(new Blob([contents],{type:"application/json"})),a=document.createElement("a");a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),0);};

export type ImportPackage={source:string;page:string;bindings:ImportBindings;report:ImportReport};
export function ModuleImportDialog({object:targetObject,profile,open,onClose,onApply,retained}:{object:string;profile:string;open:boolean;onClose:()=>void;onApply:(draft:PageDraft,pack:ImportPackage)=>void;retained?:ImportPackage}){
 const host=useHost(),[source,setSource]=useState(retained?.source??""),[page,setPage]=useState(retained?.page??""),[fileError,setFileError]=useState(false),[accepted,setAccepted]=useState(false),[bindings,setBindings]=useState<ImportBindings>(retained?.bindings??{objects:{},fields:{},actions:{},queries:{}});
 const parsed=useMemo(()=>parseWorkshopModule(source),[source]),module=parsed.module,needs=useMemo(()=>requirements(module),[module]);
 const selected=page||module?.pages[0]?.id||"",report=useMemo(()=>compileWorkshopModule(source,selected,bindings,{object:targetObject,profile,entities:host.entities,actions:host.catalog,definitions:host.definitions}),[source,selected,bindings,targetObject,profile,host.entities,host.catalog,host.definitions]);
 const shapeProblems=report.draft?[...pageLayoutDiagnostics(report.draft.document).map(i=>`${i.node}: ${i.code}`),...pageVariableDiagnostics(report.draft.document.variables??{}).map(i=>`${i.variable}: ${i.code}`)]:[];
 const changeSource=(value:string)=>{setFileError(false);setSource(value);setPage("");setAccepted(false);setBindings({objects:{},fields:{},actions:{},queries:{}});};
 return <Dialog open={open} onOpenChange={value=>!value&&onClose()} title={t("Import Workshop module")} wide><div className="grid min-w-0 gap-3">
 <p className="text-sm text-muted">{t("Map one source page into this page draft. The original module and every unsupported setting stay in the downloadable report. Nothing is saved or run until you apply and review it.")}</p>
 <label className="grid gap-1 text-xs">{t("Workshop JSON file")}<Input type="file" accept=".json,application/json" onChange={async e=>{const file=e.target.files?.[0];if(file){if(file.size>1_048_576)setFileError(true);else changeSource(await file.text());}}}/></label>
 {fileError&&<Panel role="alert">{t("The selected file exceeds the 1 MiB import limit. The previous source is retained; choose a smaller file.")}</Panel>}
 <label className="grid gap-1 text-xs">{t("Source module JSON")}<Textarea rows={6} value={source} onChange={e=>changeSource(e.target.value)}/></label>
 {module&&<><label className="grid gap-1 text-xs">{t("Source page")}<Select value={selected} onChange={e=>{setPage(e.target.value);setAccepted(false);}}>{module.pages.map(p=><option key={p.id} value={p.id}>{p.name}</option>)}</Select></label>
 <Panel className="grid gap-3"><strong>{t("Platform bindings")}</strong>{needs.objects.map(external=>{const type=Object.hasOwn(bindings.objects,external)?bindings.objects[external]:undefined,entity=host.entities.find(e=>e.type===type);return <div key={external} className="grid gap-2"><SemanticObjectSelect label={t("Map object {object}",{object:external})} value={type??""} onChange={ref=>{setAccepted(false);setBindings(b=>({...b,objects:{...b.objects,[external]:ref?.name??""},fields:{...b.fields,[external]:{}}}));}}/>{[...(needs.fields[external]??[])].map(property=><label key={property} className="grid gap-1 text-xs">{t("Map field {field}",{field:`${external}.${property}`})}<Select value={bindings.fields[external]?.[property]??""} onChange={e=>{setAccepted(false);setBindings(b=>({...b,fields:{...b.fields,[external]:{...b.fields[external],[property]:e.target.value}}}));}}><option value="">{t("Choose a platform field")}</option>{property==="id"&&<option value="id">ID</option>}{entity?.fields.map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label>)}</div>;})}
 {needs.actions.map(action=><label key={action} className="grid gap-1 text-xs">{t("Map action {action}",{action})}<Select value={bindings.actions[action]??""} onChange={e=>{setAccepted(false);setBindings(b=>({...b,actions:{...b.actions,[action]:e.target.value}}));}}><option value="">{t("Choose an original record action")}</option>{host.catalog.filter(a=>!a.new).map(a=><option key={a.schema} value={a.schema}>{a.title} · {a.schema}</option>)}</Select></label>)}
 {module.variables.filter(v=>v.type==="objectSet"&&v.definitionKind==="objectSetDefinition").map(v=><label key={v.id} className="grid gap-1 text-xs">{t("Optional query binding {variable}",{variable:v.name})}<Select value={JSON.stringify(bindings.queries[v.id]??null)} onChange={e=>{setAccepted(false);setBindings(b=>({...b,queries:{...b.queries,[v.id]:JSON.parse(e.target.value) as Api.AssetBinding|null||undefined}}));}}><option value="null">{t("Use the translated fixed source")}</option>{host.definitions.filter(d=>d.query).flatMap(d=>Object.keys(d.queryVersions??{[d.version]:d.query}).map(version=><option key={`${d.ref.app}/${d.ref.name}/${version}`} value={JSON.stringify({ref:d.ref,sourceVersion:version})}>{d.ref.name} · {version}</option>))}</Select></label>)}</Panel>
 <Panel role="region" aria-label={t("Migration diagnostics")} className="grid gap-2"><strong>{t("Migration diagnostics")}</strong><p className="text-xs">{t("Blocking diagnostics require a supported configuration or a valid platform binding. Review warnings describe retained content and native presentation or lifetime changes. Paths identify the original JSON setting.")}</p><p className="text-xs text-muted">{t("All {count} source widget types have an owner and migration status.",{count:workshopMigrationCatalog.entries.length})}</p>{report.diagnostics.map((d,i)=><p key={i} role={d.blocking?"alert":undefined} className={d.blocking?"text-sm text-danger":"text-xs text-muted"}>{d.path}: {t(diagnosticMessages[d.code]??unsupportedProfileMessage)} ({d.code})</p>)}{shapeProblems.map(p=><p key={p} role="alert">{p}</p>)}{report.draft&&<p role="status">{t("Mapped {count} widgets into the original V2 page format.",{count:report.draft.sections.length})}</p>}</Panel>
 <Checkbox checked={accepted} onChange={setAccepted}>{t("I reviewed the selected-page scope and the documented presentation, query and lifetime differences.")}</Checkbox></>}
 {!module&&source&&<Panel role="alert">{parsed.diagnostics.map(d=>t(diagnosticMessages[d.code]??unsupportedProfileMessage)).join(", ")}</Panel>}
 <div className="flex flex-wrap justify-end gap-2"><Button disabled={!source} onClick={()=>download("workshop-original.json",source)}>{t("Download original JSON")}</Button><Button disabled={!source} onClick={()=>download("workshop-mapping-report.json",JSON.stringify({...report,bindings},null,2))}>{t("Download mapping report")}</Button><Button variant="primary" disabled={fileError||!accepted||!report.draft||shapeProblems.length>0} onClick={()=>{if(report.draft&&accepted){onApply(report.draft,{source,page:selected,bindings,report});onClose();}}}>{t("Apply imported page draft")}</Button></div>
 </div></Dialog>;
}
