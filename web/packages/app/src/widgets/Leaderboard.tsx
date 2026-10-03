import {Panel,RecordLeaderboard,t,type EntityInfo,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
import type {QueryWindow} from "./QueryWindowFrame";
export function LeaderboardRenderer({window,info,fields,selected,onSelect}:{window?:QueryWindow;info?:EntityInfo;fields?:Api.PageLeaderboard;selected?:EntityRecord;onSelect:(record?:EntityRecord)=>void}) {
 if(!fields||!info||!window)return <Panel role="status">{t("Ranking window is unavailable.")}</Panel>;
 const expected=[`${fields.ascending?"":"-"}${fields.valueField}`,"id"];
 if(window.error)return <Panel role="alert">{t(window.error)}</Panel>;
 if(window.query.offset||window.query.limit!==fields.limit||JSON.stringify(window.query.sort)!==JSON.stringify(expected))return <Panel role="alert">{t("Ranking window ordering or bounds changed.")}</Panel>;
 if(!window.page)return <Panel role="status">{t("Loading…")}</Panel>;
 return <RecordLeaderboard records={window.page.records} total={window.page.total} info={info} fields={fields} selected={selected?.id} onSelect={onSelect}/>;
}
