import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {GaugeRenderer} from './Gauge';
import type {ProgressRenderer} from './Progress';
import type {SummaryRenderer} from './Summary';
import type {SparklineRenderer} from './Sparkline';
import type {AlertRenderer} from './Alert';
const define=defineWidgetPlugin<WidgetBindingContext>();
export const scalarPlugins={
 gauge:define('gauge',1,({section,gaugeValue}):Parameters<typeof GaugeRenderer>[0]=>({value:gaugeValue,fields:section.gauge,title:section.title||t('Gauge')}),()=>import('./Gauge').then(m=>m.GaugeRenderer)),
 progress:define('progress',1,({section,progressValue,progressTotal}):Parameters<typeof ProgressRenderer>[0]=>({value:progressValue,total:progressTotal,fixedTotal:section.progressTotalVariable?undefined:section.progressTotal,title:section.progressLabel??section.title??''}),()=>import('./Progress').then(m=>m.ProgressRenderer)),
 'summary-stats':define('summary-stats',1,({section,statisticsValue,info}):Parameters<typeof SummaryRenderer>[0]=>({value:statisticsValue,info,field:section.summaryField}),()=>import('./Summary').then(m=>m.SummaryRenderer)),
 'sparkline-kpi':define('sparkline-kpi',1,({section,window,sparklineValue,info}):Parameters<typeof SparklineRenderer>[0]=>({value:sparklineValue,window,info,fields:section.sparkline,title:section.title||t('Sparkline KPI')}),()=>import('./Sparkline').then(m=>m.SparklineRenderer)),
 'alert-banner':define('alert-banner',1,({section,alertValue}):Parameters<typeof AlertRenderer>[0]=>({value:alertValue,config:section.alertBanner,title:section.title||t('Alert banner')}),()=>import('./Alert').then(m=>m.AlertRenderer)),
};
