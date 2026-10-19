import {Checkbox,ContextHelp,Input,Select,Textarea,t} from '@platform/ui';
import {widgetContract} from '@platform/app';
import type {Api} from '@platform/kernel';
import type {AuthoringSection} from '../draft';

type PropertySchema=Api.ValueSchema&{title?:string;presenceTitle?:string;editor?:string;minimum?:number;maximum?:number;properties?:Record<string,PropertySchema>};
const initial=(schema:PropertySchema):unknown=>schema.type==='object'?Object.fromEntries((schema.required??[]).map(key=>[key,initial(schema.properties![key]!)])):
  schema.type==='boolean'?false:schema.type==='number'||schema.type==='integer'?0:schema.enum?.[0]??'';

/** Ordinary properties, labels, bounds and choices come from the runtime's
 * declaration. Specialized inspectors only own business bindings and gestures. */
export function SchemaPropertyInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const contract=widgetContract(section.widget);
 const schema=contract?.propsSchema as PropertySchema|undefined;
 if(!schema)return null;
 return <div className="grid min-w-0 gap-3">{Object.entries(schema.properties??{}).filter(([key])=>key!=='title'&&key!=='width').map(([key,property])=><Property key={key} name={key} schema={property} required={schema.required?.includes(key)??false} value={(section as unknown as Record<string,unknown>)[key]??(contract!.defaults as Record<string,unknown>)[key]} onChange={value=>onChange({[key]:value})}/>)}
  {schema.description&&<ContextHelp title={t(contract!.title)}>{t(schema.description)}</ContextHelp>}
 </div>;
}

function Property({name,schema,value,onChange,required}:{name:string;schema:PropertySchema;value:unknown;onChange:(value:unknown)=>void;required:boolean}){
 const label=t(schema.title??name);
 if(schema.type==='object'){
  const record=value as Record<string,unknown>|undefined;
  return <fieldset className="grid min-w-0 gap-3"><legend className="sr-only">{label}</legend>{Object.entries(schema.properties??{}).map(([key,child])=><Property key={key} name={name+'.'+key} schema={child} required={schema.required?.includes(key)??false} value={record?.[key]} onChange={next=>onChange({...initial(schema) as Record<string,unknown>,...record,[key]:next})}/>)}</fieldset>;
 }
 const input=schema.enum?.length?<Select aria-label={label} value={typeof value==='string'?value:''} onChange={event=>onChange(event.target.value)}><option value="" disabled>{t('Not set')}</option>{schema.enum.map(choice=><option key={choice} value={choice}>{t(choice)}</option>)}</Select>:
  schema.type==='boolean'?<Checkbox checked={value===true} onChange={onChange}>{label}</Checkbox>:
  schema.editor==='textarea'?<Textarea aria-label={label} value={typeof value==='string'?value:''} maxLength={schema.maxLength} onChange={event=>onChange(event.target.value)}/>:
  <Input aria-label={label} draftKey={name} optional={!required} type={schema.type==='number'||schema.type==='integer'?'number':'text'} step={schema.type==='number'?'any':schema.type==='integer'?1:undefined} min={schema.minimum} max={schema.maximum} maxLength={schema.maxLength} value={typeof value==='string'||typeof value==='number'?value:''} onChange={event=>onChange(schema.type==='number'||schema.type==='integer'?event.target.value===''?undefined:event.target.valueAsNumber:event.target.value)}/>;
 if(!required&&schema.presenceTitle)return <div className="grid gap-1 text-xs"><Checkbox checked={value!==undefined} onChange={checked=>onChange(checked?initial(schema):undefined)}>{t(schema.presenceTitle)}</Checkbox>{value!==undefined&&input}</div>;
 return schema.type==='boolean'?input:<label className="grid min-w-0 gap-1 text-xs"><span>{label}{required?' *':''}</span>{input}</label>;
}
