import {t} from '@platform/ui';
import {isStringSet} from '../runtime/decimal';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {HistogramRenderer} from './Histogram';
import type {TermCountsRenderer} from './TermCounts';
import type {CollectionAnalysisRenderer} from './CollectionAnalysis';
const define=defineWidgetPlugin<WidgetBindingContext>();
const objectOf=({page,section}:WidgetBindingContext)=>section.object?.name||page.object.name;
const selection=({groupValue}:WidgetBindingContext)=>groupValue?.status==='value'?typeof groupValue.value==='string'?[groupValue.value]:isStringSet(groupValue.value)?groupValue.value.values:[]:[];
const terms=(context:WidgetBindingContext):Parameters<typeof TermCountsRenderer>[0]=>({object:objectOf(context),window:context.window,field:context.section.group??'',label:context.section.title||t('Term counts'),info:context.info,source:context.aggregateSource});
export const distributionPlugins={
 histogram:define('histogram',1,(context):Parameters<typeof HistogramRenderer>[0]=>({object:objectOf(context),window:context.window,fields:context.section.histogram,label:context.section.title||t('Histogram'),info:context.info,source:context.aggregateSource}),()=>import('./Histogram').then(m=>m.HistogramRenderer)),
 'term-counts':define('term-counts',1,terms,()=>import('./TermCounts').then(m=>m.TermCountsRenderer)),
 treemap:define('treemap',1,(context):Parameters<typeof TermCountsRenderer>[0]=>({...terms(context),label:context.section.title||t('Treemap'),treemap:true,selected:selection(context),enabled:context.enabled,onSelect:context.onGroupFilter}),()=>import('./TermCounts').then(m=>m.TermCountsRenderer)),
 'tag-counts':define('tag-counts',1,(context):Parameters<typeof TermCountsRenderer>[0]=>({...terms(context),label:context.section.title||t('Tag counts'),tags:true,selected:selection(context),enabled:context.enabled,onSelect:context.onGroupFilter}),()=>import('./TermCounts').then(m=>m.TermCountsRenderer)),
 'collection-analysis':define('collection-analysis',1,(context):Parameters<typeof CollectionAnalysisRenderer>[0]=>({config:context.section.analysis,object:objectOf(context),info:context.info,window:context.window,source:context.aggregateSource,label:context.section.title||t('Collection analysis'),values:context.facetValues??{},xVariable:context.section.analysisXVariable,yVariable:context.section.analysisYVariable,countVariable:context.section.analysisCountVariable,meanVariable:context.section.analysisMeanVariable,onAxis:context.onFacet,enabled:context.enabled}),()=>import('./CollectionAnalysis').then(m=>m.CollectionAnalysisRenderer)),
};
