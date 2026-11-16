import { Table, proportional, type TableColumn } from "@astryxdesign/core/Table";
import type { ReactNode } from "react";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import {renderingLimits} from "../gen/presentation-limits";
import {timestampNanoseconds,validCivilDate} from "../components/date";

import {entityFrom,type EntityInfo,type EntityRecord,type RecordSource} from "./Records";
import {t} from "../i18n";

const titleTypes=["text","longtext","choice","reference"];
const fieldTypes=[...titleTypes,"integer","decimal","money","date","datetime","boolean"];

function originalValue(type:string,value:unknown):string|number|boolean|bigint|null|undefined {
 if(value===null||value===undefined)return null;
 if(titleTypes.includes(type))return typeof value==="string"?value:undefined;
 if(type==="integer")return typeof value==="number"&&Number.isSafeInteger(value)?value:undefined;
 if(type==="decimal")return typeof value==="number"&&Number.isFinite(value)?value:undefined;
 if(type==="boolean")return typeof value==="boolean"?value:undefined;
 if(type==="date")return typeof value==="string"&&validCivilDate(value)?value:undefined;
 if(type==="datetime")return typeof value==="string"?timestampNanoseconds(value):undefined;
 if(type==="money"&&typeof value==="object"&&!Array.isArray(value)){
  const amount=(value as Record<string,unknown>).amount,currency=(value as Record<string,unknown>).currency;
  return typeof amount==="number"&&Number.isSafeInteger(amount)&&typeof currency==="string"&&/^[A-Z]{3}$/.test(currency)?JSON.stringify([amount,currency]):undefined;
 }
 return undefined;
}

type ComparisonRow = Record<string, unknown> & { id: string; label: string; differs: boolean; fieldName: string; values: (string|number|boolean|bigint|null|undefined)[] };

export function RecordComparison({records,info,fields,labelField,source,onOpenReference}:{records:EntityRecord[];info:EntityInfo;fields:string[];labelField:string;source?:RecordSource;onOpenReference?:(type:string,id:string)=>void}) {
 const label=info.fields.find(f=>f.name===labelField),definitions=fields.map(name=>info.fields.find(f=>f.name===name)),limits=renderingLimits.runtime.recordComparison;
 if(labelField!=="id"&&(!label||!titleTypes.includes(label.type))||fields.length<limits.minFields||fields.length>limits.maxFields||new Set(fields).size!==fields.length||definitions.some(f=>!f||!fieldTypes.includes(f.type)))return <Text as="p" role="alert">{t("Record comparison fields or title are unavailable or incompatible.")}</Text>;
 if(records.length>limits.maxRecords)return <Text as="p" role="alert">{t("Select no more than four records to compare. All selected records are retained.")}</Text>;
 if(records.some(record=>!record||typeof record.id!=="string"||!record.id.trim()||record.archived===true)||new Set(records.map(record=>record.id)).size!==records.length)return <Text as="p" role="alert">{t("Record comparison identities are unavailable or incompatible.")}</Text>;
 if(records.length<limits.minRecords)return <Text as="p" role="status">{t("Select two to four records to compare.")}</Text>;
 const titles=records.map(record=>labelField==="id"?record.id:record[labelField]);
 const rows:ComparisonRow[]=definitions.map(field=>{const values=records.map(record=>originalValue(field!.type,record[field!.name]));return {id:field!.name,label:field!.title,differs:values.some(value=>value!==values[0]),fieldName:field!.name,values};});
 if(titles.some(value=>value!=null&&typeof value!=="string")||rows.some(row=>row.values.some(value=>value===undefined)))return <Text as="p" role="alert">{t("Record comparison values are incompatible with their original fields.")}</Text>;
 const entity=entityFrom(info,{},source,onOpenReference);
 const columns:TableColumn<ComparisonRow>[]=[
  {key:"property",header:t("Property"),width:proportional(1,{minWidth:144}),renderCell:row=><VStack gap={0}><Text weight="medium">{row.label}</Text><Text type="supporting" className={row.differs?"text-warning":undefined}>{t(row.differs?"Different":"Same")}</Text></VStack>},
  ...records.map((record,index)=>({key:`record-${index}`,header:<VStack gap={0}><Text weight="medium">{(titles[index] as string|null|undefined)??record.id}</Text><Text type="code">{record.id}</Text></VStack>,width:proportional(1,{minWidth:176}),renderCell:(row:ComparisonRow)=>{const displayed=entity.fields[row.fieldName]!.display(row.values[index] as never,record) as unknown as ReactNode;return displayed;}})),
 ];
 return <Table aria-label={t("Record comparison")} data={rows} columns={columns} idKey="id" density="compact" dividers="grid" textOverflow="wrap" />;
}
