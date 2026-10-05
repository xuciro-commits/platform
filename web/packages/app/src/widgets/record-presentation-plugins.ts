import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {RecordCardRenderer} from './RecordCard';
import type {RecordComparisonRenderer} from './RecordComparison';
import type {AvatarStackRenderer} from './ContextViews';
import type {ResourceListRenderer} from './Exploration';
import type {RecordPickerRenderer} from './RecordPicker';
const define=defineWidgetPlugin<WidgetBindingContext>();
export const recordPresentationPlugins={
 'record-card':define('record-card',1,({section,confirmedRecord,recordStatus,info}):Parameters<typeof RecordCardRenderer>[0]=>({record:confirmedRecord,status:recordStatus,info,fields:section.fields??[],config:section.recordCard}),()=>import('./RecordCard').then(m=>m.RecordCardRenderer)),
 'record-comparison':define('record-comparison',1,({section,comparisonRecords,comparisonStatus,info}):Parameters<typeof RecordComparisonRenderer>[0]=>({records:comparisonRecords??[],status:comparisonStatus,info,fields:section.fields??[],config:section.recordComparison}),()=>import('./RecordComparison').then(m=>m.RecordComparisonRenderer)),
 'avatar-stack':define('avatar-stack',1,({section,window,collection,avatarContextStatus,contextReadCurrent,info}):Parameters<typeof AvatarStackRenderer>[0]=>({readCurrent:contextReadCurrent,config:section.avatar,contextStatus:avatarContextStatus,window,collection,info,label:section.title||t('Personnel avatars')}),()=>import('./ContextViews').then(m=>m.AvatarStackRenderer)),
 'resource-list':define('resource-list',1,({section,window,collection,selected,onSelect,enabled,contextReadCurrent,info}):Parameters<typeof ResourceListRenderer>[0]=>({config:section.resourceList,window,collection,info,selected,onSelect,enabled,label:section.title||t('Resource list'),readCurrent:contextReadCurrent}),()=>import('./Exploration').then(m=>m.ResourceListRenderer)),
 'record-picker':define('record-picker',1,({page,section,window,selected,enabled,onSelect,pickerValue,onPickerID,pickerConfirmation,recordSource}):Parameters<typeof RecordPickerRenderer>[0]=>({source:recordSource,type:section.object?.name||page.object.name,window,fields:section.recordPicker,title:section.title||t('Record picker'),selected,enabled,confirmation:section.pickerValueVariable?pickerConfirmation:undefined,value:section.pickerValueVariable?pickerValue:undefined,onSelect:onPickerID??onSelect}),()=>import('./RecordPicker').then(m=>m.RecordPickerRenderer)),
};
