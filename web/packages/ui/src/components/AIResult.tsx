import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import {Button} from "../primitives/button";
import {Input} from "../primitives/input";
import {Panel} from "../primitives/card";
import {t} from "../i18n";

export type AIResultTurn={id:string;question?:string;state:string;fields?:{name:string;value:string}[];reason?:string};
export type AIResultProps={kind:string;turns:AIResultTurn[];question:string;suggestions:string[];disabled:boolean;busy:boolean;error?:string;onQuestion:(value:string)=>void;onRun:(question?:string)=>void;onReset:()=>void;onRetry?:()=>void};
export function AIResult({kind,turns,question,suggestions,disabled,busy,error,onQuestion,onRun,onReset,onRetry}:AIResultProps){
 return <VStack gap={2}>
  <Text type="supporting">{t("AI suggestions use the original authorized source. Review results before applying a business action.")}</Text>
  {turns.map(turn=><Panel key={turn.id}><VStack gap={1}>{turn.question&&<Text type="body">{turn.question}</Text>}<Text type="supporting" role={turn.state==="pending"?"status":undefined}>{t(turn.state==="pending"?"Waiting for the original model result.":turn.state==="ready"?"Original AI result":"The AI request was refused.")}</Text>{turn.reason&&<Text as="p" role="alert">{turn.reason}</Text>}{turn.fields?.map(field=><Text key={field.name} type="body"><strong>{field.name}: </strong>{field.value}</Text>)}</VStack></Panel>)}
  {kind==="chatbot"&&<><HStack gap={1} wrap="wrap">{suggestions.map(text=><Button key={text} size="sm" disabled={disabled||busy} onClick={()=>onRun(text)}>{text}</Button>)}</HStack><label className="grid gap-1 text-xs">{t("AI question")}<Input value={question} maxLength={4096} disabled={disabled||busy} onChange={e=>onQuestion(e.target.value)} onKeyDown={e=>{if(e.key==="Enter"&&!disabled&&!busy)onRun();}}/></label></>}
  <HStack gap={2} wrap="wrap"><Button loading={busy} disabled={disabled||kind==="chatbot"&&!question.trim()} onClick={()=>onRun()}>{t(kind==="chatbot"?"Send AI question":"Request original analysis")}</Button><Button disabled={busy} onClick={onReset}>{t("Reset AI view")}</Button>{onRetry&&<Button disabled={disabled||busy} onClick={onRetry}>{t("Retry original AI request")}</Button>}</HStack>
  {error&&<Text as="p" role="alert" className="text-danger">{error}</Text>}
 </VStack>;
}
