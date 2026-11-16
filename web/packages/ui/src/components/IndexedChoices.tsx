import {renderingLimits} from "../gen/presentation-limits";
import { Step, Stepper } from "@astryxdesign/core/Stepper";
import { Text } from "@astryxdesign/core/Text";
import {t} from "../i18n";

type ChoiceOption={value:string;label:string};
type SelectorProps={options:readonly ChoiceOption[];value:string;label:string;enabled?:boolean;onChange?:(value:string)=>void};
function validOptions(options:readonly ChoiceOption[]) {
 const limits=renderingLimits.runtime.choiceInput,bytes=(value:string)=>new TextEncoder().encode(value).length;
 return Array.isArray(options)&&options.length>0&&options.length<=limits.maxOptions&&new Set(options.map(option=>option?.value)).size===options.length&&options.every(option=>option&&typeof option.value==="string"&&option.value!==""&&bytes(option.value)<=limits.maxOptionBytes&&typeof option.label==="string"&&bytes(option.label)<=limits.maxOptionBytes);
}
export function CurrentChoice({value,known}:{value:string;known:boolean}) {
 return !known?<Text as="p" role="status" type="supporting">{value===""?t("No current choice is selected."):t("Current choice is outside the configured options: {value}.",{value})}</Text>:null;
}

export function StepSelector({options,value,label,enabled=true,onChange}:SelectorProps) {
 if(!validOptions(options))return <Text as="p" role="alert">{t("Choice configuration is unsupported.")}</Text>;
 const current=options.findIndex(option=>option.value===value),writable=enabled&&!!onChange;
 return <div className="grid min-w-0 gap-2"><Stepper label={label} activeStep={current} onStepClick={writable ? (index) => { const option = options[index]; if (option) onChange?.(option.value); } : undefined}>{options.map((option,index)=><Step key={option.value} step={index} label={option.label} isDisabled={!writable} />)}</Stepper><CurrentChoice value={value} known={current>=0}/></div>;
}
