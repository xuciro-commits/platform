import {Button,Input,Select,Textarea,t} from "@platform/ui";
import {pageVariableContract} from "@platform/app";
import type {Api} from "@platform/kernel";
import {variableAccessible} from "../../page-layout";
import type {AuthoringSection} from "../draft";

export function ChoiceInspector({section,document,overlay,itemOwner,onChange}:{section:AuthoringSection;document:Api.PageDocument;overlay?:string;itemOwner?:string;onChange:(patch:Partial<AuthoringSection>)=>void}){
 const fields=section.choiceInput??{variant:"select",options:[]},update=(patch:Partial<Api.PageChoiceInput>)=>onChange({choiceInput:{...fields,...patch}}),limits=pageVariableContract.choiceInput,multiple=fields.variant==="multiple",binding=multiple?"choiceSetVariable":"choiceVariable";
 return <>
  <label className="grid gap-1 text-xs">{t("Choice state variable")}<Select value={section[binding]??""} onChange={e=>onChange({[binding]:e.target.value||undefined,choiceInput:fields})}><option value="">{t(multiple?"Choose a string-set state variable":"Choose a text state variable")}</option>{Object.entries(document.variables??{}).filter(([,v])=>!itemOwner&&v.type===(multiple?"string-set":"string")&&v.mode==="state"&&["page","overlay"].includes(v.scope)&&variableAccessible(v,undefined,overlay)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>
  <label className="grid gap-1 text-xs">{t("Choice presentation")}<Select value={fields.variant} onChange={e=>onChange({choiceInput:{...fields,variant:e.target.value},...((e.target.value==="multiple")!==multiple?{choiceVariable:undefined,choiceSetVariable:undefined}:{})})}><option value="select">{t("Dropdown")}</option><option value="radio">{t("Radio group")}</option><option value="segments">{t("Segmented control")}</option><option value="multiple">{t("Multiple selection")}</option></Select></label>
  <label className="grid gap-1 text-xs">{t("Choice label")}<Input maxLength={1024} value={fields.label??""} onChange={e=>update({label:e.target.value})}/></label>
  {fields.options.map((option,index)=><div className="grid min-w-0 gap-1" key={index}><label className="grid gap-1 text-xs">{t("Choice option {n}",{n:index+1})}<Textarea rows={1} maxLength={limits.maxOptionBytes} value={option} onChange={e=>update({options:fields.options.map((value,i)=>i===index?e.target.value:value)})}/></label><Button size="sm" variant="ghost" onClick={()=>update({options:fields.options.filter((_,i)=>i!==index)})}>{t("Remove choice {n}",{n:index+1})}</Button></div>)}
  <Button size="sm" disabled={fields.options.length>=limits.maxOptions} onClick={()=>update({options:[...fields.options,""]})}>{t("Add choice")}</Button>
  <p className="text-xs text-muted">{t("Use unique nonempty static choices. Empty and unmatched original values remain unchanged until explicit selection.")}</p>
 </>;
}
