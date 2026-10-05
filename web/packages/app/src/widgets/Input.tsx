import {Input,SearchInput,t} from '@platform/ui';

export type InputProps={kind?:string;title:string;value?:string;onChange?:(value:string)=>void;enabled?:boolean;numeric?:boolean;error?:string;scopes?:string[];maxBytes?:number};
export function InputRenderer({kind,title,value,onChange,enabled,numeric,error,scopes,maxBytes}:InputProps){
 return <div className="grid gap-1">{kind==='search'?<SearchInput value={value??''} onChange={onChange??(()=>{})} disabled={!onChange||enabled===false} aria-label={title} scope={scopes??[]}/>:<Input inputMode={numeric?'decimal':undefined} maxLength={numeric?maxBytes:undefined} aria-invalid={!!error} aria-label={title} value={value??''} disabled={!onChange||enabled===false} onChange={event=>onChange?.(event.target.value)}/>} {error&&<p role="alert" className="text-xs text-danger">{t(error)}</p>}</div>;
}
