import {ButtonGroup,CollectionTitle} from '@platform/ui';
import type {Api} from '@platform/kernel';
export function CollectionTitleRenderer({title,value,error}:{title:string;value?:string;error?:string}){return <CollectionTitle title={title} value={value} error={error}/>;}
export function ButtonGroupRenderer({buttons,label,onActivate,isBound,enabled}:{buttons:Api.PageButton[];label:string;onActivate:(id:string)=>void;isBound:(id:string)=>boolean;enabled?:boolean}){return <ButtonGroup buttons={buttons} label={label} onActivate={onActivate} isBound={isBound} enabled={enabled}/>;}
