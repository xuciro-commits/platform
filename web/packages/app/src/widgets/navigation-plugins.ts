import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {BreadcrumbRenderer,StaticImageRenderer} from './ContextViews';
import type {NotepadRenderer} from './RecordWork';
import type {CollectionTitleRenderer,ButtonGroupRenderer} from './Navigation';
const define=defineWidgetPlugin<WidgetBindingContext>();
export const navigationPlugins={
 'collection-title':define('collection-title',1,({section,countValue,countError}):Parameters<typeof CollectionTitleRenderer>[0]=>({title:section.title||t('Collection title'),value:countValue,error:countError}),()=>import('./Navigation').then(m=>m.CollectionTitleRenderer)),
 'button-group':define('button-group',1,({section,onControl,controlBound,enabled}):Parameters<typeof ButtonGroupRenderer>[0]=>({buttons:section.buttons??[],label:section.title||t('Button group'),onActivate:id=>onControl?.(id),isBound:controlBound??(()=>false),enabled}),()=>import('./Navigation').then(m=>m.ButtonGroupRenderer)),
 'static-image':define('static-image',1,({section}):Parameters<typeof StaticImageRenderer>[0]=>({config:section.image,label:section.title||t('Image')}),()=>import('./ContextViews').then(m=>m.StaticImageRenderer)),
 breadcrumb:define('breadcrumb',1,({section,confirmedRecord,recordStatus,onControl,onClearContext,enabled,contextReadCurrent,info}):Parameters<typeof BreadcrumbRenderer>[0]=>({readCurrent:contextReadCurrent,config:section.breadcrumb,record:confirmedRecord,status:recordStatus,info,onHome:()=>onControl?.('home'),onClear:onClearContext,enabled,label:section.title||t('Breadcrumbs')}),()=>import('./ContextViews').then(m=>m.BreadcrumbRenderer)),
 notepad:define('notepad',1,({section,notepadValue,onNotepad,enabled,contextReadCurrent}):Parameters<typeof NotepadRenderer>[0]=>({value:notepadValue,onChange:onNotepad,enabled,label:section.title||t('Session notepad'),readCurrent:contextReadCurrent}),()=>import('./RecordWork').then(m=>m.NotepadRenderer)),
};
