import { ProgressBar } from "@astryxdesign/core/ProgressBar";
import { Text } from "@astryxdesign/core/Text";
import {t} from "../i18n";

export function progressRatio(value:string,total:string) {
 const parts=(text:string)=>{if(text.length>128||!/^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?$/.test(text))return;const [whole,fraction=""]=text.split(".");return {coefficient:BigInt(whole+fraction),scale:fraction.length};};
 const a=parts(value),b=parts(total);if(!a||!b||a.coefficient<0n||b.coefficient<=0n)return;
 const scale=Math.max(a.scale,b.scale),n=a.coefficient*10n**BigInt(scale-a.scale),d=b.coefficient*10n**BigInt(scale-b.scale),over=n>d;
 const bounded=over?d:n,basisPoints=(bounded*10000n+d/2n)/d,percentage=(n*100n+d/2n)/d;
 return {width:Number(basisPoints)/100,percentage:percentage.toString(),over,tone:n*100n>d*80n?"success":n*100n>d*40n?"info":"warning"};
}
export function Progress({value,total,label}:{value:string;total:string;label:string}) {
 const result=progressRatio(value,total);
 if(!result)return <Text as="p" color="primary">{t("Progress needs a valid nonnegative value and a positive total.")}</Text>;
 const text=`${value} / ${total} · ${result.percentage}%`;
 return <><ProgressBar label={label} value={result.width} max={100} hasValueLabel formatValueLabel={()=>text} variant={result.tone==="success"?"success":result.tone==="info"?"accent":"warning"} />
 {result.over&&<Text as="p" type="supporting">{t("Value exceeds total; the bar is capped at 100%.")}</Text>}</>;
}