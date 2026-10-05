import {compileWorkshopModule,parseWorkshopModule,type ImportBindings,type ImportDiagnostic,type ImportReport,type ImportTarget} from './compile';
import type {PageDraft} from '../page-editor/draft';

export type ImportPageDestination={sourcePage:string;id:string;name:string;object:string;revision?:number};
export type ApplicationImportReport={formatVersion:2;source:string;bindings:ImportBindings;destinations:ImportPageDestination[];pages:{destination:ImportPageDestination;report:ImportReport}[];diagnostics:(ImportDiagnostic&{page?:string})[];ready:boolean};
const pointer=(value:string)=>value.replaceAll('~','~0').replaceAll('/','~1');

/** All pages use the original compiler and one reviewed binding map. No writes. */
export function compileWorkshopApplication(source:string,bindings:ImportBindings,destinations:ImportPageDestination[],target:Omit<ImportTarget,'object'>):ApplicationImportReport {
 const parsed=parseWorkshopModule(source),result:ApplicationImportReport={formatVersion:2,source,bindings,destinations,pages:[],diagnostics:[...parsed.diagnostics],ready:false};
 if(!parsed.module)return result;
 const ids=new Set<string>(),names=new Set<string>(),sources=new Set<string>();
 for(const destination of destinations){
  if(!parsed.module.pages.some(p=>p.id===destination.sourcePage)||sources.has(destination.sourcePage)||!destination.id||ids.has(destination.id)||! /^[a-z][a-z0-9]{0,63}$/.test(destination.name)||names.has(destination.name)||!target.entities.some(e=>e.type===destination.object)||destination.revision!==undefined&&(!Number.isSafeInteger(destination.revision)||destination.revision<1))result.diagnostics.push({path:`/destinations/${pointer(destination.sourcePage)}`,code:'application-page-destination',blocking:true});
  sources.add(destination.sourcePage);ids.add(destination.id);names.add(destination.name);
 }
 for(const page of parsed.module.pages){
  const destination=destinations.find(d=>d.sourcePage===page.id);
  if(!destination){result.diagnostics.push({page:page.id,path:`/pages/${pointer(page.id)}`,code:'application-page-destination',blocking:true});continue;}
  const report=compileWorkshopModule(source,page.id,bindings,{...target,object:destination.object});
  result.pages.push({destination,report});
  result.diagnostics.push(...report.diagnostics.filter(d=>d.code!=='other-pages-retained').map(d=>({...d,page:page.id})));
 }
 result.ready=parsed.module.pages.length>0&&result.pages.length===parsed.module.pages.length&&!result.diagnostics.some(d=>d.blocking)&&result.pages.every(p=>!!p.report.draft);
 return result;
}

export type ImportedPageRecord={id:string;revision:number;published?:string;[key:string]:unknown};
export type ImportPageWrite={destination:ImportPageDestination;payload:{name:string;object:string}&PageDraft};
export function importedPageWrites(report:ApplicationImportReport):ImportPageWrite[]{
 if(!report.ready)throw Error('application-import-not-ready');
 // Snapshot the accepted bytes. Later mapping edits cannot alter an in-flight write.
 return JSON.parse(JSON.stringify(report.pages.map(({destination,report:r})=>({destination,payload:{name:destination.name,object:destination.object,...r.draft!}}))));
}
const canonical=(value:unknown):string=>JSON.stringify(value,(_,v)=>v&&typeof v==='object'&&!Array.isArray(v)?Object.fromEntries(Object.entries(v).sort(([a],[b])=>a.localeCompare(b))):v);
/** Go omits these typed zero values when re-encoding a page. Do not normalize
 * arbitrary values: variable initials, labels and explicit sizes retain meaning. */
function normalizedDocument(document:unknown):unknown {
 if(!document||typeof document!=='object')return document;
 const copy=JSON.parse(JSON.stringify(document)) as ApiDocument;
 for(const node of Object.values(copy.nodes??{}))for(const key of ['showHeader','border','collapsible','defaultCollapsed'])if(node.presentation?.[key]===false)delete node.presentation[key];
 for(const query of Object.values(copy.queries??{})){if(query.offset===0)delete query.offset;if(Array.isArray(query.conditions)&&!query.conditions.length)delete query.conditions;}
 return copy;
}
type ApiDocument={nodes?:Record<string,{presentation?:Record<string,unknown>}>;queries?:Record<string,{offset?:number;conditions?:unknown[]}>};
function normalizedSections(sections:unknown):unknown{
 if(!Array.isArray(sections))return sections;
 return sections.map(section=>{if(!section.embedding)return section;const embedding={...section.embedding};for(const key of ['inputs','results'])if(embedding[key]&&typeof embedding[key]==='object'&&!Object.keys(embedding[key]).length)delete embedding[key];return {...section,embedding};});
}
const matches=(record:ImportedPageRecord,payload:ImportPageWrite['payload'])=>Object.entries(payload).every(([key,value])=>key==='document'?canonical(normalizedDocument(record[key]))===canonical(normalizedDocument(value)):key==='sections'?canonical(normalizedSections(record[key]??[]))===canonical(normalizedSections(value)):canonical(record[key]??(key==='description'?'':Array.isArray(value)&&!value.length?[]:undefined))===canonical(value));
export type ImportSaveProgress={page:string;state:'saved'|'published'};
export type ImportSaveIO={
 active:()=>boolean;
 read:(id:string)=>Promise<ImportedPageRecord|undefined>;
 pending:(id:string)=>boolean;
 decide:(schema:string,id:string,payload:unknown,revision?:number)=>Promise<boolean>;
 progress:(value:ImportSaveProgress)=>void;
};
/** Original page decisions, ordered and resumable by authoritative reads.
 * A failure keeps confirmed pages; it never activates an application. */
export async function saveImportedPages(writes:ImportPageWrite[],io:ImportSaveIO):Promise<void>{
 const assertActive=()=>{if(!io.active())throw Error('application-import-scope-changed');};
 // Save every draft before making any of these new page definitions available.
 for(const {destination:d,payload} of writes){
  assertActive();let record=await io.read(d.id);assertActive();
  if(io.pending(d.id))throw Error('application-import-pending');
  if(!record||!matches(record,payload)){
   if(record&&record.revision!==d.revision||!record&&d.revision!==undefined)throw Error('application-import-conflict');
   if(!await io.decide(`build.page.${record?'edit':'create'}`,d.id,payload,record?.revision))throw Error('application-import-save-refused');
   assertActive();record=await io.read(d.id);assertActive();
   if(!record||!matches(record,payload))throw Error('application-import-conflict');
  }
  io.progress({page:d.sourcePage,state:'saved'});
 }
 for(const {destination:d,payload} of writes){
  assertActive();const record=await io.read(d.id);assertActive();
  if(io.pending(d.id))throw Error('application-import-pending');
  if(!record||!matches(record,payload))throw Error('application-import-conflict');
  let published:ImportedPageRecord|undefined;try{published=record.published?JSON.parse(record.published):undefined;}catch{throw Error('application-import-conflict');}
  if(!published||!matches(published,payload)){
   if(!await io.decide('build.page.publish',d.id,{},record.revision))throw Error('application-import-publish-refused');
   assertActive();const refreshed=await io.read(d.id);assertActive();
   try{published=refreshed?.published?JSON.parse(refreshed.published):undefined;}catch{throw Error('application-import-conflict');}
   if(!published||!matches(published,payload))throw Error('application-import-conflict');
  }
  io.progress({page:d.sourcePage,state:'published'});
 }
}
