import {Button,Select,t,type EntityInfo} from '@platform/ui';
import {modelBindingContract,modelField} from '@platform/app/model';

/** Choose one declared segment at a time; cyclic models remain bounded. */
export function ModelFieldPicker({type,value=[],lookup,onChange}:{type:string;value?:string[];lookup:(type:string)=>EntityInfo|undefined;onChange:(path:string[])=>void}){
 const levels:{type:string;index:number}[]=[];let current=type;
 for(let index=0;index<Math.max(1,value.length);index++){levels.push({type:current,index});const f=lookup(current)?.fields.find(f=>f.name===value[index]);if(index<value.length-1){if(f?.type!=='reference'||!f.ref)break;current=f.ref;}}
 const leaf=modelField(type,value,lookup);
 return <div className="grid gap-1" aria-label={t('Declared model path')}>
  {levels.map(({type,index})=><label key={`${index}/${type}`} className="grid gap-1 text-xs">{index===0?t('Model field'):t('Reference hop {hop}',{hop:index})}<Select aria-label={index===0?t('Model field'):t('Reference hop {hop}',{hop:index})} value={value[index]??''} onChange={e=>onChange([...value.slice(0,index),e.target.value].filter(Boolean))}><option value="">{t('Choose a declared field')}</option><option value="id">ID</option>{lookup(type)?.fields.filter(f=>f.name!=='id').map(f=><option key={f.name} value={f.name}>{f.title} · {f.name} · {t(f.type)}</option>)}</Select></label>)}
  {leaf?.type==='reference'&&leaf.ref&&value.length<modelBindingContract.maxPath&&<Button size="sm" onClick={()=>onChange([...value,''])}>{t('Follow declared reference')}</Button>}
  {value.length>1&&<Button size="sm" onClick={()=>onChange(value.slice(0,-1))}>{t('Remove last hop')}</Button>}
  <p className="text-xs text-muted">{value.filter(Boolean).join(' → ')} · {t('At most {count} declared segments',{count:modelBindingContract.maxPath})}</p>
 </div>;
}
