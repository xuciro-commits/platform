import {pageUIManifest} from "@platform/kernel";

export function validChoiceInput(config:{variant:string;options:readonly string[];optionLabels?:readonly string[];label?:string;clearable?:boolean}){
 const limits=pageUIManifest.runtime.choiceInput,bytes=(value:string)=>new TextEncoder().encode(value).length;
 if(!Array.isArray(config.options))return false;
 const indexed=config.variant==="steps"||config.variant==="tabs",labels=indexed?Array.isArray(config.optionLabels)&&config.optionLabels.length===config.options.length&&config.optionLabels.every(label=>typeof label==="string"&&bytes(label)<=limits.maxOptionBytes):config.optionLabels===undefined;
 return labels&&(config.clearable===undefined||typeof config.clearable==="boolean")&&(!config.clearable||config.variant==="multiple")&&(limits.variants as readonly string[]).includes(config.variant)&&Array.isArray(config.options)&&config.options.length<=limits.maxOptions&&new Set(config.options).size===config.options.length&&config.options.every((value,index)=>typeof value==="string"&&value!==""&&bytes(value)<=limits.maxOptionBytes&&(!indexed||value===String(index)))&&(!indexed||config.options.length>0)&&(config.label===undefined||typeof config.label==="string"&&bytes(config.label)<=1024);
}
