import {useEffect,useState} from 'react';
import {Panel,RecordHistory,Tasks,t,type EntityRecord,type RecordSource,type RecordView} from '@platform/ui';
import {useHost} from '../index';
export type HistoryProps={object:string;historyLimit?:number;selected?:EntityRecord;readSource?:RecordSource;confirmedRecord?:EntityRecord;recordStatus?:'empty'|'pending'|'value'|'error'};
export type TasksProps={object:string;selected?:EntityRecord;live:boolean;readSource?:RecordSource};
function useRecordView(type: string, id?: string, readSource?: RecordSource) {
  const host = useHost(), source = readSource ?? host.source;
  const key=JSON.stringify([host.source.scope,source.scope,source.revision,type,id]),[result,setResult]=useState<{key:string;view?:RecordView;error?:string}>();
  useEffect(() => {
    let current = true;
    setResult(undefined);
    if(id&&source.scope===host.source.scope)void source.get(type,id).then(value=>{if(value.record.id!==id)throw Error("Record identity mismatch");if(current)setResult({key,view:value});}).catch(error=>{if(current)setResult({key,error:error instanceof Error?error.message:String(error)});});
    return () => { current = false; };
  }, [key]);
  return result?.key===key?result:undefined;
}

/** The timeline (16b): the selected record's history from the journal. */
export function HistoryRenderer({object:type,historyLimit,selected,readSource,confirmedRecord,recordStatus}:HistoryProps) {
  const { source } = useHost();
  const info = source.entity(type);
  const original=historyLimit?confirmedRecord:selected,result=useRecordView(type,original?.id,readSource);
  if(historyLimit&&recordStatus==="error")return <Panel role="alert">{t("The original record history could not be read.")}</Panel>;
  if(historyLimit&&recordStatus==="pending")return <p role="status">{t("Confirming record access…")}</p>;
  if (!original) return <p role="status" className="text-sm text-muted">{t("Select a record to see what happened to it.")}</p>;
  if(result?.error)return <Panel role="alert">{t("The original record history could not be read.")}</Panel>;
  if (!result?.view || !info) return <p role="status" className="text-sm text-muted">{t("Loading…")}</p>;
  return <RecordHistory info={info} history={result.view.history} heading={false} limit={historyLimit||undefined} total={result.view.history.length} recordID={original.id} recordRevision={result.view.record.revision}/>;
}

/** The tasks (16b): what waits on the selected record for this member — approvals
 *  and flow steps from the work app — answered where they are. */
export function TasksRenderer({object:type,selected,live,readSource}:TasksProps) {
  const { can, decide } = useHost();
  const result = useRecordView(type, selected?.id, readSource),view=result?.view;
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see what waits on it.")}</p>;
  if(result?.error)return <Panel role="alert">{t("The original record work could not be read.")}</Panel>;
  if (!view) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  if (view.tasks.length === 0) return <p className="text-sm text-muted">{t("Nothing waits on it.")}</p>;
  const answer = live && can("work.task.complete")
    ? { answer: async (task: RecordView["tasks"][number], a?: string) => { await decide("work.task.complete", { type: "work.task", id: task.id }, a ? { answer: a } : {}); } }
    : undefined;
  return <Tasks list={view.tasks} tasks={answer} />;
}
