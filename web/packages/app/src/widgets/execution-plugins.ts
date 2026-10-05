import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {WorkViewsRenderer} from './WorkViews';
import type {AIWidget} from './AIWidget';
import type {CollectionBuilderRenderer} from './CollectionBuilder';
import type {ComputeCall} from '../capability';
const define=defineWidgetPlugin<WidgetBindingContext>();
const work=(kind:'approval-inbox'|'notification-feed',c:WidgetBindingContext):Parameters<typeof WorkViewsRenderer>[0]=>({kind,label:c.section.title||t(kind==='approval-inbox'?'Approval inbox':'Notifications'),readSource:c.session?.readSource(),enabled:c.enabled,live:c.live});
export const executionPlugins={
 'approval-inbox':define('approval-inbox',1,c=>work('approval-inbox',c),()=>import('./WorkViews').then(m=>m.WorkViewsRenderer)),
 'notification-feed':define('notification-feed',1,c=>work('notification-feed',c),()=>import('./WorkViews').then(m=>m.WorkViewsRenderer)),
 compute:define('compute',1,(c):Parameters<typeof ComputeCall>[0]=>({binding:c.section.operation,bindings:c.section.inputs,record:c.selected,recordType:c.page.object.name,live:c.live}),()=>import('../capability').then(m=>m.ComputeCall)),
 'ai-assistant':define('ai-assistant',1,(c):Parameters<typeof AIWidget>[0]=>({config:c.section.ai,functionBinding:c.section.function,record:c.explorationRoot,status:c.explorationStatus,identity:c.explorationIdentity??'',active:c.explorationActive??(()=>false),live:c.live,enabled:c.enabled,question:c.facetValues?.[c.section.ai?.questionVariable??''],onQuestion:value=>{const id=c.section.ai?.questionVariable;if(id)c.onFacet?.(id,value);}}),()=>import('./AIWidget').then(m=>m.AIWidget)),
 'collection-builder':define('collection-builder',1,(c):Parameters<typeof CollectionBuilderRenderer>[0]=>({config:c.section.collectionBuilder,builder:c.builder,live:c.live,enabled:c.enabled}),()=>import('./CollectionBuilder').then(m=>m.CollectionBuilderRenderer)),
};
