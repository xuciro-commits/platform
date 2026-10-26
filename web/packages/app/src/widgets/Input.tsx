import {Input,SearchInput,t} from '@platform/ui';
import {ScanInput} from './ScanInput';

export type InputProps={kind?:string;title:string;value?:string;onChange?:(value:string)=>void;enabled?:boolean;numeric?:boolean;error?:string;scopes?:string[];maxBytes?:number};
export function InputRenderer({kind,title,value,onChange,enabled,numeric,error,scopes,maxBytes}:InputProps){
 const disabled=!onChange||enabled===false;
 return <div className="grid gap-1">{kind==='scan'?<ScanInput value={value??''} onChange={onChange??(()=>{})} disabled={disabled} label={title}/>:kind==='search'?<SearchInput value={value??''} onChange={onChange??(()=>{})} disabled={disabled} aria-label={title} scope={scopes??[]}/>:<Input inputMode={numeric?'decimal':undefined} maxLength={numeric?maxBytes:undefined} aria-invalid={!!error} aria-label={title} value={value??''} disabled={disabled} onChange={event=>onChange?.(event.target.value)}/>} {error&&<p role="alert" className="text-xs text-danger">{t(error)}</p>}</div>;
}
