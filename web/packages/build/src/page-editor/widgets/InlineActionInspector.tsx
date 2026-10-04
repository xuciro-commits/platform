import {useHost} from '@platform/app';
import {compatibleActionField} from '@platform/app/action-defaults';
import {Select,t} from '@platform/ui';
import type {AuthoringSection} from '../draft';
export function InlineActionInspector({section,object,onChange}:{section:AuthoringSection;object:string;onChange:(patch:Partial<AuthoringSection>)=>void}) {
 const {catalog,source}=useHost(),type=section.object||object,action=catalog.find(a=>a.schema===section.actions?.[0]),fields=source.entity(type)?.fields??[];
 return <><label className="grid gap-1 text-xs">{t('Inline record action')}<Select aria-label={t('Inline record action')} value={section.actions?.[0]??''} onChange={e=>onChange({actions:e.target.value?[e.target.value]:[],actionDefaults:undefined})}><option value="">{t('Choose one record action')}</option>{catalog.filter(a=>a.target===type&&!a.new).map(a=><option key={a.schema} value={a.schema}>{a.title}</option>)}</Select></label>
 {action?.payload.map(p=><label key={p.name} className="grid gap-1 text-xs">{t('Default original field for {parameter}',{parameter:p.name})}<Select value={section.actionDefaults?.find(m=>m.parameter===p.name)?.field??''} onChange={e=>onChange({actionDefaults:[...section.actionDefaults??[]].filter(m=>m.parameter!==p.name).concat(e.target.value?[{parameter:p.name,field:e.target.value}]:[])})}><option value="">{t('Leave the parameter empty')}</option>{fields.filter(f=>compatibleActionField(f,p)).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label>)}
 <p className="text-xs text-muted">{t('Defaults seed one opened original record. Refresh keeps the draft and its revision; cancel adopts the current record.')}</p><p className="text-xs text-muted">{t('The inline form uses the original action inputs, permissions, approvals and record revision.')}</p></>;
}
