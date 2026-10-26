import {Select,t} from "@platform/ui";
import {TableInspector,type TableDraft,type TableInspectorPorts} from "./TableInspector";

type TimelineDraft=TableDraft&{timeStart?:string;timeEnd?:string;timeLabel?:string;timeGroup?:string};
export function RecordTimelineInspector({section,document,object,info,overlay,itemOwner,onChange}:Omit<TableInspectorPorts,"section"|"onChange">&{section:TimelineDraft;onChange:(patch:Partial<TimelineDraft>)=>void}) {
 const dates=info?.fields.filter(f=>["date","datetime"].includes(f.type))??[],labels=info?.fields.filter(f=>["text","longtext","choice","reference"].includes(f.type))??[],kind=dates.find(f=>f.name===section.timeStart)?.type;
 return <>
 <TableInspector section={section} document={document} object={object} info={info} overlay={overlay} itemOwner={itemOwner} widget="record-timeline" showFields={false} onChange={patch=>onChange({...patch,...("object" in patch&&(patch.object||object)!==(section.object||object)?{timeStart:undefined,timeEnd:undefined,timeLabel:"id",timeGroup:undefined}:{})})}/>
 <label className="grid gap-1 text-xs">{t("Start time field")}<Select value={section.timeStart??""} onChange={event=>onChange({timeStart:event.target.value||undefined,timeEnd:undefined})}><option value="">{t("Choose a field")}</option>{dates.map(f=><option key={f.name} value={f.name}>{f.title} · {t(f.type)}</option>)}</Select></label>
 <label className="grid gap-1 text-xs">{t("End time field")}<Select value={section.timeEnd??""} onChange={event=>onChange({timeEnd:event.target.value||undefined})}><option value="">{t("Show points")}</option>{dates.filter(f=>f.type===kind).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label>
 <label className="grid gap-1 text-xs">{t("Timeline label field")}<Select value={section.timeLabel??"id"} onChange={event=>onChange({timeLabel:event.target.value})}><option value="id">{t("Record ID")}</option>{labels.map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label>
 <label className="grid gap-1 text-xs">{t("Timeline resource field")}<Select value={section.timeGroup??""} onChange={event=>onChange({timeGroup:event.target.value||undefined})}><option value="">{t("No resource grouping")}</option>{labels.map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label>
 <p className="text-xs text-muted">{t("The timeline reads one authorized query window. Selection opens the original record work.")}</p>
 </>;
}
