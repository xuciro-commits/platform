import type {Api} from "@platform/kernel";
import {compareDecimal,isDecimal,parseDecimal} from "./decimal";
import type {VariableResult} from "./variables";
type AlertResult={status:"message";text:string}|{status:"clear"|"pending"|"empty"}|{status:"error";code:string};
export function evaluateAlert(value:VariableResult|undefined,config:Api.PageAlertBanner|undefined,limits:{maxMessageBytes:number;tones:readonly string[]}):AlertResult {
 const threshold=config&&parseDecimal(config.threshold);
 if(!config||!threshold||threshold.value!==config.threshold||!limits.tones.includes(config.tone)||!config.message||new TextEncoder().encode(config.message).length>limits.maxMessageBytes)return {status:"error",code:"Alert configuration is unavailable or incompatible."};
 if(!value||value.status==="empty")return {status:"empty"};
 if(value.status==="pending")return {status:"pending"};
 if(value.status==="error")return {status:"error",code:value.code};
 if(!isDecimal(value.value))return {status:"error",code:"Alert needs an exact decimal value."};
 return compareDecimal(value.value,threshold)>0?{status:"message",text:config.message.replace("{value}",value.value.value)}:{status:"clear"};
}
