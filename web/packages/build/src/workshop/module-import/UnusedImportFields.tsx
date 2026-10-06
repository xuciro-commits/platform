import {useHost} from '@platform/app';
import {Select,Toggles,t} from '@platform/ui';
import type {SourceModule,ImportBindings} from './compile';
import {unusedConfigurationKind,type UnusedImportBinding} from './unused';
export function UnusedImportFields({widget,module,bindings,value,onChange}:{widget:SourceModule['widgets'][string];module:SourceModule;bindings:ImportBindings;value?:UnusedImportBinding;onChange:(value:UnusedImportBinding|undefined)=>void}){
 const {entities}=useHost(),kind=unusedConfigurationKind(widget),set=module.variables.find(v=>v.id===value?.collection),external=String((set?.objectSet as {objectType?:string}|undefined)?.objectType??set?.sourceObjectType??''),entity=entities.find(e=>e.type===bindings.objects[external]);
 if(!kind)return null;
 const properties=Object.entries(bindings.fields[external]??{}).flatMap(([source,native])=>{const f=entity?.fields.find(f=>f.name===native);return native==='id'&&source==='id'?[{source,native,type:'text',title:t('Record ID')}]:f?[{source,native,type:f.type,title:f.title}]:[];});
 const update=(patch:Partial<UnusedImportBinding>)=>onChange({migration:'',collection:'',...value,...patch});
 return <fieldset className="grid gap-2"><legend>{t('Complete unused widget {widget}',{widget:`${widget.name} · ${widget.id}`})}</legend>
  <label className="grid gap-1 text-xs">{t('Unused configuration interpretation')}<Select value={value?.migration??''} onChange={e=>e.target.value?update({migration:'complete-unused-configuration'}):onChange(undefined)}><option value="">{t('Choose an explicit migration')}</option><option value="complete-unused-configuration">{t('Complete the original unused configuration')}</option></Select></label>
  {value?.migration==='complete-unused-configuration'&&<>
   <label className="grid gap-1 text-xs">{t('Unused source collection')}<Select value={value.collection} onChange={e=>onChange({migration:value.migration,collection:e.target.value,...kind==='table'?{columns:[]}:{xProperty:'',yProperty:'',colorBy:'',labelField:''}})}><option value="">{t('Choose a source collection')}</option>{module.variables.filter(v=>v.type==='objectSet'&&v.definitionKind==='objectSetDefinition').map(v=><option key={v.id} value={v.id}>{v.name||v.id}</option>)}</Select></label>
   {kind==='table'?<Toggles aria-label={t('Unused table columns')} options={properties.map(p=>({value:p.source,label:`${p.source} · ${p.title}`}))} value={value.columns??[]} onChange={columns=>update({columns})}/>:<>
    {([['xProperty','Unused scatter X field'],['yProperty','Unused scatter Y field'],['colorBy','Unused scatter color field']]as const).map(([key,label])=><label key={key} className="grid gap-1 text-xs">{t(label)}<Select value={value[key]??''} onChange={e=>update({[key]:e.target.value})}><option value="">{t('Choose a source field')}</option>{properties.filter(p=>(key==='colorBy'?['text','choice']:['integer','decimal']).includes(p.type)).map(p=><option key={p.source} value={p.source}>{p.source} · {p.title}</option>)}</Select></label>)}
    <label className="grid gap-1 text-xs">{t('Unused scatter record title')}<Select value={value.labelField??''} onChange={e=>update({labelField:e.target.value})}><option value="">{t('Choose a field')}</option><option value="id">{t('Record ID')}</option>{entity?.fields.filter(f=>['text','longtext','choice','reference'].includes(f.type)).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label>
   </>}
  </>}
  <p className="text-xs text-muted">{t('The original JSON stays unchanged. This explicit completion is stored in the mapping report; the widget stays unused until placed.')}</p>
 </fieldset>;
}
