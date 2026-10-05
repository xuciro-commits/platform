import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {FilterRenderer} from './FilterSurface';
import type {CreateFormRenderer} from './CreateForm';
import type {HistoryRenderer,TasksRenderer} from './RecordActivity';
import type {FunctionAdviceRenderer} from './FunctionAdvice';
const define=defineWidgetPlugin<WidgetBindingContext>();
const object=(c:WidgetBindingContext)=>c.section.object?.name||c.page.object.name;
const parent=(c:WidgetBindingContext)=>c.section.parentSelection?c.page.selections?.find(v=>v.name===c.section.parentSelection)?.object.name??'':c.page.object.name;
export const authoringPlugins={
 filter:define('filter',1,(c):Parameters<typeof FilterRenderer>[0]=>({object:object(c),config:{title:c.section.title,fields:c.section.fields,facets:c.section.facets,filterSearchVariable:c.section.filterSearchVariable},source:c.recordSource,filters:c.narrowed[object(c)]??{},onNarrow:c.onNarrow,facetValues:c.facetValues,onFacet:c.onFacet,window:c.window,aggregateScope:c.aggregateScope}),()=>import('./FilterSurface').then(m=>m.FilterRenderer)),
 form:define('form',1,(c):Parameters<typeof CreateFormRenderer>[0]=>({object:object(c),parentObject:parent(c),config:{fields:c.section.fields,inputs:c.section.inputs,relation:c.section.relation},live:c.live,master:c.master}),()=>import('./CreateForm').then(m=>m.CreateFormRenderer),c=>`${object(c)}/${c.section.relation??''}/${c.section.parentSelection??''}/${c.section.relation?c.master?.id??'':''}`),
 timeline:define('timeline',1,(c):Parameters<typeof HistoryRenderer>[0]=>({object:object(c),historyLimit:c.section.historyLimit,selected:c.selected,readSource:c.readSource??c.session?.readSource(),confirmedRecord:c.confirmedRecord,recordStatus:c.recordStatus}),()=>import('./RecordActivity').then(m=>m.HistoryRenderer)),
 tasks:define('tasks',1,(c):Parameters<typeof TasksRenderer>[0]=>({object:object(c),selected:c.selected,live:c.live,readSource:c.readSource}),()=>import('./RecordActivity').then(m=>m.TasksRenderer)),
 function:define('function',1,(c):Parameters<typeof FunctionAdviceRenderer>[0]=>({recordType:c.page.object.name,functionBinding:c.section.function,selected:c.selected,live:c.live}),()=>import('./FunctionAdvice').then(m=>m.FunctionAdviceRenderer)),
};
