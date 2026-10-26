const decimal=(text:string)=>{
 if(text.length>128||! /^-?(0|[1-9][0-9]*)(\.[0-9]+)?$/.test(text))return;
 const negative=text.startsWith("-"),parts=(negative?text.slice(1):text).split("."),scale=parts[1]?.length??0;
 return {coefficient:BigInt(parts.join(""))*(negative?-1n:1n),scale};
};
const format=(coefficient:bigint,scale:number)=>{
 const negative=coefficient<0n,digits=(negative?-coefficient:coefficient).toString().padStart(scale+1,"0"),text=scale?`${digits.slice(0,-scale)}.${digits.slice(-scale)}`.replace(/0+$/,"").replace(/\.$/,""):digits;
 return `${negative&&coefficient!==0n?"-":""}${text}`;
};
/** Sliders use bounded integer indices; emitted drafts stay exact decimals. */
export function rangeGrid(min:string,max:string,step:string){
 const values=[min,max,step].map(decimal);
 if(values.some(v=>!v))return;
 const scale=Math.max(...values.map(v=>v!.scale)),aligned=values.map(v=>v!.coefficient*10n**BigInt(scale-v!.scale));
 const lo=aligned[0]!,hi=aligned[1]!,increment=aligned[2]!;
 if(values.some((v,i)=>format(v!.coefficient,v!.scale)!==[min,max,step][i])||lo>=hi||increment<=0n||(hi-lo)%increment!==0n||(hi-lo)/increment>10000n)return;
 const ticks=Number((hi-lo)/increment);
 // Endpoints alone do not bound the intermediate decimal text: a long
 // integer bound plus a fractional step may exceed the original input limit.
 for(let i=1;i<ticks;i++)if(format(lo+BigInt(i)*increment,scale).length>128)return;
 return {ticks,text:(index:number)=>format(lo+BigInt(index)*increment,scale),index:(text:string)=>{
  const value=decimal(text);if(!value)return;
  const common=Math.max(scale,value.scale),base=10n**BigInt(common-scale),offset=value.coefficient*10n**BigInt(common-value.scale)-lo*base,tick=increment*base;
  if(offset<0n||offset>(hi-lo)*base||offset%tick!==0n)return;
  return Number(offset/tick);
 }};
}
export function rangeDrafts(grid:ReturnType<typeof rangeGrid>,lower:string,upper:string){
 if(!grid)return;
 const a=lower===""?0:grid.index(lower),b=upper===""?grid.ticks:grid.index(upper);
 return a!==undefined&&b!==undefined&&a<=b?{a,b}:undefined;
}
