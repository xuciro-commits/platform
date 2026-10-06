import {Button,Input,Select,t} from "@platform/ui";
import {pageVariableContract,pageUIProfile} from "@platform/app";
import {layoutID} from "../../page-layout";
import {EventEffects, groupEffects} from "../EffectsPanel";
import type {ButtonInspectorPorts} from "./ButtonInspector";

export function ButtonGroupInspector(props:ButtonInspectorPorts){
 const limits=pageVariableContract.buttonGroup,buttons=props.buttons??[];
 const patch=(id:string,value:Partial<typeof buttons[number]>)=>props.onGroupChange?.(buttons.map(b=>b.id===id?{...b,...value}:b),{...props.document,uiProfile:pageUIProfile});
 return <div className="grid gap-3">{buttons.map(b=><fieldset key={b.id} className="grid gap-2 border-b border-border pb-2"><Input aria-label={t("Button title")} value={b.title} maxLength={limits.maxTitleBytes} onChange={e=>patch(b.id,{title:e.target.value})}/><Select aria-label={t("Button style")} value={b.variant||"default"} onChange={e=>patch(b.id,{variant:e.target.value})}>{limits.variants.map(value=><option key={value} value={value}>{t(value)}</option>)}</Select><Select aria-label={t("Button icon")} value={b.icon??""} onChange={e=>patch(b.id,{icon:e.target.value||undefined})}><option value="">{t("No icon")}</option>{limits.icons.map(value=><option key={value} value={value}>{t(value)}</option>)}</Select><EventEffects {...props} eventName="click" control={b.id} allowed={groupEffects}/><Button size="sm" onClick={()=>props.onGroupChange?.(buttons.filter(other=>other.id!==b.id),{...props.document,events:props.document.events?.filter(e=>e.source!==props.section||e.control!==b.id)})}>{t("Remove button")}</Button></fieldset>)}<Button disabled={buttons.length>=limits.maxButtons} onClick={()=>props.onGroupChange?.([...buttons,{id:layoutID("control"),title:t("Button"),variant:"default"}],{...props.document,uiProfile:pageUIProfile})}>{t("Add button")}</Button></div>;
}
