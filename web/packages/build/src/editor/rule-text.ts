import type {Api} from "@platform/api";

export function formulaText(e:Api.Expr):string{
 if(e.ref)return e.ref;
 if(e.lit)return String(e.lit.value);
 if(e.op==="round"&&e.args?.length===1)return formulaText(e.args[0]!);
 if(e.op==="negate")return `-(${formulaText(e.args![0]!)})`;
 if(["+","-","*","/"].includes(e.op)&&e.args?.length===2)return `(${formulaText(e.args[0]!)} ${e.op} ${formulaText(e.args[1]!)})`;
 return JSON.stringify(e);
}
export function cellRuleText(e:Api.Expr):string{
 if(e.lit?.value===true)return "*";
 const args=e.args??[],text=(value:Api.Expr)=>String(value.lit?.value??"");
 if(e.op==="in"&&args[1]?.lit&&Array.isArray(args[1].lit.value))return args[1].lit.value.join("|");
 if(e.op==="range")return `${text(args[1]!)}..${text(args[2]!)}`;
 if(["=","!=","<","<=",">",">="].includes(e.op)&&args[0]?.ref&&args[1]?.lit)return `${e.op} ${text(args[1])}`;
 return JSON.stringify(e);
}
