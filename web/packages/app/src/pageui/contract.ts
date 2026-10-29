import {t,validExternalFrame,validStaticImage,validSceneConfig,validCivilDate,validTimestampOffset,rangeGrid} from "@platform/ui";
import {pageLayoutDiagnostics} from "../pages/layout";
import type * as PageUI from "./types";
import {parseDecimal} from "../runtime/decimal";
import type {Api} from "@platform/kernel";
import {pageUIManifest} from "./manifest";
import {decodePage} from "./wire";
import {pageSafetyContract} from "./safety";
export {pageUIManifest} from "./manifest";

type PresentationSchema={type:string;nullable?:boolean;discriminator?:string;variants?:Record<string,PresentationSchema>;properties?:Record<string,PresentationSchema>;required?:readonly string[];items?:PresentationSchema;enum?:readonly string[];pattern?:string;minItems?:number;maxItems?:number;minLength?:number;maxLength?:number;minimum?:number;maximum?:number;exclusiveMinimum?:number};
export function schemaIssue(schema: PresentationSchema, value: unknown): string | undefined {
  if (value === null && schema.nullable) return undefined;
  if (schema.type === "variant") {
    if (!value || typeof value !== "object" || Array.isArray(value)) return t("Expected a tagged object.");
    const tag = (value as Record<string, unknown>)[schema.discriminator ?? ""];
    if (typeof tag !== "string" || !schema.variants?.[tag]) return t("Choose a declared variant tag.");
    return schemaIssue(schema.variants[tag], value);
  }
  if (schema.type === "object") {
    if (!value || typeof value !== "object" || Array.isArray(value)) return t("Expected an object.");
    const record = value as Record<string, unknown>;
    const declared=(name:string)=>schema.properties&&Object.hasOwn(schema.properties,name)?schema.properties[name]:undefined;
    for (const name of Object.keys(record)) if (!declared(name)) return t("Unknown field: {name}", { name });
    for (const name of schema.required ?? []) if (!(name in record)) return t("Required field: {name}", { name });
    for (const [name, child] of Object.entries(record)) {
      const issue = schemaIssue(declared(name)!, child); if (issue) return `${name}: ${issue}`;
    }
  } else if (schema.type === "array") {
    if (!Array.isArray(value)) return t("Expected an array.");
    if (schema.maxItems !== undefined && value.length > schema.maxItems) return t("The array exceeds its limit.");
    if (schema.minItems !== undefined && value.length < schema.minItems) return t("The array needs more entries.");
    if (schema.items) for (const item of value) { const issue = schemaIssue(schema.items, item); if (issue) return issue; }
  } else if (schema.type === "string") {
    if (typeof value !== "string") return t("Expected text.");
    if (schema.enum?.length && !schema.enum.includes(value)) return t("Choose a declared option.");
    if (schema.pattern&&value!==""&&!new RegExp(schema.pattern).test(value))return t("Enter a valid value.");
    if (schema.maxLength !== undefined && new TextEncoder().encode(value).length > schema.maxLength) return t("The text exceeds its limit.");
    if (schema.minLength !== undefined && value.trim().length < schema.minLength) return t("Enter a value.");
  } else if (schema.type === "boolean" && typeof value !== "boolean") return t("Expected true or false.");
  else if (schema.type === "number" && (typeof value !== "number" || !Number.isFinite(value))) return t("Expected a number.");
  else if (schema.type === "integer" && (typeof value !== "number" || !Number.isSafeInteger(value))) return t("Expected a safe integer.");
  if ((schema.type === "number" || schema.type === "integer") && typeof value === "number" && (schema.minimum !== undefined && value < schema.minimum || schema.maximum !== undefined && value > schema.maximum || schema.exclusiveMinimum !== undefined && value <= schema.exclusiveMinimum)) return t("Enter a number within the allowed range.");
  return undefined;
}

type Configuration = Record<string, unknown>;
const propertyNames = new Set(pageUIManifest.widgets.flatMap(widget=>[...Object.keys(widget.propsSchema.properties),...("configurationFields" in widget?widget.configurationFields:[])]));
const portNames = new Set(pageUIManifest.widgets.flatMap(widget=>[...widget.inputPorts,...widget.outputPorts].map(port=>port.bindingField)).filter(field=>field!=='enabledWhen'));
const bindingKeys = new Set([...pageSafetyContract.section.bindingPaths,...pageSafetyContract.section.resourcePaths].map(path=>path.split(".")[0]!));
const atPath = (record: unknown,path: string): unknown => path.split('.').reduce<unknown>((value,key)=>value&&typeof value==='object'?(value as Configuration)[key]:undefined,record);

export function widgetDeclarationIssues(value: Configuration): Api.FieldIssue[] {
  if (value.sections!==undefined&&!Array.isArray(value.sections))return [{path:["sections"],code:"schema",message:t("Expected an array.")}];
  if (!Array.isArray(value.sections)) return [];
  return value.sections.flatMap((section: unknown,index): Api.FieldIssue[]=>{
    if(!section || typeof section!=='object' || Array.isArray(section))return [{path:['sections',String(index)],code:'schema',message:t('Expected an object.')}];
    const record=section as Configuration,widget=pageUIManifest.widgets.find(item=>item.componentID===record.widget);
    const issue=(path:string[],message:string):Api.FieldIssue=>({path:['sections',String(index),...path],code:'schema',message});
    const unsupported=t('Unsupported widget configuration: {widget}, version {version}. Your page draft is preserved.',{widget:String(record.widget),version:String(record.configVersion)});
    if(!widget)return [issue(['widget'],unsupported)];
    if(record.configVersion!==widget.configVersion)return [issue(['configVersion'],unsupported)];
    const declaredPorts = new Set<string>([...widget.inputPorts,...widget.outputPorts].map(port=>port.bindingField));
    for(const field of portNames){
      const binding=atPath(record,field);
      if(binding!==undefined&&binding!==''){
        if(!declaredPorts.has(field))return [issue(field.split('.'),t('Unknown field: {name}',{name:field}))];
        if(typeof binding!=='string')return [issue(field.split('.'),t('Expected text.'))];
      }
    }
    const properties: Configuration={};
    for(const [key,item] of Object.entries(record))if(item!==undefined&&propertyNames.has(key))properties[key]=item;
    for(const key of Object.keys(properties))if(!Object.hasOwn(widget.propsSchema.properties,key)){
     if(("configurationFields"in widget&&(widget.configurationFields as readonly string[]).includes(key))||bindingKeys.has(key))delete properties[key];
    }
    const message=schemaIssue(widget.propsSchema as unknown as PresentationSchema,properties);
    if(message)return [issue([],message)];
    if(record.widget==="external-frame"&&!validExternalFrame(record.externalFrame))return [issue(["externalFrame"],t("Use a reviewed HTTPS address and its exact origin."))];
    if(record.widget==="static-image"&&!validStaticImage(record.image))return [issue(["image"],t("Use a supported image address, caption and height."))];
    if(record.widget==="range-input"){
     const range=record.rangeInput as PageUI.PageRangeInput;
     if(!rangeGrid(range.min,range.max,range.step))return [issue(["rangeInput"],t("Range bounds and step must form an exact bounded grid."))];
    }
    if(record.widget==="choice-input"){
     const choice=record.choiceInput as PageUI.PageChoiceInput,indexed=["steps","tabs"].includes(choice.variant);
     if(new Set(choice.options).size!==choice.options.length||choice.options.some(option=>!option)||indexed&&(!choice.options.length||choice.options.some((option,index)=>option!==String(index))||choice.optionLabels?.length!==choice.options.length)||!indexed&&choice.optionLabels!==undefined||choice.clearable&&choice.variant!=="multiple")return [issue(["choiceInput"],t("Choice options need unique values and matching labels for indexed choices."))];
    }
    if(record.widget==="scene-3d"&&!validSceneConfig(record.scene as PageUI.PageSceneConfig))return [issue(["scene"],t("Scene layers and mappings need valid unique identities and finite ranges."))];
    if(record.widget==="date-input"&&(record.dateKind==="datetime"?!validTimestampOffset(String(record.dateOffset??"")):!!record.dateOffset))return [issue(["dateOffset"],t("Datetime input needs an explicit valid offset; a civil date has no offset."))];
    if(record.widget==="record-calendar"&&!(record.recordCalendar as PageUI.PageRecordCalendar|undefined)?.initialMonth)return [issue(["recordCalendar","initialMonth"],t("Enter a valid value."))];
    if(record.widget==="record-gantt"){
     const gantt=record.recordGantt as PageUI.PageRecordGantt|undefined;
     if(!gantt||!validCivilDate(gantt.rangeStart)||!validCivilDate(gantt.rangeEnd)||gantt.rangeStart>=gantt.rangeEnd)return [issue(["recordGantt"],t("Gantt display bounds need two ordered civil dates."))];
    }
    for(const key of ["recordGantt","recordEvents","resourceList"]){
     const config=record[key]as {tones?:{value:string;tone:string}[];statusTones?:{value:string;tone:string}[]}|undefined,tones=config?.tones??config?.statusTones;
     if(tones&&(new Set(tones.map(item=>item.value)).size!==tones.length||tones.some(item=>!item.value)))return [issue([key],t("Status tones need unique nonempty values."))];
    }
    for(const [name,raw]of [["alertBanner.threshold",(record.alertBanner as PageUI.PageAlertBanner|undefined)?.threshold],["progressTotal",record.progressTotal]]as const){
     if(raw!==undefined&&raw!==""&&(typeof raw!=="string"||parseDecimal(raw)?.value!==raw||name==="progressTotal"&&(raw.startsWith("-")||raw==="0")))return [issue(name.split("."),t("Use a canonical decimal value; a progress denominator must be positive."))];
    }
    const template=(record.tablePresentation as PageUI.PageTablePresentation|undefined)?.titleTemplate;
    if(template!==undefined&&/[{}]/.test(template.replace("{count}","")))return [issue(["tablePresentation","titleTemplate"],t("A table title supports one count placeholder."))];
    return [];
  });
}

export function pagePresentationIssues(page:{sections?:unknown[];document?:{formatVersion:number;uiProfile:string;nodes?:PageUI.PageDocument["nodes"];root?:string}}):Api.FieldIssue[] {
 if(page.sections!==undefined&&!Array.isArray(page.sections))return [{path:["sections"],code:"schema",message:t("Expected an array.")}];
 const sections=page.sections??[],document=page.document;
 if(document&&(document.formatVersion!==2||!(pageUIManifest.supportedProfiles as readonly string[]).includes(document.uiProfile)))return [{path:["document","uiProfile"],code:"schema",message:t("This page uses an unsupported format or UI profile. Your page draft is preserved.")}];
 const legacy=!document;
 const normalized=sections.map(section=>section&&typeof section==='object'&&!Array.isArray(section)?{...section,...legacy&&!("configVersion" in section)?{configVersion:1}:{}}:section);
 const issues=widgetDeclarationIssues({sections:normalized});
 const ids=new Set<string>();
 for(const [index,section]of normalized.entries())if(section&&typeof section==="object"&&"id"in section&&typeof section.id==="string"){if(ids.has(section.id))issues.push({path:["sections",String(index),"id"],code:"schema",message:t("Page section identities must be unique.")});ids.add(section.id);}
 if(document?.nodes&&document.root)for(const problem of pageLayoutDiagnostics(document as PageUI.PageDocument,normalized as PageUI.Section[]))issues.push({path:["document","nodes",problem.node],code:"layout",message:t(problem.code)});
 for(const [index,section] of normalized.entries()){
  if(!section||typeof section!=="object"||Array.isArray(section))continue;
  const value=section as Configuration,widget=pageUIManifest.widgets.find(widget=>widget.componentID===value.widget);
  if(!widget)continue;
  const supported=legacy?(pageUIManifest.documentless.widgets as readonly unknown[]).includes(value.widget):pageUIManifest.supportedProfiles.indexOf(document.uiProfile as typeof pageUIManifest.supportedProfiles[number])>=pageUIManifest.supportedProfiles.indexOf(widget.requiredUIProfile);
  if(!supported)issues.push({path:["sections",String(index),"widget"],code:"schema",message:t("Unsupported widget configuration: {widget}, version {version}. Your page draft is preserved.",{widget:String(value.widget),version:String(value.configVersion)})});
  if(document&&"propertyProfiles"in widget)for(const feature of widget.propertyProfiles){
   const configured=atPath(value,feature.path),active="values"in feature?(feature.values as readonly unknown[]).includes(configured):configured!==undefined&&configured!==""&&configured!==false;
   if(active&&pageUIManifest.supportedProfiles.indexOf(document.uiProfile as typeof pageUIManifest.supportedProfiles[number])<pageUIManifest.supportedProfiles.indexOf(feature.requiredUIProfile))issues.push({path:["sections",String(index),...feature.path.split(".")],code:"schema",message:t("This presentation property needs a newer page profile.")});
  }
 }
 return issues;
}

export function presentPage<T extends {presentationWords?:Record<string,string>;sections?:unknown[];document?:{nodes:Record<string,unknown>}}>(page:T):T {
 const words=page.presentationWords;
 if(!words)return page;
 const copy=structuredClone(page);
 const replace=(value:Configuration,path:string[]):void=>{
  const key=path[0]!,rest=path.slice(1);
  if(key==='*'){for(const entry of Object.keys(value)){const child=value[entry];if(!rest.length&&typeof child==='string')value[entry]=t(child,undefined,words);else if(child&&typeof child==='object')replace(child as Configuration,rest);}return;}
  const child=value[key];
  if(!rest.length){if(typeof child==='string')value[key]=t(child,undefined,words);return;}
  if(child&&typeof child==='object')replace(child as Configuration,rest);
 };
 for(const section of copy.sections??[]){
  if(!section||typeof section!=='object')continue;
  const value=section as Configuration,widget=pageUIManifest.widgets.find(item=>item.componentID===value.widget);
  for(const path of widget?.declarationTexts??[])replace(value,path.split('.'));
 }
 for(const node of Object.values(copy.document?.nodes??{}))if(node&&typeof node==='object')replace(node as Configuration,['title']);
 const document=copy.document as unknown as Configuration|undefined;
 for(const overlay of Object.values(document?.overlays as Configuration??{}))if(overlay&&typeof overlay==='object')replace(overlay as Configuration,['title']);
 return copy;
}

export function pageAuthoringIssues(value:Record<string,unknown>):Api.FieldIssue[]{
 if(value.format===3){
  const spec=value.widgetSpec as {format?:string;body?:{sections?:unknown[];document?:unknown}}|undefined;
  if(spec?.format!=="platform.page.web.v1"||!spec.body||typeof spec.body!=="object"||!Array.isArray(spec.body.sections))return [{path:["widgetSpec"],code:"schema",message:t("This page uses an unsupported format or UI profile. Your page draft is preserved.")}];
  return pagePresentationIssues(decodePage(value as unknown as Api.Page));
 }
 return pagePresentationIssues(value as Parameters<typeof pagePresentationIssues>[0]);
}

export {decodePage};
export {encodePage} from "./wire";
