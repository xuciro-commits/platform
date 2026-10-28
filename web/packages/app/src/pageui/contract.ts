import {t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {pageUIManifest} from "./manifest";
export {pageUIManifest} from "./manifest";

export function schemaIssue(schema: Api.ValueSchema & {minimum?:number;maximum?:number}, value: unknown): string | undefined {
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
    for (const name of schema.required ?? []) if (!(name in record)) return t("Required field: {name}", { name });
    for (const [name, child] of Object.entries(record)) {
      const property = schema.properties&&Object.hasOwn(schema.properties,name)?schema.properties[name]:undefined;
      if (!property) return t("Unknown field: {name}", { name });
      const issue = schemaIssue(property, child); if (issue) return `${name}: ${issue}`;
    }
  } else if (schema.type === "array") {
    if (!Array.isArray(value)) return t("Expected an array.");
    if (schema.maxItems && value.length > schema.maxItems) return t("The array exceeds its limit.");
    if (schema.items) for (const item of value) { const issue = schemaIssue(schema.items, item); if (issue) return issue; }
  } else if (schema.type === "string") {
    if (typeof value !== "string") return t("Expected text.");
    if (schema.enum?.length && !schema.enum.includes(value)) return t("Choose a declared option.");
    if (schema.maxLength && new TextEncoder().encode(value).length > schema.maxLength) return t("The text exceeds its limit.");
  } else if (schema.type === "boolean" && typeof value !== "boolean") return t("Expected true or false.");
  else if (schema.type === "number" && (typeof value !== "number" || !Number.isFinite(value))) return t("Expected a number.");
  else if (schema.type === "integer" && (typeof value !== "number" || !Number.isSafeInteger(value))) return t("Expected a safe integer.");
  if ((schema.type === "number" || schema.type === "integer") && typeof value === "number" && (schema.minimum !== undefined && value < schema.minimum || schema.maximum !== undefined && value > schema.maximum)) return t("Enter a number within the allowed range.");
  return undefined;
}

type Configuration = Record<string, unknown>;
const propertyNames = new Set(pageUIManifest.widgets.flatMap(widget=>Object.keys(widget.propsSchema.properties)));
const portNames = new Set(pageUIManifest.widgets.flatMap(widget=>[...widget.inputPorts,...widget.outputPorts].map(port=>port.bindingField)).filter(field=>field!=='enabledWhen'));
const atPath = (record: unknown,path: string): unknown => path.split('.').reduce<unknown>((value,key)=>value&&typeof value==='object'?(value as Configuration)[key]:undefined,record);

/** All authoring modes meet the legacy flat Section here. Only properties
 * owned by the shared manifest are projected; model/action validation stays in
 * the existing page owner and never copies or reinterprets a business model. */
export function widgetDeclarationIssues(value: Configuration): Api.FieldIssue[] {
  if (!Array.isArray(value.sections)) return [];
  return value.sections.flatMap((section: unknown,index): Api.FieldIssue[]=>{
    if(!section || typeof section!=='object' || Array.isArray(section))return [];
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
    const message=schemaIssue(widget.propsSchema as unknown as Api.ValueSchema,properties);
    return message?[issue([],message)]:[];
  });
}

/** Rendering and import use the author's contract before mounting readers or
 * effect handlers. The input is retained; validation never edits a draft. */
export function pagePresentationIssues(page:{sections?:unknown[];document?:{formatVersion:number;uiProfile:string}}):Api.FieldIssue[] {
 const sections=page.sections??[],document=page.document;
 if(document&&(document.formatVersion!==2||!(pageUIManifest.supportedProfiles as readonly string[]).includes(document.uiProfile)))return [{path:["document","uiProfile"],code:"schema",message:t("This page uses an unsupported format or UI profile. Your page draft is preserved.")}];
 const legacy=!document;
 const normalized=sections.map(section=>section&&typeof section==='object'&&!Array.isArray(section)?{...section,...legacy&&!("configVersion" in section)?{configVersion:1}:{}}:section);
 const issues=widgetDeclarationIssues({sections:normalized});
 for(const [index,section] of normalized.entries()){
  if(!section||typeof section!=="object"||Array.isArray(section))continue;
  const value=section as Configuration,widget=pageUIManifest.widgets.find(widget=>widget.componentID===value.widget);
  if(!widget)continue;
  const supported=legacy?(pageUIManifest.documentless.widgets as readonly unknown[]).includes(value.widget):pageUIManifest.supportedProfiles.indexOf(document.uiProfile as typeof pageUIManifest.supportedProfiles[number])>=pageUIManifest.supportedProfiles.indexOf(widget.requiredUIProfile);
  if(!supported)issues.push({path:["sections",String(index),"widget"],code:"schema",message:t("Unsupported widget configuration: {widget}, version {version}. Your page draft is preserved.",{widget:String(value.widget),version:String(value.configVersion)})});
 }
 return issues;
}
