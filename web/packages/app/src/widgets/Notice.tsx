import type * as PageUI from "../pageui/types";
import {Notice,Panel,t} from "@platform/ui";
import {pageVariableContract} from "../runtime/PageRuntime";
export function NoticeRenderer({config,label}:{config?:PageUI.PageNotice;label:string}){
 const limits=pageVariableContract.notice;
 if(!config||typeof config.message!=="string"||config.title!==undefined&&typeof config.title!=="string"||new TextEncoder().encode(config.title??"").length>limits.maxTitleBytes||new TextEncoder().encode(config.message).length>limits.maxMessageBytes||!(limits.tones as readonly string[]).includes(config.tone))return <Panel role="alert">{t("Notice configuration is unavailable or incompatible.")}</Panel>;
 return <Notice title={config.title} label={label} message={config.message} tone={config.tone as "info"|"success"|"warning"|"danger"} role="note"/>;
}
