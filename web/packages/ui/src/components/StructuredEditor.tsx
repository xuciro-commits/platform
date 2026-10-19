import { useRef, useState, type ReactNode } from 'react';
import type { Api } from '@platform/kernel';
import { Button } from '../primitives/button';
import { Select, Textarea } from '../primitives/input';
import { useDraftInput, useInputDrafts } from '../fields/draft';
import { formatStructuredSource, parseStructuredSource, structuredExample, structuredIssues, type SourceFormat, type StructuredValue } from '../fields/structured-source';
import { t } from '../i18n';
import {ContextHelp} from './ContextHelp';
import {Disclosure} from '../primitives/controls';

export function StructuredEditor({value,onChange,children,scope,fields,fieldInput,validate,onTrial,expectedRevision,readOnly=false,disabled=false,context}: {
  value: StructuredValue; onChange?: (value: StructuredValue) => void; children?: ReactNode; scope: string; fields?: Api.FieldInfo[];
  fieldInput?: (field:Api.FieldInfo,value:unknown,onChange:(value:unknown)=>void)=>ReactNode;
  validate?: (value: StructuredValue) => Api.FieldIssue[]; onTrial?: (value: StructuredValue) => Promise<Api.Simulation>;
  readOnly?: boolean; disabled?: boolean; context?: ReactNode;
  expectedRevision?: number;
}) {
  const [mode,setMode] = useState<'visual'|'source'>(children ? 'visual' : 'source');
  const [format,setFormat] = useState<SourceFormat>('json');
  const input = useDraftInput('source:'+scope,formatStructuredSource(value,format)), buffers=useInputDrafts();
  const [issues,setIssues]=useState<Api.FieldIssue[]>([]),[message,setMessage]=useState(''),[running,setRunning]=useState(false);
  const [trial,setTrial]=useState<{signature:string;result:Api.Simulation}>();
  const [fieldPath,setFieldPath]=useState('');
  const fieldEntries=(items:Api.FieldInfo[],prefix=''):{field:Api.FieldInfo;path:string;prefix:string}[]=>items.filter(field=>!field.readOnly).sort((a,b)=>Number(!!b.required)-Number(!!a.required)).flatMap(field=>[{field,path:prefix+field.name,prefix},...(field.fields?fieldEntries(field.fields,prefix+field.name+'.[].'):[])]);
  const entries=fieldEntries(fields??[]),selectedField=entries.find(entry=>entry.path===fieldPath)??entries[0];
  const signature=JSON.stringify([scope,typeof context==='string'?context:'',expectedRevision,value]), latest=useRef(signature); latest.current=signature;
  const check = (next: StructuredValue) => [...(fields ? structuredIssues(next,fields) : []), ...(validate?.(next) ?? [])].filter((issue,index,all)=>all.findIndex(other=>other.code===issue.code&&other.path.join('.')===issue.path.join('.')&&other.message===issue.message)===index);
  const parse = () => { try { const next=parseStructuredSource(input.text,format), found=check(next);setIssues(found);if(found.length) return;return next; } catch(error) { setMessage(error instanceof Error?error.message:String(error)); } };
  const apply = () => {const next=parse();if(!next)return false;input.clear();onChange?.(next);setMessage(t('Source applied to this draft. Nothing has been saved.'));setTrial(undefined);return true;};
  const switchMode = (next:'visual'|'source') => {
    if(next===mode)return;
    if(next==='source'&&buffers?.invalid){setMessage(t('Correct the input drafts before switching modes.'));return;}
    if(next==='visual'&&!readOnly&&input.text!==formatStructuredSource(value,format)&&!apply())return;
    input.clear();setIssues([]);setMessage('');setMode(next);
  };
  let sourceValue:StructuredValue|undefined;
  try { sourceValue=parseStructuredSource(input.text,format); } catch { /* Keep the incomplete source untouched. */ }
  const writeField=(field:Api.FieldInfo,next:unknown)=>{
    if(!sourceValue)return;
    const changed={...sourceValue,[field.name]:next};
    input.write(formatStructuredSource(changed,format),t('Apply source before saving.'));
    setIssues([]);setMessage('');setTrial(undefined);
  };
  const guide=(items:Api.FieldInfo[],path=''):ReactNode=><dl className="grid gap-3">{items.filter(field=>!field.readOnly).sort((a,b)=>Number(!!b.required)-Number(!!a.required)).map(field=><div key={field.name} className="grid min-w-0 gap-1">
    <dt className="flex flex-wrap items-baseline gap-x-2 text-sm"><span className="font-medium">{field.title}</span><code className="break-all text-xs text-muted">{path+field.name}</code><span className="text-xs text-muted">{field.type}{field.required?' · '+t('Required'):''}</span></dt>
    <dd className="grid min-w-0 gap-1 text-xs text-muted">
      {field.help&&<p>{field.help}</p>}
      {field.when&&<p>{t('Applies when {field} is {values}.',{field:field.when.field,values:field.when.in.join(', ')})}</p>}
      {field.example!==undefined&&<p className="break-words">{t('Example')}: <code>{field.example}</code></p>}
      {field.choices?.length? <p className="break-words">{t('Allowed values')}: {field.choices.join(', ')}</p>:null}
      {field.ref&&<p className="break-words">{t('Reference to {type}. Select a record; the source stores its ID.',{type:field.ref})}</p>}
      {!path&&fieldInput&&<fieldset disabled={disabled||running||!sourceValue} className="min-w-0">{fieldInput(field,sourceValue?.[field.name],next=>writeField(field,next))}</fieldset>}
    </dd>
  </div>)}</dl>;
  return <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3" data-structured-editor={scope}>
    <div className="flex shrink-0 flex-wrap items-center justify-between gap-2">
      <div className="flex gap-1" role="group" aria-label={t('Editing mode')}>
        {children&&<Button size="sm" variant={mode==='visual'?'primary':'ghost'} disabled={disabled||running} aria-pressed={mode==='visual'} onClick={()=>switchMode('visual')}>{t('Visual')}</Button>}
        <Button size="sm" variant={mode==='source'?'primary':'ghost'} disabled={disabled||running} aria-pressed={mode==='source'} onClick={()=>switchMode('source')}>{t('Source editor')}</Button>
      </div>
      {mode==='source'&&<div className="flex flex-wrap items-center gap-2">
        <Select aria-label={t('Source format')} value={format} disabled={disabled||running||!!input.problem&&input.problem!==t('Apply source before saving.')} onChange={event=>{const next=event.target.value as SourceFormat;try{const parsed=parseStructuredSource(input.text,format);setFormat(next);input.clear();if(JSON.stringify(parsed)!==JSON.stringify(value)&&!readOnly)input.write(formatStructuredSource(parsed,next),t('Apply source before saving.'));setMessage('');}catch(error){setMessage(String(error));}}}><option value="json">JSON</option><option value="yaml">YAML</option></Select>
        <Button size="sm" disabled={disabled||running} onClick={()=>{try{const parsed=parseStructuredSource(input.text,format);if(JSON.stringify(parsed)===JSON.stringify(value)||readOnly)input.clear();else input.write(formatStructuredSource(parsed,format),t('Apply source before saving.'));setMessage('');}catch(error){setMessage(String(error));}}}>{t('Format')}</Button>
        <Button size="sm" onClick={async()=>{try{await navigator.clipboard.writeText(input.text);setMessage(t('Copied.'));}catch{setMessage(t('Copy failed. Select and copy the source.'));}}}>{t('Copy')}</Button>
      </div>}
    </div>
    {context&&<Disclosure className="shrink-0 text-xs text-muted" summary={t('Operation details')}><div className="py-2 break-words">{context}</div></Disclosure>}
    {mode==='visual'?<div className="flex min-h-0 flex-1 flex-col overflow-auto">{children}</div>:<div className="grid gap-2">
      {!readOnly&&!!fields?.length&&<details className="min-w-0 rounded-md border border-border p-3" open={!Object.values(value).some(item=>item!==undefined)||undefined}>
        <summary className="cursor-pointer text-sm font-medium">{t('Fields and examples')}</summary>
        <div className="mt-3 grid gap-4">
          <div className="flex flex-wrap items-center gap-2"><label className="grid min-w-0 flex-1 gap-1 text-xs text-muted">{t('Field reference')}<Select value={selectedField?.path??''} onChange={event=>setFieldPath(event.target.value)}>{entries.map(entry=><option key={entry.path} value={entry.path}>{entry.field.title} · {entry.path}{entry.field.required?' *':''}</option>)}</Select></label><ContextHelp title={t('Fields and examples')}>{t('Existing values and declared defaults are included. Optional fields may be omitted. Examples are guidance and are never submitted automatically.')}</ContextHelp></div>
          {selectedField&&guide([selectedField.field],selectedField.prefix)}
          <details><summary className="cursor-pointer text-xs font-medium">{t('Example document')}</summary><div className="mt-2 grid gap-2"><p className="text-xs text-muted">{t('Illustrative only. Replace placeholders and choose actual references before applying.')}</p><pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words text-xs">{formatStructuredSource(structuredExample(fields),format)}</pre><Button size="sm" className="w-fit" onClick={async()=>{try{await navigator.clipboard.writeText(formatStructuredSource(structuredExample(fields),format));setMessage(t('Copied.'));}catch{setMessage(t('Copy failed. Select and copy the source.'));}}}>{t('Copy example')}</Button></div></details>
        </div>
      </details>}
      <Textarea data-draft-key={input.path} aria-label={t('Configuration source')} value={input.text} readOnly={readOnly} disabled={disabled||running} rows={16} spellCheck={false} className="min-h-64 font-mono text-sm" onChange={event=>{const raw=event.target.value;let problem='';try{parseStructuredSource(raw,format);problem=t('Apply source before saving.');}catch(error){problem=error instanceof Error?error.message:String(error);}input.write(raw,problem);setIssues([]);setMessage('');setTrial(undefined);}}/>
      {!readOnly&&<Button className="w-fit" disabled={disabled||running} onClick={apply}>{t('Apply source')}</Button>}
    </div>}
    {!readOnly&&<div className="flex shrink-0 flex-wrap gap-2">
      <Button size="sm" disabled={disabled||running} onClick={()=>{setMessage('');const next=mode==='source'?parse():value;if(!next)return;const found=check(next);setIssues(found);if(!found.length)setMessage(t('Input checks passed. Business rules have not been run.'));}}>{t('Check inputs')}</Button>
      {onTrial&&<Button size="sm" disabled={disabled||running||!!input.problem} onClick={async()=>{const found=check(value);setIssues(found);if(found.length)return;const captured=signature;setRunning(true);try{const result=await onTrial(value);if(latest.current===captured)setTrial({signature:captured,result});}catch(error){if(latest.current===captured)setMessage(error instanceof Error?error.message:String(error));}finally{setRunning(false);}}}>{t(running?'Trying…':'Try operation')}</Button>}
    </div>}
    {input.problem&&<p role="alert" className="break-words text-sm text-danger">{input.problem}</p>}
    {!!issues.length&&<ul role="alert" className="grid gap-1 text-sm text-danger">{issues.map((issue,index)=><li key={index}>{issue.path.join('.')}: {issue.message}</li>)}</ul>}
    {message&&<p role="status" className="break-words text-sm text-muted">{message}</p>}
    {trial?.signature===signature&&<div className="grid gap-2 text-sm"><p role="status">{trial.result.accepted?t('Business trial passed. Nothing was saved; submission checks again.'):trial.result.refusal||t('This operation could not be tried.')}</p>{trial.result.issues?.map((issue,index)=><p key={index} className="text-danger">{issue.path.join('.')}: {issue.message}</p>)}{trial.result.changes.length>0&&<pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(trial.result.changes,null,2)}</pre>}</div>}
  </div>;
}
