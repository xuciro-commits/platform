import {renderingLimits} from "../gen/presentation-limits";
import { HStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import {t} from "../i18n";
export type TermCount={value:string|null;count:number};
export function validTermCounts(terms:TermCount[]) {
 return Array.isArray(terms)&&terms.length<=renderingLimits.runtime.terms.maxGroups&&terms.every(term=>term&&typeof term==="object"&&!Array.isArray(term)&&(term.value===null||typeof term.value==="string")&&Number.isSafeInteger(term.count)&&term.count>0)&&new Set(terms.map(term=>JSON.stringify(term.value))).size===terms.length;
}
export function TermCounts({terms,label}:{terms:TermCount[];label:string}){
 if(!validTermCounts(terms))return <Text as="p" role="alert">{t("Term counts are invalid or exceed their budget.")}</Text>;
 if(!terms.length)return <Text type="supporting" role="status">{t("No terms in the matching records.")}</Text>;
 const maximum=Math.max(...terms.map(term=>term.count));
 return <HStack gap={2} wrap="wrap" role="group" aria-label={label}>{terms.map(term=><Token key={JSON.stringify(term.value)} size={term.count===maximum?"lg":term.count*2>=maximum?"md":"sm"} label={`${term.value===null?t("No value"):term.value===""?t("Empty text"):term.value} · ${term.count}`} />)}</HStack>;
}
