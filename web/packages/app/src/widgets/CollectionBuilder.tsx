import {useState} from 'react';
import {Button,FilterBar,Panel,entityFrom,field as fieldTypes,t,type Filter} from '@platform/ui';
import type {Api} from '@platform/kernel';
import {useHost} from '../index';
import type {usePageQueries} from '../runtime/PageQueries';
import {builderOperators,builderConditions} from '../runtime/collection-builder';

export function CollectionBuilderRenderer({section,builder,enabled=true}:{section:Api.Section;builder?:ReturnType<typeof usePageQueries>['builders'][string];live:boolean;enabled?:boolean}) {
 return <CollectionBuilderEditor key={builder?.key??''} section={section} builder={builder} enabled={enabled}/>;
}
function CollectionBuilderEditor({section,builder,enabled}:{section:Api.Section;builder?:ReturnType<typeof usePageQueries>['builders'][string];enabled:boolean}){
 const {source}=useHost(),[filters,setFilters]=useState<Filter[]>([]),input=builder?.result,descriptor=input?.status==='value'&&'value' in input&&typeof input.value==='object'&&input.value?.kind==='object-set-input'?input.value:undefined,info=descriptor?source.entity(descriptor.object.name):undefined;
 if(!info||!descriptor||!section.collectionBuilder)return <Panel role={input?.status==='error'?'alert':'status'}>{t('Collection builder source is unavailable.')}</Panel>;
 const fields=section.collectionBuilder.fields,original=entityFrom(info),entity={...original,fields:Object.fromEntries(fields.filter(name=>!!original.fields[name]).map(name=>{const f=info.fields.find(f=>f.name===name)!,field=original.fields[name]!;return [name,{...field,...['integer','decimal'].includes(f.type)?{editor:fieldTypes.text({label:field.label}).editor}:{},operators:builderOperators(f.type).map(id=>({id,label:t({is:'is',isNot:'is not',gte:'at least',lte:'at most',contains:'contains'}[id]!),needsArg:true}))}];}))},conditions=builderConditions(filters,fields,info);
 return <div className="grid gap-3"><fieldset disabled={!enabled}><FilterBar entity={entity} filters={filters} onChange={setFilters} maxFilters={16}/></fieldset>{!conditions&&<p role="alert" className="text-xs text-danger">{t('Complete valid original conditions before applying. The previous collection remains applied.')}</p>}<div className="flex gap-2"><Button size="sm" disabled={!enabled||!conditions} onClick={()=>builder?.apply(conditions!)}>{t('Apply collection conditions')}</Button><Button size="sm" variant="ghost" disabled={!enabled} onClick={()=>{setFilters([]);builder?.apply([]);}}>{t('Clear collection conditions')}</Button></div><p className="text-xs text-muted">{t('Consumers query the complete authorized collection; their pagination is independent.')}</p></div>;
}
