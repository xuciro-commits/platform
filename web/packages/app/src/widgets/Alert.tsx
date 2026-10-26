import {Notice,Panel,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {pageVariableContract} from "../runtime/PageRuntime";
import {evaluateAlert} from "../runtime/alerts";
import type {VariableResult} from "../runtime/variables";
export function AlertRenderer({value,config,title}:{value?:VariableResult;config?:Api.PageAlertBanner;title:string}){
 const result=evaluateAlert(value,config,pageVariableContract.alertBanner);
 if(result.status==="error")return <Panel role="alert" aria-label={title} title={title}>{t(result.code)}</Panel>;
 if(result.status==="pending"||result.status==="empty")return <p role="status" aria-label={title} className="text-xs text-muted">{t(result.status==="pending"?"Loading alert value…":"Alert value is unavailable.")}</p>;
 if(result.status!=="message")return null;
 return <Notice title={title} message={result.text} tone={config!.tone as "info"|"warning"|"danger"}/>;
}
