import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {ExplorationRenderer} from './Exploration';
import type {ActionTableRenderer} from './RecordWork';
import type {ObservationRenderer} from './Observation';
const define=defineWidgetPlugin<WidgetBindingContext>();
const graph=(kind:'graph-explorer'|'vertex-graph',c:WidgetBindingContext):Parameters<typeof ExplorationRenderer>[0]=>({kind,config:c.section.graphExplorer,vertex:c.section.vertexGraph,root:c.explorationRoot,status:c.explorationStatus,rootObject:c.section.object?.name?c.section.object:c.page.object,source:c.session?.readSource()??c.recordSource!,definitions:c.definitions??[],identity:c.explorationIdentity??'',isActive:c.explorationActive??(()=>false),onOutput:c.onGraphOutput,selected:c.graphSelected,enabled:c.enabled,label:c.section.title||t('Record exploration'),readCurrent:c.contextReadCurrent});
export const analysisServicePlugins={
 'graph-explorer':define('graph-explorer',1,c=>graph('graph-explorer',c),()=>import('./Exploration').then(m=>m.ExplorationRenderer)),
 'vertex-graph':define('vertex-graph',1,c=>graph('vertex-graph',c),()=>import('./Exploration').then(m=>m.ExplorationRenderer)),
 'action-table':define('action-table',1,(c):Parameters<typeof ActionTableRenderer>[0]=>({config:c.section.actionTable,actionSchema:c.section.actions?.[0]?.name,object:c.section.object?.name||c.page.object.name,window:c.window,readSource:c.session?.readSource(),scope:c.aggregateScope??'',enabled:c.enabled,live:c.live,readCurrent:c.contextReadCurrent}),()=>import('./RecordWork').then(m=>m.ActionTableRenderer)),
 observation:define('observation',1,(c):Parameters<typeof ObservationRenderer>[0]=>{const section=c.section,primary=section.object?.name||c.page.object.name,plan=c.page.document?.variables?.[section.observation?.kind==='availability'?section.observationHistoryVariable??'':section.collectionVariable??'']?.source?.query;return {primary,object:c.page.document?.queries?.[plan??'']?.object.name??primary,config:section.observation,title:section.title,signalVariable:section.observationSignalVariable,thresholdVariable:section.observationThresholdVariable,rowsVariable:section.observationRowsVariable,countVariable:section.observationCountVariable,meanVariable:section.observationMeanVariable,window:c.window,history:c.observationHistory,context:c.observationContext,asset:c.observationAsset,selectedRow:c.observationSelected,onRow:c.onObservationRow,values:c.facetValues??{},onState:c.onFacet,readSource:c.session?.readSource(),scope:c.aggregateScope??'',enabled:c.enabled,readCurrent:c.contextReadCurrent,live:c.live};},()=>import('./Observation').then(m=>m.ObservationRenderer)),
};
