import {Panel,RecordPage,RecordStatus,RecordLinks,t,type EntityRecord,type RecordSource} from '@platform/ui';
import type {Api} from '@platform/kernel';
import {NewActions,RecordActions} from '../actions/actions';
import {useOpenRecord} from '../index';
import {pageVariableContract} from '../runtime/PageRuntime';
type RecordPorts={source?:RecordSource;object:string;record?:EntityRecord};
export function DetailRenderer({source,object,record,fields,presentation}:RecordPorts&{fields?:string[];presentation?:Api.PageDetailPresentation}){
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 return <RecordPage key={`${object}/${record.id}`} source={source} type={object} id={record.id} fields={fields} detailPresentation={presentation} detailOnly/>;
}
export function StatusRenderer({source,object,record,config}:RecordPorts&{config?:Api.PageStatusTracker}){
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 return <RecordStatus key={JSON.stringify([source.scope,object,record.id])} source={source} type={object} id={record.id} config={config}/>;
}
type Open=(type:string,record:EntityRecord)=>void;
export function LinksRenderer({source,object,record,groups,live,onOpen}:RecordPorts&{groups:Api.PageRecordLink[];live:boolean;onOpen?:Open}){
 const open=useOpenRecord();
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 return <RecordLinks key={JSON.stringify([source.scope,object,record.id])} source={source} type={object} id={record.id} groups={groups} onOpen={live?onOpen??((type,record)=>open({type,id:record.id})):undefined}/>;
}
export function RecordViewRenderer({source,object,record,fields,tabs,allowed,live,onOpen}:RecordPorts&{fields:string[];tabs?:string[];allowed:string[];live:boolean;onOpen?:Open}){
 const open=useOpenRecord();
 if(!record)return <p className="text-sm text-muted">{t('Select a record to see it here.')}</p>;
 if(!source)return <Panel role="alert">{t('No source for records')}</Panel>;
 return <RecordPage key={JSON.stringify([source.scope,object,record.id])} source={source} type={object} id={record.id} fields={fields} recordTabs={tabs??pageVariableContract.recordView.tabs} onOpen={live?onOpen??((type,record)=>open({type,id:record.id})):undefined} actions={record=>live?<RecordActions type={object} record={record} allowed={allowed} steps/>:<p className="text-xs text-muted">{t('Actions do not run while you compose.')}</p>}/>;
}
export function ActionsRenderer({object,record,allowed,live}:{object:string;record?:EntityRecord;allowed:string[];live:boolean}){
 if(!live)return <p className="text-sm text-muted">{t('Actions do not run while you compose.')}</p>;
 return <div className="flex flex-wrap gap-2"><NewActions type={object} allowed={allowed}/>{record?<RecordActions type={object} record={record} allowed={allowed} steps/>:<span className="self-center text-sm text-muted">{t('Select a record to act on it.')}</span>}</div>;
}
