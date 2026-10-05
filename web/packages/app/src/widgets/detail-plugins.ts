import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {DetailRenderer,StatusRenderer,LinksRenderer,RecordViewRenderer,ActionsRenderer} from './RecordDetails';
const define=defineWidgetPlugin<WidgetBindingContext>();
const record=(c:WidgetBindingContext)=>({source:c.readSource??c.recordSource,object:c.section.object?.name||c.page.object.name,record:c.selected});
export const detailPlugins={
 detail:define('detail',1,(c):Parameters<typeof DetailRenderer>[0]=>({...record(c),fields:c.section.fields,presentation:c.section.detailPresentation}),()=>import('./RecordDetails').then(m=>m.DetailRenderer)),
 'status-tracker':define('status-tracker',1,(c):Parameters<typeof StatusRenderer>[0]=>({...record(c),config:c.section.statusTracker}),()=>import('./RecordDetails').then(m=>m.StatusRenderer)),
 'record-links':define('record-links',1,(c):Parameters<typeof LinksRenderer>[0]=>({...record(c),groups:c.section.recordLinks??[],live:c.live,onOpen:c.onRecordOpen}),()=>import('./RecordDetails').then(m=>m.LinksRenderer)),
 'record-view':define('record-view',1,(c):Parameters<typeof RecordViewRenderer>[0]=>({...record(c),fields:c.section.fields??[],tabs:c.section.recordView?.tabs,allowed:(c.section.actions??[]).map(a=>a.name),live:c.live,onOpen:c.onRecordOpen}),()=>import('./RecordDetails').then(m=>m.RecordViewRenderer)),
 actions:define('actions',1,(c):Parameters<typeof ActionsRenderer>[0]=>({object:c.section.object?.name||c.page.object.name,record:c.selected,allowed:(c.section.actions??[]).map(a=>a.name),live:c.live}),()=>import('./RecordDetails').then(m=>m.ActionsRenderer)),
};
