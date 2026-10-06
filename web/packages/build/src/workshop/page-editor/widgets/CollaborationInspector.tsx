import {Select,t} from "@platform/ui";
import type {AuthoringSection} from "../draft";
import type {TableInspectorPorts} from "./TableInspector";

export function CollaborationInspector({section,document,overlay,itemOwner,onChange}:Omit<TableInspectorPorts,"section"|"onChange">&{section:AuthoringSection;onChange:(patch:Partial<AuthoringSection>)=>void}) {
 const fields=section.widget==="record-comments"?[{key:"commentDraftVariable" as const,label:"Comment draft state",constant:false}]:[{key:"fileVariable" as const,label:"File ID binding",constant:section.widget!=="record-uploader"},...(section.widget==="pdf-viewer"?[{key:"pdfPageVariable" as const,label:"PDF page state",constant:false}]:[])];
 return <>{fields.map(field=><label key={field.key} className="grid gap-1 text-xs">{t(field.label)}<Select value={section[field.key]??""} onChange={e=>onChange({[field.key]:e.target.value||undefined})}><option value="">{t(field.constant?"Choose a file ID state or constant":"Choose a text state variable")}</option>{Object.entries(document.variables??{}).filter(([,v])=>!itemOwner&&v.type==="string"&&(v.mode==="state"||field.constant&&v.mode==="constant")&&v.scope===(overlay?"overlay":"page")&&v.owner===overlay).map(([id,v])=><option key={id} value={id}>{v.title||id}{v.mode==="constant"?` · ${String(v.initial??"")}`:""}</option>)}</Select></label>)}<p className="text-xs text-muted">{t("Bind the original confirmed record and state in the same page or overlay. Comments and attachments use their original platform services; file IDs must identify attachments of that record.")}</p></>;
}
