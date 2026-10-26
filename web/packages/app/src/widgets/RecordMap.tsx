import {RecordMap,t,type EntityInfo,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
export function RecordMapRenderer({window,info,config,scope,selected,enabled,onSelect,confirmation}:{window?:QueryWindow;info?:EntityInfo;config?:Api.PageRecordMap;scope:string;selected?:EntityRecord;enabled?:boolean;onSelect:(record?:EntityRecord)=>void;confirmation?:string}){
 if(!config||!info)return <p role="alert">{t("Map fields or record coordinates are unavailable or incompatible.")}</p>;
 return <div className="grid min-w-0 grid-cols-1 gap-1">{confirmation==="pending"&&<p role="status">{t("Confirming record access…")}</p>}{confirmation==="error"&&<p role="alert">{t("The selected record could not be confirmed.")}</p>}<QueryWindowFrame viewIdentity={JSON.stringify([scope,info,config])} window={window} description={t("This map shows the current authorized record window.")}>{(page,ready)=><RecordMap records={page.records} info={info} fields={config} clusterEnabled={config.clusterEnabled} scope={JSON.stringify([scope,window?.query])} selected={selected?.id} enabled={enabled&&ready} onSelect={ready?onSelect:undefined}/>}</QueryWindowFrame></div>;
}
