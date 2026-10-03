import {useMemo} from "react";
import type {Api} from "@platform/kernel";
import {columnsFor} from "../fields/entity";
import {EditableRecordGrid,type RecordEditPort} from "./EditableRecordGrid";
import {entityFrom,type EntityInfo,type EntityRecord,type RecordSource} from "./Records";
import {t} from "../i18n";

export function projectActionRows(records:readonly EntityRecord[],parameters:Api.PageActionParameter[]):EntityRecord[]|undefined {
 if(records.length>50||new Set(records.map(r=>r.id)).size!==records.length||parameters.length<1||parameters.length>16||new Set(parameters.map(p=>p.parameter)).size!==parameters.length||parameters.some(p=>["id","revision","created","changed","archived","__proto__","constructor","prototype"].includes(p.parameter)))return;
 return records.map(record=>Object.assign(Object.create(null),record,Object.fromEntries(parameters.map(p=>[p.parameter,record[p.field]]))));
}
/** Original action parameters are presentation fields; the original record identity and revision remain intact. */
export function RecordActionGrid({records,info,parameters,payload,source,port}:{records:EntityRecord[];info:EntityInfo;parameters:Api.PageActionParameter[];payload:Api.Field[];source?:RecordSource;port:RecordEditPort}) {
 const data=useMemo(()=>projectActionRows(records,parameters),[records,parameters]),descriptor={...info,display:"id",fields:parameters.flatMap(mapping=>{const p=payload.find(p=>p.name===mapping.parameter),original=info.fields.find(f=>f.name===mapping.field);if(!p||!original||!["string","number","integer","boolean","date","datetime"].includes(p.type))return [];return [{...original,name:p.name,title:p.description||p.name,required:p.required,readOnly:false,choices:p.choices,ref:p.ref,type:(p.ref?"reference":p.choices?.length?"choice":p.type==="string"?"text":p.type==="number"?"decimal":p.type) as Api.FieldInfo["type"]}];})},entity=entityFrom(descriptor,{},source);
 if(!data||descriptor.fields.length!==parameters.length)return <p role="alert">{t("Original action parameters or record identities are unavailable or incompatible.")}</p>;
 const columns=[{id:"id",header:t("Record ID"),accessorKey:"id",meta:{width:160}},...columnsFor(entity,parameters.map(p=>p.parameter)).map(c=>({...c,enableSorting:false}))];
 return <EditableRecordGrid data={data} columns={columns} entity={entity} height={360} port={port} loading={false} empty={t("No records in this original action window.")}/>;
}
