import {Chart} from "../charts/Chart";
import type {ChartSpec} from "../charts/spec";
import {t} from "../i18n";
import type {EntityInfo,EntityRecord} from "./Records";

export type RecordChartFields={mark:"bar"|"line";xField:string;yField:string};

/** Compile only the caller's authorized window; no aggregation, reading or writing. */
export function recordChartSpec(records:EntityRecord[],info:EntityInfo,fields:RecordChartFields,maxPoints:number):ChartSpec|undefined {
 const x=info.fields.find(f=>f.name===fields.xField),y=info.fields.find(f=>f.name===fields.yField);
 if(!["bar","line"].includes(fields.mark)||fields.xField!=="id"&&(!x||!["text","longtext","choice","reference","integer","decimal","boolean","date","datetime"].includes(x.type))||!y||!["integer","decimal"].includes(y.type)||!Number.isInteger(maxPoints)||maxPoints<1||maxPoints>40)return;
 const points=records.slice(0,maxPoints).map(record=>({id:record.id,x:fields.xField==="id"?record.id:String(record[fields.xField]??""),y:Number(record[fields.yField]??0)}));
 if(points.some(p=>!p.id||!Number.isFinite(p.y))||new Set(points.map(p=>p.id)).size!==points.length)return;
 return {rowIdentity:"id",data:{values:points},mark:fields.mark,encoding:{x:{field:"x",type:"nominal",title:x?.title??t("Record ID")},y:{field:"y",type:"quantitative",title:y.title}}};
}

export function RecordChart({records,info,fields,maxPoints=40}:{records:EntityRecord[];info:EntityInfo;fields:RecordChartFields;maxPoints?:number}) {
 const spec=recordChartSpec(records,info,fields,maxPoints);
 if(!spec)return <p role="alert" className="text-sm text-danger">{t("Record chart fields or values are unavailable or incompatible.")}</p>;
 return <div className="grid min-w-0 grid-cols-1 gap-2"><p role="status" className="text-xs text-muted">{t("Plotting {points} of {records} records in this window.",{points:spec.data&&"values" in spec.data?spec.data.values.length:0,records:records.length})}</p><Chart spec={spec} frame={false}/></div>;
}
