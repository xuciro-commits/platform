import type {Api} from "@platform/kernel";
import type {ImportTarget} from "./compile";

export const observationSourceTypes=["TelemetryTable","TelemetryStats","ObservabilityChart","TimeSeriesAnalysis"];
export type ObservationImportBinding={migration:"actual-business-observations"|"";sampleQuery:Api.AssetBinding;timeField:string;signals:(Api.PageObservationSignal&{sourceIndex?:number})[];metadata?:Api.PageObservationMetadata;assetField?:string;contextQuery?:Api.AssetBinding;assetConsumers?:"original-producer"|"observation-table"};
function retainedObservationQuery(target:Pick<ImportTarget,"definitions">,binding:Api.AssetBinding|undefined):Api.NamedQuery|undefined {
 if(!binding||!binding.ref||binding.ref.kind!=="query"||typeof binding.sourceVersion!=="string"||!binding.sourceVersion||Object.keys(binding).some(k=>!["ref","sourceVersion"].includes(k))||Object.keys(binding.ref).some(k=>!["app","kind","name"].includes(k)))return;
 const d=target.definitions?.find(d=>d.ref.app===binding.ref.app&&d.ref.kind==="query"&&d.ref.name===binding.ref.name);
 return d?.queryVersions?.[binding.sourceVersion]??(d?.version===binding.sourceVersion?d.query:undefined);
}
/** An explicit replacement for simulated telemetry, without executing source workers or curves. */
export function observationMapping(type:string,b:ObservationImportBinding|undefined,target:ImportTarget):{config:Api.PageObservation;sample:Api.AssetRef;context?:Api.AssetBinding}|undefined {
 if(!b||b.migration!=="actual-business-observations"||!b.sampleQuery||typeof b.timeField!=="string"||!Array.isArray(b.signals)||!b.signals.length||b.signals.length>100)return;
 if(b.signals.some(s=>!s||typeof s!=="object"||typeof s.field!=="string")||b.metadata!==undefined&&(!b.metadata||typeof b.metadata!=="object"||Array.isArray(b.metadata)||Object.values(b.metadata).some(v=>v!==undefined&&typeof v!=="string")))return;
 const q=retainedObservationQuery(target,b.sampleQuery),e=target.entities.find(e=>e.type===q?.object),fields=e?.fields;
 if(!q||q.by||!e||b.sampleQuery.ref.app!==e.app||q.limit&&q.limit<100||q.sort?.length&&JSON.stringify(q.sort)!==JSON.stringify([`-${b.timeField}`,"id"])||fields?.find(f=>f.name===b.timeField)?.type!=="datetime"||new Set(b.signals.map(s=>s.field)).size!==b.signals.length||b.signals.some(s=>!s||!fields?.some(f=>f.name===s.field&&["integer","decimal"].includes(f.type))||typeof s.unit!=="string"||new TextEncoder().encode(s.unit).length>64||s.group!==undefined&&(typeof s.group!=="string"||new TextEncoder().encode(s.group).length>128)))return;
 const telemetry=type==="TelemetryTable"||type==="TelemetryStats";
 if(telemetry&&(new Set(b.signals.map(s=>s.sourceIndex)).size!==b.signals.length||b.signals.some(s=>!Number.isInteger(s.sourceIndex)||s.sourceIndex!<0||s.sourceIndex!>99))||!telemetry&&b.signals.some(s=>s.sourceIndex!==undefined))return;
 if(type==="TimeSeriesAnalysis"&&b.signals.length!==3||type==="ObservabilityChart"&&b.signals.length!==1)return;
 const metadata=Object.values(b.metadata??{}).filter(Boolean),signalFields=new Set([b.timeField,...b.signals.map(s=>s.field)]);
 if(new Set(metadata).size!==metadata.length||metadata.some(name=>signalFields.has(name)||!fields?.some(f=>f.name===name&&["text","longtext","choice","reference"].includes(f.type)))||type!=="TelemetryTable"&&b.metadata)return;
 const config:Api.PageObservation={kind:type==="TelemetryTable"?"table":type==="TelemetryStats"?"statistics":type==="ObservabilityChart"?"availability":"series",timeField:b.timeField,signals:b.signals.map(({field,unit,group})=>({field,unit,group}))};
 if(telemetry){const f=fields?.find(f=>f.name===b.assetField),asset=target.entities.find(e=>e.type===f?.ref);if(f?.type!=="reference"||!asset)return;config.assetObject={app:asset.app,kind:"object",name:asset.type};config.assetField=b.assetField;
  if(type==="TelemetryTable"){if(b.metadata?.asset!==undefined&&b.metadata.asset!==b.assetField)return;if(!["original-producer","observation-table"].includes(b.assetConsumers??"")||b.contextQuery)return;config.metadata={...b.metadata,asset:b.assetField};}
  else{const context=retainedObservationQuery(target,b.contextQuery);if(!context||context.object!==e.type||context.by!==b.assetField||b.contextQuery?.ref.app!==e.app||context.limit&&context.limit<100||context.sort?.length&&JSON.stringify(context.sort)!==JSON.stringify([`-${b.timeField}`,"id"])||b.assetConsumers)return;}
 }else if(b.assetField||b.contextQuery||b.assetConsumers)return;
 return {config,sample:{app:e.app,kind:"object",name:e.type},context:b.contextQuery};
}
