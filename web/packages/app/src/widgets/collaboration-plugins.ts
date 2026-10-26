import {t} from '@platform/ui';
import {defineWidgetPlugin} from './plugin';
import {recordResourceSlot} from '../runtime/resources';
import type {WidgetBindingContext} from './bindings';
import type {RecordCollaborationRenderer} from './RecordCollaboration';
const define=defineWidgetPlugin<WidgetBindingContext>();
const bind=(c:WidgetBindingContext):Parameters<typeof RecordCollaborationRenderer>[0]=>{
 const {page,section,session}=c,source=session?.readSource()??c.recordSource,slot=recordResourceSlot(page,section.sceneSampleVariable),sample=slot?session?.confirmedSelected(slot):undefined,reference=slot?session?.snapshot().records[slot]:undefined,info=reference?.status==='value'?source?.entity(reference.value.object):undefined;
 return {sceneWindow:c.sceneWindow,sceneSampleObject:page.document?.queries?.[page.document?.variables?.[section.sceneSampleCollectionVariable??'']?.source?.query??'']?.object.name,scene:section.scene,sample:sample&&info?{record:sample,info}:undefined,part:c.facetValues?.[section.scenePartVariable??''],onPart:value=>{if(section.scenePartVariable)c.onFacet?.(section.scenePartVariable,value);},kind:section.widget,label:section.title||t('Record collaboration'),record:c.collaborationRecord,reference:c.collaborationReference,status:c.collaborationStatus,slot:c.collaborationSlot,bindingEpoch:c.collaborationSlot?session?.recordBindingEpoch(c.collaborationSlot):undefined,captureRecordLease:session?slot=>session.captureRecordLease(slot):undefined,readSource:session?.readSource(),draft:c.commentDraft,fileValue:c.fileValue,pageValue:c.pdfPageValue,onDraft:c.onCommentDraft,onFileID:c.onFileID,onPage:c.onPdfPage,enabled:c.enabled,live:c.live};
};
export const collaborationPlugins={
 'record-comments':define('record-comments',1,bind,()=>import('./RecordCollaboration').then(m=>m.RecordCollaborationRenderer)),
 'record-uploader':define('record-uploader',1,bind,()=>import('./RecordCollaboration').then(m=>m.RecordCollaborationRenderer)),
 'media-preview':define('media-preview',1,bind,()=>import('./RecordCollaboration').then(m=>m.RecordCollaborationRenderer)),
 'pdf-viewer':define('pdf-viewer',1,bind,()=>import('./RecordCollaboration').then(m=>m.RecordCollaborationRenderer)),
 'image-annotation':define('image-annotation',1,bind,()=>import('./RecordCollaboration').then(m=>m.RecordCollaborationRenderer)),
 'scene-3d':define('scene-3d',1,bind,()=>import('./RecordCollaboration').then(m=>m.RecordCollaborationRenderer)),
};
