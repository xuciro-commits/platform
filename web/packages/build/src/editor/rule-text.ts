import type {Api} from "@platform/kernel";

// Presentation of existing sugar; evaluation belongs to @platform/app/rules.
export function formulaText(e:Api.Expr):string{
 if(e.ref)return e.ref;
 if(e.lit)return String(e.lit.value);
 if(e.op==="round"&&e.args?.length===1)return formulaText(e.args[0]!);
 if(e.op==="negate")return `-(${formulaText(e.args![0]!)})`;
 if(["+","-","*","/"].includes(e.op)&&e.args?.length===2)return `(${formulaText(e.args[0]!)} ${e.op} ${formulaText(e.args[1]!)})`;
 return JSON.stringify(e);
}
export function pageSugar(e:Api.Expr):Api.PageExpression|undefined{
 const names:Record<string,string>={"=":"equal","not-empty":"present","+":"decimal-add","-":"decimal-subtract","<":"decimal-less","parse-decimal":"parse-decimal","parse-number":"parse-number",not:"not",and:"and",or:"or",concat:"concat"},op=names[e.op];
 if(!op||!e.args?.every(a=>a.ref||a.lit))return;
 return {op,args:e.args.map(a=>a.ref?{variable:a.ref}:{literal:["decimal","number"].includes(a.kind)?{kind:a.kind,value:a.lit!.value}:a.lit!.value})};
}
export function cellRuleText(e:Api.Expr):string{
 if(e.lit?.value===true)return "*";
 const args=e.args??[],text=(value:Api.Expr)=>String(value.lit?.value??"");
 if(e.op==="in"&&args[1]?.lit&&Array.isArray(args[1].lit.value))return args[1].lit.value.join("|");
 if(e.op==="range")return `${text(args[1]!)}..${text(args[2]!)}`;
 if(["=","!=","<","<=",">",">="].includes(e.op)&&args[0]?.ref&&args[1]?.lit)return `${e.op} ${text(args[1])}`;
 return JSON.stringify(e);
}

export function assignmentSugar(e:Api.Expr):string|undefined{
 if(e.ref==="caller.id")return "$me";
 if(e.ref?.startsWith("input."))return e.ref.slice(6);
 if(e.op==="$now"||e.op==="$today")return "$now";
 if(e.lit)return `=${e.lit.value}`;
}
export function conditionSugar(e:Api.Expr):{field:string;operator:string;value?:string;valueField?:string}|undefined{
 const a=e.args??[],left=a[0];if(!left)return;
 const subject=(x:Api.Expr)=>x.ref??(x.op==="path"?`${x.args?.[0]?.ref}.${x.path?.join(".")}`:undefined);
 const field=subject(left);if(!field)return;
 if(e.op==="empty"||e.op==="not-empty")return {field,operator:e.op==="not-empty"?"not empty":"empty"};
 if(!["=","!=","<","<=",">",">="].includes(e.op)||!a[1])return;
 if(a[1].ref==="caller.id")return {field,operator:e.op,value:"$me"};
 if(a[1].lit)return {field,operator:e.op,value:String(a[1].lit.value)};
 const valueField=subject(a[1]);if(valueField)return {field,operator:e.op,valueField};
}
