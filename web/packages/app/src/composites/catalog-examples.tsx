import {useState} from 'react';
import {Disclosure,Select,t,type EntityRecord} from '@platform/ui';
import {LayeredGraphRenderer,ModelMatrixRenderer} from '../widgets/CompositeViews';
import {professionalConfigurations} from './configurations';

function ConfigurationExample({matrix=false}:{matrix?:boolean}){
 const [name,setName]=useState<keyof typeof professionalConfigurations>(matrix?'capability':'routing'),[selected,select]=useState<EntityRecord>();
 const example=professionalConfigurations[name],section=example.page.sections![0]!;
 return <div className="grid min-w-0 gap-3"><p role="status" className="text-xs text-muted">{t('Synthetic configuration samples. Live model reads and expressions run on the host.')}</p><label className="text-xs">{t('Application configuration')}<Select value={name} onChange={e=>{setName(e.target.value as typeof name);select(undefined);}}>{(matrix?['capability','rates'] as const:['routing','organization','lineage'] as const).map(key=><option key={key} value={key}>{t(professionalConfigurations[key].page.title)}</option>)}</Select></label>{matrix?<ModelMatrixRenderer config={section.composite} data={example.answer} selected={selected} onSelect={select}/>:<LayeredGraphRenderer config={section.composite} data={example.answer} label={t(example.page.title)} selected={selected} onSelect={select} storeKey={`catalog:${name}`}/>}<Disclosure summary={t('Complete application configuration')}><pre className="max-h-80 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify({page:example.page,models:example.sources},null,2)}</pre></Disclosure></div>;
}
export const ProfessionalGraphExample=()=> <ConfigurationExample/>;
export const ProfessionalMatrixExample=()=> <ConfigurationExample matrix/>;
