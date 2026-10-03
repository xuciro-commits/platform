import {Separator,Panel,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {pageVariableContract} from "../runtime/PageRuntime";
export function SeparatorRenderer({config,name}:{config?:Api.PageSeparator;name:string}){
 if(!config||config.label!==undefined&&typeof config.label!=="string"||new TextEncoder().encode(config.label??"").length>pageVariableContract.separator.maxLabelBytes)return <Panel role="alert">{t("Separator configuration is unavailable or incompatible.")}</Panel>;
 return <Separator label={config.label} name={name}/>;
}
