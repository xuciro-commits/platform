import {useHost} from "@platform/app";
import {Select,Toggles,t} from "@platform/ui";
import {TableInspector,type TableDraft,type TableInspectorPorts} from "./TableInspector";

type KanbanDraft=TableDraft&{cardLabel?:string;actions?:string[]};
export function KanbanInspector({section,document,object,info,overlay,itemOwner,onChange}:Omit<TableInspectorPorts,"section"|"onChange">&{section:KanbanDraft;onChange:(patch:Partial<KanbanDraft>)=>void}) {
 const {catalog}=useHost(),labels=info?.fields.filter(f=>["text","longtext","choice","reference"].includes(f.type))??[],l=info?.lifecycle;
 const summary=info?.fields.filter(f=>["text","longtext","choice","reference","integer","decimal","money","date","datetime","boolean"].includes(f.type))??[];
 const moves=(l?.transitions??[]).filter(m=>m.to.length===1&&catalog.some(a=>a.schema===m.schema&&a.target===info?.type));
 return <>
 <TableInspector section={section} document={document} object={object} info={info} overlay={overlay} itemOwner={itemOwner} widget="kanban" showFields={false} onChange={patch=>onChange({...patch,...("object" in patch&&(patch.object||object)!==(section.object||object)?{cardLabel:"id",fields:[],actions:[]}:{})})}/>
 {!l?<p role="alert" className="text-xs text-danger">{t("Choose an object with an original lifecycle.")}</p>:<p className="text-xs text-muted">{t("Columns follow the original lifecycle.")} {l.states.map(s=>s.title).join(" · ")}</p>}
 <label className="grid gap-1 text-xs">{t("Card title field")}<Select value={section.cardLabel??"id"} onChange={e=>onChange({cardLabel:e.target.value})}><option value="id">{t("Record ID")}</option>{labels.map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select></label>
 <fieldset className="grid gap-1 text-xs"><legend>{t("Card summary fields")}</legend><Toggles options={summary.map(f=>({value:f.name,label:f.title}))} value={section.fields??[]} onChange={fields=>onChange({fields})}/><p className="text-xs text-muted">{t("Choose at most four visible summary fields.")}</p></fieldset>
 <fieldset className="grid gap-1 text-xs"><legend>{t("Allowed move actions")}</legend><Toggles options={moves.map(m=>({value:m.schema,label:`${m.title} → ${l?.states.find(s=>s.name===m.to[0])?.title??m.to[0]}`}))} value={section.actions??[]} onChange={actions=>onChange({actions})}/></fieldset>
 <p className="text-xs text-muted">{t("Moves use original action inputs, approvals and record revisions. No action runs while composing.")}</p>
 </>;
}
