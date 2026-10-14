import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {LayeredGraphRenderer,ModelMatrixRenderer} from './CompositeViews';
const define=defineWidgetPlugin<WidgetBindingContext>();
export const compositePlugins={
 'layered-graph':define('layered-graph',1,({section,compositeData,enabled,onSelect,onRecordOpen,selected,sourceScope}):Parameters<typeof LayeredGraphRenderer>[0]=>({config:section.composite,data:compositeData,label:section.title||t('Layered graph'),enabled,onSelect,onOpen:onRecordOpen,selected,storeKey:JSON.stringify([sourceScope,section.id,section.composite])}),()=>import('./CompositeViews').then(m=>m.LayeredGraphRenderer)),
 'model-matrix':define('model-matrix',1,({section,compositeData,enabled,onSelect,selected}):Parameters<typeof ModelMatrixRenderer>[0]=>({config:section.composite,data:compositeData,enabled,onSelect,selected}),()=>import('./CompositeViews').then(m=>m.ModelMatrixRenderer)),
};
