import {useEffect,useState} from 'react';
import type {Api} from '@platform/kernel';
import {checkRule,initializeRules,type RuleSchema} from '@platform/app/rules';
import {Button,Input,Select,Textarea,t,type EntityInfo} from '@platform/ui';
import {ModelFieldPicker} from '../model-editor/ModelFieldPicker';

export type ExpressionChoice={value:string;label:string;type:string;ref?:string};
const kindOf=(kind:string)=>kind==='longtext'?'text':kind;
export const literalExpression=(kind:string):Api.Expr=>({op:'',kind,lit:{value:kind==='boolean'?true:kind==='integer'||kind==='decimal'?'0':''}});
function schemaFor(expr:Api.Expr,choices:ExpressionChoice[],lookup?:(type:string)=>EntityInfo|undefined):RuleSchema{
 const refs:RuleSchema['refs']=Object.fromEntries(choices.map(c=>[c.value,{kind:kindOf(c.type),object:c.ref}]));
 const visit=(e:Api.Expr)=>{if(e.op==='path'&&e.args?.[0]?.ref){const root=e.args[0].ref,choice=choices.find(c=>c.value===root);let type=choice?.ref,node=refs[root];for(const name of e.path??[]){const field=type?lookup?.(type)?.fields.find(f=>f.name===name):undefined;if(!node||!field)break;node.fields??={};node.fields[name]??={kind:kindOf(field.type),object:field.ref};node=node.fields[name];type=field.ref;}}e.args?.forEach(visit);if(e.in)visit(e.in);};visit(expr);return {refs};
}
/** Edits the ADR-0101 tree. Validation calls the original Go compiler in Wasm;
 * execution and record/collection authorization remain with original owners. */
export function ExpressionEditor({value,onChange,choices=[],lookup,kind='boolean'}:{value:Api.Expr;onChange:(expr:Api.Expr)=>void;choices?:ExpressionChoice[];lookup?:(type:string)=>EntityInfo|undefined;kind?:string}){
 const [error,setError]=useState(''),[advanced,setAdvanced]=useState(false),[raw,setRaw]=useState('');
 useEffect(()=>{let active=true;void initializeRules().then(()=>{try{checkRule(value,schemaFor(value,choices,lookup));if(active)setError('');}catch(e){if(active)setError(e instanceof Error?e.message:t('Expression type check failed.'));}},()=>{if(active)setError(t('Rule runtime is unavailable.'));});return()=>{active=false;};},[JSON.stringify(value),JSON.stringify(choices)]);
 return <div className="grid gap-2 text-xs" aria-label={t('Rule expression editor')}>
  <ExpressionNode value={value} onChange={onChange} choices={choices} lookup={lookup} kind={kind} depth={0}/>
  {error&&<p role="alert" className="break-words text-danger">{error}</p>}
  <Button size="sm" onClick={()=>{setAdvanced(!advanced);setRaw(JSON.stringify(value,null,2));}}>{t('Expression source')}</Button>
  {advanced&&<><Textarea aria-label={t('Expression source')} value={raw} onChange={e=>setRaw(e.target.value)} rows={7}/><Button size="sm" onClick={()=>{try{const expr=JSON.parse(raw) as Api.Expr;checkRule(expr,schemaFor(expr,choices,lookup));if(expr.kind!==kind)throw Error(t('Expression result type differs.'));onChange(expr);setError('');}catch(e){setError(e instanceof Error?e.message:t('Expression type check failed.'));}}}>{t('Apply checked expression')}</Button></>}
 </div>;
}
function ExpressionNode({value:e,onChange,choices,lookup,kind,depth}:{value:Api.Expr;onChange:(expr:Api.Expr)=>void;choices:ExpressionChoice[];lookup?: (type:string)=>EntityInfo|undefined;kind:string;depth:number}){
 if(depth>=16)return <p role="alert">{t('Expression editor depth exceeded.')}</p>;
 const selectedOp=e.ref?'ref':e.lit?'lit':e.op;
 const logical=['and','or','not'],comparison=['=','!=','<','<=','>','>=','empty','not-empty','contains','starts-with','ends-with'],ops=kind==='boolean'?['lit','ref','path',...logical,...comparison]:kind==='text'?['lit','ref','path','concat','lower','upper']:['lit','ref','path','+','-','*','/'];
 const replace=(op:string)=>{if(op==='lit'){onChange(literalExpression(kind));return;}if(op==='ref'){const choice=choices.find(c=>kindOf(c.type)===kind);onChange({op:'',kind,ref:choice?.value??''});return;}if(op==='path'){const choice=choices.find(c=>c.type==='reference');onChange({op,kind,args:[{op:'',kind:'reference',ref:choice?.value??''}],path:[]});return;}const operand=logical.includes(op)?'boolean':comparison.includes(op)?'text':kind;onChange({op,kind,args:Array.from({length:op==='not'||op.includes('empty')||op==='upper'||op==='lower'?1:2},()=>literalExpression(operand))});};
 const root=e.args?.[0]?.ref,rootChoice=choices.find(c=>c.value===root),args=e.args??[];
 return <div className="grid gap-1 rounded border border-border p-2">
  <label className="grid gap-1">{t('Expression operation')}<Select value={selectedOp} onChange={v=>replace(v.target.value)}>{!ops.includes(selectedOp)&&<option value={selectedOp}>{e.op}</option>}{ops.map(op=><option key={op} value={op}>{op}</option>)}</Select></label>
  {selectedOp==='lit'&&<><label>{t('Value type')}<Select value={e.kind} onChange={v=>onChange(literalExpression(v.target.value))}>{['text','choice','reference','boolean','integer','decimal','date','datetime'].map(k=><option key={k} value={k}>{t(k)}</option>)}</Select></label><Input aria-label={t('Literal value')} value={String(e.lit?.value??'')} onChange={v=>onChange({...e,lit:{value:e.kind==='boolean'?v.target.value==='true':v.target.value}})}/></>}
  {selectedOp==='ref'&&<Select aria-label={t('Expression model reference')} value={e.ref??''} onChange={v=>{const c=choices.find(c=>c.value===v.target.value);if(c)onChange({op:'',kind:kindOf(c.type),ref:c.value});}}><option value="">{t('Choose a model reference')}</option>{choices.map(c=><option key={c.value} value={c.value}>{c.label} · {t(c.type)}</option>)}</Select>}
  {selectedOp==='path'&&<><Select aria-label={t('Reference root')} value={root??''} onChange={v=>onChange({...e,args:[{op:'',kind:'reference',ref:v.target.value}],path:[]})}><option value="">{t('Choose a model reference')}</option>{choices.filter(c=>c.type==='reference').map(c=><option key={c.value} value={c.value}>{c.label}</option>)}</Select>{rootChoice?.ref&&lookup&&<ModelFieldPicker type={rootChoice.ref} lookup={lookup} value={e.path} onChange={path=>{let type=rootChoice.ref,field:Api.FieldInfo|undefined;for(const name of path){field=type?lookup(type)?.fields.find(f=>f.name===name):undefined;type=field?.ref;}onChange({...e,path,kind:field?kindOf(field.type):kind});}}/>}</>}
  {!['lit','ref','path'].includes(selectedOp)&&args.map((arg,i)=><ExpressionNode key={i} value={arg} kind={arg.kind} choices={choices} lookup={lookup} depth={depth+1} onChange={next=>onChange({...e,args:args.map((a,at)=>at===i?next:a)})}/>)}
  {['and','or','concat'].includes(e.op)&&args.length<32&&<Button size="sm" onClick={()=>onChange({...e,args:[...args,literalExpression(e.op==='concat'?'text':'boolean')]})}>{t('Add expression operand')}</Button>}
  {args.length>2&&<Button size="sm" onClick={()=>onChange({...e,args:args.slice(0,-1)})}>{t('Remove expression operand')}</Button>}
 </div>;
}
