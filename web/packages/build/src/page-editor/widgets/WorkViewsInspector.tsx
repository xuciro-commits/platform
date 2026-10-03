import {Input,t} from "@platform/ui";
import {pageVariableContract} from "@platform/app";
import type {AuthoringSection} from "../draft";

export function WorkViewsInspector({section}:{section:AuthoringSection}) {
 return <p className="text-xs text-muted">{t(section.widget==="approval-inbox"?"This view shows the current member's actual offered approval tasks. Approval and rejection use the original Work service and request IDs; source business rows and actions do not define approvals.":"This view shows the current member's original notifications. Opening or marking a notification read uses its original service; a local action log is not treated as a notification list.")}</p>;
}

export function HistoryInspector({section,onChange}:{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}) {
 const limits=pageVariableContract.workViews;
 return <><label className="grid gap-1 text-xs">{t("History window size")}<Input type="number" min={1} max={limits.maxHistoryWindow} value={section.historyLimit??""} placeholder={t("Legacy record history")} onChange={e=>onChange({historyLimit:e.target.value===""?undefined:e.target.valueAsNumber,selection:undefined})}/></label><p className="text-xs text-muted">{t("A bounded history view reads the original confirmed record resource in the same page or overlay. It displays actual journal entries, authors and timestamps; larger histories remain explicitly limited.")}</p></>;
}
