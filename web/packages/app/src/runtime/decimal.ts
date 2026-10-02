/** Exact decimal text in a tagged scalar; never a binary floating value. */
export type DecimalValue={kind:"decimal";value:string};
export type StringSetValue={kind:"string-set";values:string[]};
export function isStringSet(value:unknown):value is StringSetValue {return !!value&&typeof value==="object"&&!Array.isArray(value)&&Object.keys(value).length===2&&(value as StringSetValue).kind==="string-set"&&Array.isArray((value as StringSetValue).values)&&(value as StringSetValue).values.length<=64&&new Set((value as StringSetValue).values).size===(value as StringSetValue).values.length&&(value as StringSetValue).values.every(v=>typeof v==="string"&&new TextEncoder().encode(v).length<=4096);}
export type ScalarValue=string|boolean|DecimalValue|StringSetValue;
export function decimalDraft(value:unknown,maxBytes=128):value is DecimalValue {
 return !!value&&typeof value==="object"&&!Array.isArray(value)&&Object.keys(value).length===2&&Object.hasOwn(value,"kind")&&Object.hasOwn(value,"value")&&(value as DecimalValue).kind==="decimal"&&typeof (value as DecimalValue).value==="string"&&new TextEncoder().encode((value as DecimalValue).value).length<=maxBytes;
}
export function parseDecimal(text:string,maxBytes=128):DecimalValue|undefined {
 if(new TextEncoder().encode(text).length>maxBytes||!/^-(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$|^(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$/.test(text))return;
 if(text.includes("."))text=text.replace(/0+$/,"").replace(/\.$/,"");if(text==="-0")text="0";return {kind:"decimal",value:text};
}
export function isDecimal(value:unknown,maxBytes=128):value is DecimalValue {return decimalDraft(value,maxBytes)&&parseDecimal(value.value,maxBytes)?.value===value.value;}
export function scalarAssignable(type:string,value:unknown,maxBytes=4096,decimalMaxBytes=128):value is ScalarValue {return type==="string-set"?isStringSet(value):type==="decimal"?decimalDraft(value,decimalMaxBytes):typeof value===type&&(typeof value!=="string"||new TextEncoder().encode(value).length<=maxBytes);}
function parts(value:DecimalValue){const [whole,fraction=""]=value.value.split(".");return {coefficient:BigInt(whole+fraction),scale:fraction.length};}
function aligned(a:DecimalValue,b:DecimalValue){const x=parts(a),y=parts(b),scale=Math.max(x.scale,y.scale);return {a:x.coefficient*10n**BigInt(scale-x.scale),b:y.coefficient*10n**BigInt(scale-y.scale),scale};}
export function compareDecimal(a:DecimalValue,b:DecimalValue){const x=aligned(a,b);return x.a<x.b?-1:x.a>x.b?1:0;}
export function decimalArithmetic(a:DecimalValue,b:DecimalValue,subtract=false,maxBytes=128):DecimalValue {
 const x=aligned(a,b),n=subtract?x.a-x.b:x.a+x.b,negative=n<0n,text=(negative?-n:n).toString().padStart(x.scale+1,"0");const raw=(negative?"-":"")+(x.scale?`${text.slice(0,-x.scale)}.${text.slice(-x.scale)}`:text),value=parseDecimal(raw,maxBytes);if(!value)throw new Error("Numeric result exceeds its budget.");return value;
}
