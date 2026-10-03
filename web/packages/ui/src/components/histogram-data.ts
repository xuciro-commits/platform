import {pageUIManifest,type Api} from "@platform/kernel";
type Rational={n:bigint;d:bigint};
function bound(text:unknown):Rational|undefined{
 if(typeof text!=="string"||text.length>1024)return;let m=/^(-?(?:0|[1-9][0-9]*))\/([1-9][0-9]*)$/.exec(text);if(m)return {n:BigInt(m[1]!),d:BigInt(m[2]!)};m=/^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?$/.exec(text);if(!m)return;return {n:BigInt(`${m[1]}${m[2]}${m[3]??""}`),d:10n**BigInt(m[3]?.length??0)};
}
const compare=(a:Rational,b:Rational)=>{const x=a.n*b.d-b.n*a.d;return x<0n?-1:x>0n?1:0;};
export function validHistogram(value:Api.HistogramResult|undefined,field?:string,bins?:number):value is Api.HistogramResult{
 if(!value||typeof value.field!=="string"||!value.field||field!==undefined&&value.field!==field||bins!==undefined&&value.requestedBins!==bins||!Number.isInteger(value.requestedBins)||value.requestedBins<1||value.requestedBins>pageUIManifest.runtime.histogram.maxBins||!Number.isSafeInteger(value.valid)||value.valid<0||!Number.isSafeInteger(value.missing)||value.missing<0||!Number.isSafeInteger(value.valid+value.missing)||!Array.isArray(value.buckets))return false;
 if(!value.valid)return value.buckets.length===0&&value.minimum===""&&value.maximum==="";
 const min=bound(value.minimum),max=bound(value.maximum);if(!min||!max||compare(min,max)>0)return false;const constant=compare(min,max)===0;
 const edge=(i:number):Rational=>{const k=BigInt(value.requestedBins);return {n:min.n*max.d*k+(max.n*min.d-min.n*max.d)*BigInt(i),d:min.d*max.d*k};};
 if(value.buckets.length!==(constant?1:value.requestedBins))return false;let sum=0,previous=min;
 for(const [i,bucket] of value.buckets.entries()){const lower=bound(bucket.lower),upper=bound(bucket.upper);if(!lower||!upper||compare(lower,previous)!==0||!constant&&(compare(lower,edge(i))!==0||compare(upper,edge(i+1))!==0)||compare(lower,upper)>0||!constant&&compare(lower,upper)===0||typeof bucket.upperInclusive!=="boolean"||bucket.upperInclusive!==(i===value.buckets.length-1)||!Number.isSafeInteger(bucket.count)||bucket.count<0)return false;sum+=bucket.count;previous=upper;}
 return Number.isSafeInteger(sum)&&sum===value.valid&&compare(previous,max)===0;
}
