import type {PageUI} from "@platform/app";
import {useState} from 'react';
import type {Api} from '@platform/kernel';
import {widgetContract,type Definition} from '@platform/app';
import {Button,Checkbox,Input,Select,t,type EntityInfo} from '@platform/ui';
import {ModelBindingInspector} from './ModelBindingInspector';
import {ModelFieldPicker} from '../../model-editor/ModelFieldPicker';
import {ExpressionEditor,literalExpression} from '../../editor/ExpressionEditor';
import type {AuthoringSection} from './draft';

type Port={id:string;required?:boolean;roles:Record<string,{kinds:readonly string[];required?:boolean;target?:string}>};
export function CompositeBindingInspector({section,sections,document,object,selections,lookup,definitions,onChange}:{section:AuthoringSection;sections:AuthoringSection[];document:PageUI.PageDocument;object:string;selections:PageUI.SelectionVariable[];lookup:(type:string)=>EntityInfo|undefined;definitions:readonly Definition[];onChange:(patch:Partial<AuthoringSection>)=>void}){
 const contract=widgetContract(section.widget),ports:readonly Port[]=contract&&'modelPorts' in contract?contract.modelPorts:[];
 const config=section.composite??{bindings:{}},[active,setActive]=useState(ports[0]?.id??'');
 const port=ports.find(p=>p.id===active)??ports[0],binding=port?config.bindings[port.id]:undefined;
 const patch=(next:Partial<PageUI.PageComposite>)=>onChange({composite:{...config,...next},model:undefined,object:undefined,fields:undefined,query:undefined});
 const edit=(value:Api.ModelViewBinding|undefined)=>{if(!port)return;const bindings={...config.bindings};if(value)bindings[port.id]=value;else delete bindings[port.id];patch({bindings});};
 const expressionChoices=(info?:EntityInfo)=>(info?.fields??[]).map(f=>({value:f.name,label:f.title,type:f.type,ref:f.ref}));
 return <div className="grid gap-3 text-xs" aria-label={t('Composite model bindings')}>
  <label>{t('Model source')}<Select value={port?.id??''} onChange={e=>setActive(e.target.value)}>{ports.map(p=><option key={p.id} value={p.id}>{t(p.id)}{p.required?' *':''}</option>)}</Select></label>
  {port&&<Checkbox checked={!!binding} onChange={checked=>edit(checked?{model:{type:object}}:undefined)}>{t('Bind this model source')}</Checkbox>}
  {binding&&port&&<><ModelBindingInspector allowRelation={false} section={{id:`${section.id}:${port.id}`,widget:'table',model:binding.model}} sections={sections} document={document} object={object} selections={selections} lookup={lookup} definitions={definitions} onChange={next=>{if(next.model)edit({...binding,model:next.model});}}/>
   <strong>{t('Visual field roles')}</strong>{Object.entries(port.roles).map(([role,definition])=>{const field=binding.roles?.[role],expression=binding.expressions?.[role];return <div key={role} className="grid gap-2 rounded border border-border p-2"><label>{t(role)}{definition.required?' *':''}<Select value={expression?'expression':field?'field':'default'} onChange={e=>{const roles={...binding.roles},expressions={...binding.expressions};delete roles[role];delete expressions[role];if(e.target.value==='field')roles[role]={name:lookup(binding.model.type)?.display||'id'};if(e.target.value==='expression')expressions[role]=literalExpression(definition.kinds.includes('text')?'text':definition.kinds[0]!);edit({...binding,roles,expressions});}}><option value="default">{t('Inferred from model')}</option><option value="field">{t('Model field')}</option><option value="expression">{t('Expression result')}</option></Select></label>{field&&<ModelFieldPicker type={binding.model.type} lookup={lookup} value={field.path??[field.name??'']} onChange={path=>edit({...binding,roles:{...binding.roles,[role]:path.length===1?{name:path[0]}:{path}}})}/ >}{expression&&<ExpressionEditor draftKey={`composite:${section.id}:${port.id}:${role}`} value={expression} kind={expression.kind} choices={expressionChoices(lookup(binding.model.type))} lookup={lookup} onChange={expr=>edit({...binding,expressions:{...binding.expressions,[role]:expr}})}/>}</div>;})}
  </>}
  {section.widget==='layered-graph'&&<><label>{t('Graph direction')}<Select value={config.direction??'right'} onChange={e=>patch({direction:e.target.value as PageUI.PageComposite['direction']})}><option value="right">{t('Right')}</option><option value="down">{t('Down')}</option></Select></label><Checkbox checked={!!config.acyclic} onChange={acyclic=>patch({acyclic})}>{t('Require an acyclic graph')}</Checkbox></>}
  {section.widget==='model-matrix'&&<><strong>{t('Cell expression')}</strong><ExpressionEditor draftKey={`composite:${section.id}:value`} value={config.value??literalExpression('boolean')} kind={config.value?.kind??'boolean'} choices={[{value:'input.row',label:t('Row'),type:'reference',ref:config.bindings.rows?.model.type},{value:'input.column',label:t('Column'),type:'reference',ref:config.bindings.columns?.model.type}]} lookup={lookup} onChange={value=>patch({value})}/></>}
  <strong>{t('Declared state tones')}</strong>{Object.entries(config.tones??{}).map(([value,tone])=><div key={value} className="flex gap-1"><span className="min-w-0 flex-1 break-words">{value}</span><Select value={tone} onChange={e=>patch({tones:{...config.tones,[value]:e.target.value}})}>{['neutral','info','success','warning','danger'].map(t=><option key={t} value={t}>{t}</option>)}</Select><Button size="sm" onClick={()=>{const tones={...config.tones};delete tones[value];patch({tones});}}>{t('Remove')}</Button></div>)}<ToneInput onAdd={value=>patch({tones:{...config.tones,[value]:'info'}})}/>
 </div>;
}
function ToneInput({onAdd}:{onAdd:(value:string)=>void}){const [value,setValue]=useState('');return <div className="flex gap-1"><Input aria-label={t('State value')} value={value} onChange={e=>setValue(e.target.value)}/><Button size="sm" disabled={!value} onClick={()=>{onAdd(value);setValue('');}}>{t('Add tone')}</Button></div>;}
