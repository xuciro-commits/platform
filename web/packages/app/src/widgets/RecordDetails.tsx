import type * as PageUI from "../pageui/types";
import {ObjectView,Panel,RecordStatus,RecordLinks,objectViewSections,t,type EntityRecord,type RecordSection,type RecordSource} from '@platform/ui';
import type {Api} from '@platform/kernel';
import {NewActions,RecordActions} from '../actions/actions';
import {RecordDetail,useOpenRecord} from '../index';
import {pageVariableContract} from '../runtime/PageRuntime';
type RecordPorts={source?:RecordSource;object:string;record?:EntityRecord};
export function DetailRenderer({source,object,record,fields,presentation,model,onOpen}:RecordPorts&{fields?:string[];presentation?:PageUI.PageDetailPresentation;model?:Api.ModelBinding;onOpen?:(type:string,record:EntityRecord)=>void}){
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 return <ObjectView key={`${object}/${record.id}`} source={source} type={object} id={record.id} fields={fields} sections={model?['overview','links']:['overview']} sectionNavigation="inline" detailPresentation={presentation} onOpen={onOpen}/>;
}
export function StatusRenderer({source,object,record,config}:RecordPorts&{config?:PageUI.PageStatusTracker}){
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 return <RecordStatus key={JSON.stringify([source.scope,object,record.id])} source={source} type={object} id={record.id} config={config}/>;
}
type Open=(type:string,record:EntityRecord)=>void;
export function LinksRenderer({source,object,record,groups,live,onOpen}:RecordPorts&{groups:PageUI.PageRecordLink[];live:boolean;onOpen?:Open}){
 const open=useOpenRecord();
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 return <RecordLinks key={JSON.stringify([source.scope,object,record.id])} source={source} type={object} id={record.id} groups={groups} onOpen={live?onOpen??((type,record)=>open({type,id:record.id})):undefined}/>;
}
export function RecordViewRenderer({source,object,record,fields,tabs,allowed,live,onOpen}:RecordPorts&{fields:string[];tabs?:string[];allowed:string[];live:boolean;onOpen?:Open}){
 const open=useOpenRecord();
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 if(live)return <RecordDetail key={JSON.stringify([source.scope,object,record.id])} source={source} type={object} id={record.id} fields={fields} recordTabs={tabs??pageVariableContract.recordView.tabs} sectionNavigation="inline" allowed={allowed} onOpen={onOpen??((type,record)=>open({type,id:record.id}))}/>;
 const chosen=tabs??pageVariableContract.recordView.tabs, sections:RecordSection[]=[...new Set<RecordSection>(["overview",...(chosen.includes("links")?["links" as const]:[]),...(chosen.includes("history")?["activity" as const]:[])])];
 return <ObjectView source={source} type={object} id={record.id} fields={fields} sections={sections.length?sections:objectViewSections} sectionNavigation="inline" actions={()=><p className="text-xs text-muted">{t('Actions do not run while you compose.')}</p>}/>;
}
export function ActionsRenderer({object,record,allowed,live}:{object:string;record?:EntityRecord;allowed:string[];live:boolean}){
 if(!live)return <p className="text-sm text-muted">{t('Actions do not run while you compose.')}</p>;
 return <div className="flex flex-wrap gap-2"><NewActions type={object} allowed={allowed}/>{record?<RecordActions type={object} record={record} allowed={allowed} steps/>:<span className="self-center text-sm text-muted">{t('Select a record to act on it.')}</span>}</div>;
}
