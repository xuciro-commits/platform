import type * as PageUI from "../pageui/types";
import {Separator,Panel,t} from "@platform/ui";
import {pageVariableContract} from "../runtime/PageRuntime";
export function SeparatorRenderer({config,name}:{config?:PageUI.PageSeparator;name:string}){
 if(!config||config.label!==undefined&&typeof config.label!=="string"||new TextEncoder().encode(config.label??"").length>pageVariableContract.separator.maxLabelBytes)return <Panel role="alert">{t("Separator configuration is unavailable or incompatible.")}</Panel>;
 return <Separator label={config.label} name={name}/>;
}
