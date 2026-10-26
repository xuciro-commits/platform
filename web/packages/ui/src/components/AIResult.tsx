import {Button} from "../primitives/button";
import {Input} from "../primitives/input";
import {Panel} from "../primitives/card";
import {t} from "../i18n";

export type AIResultTurn={id:string;question?:string;state:string;fields?:{name:string;value:string}[];reason?:string};
export type AIResultProps={kind:string;turns:AIResultTurn[];question:string;suggestions:string[];disabled:boolean;busy:boolean;error?:string;onQuestion:(value:string)=>void;onRun:(question?:string)=>void;onReset:()=>void;onRetry?:()=>void};
/** Only the caller supplies authorized retained results; this UI never infers. */
export function AIResult({kind,turns,question,suggestions,disabled,busy,error,onQuestion,onRun,onReset,onRetry}:AIResultProps){
 return <div className="grid min-w-0 gap-2">
  <p className="text-xs text-muted">{t("AI suggestions use the original authorized source. Review results before applying a business action.")}</p>
  {turns.map(turn=><Panel key={turn.id} className="grid gap-1">{turn.question&&<p className="whitespace-pre-wrap break-words text-sm">{turn.question}</p>}<p role={turn.state==="pending"?"status":undefined} className="text-xs text-muted">{t(turn.state==="pending"?"Waiting for the original model result.":turn.state==="ready"?"Original AI result":"The AI request was refused.")}</p>{turn.reason&&<p role="alert" className="text-sm">{turn.reason}</p>}{turn.fields?.map(field=><p key={field.name} className="whitespace-pre-wrap break-words text-sm"><strong>{field.name}: </strong>{field.value}</p>)}</Panel>)}
  {kind==="chatbot"&&<><div className="flex flex-wrap gap-1">{suggestions.map(text=><Button key={text} size="sm" disabled={disabled||busy} onClick={()=>onRun(text)}>{text}</Button>)}</div><label className="grid gap-1 text-xs">{t("AI question")}<Input value={question} maxLength={4096} disabled={disabled||busy} onChange={e=>onQuestion(e.target.value)} onKeyDown={e=>{if(e.key==="Enter"&&!disabled&&!busy)onRun();}}/></label></>}
  <div className="flex flex-wrap gap-2"><Button disabled={disabled||busy||kind==="chatbot"&&!question.trim()} onClick={()=>onRun()}>{t(busy?"Requesting AI…":kind==="chatbot"?"Send AI question":"Request original analysis")}</Button><Button disabled={busy} onClick={onReset}>{t("Reset AI view")}</Button>{onRetry&&<Button disabled={disabled||busy} onClick={onRetry}>{t("Retry original AI request")}</Button>}</div>
  {error&&<p role="alert" className="text-sm text-danger">{error}</p>}
 </div>;
}
