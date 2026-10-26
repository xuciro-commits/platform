import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import type {WidgetBindingContext} from './bindings';
import type {RecordMapRenderer} from './RecordMap';
import type {AssetDirectoryRenderer} from './Exploration';
import type {ButtonRenderer} from './Button';
import type {EmbeddedPageRenderer,ExternalDocumentRenderer} from './EmbeddedPage';
const define=defineWidgetPlugin<WidgetBindingContext>();
export const surfacePlugins={
 'record-map':define('record-map',1,({section,window,selected,enabled,onSelect,pickerConfirmation,info,sourceScope}):Parameters<typeof RecordMapRenderer>[0]=>({window,info,config:section.map,scope:sourceScope??'',confirmation:pickerConfirmation,selected,enabled,onSelect}),()=>import('./RecordMap').then(m=>m.RecordMapRenderer)),
 'asset-directory':define('asset-directory',1,({section,onControl,controlBound,enabled,definitions}):Parameters<typeof AssetDirectoryRenderer>[0]=>{const pages=section.assetDirectory?.items.filter(i=>i.asset.ref.kind==='page')??[],canOpen=pages.some(i=>controlBound?.(i.id));return {config:section.assetDirectory,definitions:definitions??[],onOpen:canOpen?id=>{if(controlBound?.(id))onControl?.(id);}:undefined,canOpen:controlBound,enabled,label:section.title||t('Asset directory')};},()=>import('./Exploration').then(m=>m.AssetDirectoryRenderer)),
 button:define('button',1,({section,onClick,enabled}):Parameters<typeof ButtonRenderer>[0]=>({title:section.title,onClick,enabled}),()=>import('./Button').then(m=>m.ButtonRenderer)),
 'external-frame':define('external-frame',1,({section,contextReadCurrent}):Parameters<typeof ExternalDocumentRenderer>[0]=>({config:section.externalFrame,title:section.title,readCurrent:contextReadCurrent}),()=>import('./EmbeddedPage').then(m=>m.ExternalDocumentRenderer)),
 'embedded-page':define('embedded-page',1,({section,embeddingInputs,facetValues,aggregateScope,live,contextReadCurrent,enabled,embeddingReturn}):Parameters<typeof EmbeddedPageRenderer>[0]=>({config:section.embedding,title:section.title,values:embeddingInputs??facetValues??{},scope:aggregateScope??'',live,readCurrent:contextReadCurrent,enabled,onReturn:embeddingReturn}),()=>import('./EmbeddedPage').then(m=>m.EmbeddedPageRenderer)),
};
