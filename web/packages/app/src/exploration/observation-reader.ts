import {timestampNanoseconds,type RecordSource,type RecordQuery,type AggregateData,type ObservationStatisticsValue} from "@platform/ui";
import type {Api} from "@platform/kernel";

export type ObservationStatisticsRequest={object:string;query:RecordQuery;window:Api.AggregateWindowQuery};
export type ObservationStatisticsRead={scope:string;statistics:ObservationStatisticsValue;window:Api.AggregateWindowResult};
type Source=Pick<RecordSource,"scope"|"revision"|"entity"|"aggregate">;
const bad=()=>Error("The original observation statistics answer is invalid.");
const integer=(v:unknown)=>typeof v==="number"&&Number.isSafeInteger(v)&&v>=0;
function validRequest(source:Source,request:ObservationStatisticsRequest){
 const info=source.entity(request.object),w=request.window;
 if(!source.scope||!source.aggregate||info?.type!==request.object||info.fields.find(f=>f.name===w.timeField)?.type!=="datetime"||!info.fields.some(f=>f.name===w.field&&["integer","decimal"].includes(f.type))||!integer(w.rows)||w.rows<1||w.rows>100000||typeof w.threshold!=="string"||w.threshold.length>80||!/^[+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?$/.test(w.threshold)||!Number.isFinite(Number(w.threshold)))throw Error("Original observation statistics bindings are unavailable or incompatible.");
 const exponent=/[eE]([+-]?\d+)$/.exec(w.threshold);if(exponent&&(!Number.isSafeInteger(Number(exponent[1]))||Number(exponent[1]) < -324||Number(exponent[1]) > 308)||Number(w.threshold)===0&&/[1-9]/.test(w.threshold.split(/[eE]/)[0]!))throw Error("Original observation statistics bindings are unavailable or incompatible.");
 return info;
}
function validateAnswer(data:AggregateData,request:Api.AggregateWindowQuery):Api.AggregateWindowResult {
 const w=data?.window;
 if(!w||!Array.isArray(data.columns)||data.columns.length||!Array.isArray(data.rows)||data.rows.length||data.histogram||w.timeField!==request.timeField||w.field!==request.field||w.requestedRows!==request.rows||w.threshold!==request.threshold||typeof w.generation!=="string"||!/^(0|[1-9]\d{0,19})$/.test(w.generation)||BigInt(w.generation)>18446744073709551615n||![w.total,w.timed,w.missingTime,w.count,w.valid,w.missing,w.above].every(integer)||w.timed+w.missingTime!==w.total||w.count!==Math.min(w.timed,request.rows)||w.valid+w.missing!==w.count||w.above>w.valid)throw bad();
 if(w.valid===0){if(w.min!==undefined||w.mean!==undefined||w.max!==undefined||w.above!==0)throw bad();}else if([w.min,w.mean,w.max].some(n=>typeof n!=="number"||!Number.isFinite(n))||w.min!>w.mean!||w.mean!>w.max!)throw bad();
 if(w.count===0){if(w.first!==undefined||w.last!==undefined)throw bad();return w;}
 const edge=(e?:Api.AggregateWindowEdge)=>!!e&&typeof e.id==="string"&&e.id.length>0&&e.id.length<=1024&&integer(e.revision)&&e.revision<=4294967295&&typeof e.time==="string"&&timestampNanoseconds(e.time)!==undefined;
 if(!edge(w.first)||!edge(w.last))throw bad();const first=timestampNanoseconds(w.first!.time)!,last=timestampNanoseconds(w.last!.time)!;
 if(first<last||first===last&&w.first!.id>w.last!.id||w.count===1&&(w.first!.id!==w.last!.id||first!==last||w.first!.revision!==w.last!.revision)||w.count>1&&w.first!.id===w.last!.id)throw bad();
 return w;
}
/** One leased view over the original aggregate owner, without a second business cache or read fallback. */
export function createObservationStatisticsReader(source:Source,active:()=>boolean){
 let sequence=0;
 const scope=(input:ObservationStatisticsRequest)=>{const request=structuredClone(input);validRequest(source,request);const {domain,search,set,archived,traversal}=request.query;return JSON.stringify([source.scope,source.revision,active(),request.object,{domain,search,set,archived,traversal,window:request.window}]);};
 const read=async(input:ObservationStatisticsRequest):Promise<ObservationStatisticsRead|undefined>=>{
  const attempt=++sequence;if(!active())return;
  const request=structuredClone(input),info=validRequest(source,request),memberScope=source.scope,revision=source.revision;
  const current=()=>attempt===sequence&&active()&&source.scope===memberScope&&source.revision===revision&&source.entity(request.object)?.type===request.object&&source.entity(request.object)?.fields.find(f=>f.name===request.window.timeField)?.type==="datetime"&&source.entity(request.object)?.fields.find(f=>f.name===request.window.field)?.type===info.fields.find(f=>f.name===request.window.field)?.type;
  // Record-page presentation never silently limits aggregate membership. The
  // explicit window alone defines the event-time ordering and recent N rows.
  const {domain,search,set,archived,traversal}=request.query,query={domain,search,set,archived,traversal,window:request.window},scope=JSON.stringify([memberScope,revision,true,request.object,query]);
  try{const data=await source.aggregate!(request.object,query);if(!current())return;const window=validateAnswer(data,request.window);return {scope,window,statistics:{scope,signal:window.field,threshold:Number(window.threshold),windowRows:window.requestedRows,count:window.count,valid:window.valid,above:window.above,min:window.min,mean:window.mean,max:window.max}};}catch(error){if(!current())return;throw error;}
 };
 return {scope,read};
}
