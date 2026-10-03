import {pageUIManifest} from "@platform/kernel";

export function validChoiceInput(config:{variant:string;options:readonly string[];label?:string}){
 const limits=pageUIManifest.runtime.choiceInput,bytes=(value:string)=>new TextEncoder().encode(value).length;
 return (limits.variants as readonly string[]).includes(config.variant)&&Array.isArray(config.options)&&config.options.length<=limits.maxOptions&&new Set(config.options).size===config.options.length&&config.options.every(value=>typeof value==="string"&&value!==""&&bytes(value)<=limits.maxOptionBytes)&&(config.label===undefined||typeof config.label==="string"&&bytes(config.label)<=1024);
}
