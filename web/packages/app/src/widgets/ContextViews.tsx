import {BreadcrumbTrail,RecordAvatarStack,StaticImage,Panel,t,type EntityInfo,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
import type {VariableResult} from "../runtime/variables";
import type {QueryWindow} from "./QueryWindowFrame";

type Status="empty"|"pending"|"value"|"error";
export function BreadcrumbRenderer({config,record,status,info,onHome,onClear,enabled,label,readCurrent=true}:{config?:Api.PageBreadcrumb;record?:EntityRecord;status?:Status;info?:EntityInfo;onHome?:()=>void;onClear?:()=>void;enabled?:boolean;label:string;readCurrent?:boolean}) {
 if(!readCurrent)return <p role="status">{t("Confirming record access…")}</p>;
 if(!config)return <Panel role="alert">{t("Breadcrumb configuration is unavailable.")}</Panel>;
 const items:{id:string;label:string;detail?:string}[]=[{id:"home",label:config.homeLabel},{id:"page",label:config.pageLabel}];
 let error:string|undefined;
 if(status==="error")error=t("The breadcrumb record could not be confirmed.");
 if(record&&status==="value") {
  const field=config.labelField??"id",meta=info?.fields.find(f=>f.name===field),raw=field==="id"?record.id:record[field];
  if(!info||field!=="id"&&(!meta||!["text","longtext","choice","reference"].includes(meta.type))||raw!=null&&typeof raw!=="string")error=t("Breadcrumb record title is unavailable or incompatible.");
  else items.push({id:"record",label:raw??record.id,detail:`${info.type}/${record.id}`});
 }
 return <div className="grid min-w-0 gap-1"><BreadcrumbTrail label={label} items={items} currentID={items.some(i=>i.id==="record")?"record":"page"} onCurrentActivate={!items.some(i=>i.id==="record")?onClear:undefined} enabled={enabled} onActivate={item=>{if(enabled===false)return;if(item.id==="home")onHome?.();else if(item.id==="page")onClear?.();}}/>{status==="pending"&&<p role="status">{t("Confirming record access…")}</p>}{error&&<Panel role="alert">{error}</Panel>}</div>;
}
export function AvatarStackRenderer({config,contextStatus,window,collection,info,label,readCurrent=true}:{config?:Api.PageAvatarStack;contextStatus?:Status;window?:QueryWindow;collection?:VariableResult;info?:EntityInfo;label:string;readCurrent?:boolean}) {
 if(!readCurrent)return <p role="status">{t("Confirming record access…")}</p>;
 if(!config||!info)return <Panel role="alert">{t("Personnel avatar configuration is unavailable.")}</Panel>;
 if(config.contextVariable&&contextStatus==="error")return <Panel role="alert">{t("The personnel context could not be confirmed.")}</Panel>;
 if(config.contextVariable&&contextStatus!=="empty"&&contextStatus!=="value")return <p role="status">{t("Confirming record access…")}</p>;
 if(collection?.status==="error"||window?.error)return <Panel role="alert">{t("The original personnel query could not be read.")}</Panel>;
 if(!window||collection?.status==="pending"||!window.page)return <p role="status">{t("Loading…")}</p>;
 if(window.query.limit!==6||!!window.query.offset||JSON.stringify(window.query.sort)!==JSON.stringify(["id"]))return <Panel role="alert">{t("Personnel avatar query bounds or ordering changed.")}</Panel>;
 return <RecordAvatarStack label={label} records={window.page.records} total={window.page.total} info={info} labelField={config.labelField} detailFields={config.detailFields}/>;
}
export function StaticImageRenderer({config,label}:{config?:Api.PageStaticImage;label:string}) {
 if(!config)return <Panel role="alert">{t("Image configuration is unavailable.")}</Panel>;
 return <StaticImage src={config.url} caption={config.caption} alt={config.caption??label} height={config.height}/>;
}
