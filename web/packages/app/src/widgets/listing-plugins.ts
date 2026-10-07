import {t} from '@platform/ui';
import {pageUIManifest} from '@platform/kernel';
import {tableEditableFields} from './table-edit';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {TableSurfaceProps} from './TableSurface';
import type {RecordTimelineRenderer} from './RecordTimeline';
import type {KanbanRenderer} from './Kanban';
import type {InlineActionForm} from '../actions/actions';
const define=defineWidgetPlugin<WidgetBindingContext>();
const object=(c:WidgetBindingContext)=>c.section.object?.name||c.page.object.name;
const parent=(c:WidgetBindingContext)=>c.section.parentSelection?c.page.selections?.find(v=>v.name===c.section.parentSelection)?.object.name??'':c.page.object.name;
const domain=(values:Record<string,unknown>)=>Object.entries(values).filter(([,v])=>v!==undefined&&v!=='').map(([k,v])=>[k,'=',v]);
export function bindTable(c:WidgetBindingContext):TableSurfaceProps{
 const {page,section,master,recordSource:source,info,window,collection}=c,type=object(c);
 if(section.widget==='record-list'&&section.recordList?.layout==='tiles')return {tiles:{window,object:type,labelField:section.cardLabel??'id',selected:c.selected?.id,onSelect:c.enabled===false||!section.selection?undefined:record=>c.onSelect(record),label:section.title||t('Record tiles'),readCurrent:c.contextReadCurrent}};
 if(!source)return {message:'No source for records'};
 const isMaster=type===parent(c)&&!section.parentSelection&&!section.relation;
 const query=section.query?.name?c.definitions?.find(d=>d.ref.app===section.query?.app&&d.ref.kind===section.query.kind&&d.ref.name===section.query.name)?.query:undefined;
 const reference=!isMaster?(query?.by?info?.fields.find(f=>f.name===query.by):info?.fields.find(f=>f.type==='reference'&&f.ref===parent(c)&&(!section.relation||f.inverse===section.relation))):undefined;
 const action=section.inlineEdit?c.catalog?.find(a=>a.schema===section.inlineEdit!.action.name):undefined,fields=tableEditableFields(info,action,section.inlineEdit?.fields??[]);
 const inlineEdit=action&&fields.length&&c.decide?{schema:action.schema,fields,scope:JSON.stringify([c.aggregateScope,action]),preview:!c.live,maxRows:pageUIManifest.runtime.tableEditing.maxRows,submit:async(record:import('@platform/ui').EntityRecord,patch:Record<string,unknown>)=>{let error:string|undefined;const accepted=await c.decide!(action.schema,{type,id:record.id},patch,{expectedRevision:record.revision,quiet:true,onRefused:reason=>error=reason});return {accepted,error};}}:undefined;
 if(section.collectionVariable&&collection?.status==='error')return {message:collection.code};
 const status=section.collectionVariable?!window?'missing-window':undefined:(section.relation||section.parentSelection)&&!reference?'invalid-reference':reference&&!master?'missing-parent':undefined;
 const predicates=[...((query?.domain as unknown[]|undefined)??[]),...domain(c.narrowed[type]??{}),...domain(c.sharedFilter??{}),...reference&&master?[[reference.name,'=',master.id]]:[]];
 return {tableKey:section.collectionVariable?type:reference?`${type}/${reference.name}/${master?.id}`:type,navigate:section.widget==='record-list'&&c.live,ports:{cards:section.widget==='record-list'?{layout:section.recordList?.layout as 'grid'|'list'??'grid',labelField:section.cardLabel??'id'}:undefined,source:section.collectionVariable?source:c.session?.querySource(section.id??`section:${page.sections?.indexOf(section)}`)??source,presentation:section.tablePresentation,columns:section.tableColumns,showSearch:section.showSearch,keepActive:section.widget==='record-list'||c.keepActive,selectionSet:c.selectionSet,inlineEdit,object:type,fields:section.fields,domain:section.collectionVariable?undefined:predicates,window,selected:c.selected,onSelect:c.onSelect,status,plural:info?.plural?.toLowerCase()}};
}
export const listingPlugins={
 table:define('table',1,bindTable,()=>import('./TableSurface').then(m=>m.TableSurfaceRenderer)),
 'record-list':define('record-list',1,bindTable,()=>import('./TableSurface').then(m=>m.TableSurfaceRenderer)),
 'record-timeline':define('record-timeline',1,(c):Parameters<typeof RecordTimelineRenderer>[0]=>{const {section,info}=c,start=info?.fields.find(f=>f.name===section.timeStart),end=section.timeEnd?info?.fields.find(f=>f.name===section.timeEnd):undefined,label=(name?:string)=>name==='id'||!!info?.fields.some(f=>f.name===name&&['text','longtext','choice','reference'].includes(f.type)),valid=start&&['date','datetime'].includes(start.type)&&(!section.timeEnd||end?.type===start.type)&&label(section.timeLabel)&&(!section.timeGroup||label(section.timeGroup));return {window:c.window,fields:valid?{start:section.timeStart!,end:section.timeEnd,label:section.timeLabel!,group:section.timeGroup,kind:start.type as 'date'|'datetime'}:undefined,selected:c.selected,onSelect:c.onSelect,title:section.title||t('Record timeline')};},()=>import('./RecordTimeline').then(m=>m.RecordTimelineRenderer)),
 kanban:define('kanban',1,(c):Parameters<typeof KanbanRenderer>[0]=>{const {section,info}=c,type=object(c),catalog=c.catalog??[],allowed=new Set((section.actions??[]).map(a=>a.name)),moves=(info?.lifecycle?.transitions??[]).filter(m=>allowed.has(m.schema)&&catalog.some(a=>a.schema===m.schema&&a.target===type)&&(!m.toInput&&m.to.length===1||!!m.toInput&&Number(c.page.document?.uiProfile.split('.').at(-1))>=99&&catalog.some(a=>a.schema===m.schema&&a.payload.some(p=>p.name===m.toInput&&p.type==='string'&&m.to.every(state=>p.choices?.includes(state)))))).flatMap(m=>m.to.map(to=>({schema:m.schema,title:m.title,from:m.from,to,input:m.toInput})));return {object:type,info,window:c.window,cardLabel:section.cardLabel??'',fields:section.fields,selected:c.selected,onSelect:c.onSelect,moves,live:c.live,title:section.title||t('Kanban board')};},()=>import('./Kanban').then(m=>m.KanbanRenderer),c=>JSON.stringify([object(c),c.window?.query,c.sourceScope])),
 'inline-action':define('inline-action',1,(c):Parameters<typeof InlineActionForm>[0]=>({type:object(c),schema:c.section.actions?.[0]?.name??'',record:c.selected,live:c.live,scope:c.aggregateScope,defaults:c.section.actionDefaults,ready:c.actionReady}),()=>import('../actions/actions').then(m=>m.InlineActionForm)),
};
