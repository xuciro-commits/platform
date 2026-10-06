import {useHost} from '@platform/app';
import {Select,t} from '@platform/ui';
import type {Api} from '@platform/kernel';
import {sourceInterfaceObject} from './application';
import type {ImportBindings,SourceModule} from './compile';
export function ComputationImportFields({module,original,objects,value,onChange}:{module:SourceModule;original:SourceModule;objects:ImportBindings['objects'];value:ImportBindings['computations'];onChange:(value:NonNullable<ImportBindings['computations']>)=>void}){
 const host=useHost();
 return <>{module.variables.filter(v=>v.type==='numeric'&&v.definitionKind==='function').map(v=>{
  const b=value?.[v.id],external=sourceInterfaceObject(original,String(v.inputVarId??'')),entity=host.entities.find(e=>e.type===objects[external??'']);
  const definitions=host.definitions.filter(d=>d.ref.kind==='compute'&&d.operation?.input.type==='object'),selected=definitions.find(d=>d.ref.app===b?.operation.ref.app&&d.ref.name===b?.operation.ref.name&&d.version===b.operation.sourceVersion),op=selected?.operation;
  const fields=entity?.fields.map(f=>({object:entity.type,name:f.name,title:f.title,type:f.type}))??[];
  const update=(patch:Partial<NonNullable<ImportBindings['computations']>[string]>)=>onChange({...value,[v.id]:{operation:b?.operation??{ref:{app:'',kind:'compute',name:''},sourceVersion:''},inputs:b?.inputs??{},...patch}});
  
  return <fieldset key={v.id} className="grid gap-2"><legend>{t('Map calculation result {variable}',{variable:v.name})}</legend><p className="text-xs">{String(v.functionApiName)}({String(v.inputVarId)})</p><label className="grid gap-1 text-xs">{t('Published calculation')}<Select value={b?JSON.stringify(b.operation):''} onChange={e=>{if(!e.target.value){const next={...value};delete next[v.id];onChange(next);}else update({operation:JSON.parse(e.target.value) as Api.AssetBinding,inputs:{},outputField:undefined});}}><option value="">{t('Choose a published code function')}</option>{definitions.map(d=><option key={`${d.ref.app}/${d.ref.name}/${d.version}`} value={JSON.stringify({ref:d.ref,sourceVersion:d.version})}>{d.operation?.title} · {d.version}</option>)}</Select></label>
  {op&&<label className="grid gap-1 text-xs">{t('Numeric output field')}<Select value={b?.outputField??''} onChange={e=>update({outputField:e.target.value||undefined})}><option value="">{t('Entire numeric result')}</option>{Object.entries(op.output.properties??{}).filter(([name,s])=>['number','integer'].includes(s.type)&&!s.nullable&&op.output.required?.includes(name)).map(([name])=><option key={name} value={name}>{name}</option>)}</Select></label>}
  {Object.entries(op?.input.properties??{}).map(([name,schema])=><label key={name} className="grid gap-1 text-xs">{t('Calculation input {input}',{input:name})}<Select value={b?.inputs[name]?.path?.[0]??''} onChange={e=>update({inputs:{...b?.inputs,[name]:{source:'subject',path:[e.target.value]}}})}><option value="">{t('Choose a field')}</option>{fields.filter(f=>({text:'string',longtext:'string',choice:'string',boolean:'boolean',integer:'integer',decimal:'number',date:'string',datetime:'string'} as Record<string,string>)[f.type]===schema.type||f.type==='integer'&&schema.type==='number').map(f=><option key={`${f.object}/${f.name}`} value={f.name}>{f.title} · {f.object}</option>)}</Select></label>)}
  </fieldset>;
 })}</>;
}
