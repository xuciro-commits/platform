import {useId} from 'react';
import {Button} from '../primitives/button';
import {Input,Select} from '../primitives/input';
import {useDraftInput,useInputDrafts} from './draft';
import {t} from '../i18n';

type Props={id?:string;label:string;value:unknown;onChange:(value:unknown)=>void;invalid?:boolean;describedBy?:string};
const kindOf=(value:unknown)=>value===undefined?'unset':value===null?'null':Array.isArray(value)?'array':typeof value==='object'?'object':typeof value;
const initial=(kind:string):unknown=>({object:{},array:[],string:'',number:0,boolean:false,null:null} as Record<string,unknown>)[kind];

export function StructuredValueInput({id,label,value,onChange,invalid,describedBy}:Props){
  const generated=useId();
  return <div id={id} role="group" aria-label={label} aria-invalid={invalid} aria-describedby={describedBy} className="grid min-w-0 gap-2">
    <ValueInput label={label} value={value} onChange={onChange} path={generated}/>
  </div>;
}

function ValueInput({label,value,onChange,path,optional=true}:{label:string;value:unknown;onChange:(value:unknown)=>void;path:string;optional?:boolean}){
  const kind=kindOf(value),buffers=useInputDrafts();
  const types=[['unset','Not set'],['object','Object'],['array','Array'],['string','Text'],['number','Number'],['boolean','Boolean'],['null','Null']];
  return <div className="grid min-w-0 gap-2">
    <div className="flex flex-wrap items-center gap-2">
      <Select aria-label={t('{field} type',{field:label})} className="w-auto" value={kind} disabled={buffers?.invalid} onChange={event=>onChange(initial(event.target.value))}>{types.filter(([key])=>optional||key!=='unset').map(([key,title])=><option key={key} value={key}>{t(title!)}</option>)}</Select>
      {kind==='unset'&&<span className="text-xs text-muted">{t('Omitted from the source.')}</span>}
    </div>
    {kind==='string'&&<Input aria-label={label} value={value as string} onChange={event=>onChange(event.target.value)}/>}
    {kind==='number'&&<Input aria-label={label} draftKey={path} type="number" step="any" optional={false} value={value as number} onChange={event=>onChange(event.target.valueAsNumber)}/>}
    {kind==='boolean'&&<Select aria-label={label} value={String(value)} onChange={event=>onChange(event.target.value==='true')}><option value="true">true</option><option value="false">false</option></Select>}
    {kind==='object'&&<ObjectInput label={label} value={value as Record<string,unknown>} onChange={onChange} path={path}/>}
    {kind==='array'&&<div className="grid min-w-0 gap-3 pl-3">
      {(value as unknown[]).map((item,index)=><div key={index} className="grid min-w-0 gap-1">
        <div className="flex items-center justify-between gap-2"><span className="text-xs text-muted">{label}[{index}]</span><Button size="sm" aria-label={t('Remove {field}',{field:label+'['+index+']'})} disabled={buffers?.invalid} onClick={()=>onChange((value as unknown[]).filter((_,at)=>at!==index))}>{t('Remove')}</Button></div>
        <ValueInput label={label+'['+index+']'} value={item} onChange={next=>onChange((value as unknown[]).map((old,at)=>at===index?next:old))} path={path+':'+index} optional={false}/>
      </div>)}
      <Button size="sm" className="w-fit" disabled={buffers?.invalid} onClick={()=>onChange([...(value as unknown[]),null])}>{t('Add item')}</Button>
    </div>}
  </div>;
}

function ObjectInput({label,value,onChange,path}:{label:string;value:Record<string,unknown>;onChange:(value:unknown)=>void;path:string}){
  const name=useDraftInput('property:'+path,''),buffers=useInputDrafts();
  return <div className="grid min-w-0 gap-3 pl-3">
    {Object.entries(value).map(([key,item])=><div key={key} className="grid min-w-0 gap-1">
      <div className="flex items-center justify-between gap-2"><span className="break-all text-xs font-medium">{key}</span><Button size="sm" aria-label={t('Remove {field}',{field:label+'.'+key})} disabled={buffers?.invalid} onClick={()=>onChange(Object.fromEntries(Object.entries(value).filter(([name])=>name!==key)))}>{t('Remove')}</Button></div>
      <ValueInput label={label+'.'+key} value={item} onChange={next=>onChange({...value,[key]:next})} path={path+':'+key}/>
    </div>)}
    <div className="flex flex-wrap gap-2">
      <Input aria-label={t('Property name')} placeholder={t('Property name')} className="min-w-28 flex-1" data-draft-key={name.path} value={name.text} onChange={event=>name.write(event.target.value,event.target.value?t('Add the property or discard its name.'):'')}/>
      <Button size="sm" disabled={!name.text||!!buffers?.busy} onClick={()=>{
        if(Object.hasOwn(value,name.text)){name.write(name.text,t('This property already exists.'));return;}
        const next={...value,[name.text]:null};name.clear();onChange(next);
      }}>{t('Add property')}</Button>
    </div>
    {name.problem&&<p className="text-xs text-danger">{name.problem}</p>}
  </div>;
}
