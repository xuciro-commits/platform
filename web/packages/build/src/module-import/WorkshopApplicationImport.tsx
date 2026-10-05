import {useEffect,useRef,useState} from 'react';
import {newId,pageUIProfile,useHost,useRecordInventory} from '@platform/app';
import {Button,Input,Panel,Select,t} from '@platform/ui';
import {SemanticObjectSelect} from '@platform/app';
import type {Api} from '@platform/kernel';
import {ModuleImportDialog} from './ModuleImportDialog';
import {useDirectInstall} from '../release-profile';
import {parseWorkshopModule} from './compile';
import {importedPageWrites,saveImportedPages,type ApplicationImportReport,type ImportPageDestination,type ImportedPageRecord,type ImportPageWrite,type ImportSaveProgress} from './application-import';

type PageRecord=ImportedPageRecord&{name:string;object:string;title:string;archived?:boolean};
const messages:Record<string,string>={
 'application-import-pending':'An earlier page submission is still unconfirmed. Resend it from Pending changes before continuing.',
 'application-import-conflict':'An imported page changed. Review the saved page before continuing; the confirmed pages are retained.',
 'application-import-save-refused':'A page could not be saved. The confirmed pages are retained; review the refusal before continuing.',
 'application-import-publish-refused':'A page could not be published. The saved pages are retained; review the refusal before continuing.',
 'application-import-scope-changed':'The member, application or definition changed. Further import writes stopped.',
};
/** Application authoring orchestration; execution and release remain with their owners. */
export function WorkshopApplicationImport({open,onClose,application,onPrepared}:{open:boolean;onClose:()=>void;application:Api.AssetBinding;onPrepared:(names:string[],header?:Api.ApplicationHeader)=>void}){
 const host=useHost(),inventory=useRecordInventory<PageRecord>('build.page'),directInstall=useDirectInstall();
 const definition=host.definitions.find(d=>d.ref.app===application.ref.app&&d.ref.kind==='app'&&d.ref.name===application.ref.name&&d.version===application.sourceVersion);
 const [destinations,setDestinations]=useState<ImportPageDestination[]>([]),[busy,setBusy]=useState(false),[writes,setWrites]=useState<ImportPageWrite[]>(),[progress,setProgress]=useState<ImportSaveProgress[]>([]),[error,setError]=useState(''),[done,setDone]=useState(false),[acceptedHeader,setAcceptedHeader]=useState<Api.ApplicationHeader>();
 // Page publications change the directory. The captured member, original
 // schemas/actions and application binding must survive our own publications.
 const scope=JSON.stringify([host.me,host.entities,host.catalog,definition?.version,application]),current=useRef({scope,client:host.client,mounted:true}),running=useRef(false);current.current.scope=scope;current.current.client=host.client;
 useEffect(()=>{current.current.mounted=true;return()=>{current.current.mounted=false;};},[]);
 const sourceChanged=(source:string)=>{const parsed=parseWorkshopModule(source),prefix=application.ref.name.slice(0,40),initialObject=host.definitions.find(d=>d.ref.kind==='page'&&d.ref.app==='build'&&d.ref.name===definition?.application?.pages[0])?.page?.object?.name??'';setDestinations(parsed.module?.pages.map((p,i)=>({sourcePage:p.id,id:newId('PAGE'),name:`${prefix}import${i+1}`,object:initialObject}))??[]);setError('');};
 const update=(index:number,patch:Partial<ImportPageDestination>)=>setDestinations(previous=>previous.map((d,i)=>i===index?{...d,...patch}:d));
 const prepare=async(report?:ApplicationImportReport)=>{
  if(running.current||done)return;
  if(!writes&&!report?.ready)return;
  if(report?.bindings.application&&(JSON.stringify(report.bindings.application.binding)!==JSON.stringify(application))){setError(t('Map shared ports to this original application before importing.'));return;}
  const captured=current.current.scope,capturedClient=host.client,active=()=>current.current.mounted&&current.current.scope===captured&&current.current.client===capturedClient;
  const accepted=writes??importedPageWrites(report!);const header=writes?acceptedHeader:report?.header;if(!writes)setAcceptedHeader(header);setWrites(accepted);running.current=true;setBusy(true);setError('');
  try{
   await saveImportedPages(accepted,{
    active,
    read:async id=>{try{return (await host.client.get<{record:ImportedPageRecord}>(`/v1/records/build.page/${encodeURIComponent(id)}`)).record;}catch(failure){if(failure instanceof Error&&/HTTP 404$/.test(failure.message))return;throw failure;}},
    pending:id=>host.client.authorities.outbox.some(e=>e.submission.tenantId===host.client.connection.tenant&&e.submission.target?.type==='build.page'&&e.submission.target.id===id&&['SUBMISSION_STATE_PENDING','SUBMISSION_STATE_SENDING','SUBMISSION_STATE_UNKNOWN'].includes(e.state)),
    decide:async(schema,id,payload,expectedRevision)=>host.decide(schema,{type:'build.page',id},payload,{expectedRevision,quiet:true,onRefused:reason=>{if(active())setError(reason);}}),
    directInstall,
    progress:value=>{if(active())setProgress(previous=>[...previous.filter(p=>p.page!==value.page),value]);},
   });
   if(active()){setDone(true);onPrepared(accepted.map(w=>w.destination.name),header);}
  }catch(failure){if(active())setError(previous=>previous||t(messages[failure instanceof Error?failure.message:'']??'The module import stopped. Confirmed pages are retained; check the original pending changes and saved pages.'));}
  finally{running.current=false;if(current.current.mounted){setBusy(false);if(!active())setError(t(messages['application-import-scope-changed']!));}}
 };
 const controls=<Panel title={t('Application page destinations')} className="grid gap-3">
  <p className="text-xs">{definition?.application?.title} · {application.ref.name}</p>
  <p className="text-xs text-muted">{directInstall?t('All page drafts are saved before page publication. Confirmed pages remain on failure. Application release is reviewed separately.'):t('The pages are saved as drafts. Nothing is installed: the application release delivers them together, and confirmed pages remain on failure.')}</p>
  {inventory.isError&&<p role="alert">{t('The page inventory could not be loaded.')}</p>}
  <fieldset disabled={!!writes||inventory.isLoading||inventory.isError} className="grid gap-3">{destinations.map((d,i)=><div key={d.sourcePage} className="grid gap-2 rounded border border-border p-2">
   <strong className="text-xs">{d.sourcePage}</strong>
   <label className="grid gap-1 text-xs">{t('Destination for {page}',{page:d.sourcePage})}<Select value={d.revision===undefined?'new':d.id} onChange={e=>{const existing=inventory.data?.records.find(p=>p.id===e.target.value);update(i,existing?{id:existing.id,name:existing.name,object:existing.object,revision:existing.revision}:{id:newId('PAGE'),name:`${application.ref.name.slice(0,40)}import${i+1}`,revision:undefined});}}><option value="new">{t('Create a new page')}</option>{inventory.data?.records.filter(p=>!p.archived).map(p=><option key={p.id} value={p.id}>{p.title} · {p.name}</option>)}</Select></label>
   <label className="grid gap-1 text-xs">{t('Imported page name {page}',{page:d.sourcePage})}<Input value={d.name} disabled={d.revision!==undefined} onChange={e=>update(i,{name:e.target.value})}/></label>
   <SemanticObjectSelect label={t('Imported page object {page}',{page:d.sourcePage})} value={d.object} onChange={ref=>update(i,{object:ref?.name??''})}/>
   <p role="status" className="text-xs">{progress.find(p=>p.page===d.sourcePage)?.state==='published'?t('Published'):progress.find(p=>p.page===d.sourcePage)?.state==='draft'?t('Saved draft, delivered with the application release'):progress.some(p=>p.page===d.sourcePage)?t('Saved'):t('Not saved')}</p>
  </div>)}</fieldset>
  {error&&<p role="alert">{error}</p>}{done&&<p role="status">{t('Imported pages are ready in the application draft. Save the application, then review its release.')}</p>}
  {done&&!directInstall&&<p className="text-xs text-muted">{t('Review the application release and add the drafts it depends on; the candidate installs the pages with the application.')}</p>}
  {!!writes&&!done&&<Button disabled={busy} onClick={()=>void prepare()}>{t('Continue page import')}</Button>}
 </Panel>;
 return <ModuleImportDialog open={open} onClose={onClose} profile={pageUIProfile} object={destinations[0]?.object??''} onApply={()=>{}} application={{destinations,onSource:sourceChanged,controls,busy:busy||inventory.isLoading||inventory.isError,locked:!!writes,onApply:report=>void prepare(report)}}/>;
}
