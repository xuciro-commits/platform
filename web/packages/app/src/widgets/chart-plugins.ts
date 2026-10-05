import {t} from '@platform/ui';
import {compileChartSpec} from './chart-spec';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {ChartSurfaceProps} from './ChartSurface';
import type {RecordChartRenderer} from './RecordChart';
const define=defineWidgetPlugin<WidgetBindingContext>();
const domainOf=(values:Record<string,unknown>)=>Object.entries(values).filter(([,v])=>v!==undefined&&v!=='').map(([field,v])=>[field,'=',v]);
/** Binding owns query composition; renderers receive the original aggregate port. */
export function bindChart(c:WidgetBindingContext,kpi=false,pivot=false):ChartSurfaceProps{
 const {page,section,window,collection,info,master}=c,object=section.object?.name||page.object.name;
 let domain:unknown[];
 if(section.collectionVariable){
  if(!window)return {kpi,message:t(collection?.status==='error'?collection.code:'Query window is unavailable.'),alert:collection?.status==='error',panel:true};
  if(window.error)return {kpi,message:t(window.error),alert:true,panel:true};
  domain=window.query.domain??[];
 }else{
  const parent=section.parentSelection?page.selections?.find(v=>v.name===section.parentSelection)?.object.name??'':page.object.name;
  const isMaster=object===parent&&!section.parentSelection&&!section.relation;
  const reference=!isMaster?info?.fields.find(f=>f.type==='reference'&&f.ref===parent&&(!section.relation||f.inverse===section.relation)):undefined;
  if((section.relation||section.parentSelection)&&!reference)return {kpi,message:t("This section's parent reference is unavailable."),alert:true};
  if(reference&&!master)return {kpi,message:t('Select a record to see related {records}.',{records:info?.plural?.toLowerCase()??object})};
  domain=[...domainOf(c.narrowed[object]??{}),...domainOf(c.sharedFilter??{}),...reference&&master?[[reference.name,'=',master.id]]:[]];
 }
 const spec={...compileChartSpec({object,title:section.title,group:section.group,measure:section.measure,mark:section.mark,chartVariant:section.chartVariant,kpi,domain}),metric:kpi?section.metricPresentation:undefined};
 if(section.collectionVariable&&window){const {domain,search,set,archived,traversal}=window.query;spec.data={entity:object,domain,search,set,archived,traversal};}
 const source=c.aggregateSource;
 return {kpi,spec,source,...pivot?{pivot:{filterAxes:{row:!!(section.rowValueVariable||section.rowSetVariable),column:!!(section.columnValueVariable||section.columnSetVariable)},heatmap:section.widget==='heatmap',enabled:c.enabled,onCellFilter:c.onHeatmap,onClearFilters:c.onHeatmap?()=>c.onHeatmap?.():undefined,object,query:'entity' in spec.data?{domain:spec.data.domain,search:spec.data.search,set:spec.data.set,traversal:spec.data.traversal,archived:spec.data.archived}:{},rows:section.group??'',columns:section.columnGroup,measure:section.measure??'count',source}}:{}};
}
export const chartPlugins={
 chart:define('chart',1,c=>bindChart(c),()=>import('./ChartSurface').then(m=>m.ChartSurfaceRenderer)),
 metric:define('metric',1,c=>bindChart(c,true),()=>import('./ChartSurface').then(m=>m.ChartSurfaceRenderer)),
 pivot:define('pivot',1,c=>bindChart(c,false,true),()=>import('./ChartSurface').then(m=>m.ChartSurfaceRenderer)),
 heatmap:define('heatmap',1,c=>bindChart(c,false,true),()=>import('./ChartSurface').then(m=>m.ChartSurfaceRenderer)),
 'record-chart':define('record-chart',1,({section,window,info}):Parameters<typeof RecordChartRenderer>[0]=>({window,info,fields:{mark:section.recordChart?.mark as 'bar'|'line'??'line',xField:section.recordChart?.xField??'',yField:section.recordChart?.yField??''}}),()=>import('./RecordChart').then(m=>m.RecordChartRenderer)),
};
