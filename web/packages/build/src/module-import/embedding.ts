import type {Api} from "@platform/kernel";
import type {SourceModule,ImportTarget} from "./compile";

export const embeddingSourceTypes=["EmbeddedModule","CustomWidget","QuiverDashboard"];
export type EmbeddingImportBinding={
 migration:"original-page"|"registered-page"|"actual-analysis-page"|"";
 page:Api.AssetBinding;
 contentVersion:string;
 interfaceVersion:number;
 ports:Record<string,string>;
 results?:Record<string,string>;
};
export type ExternalFrameImportBinding={migration:"sandboxed-document"|"";origin:string};
const object=(v:unknown):v is Record<string,unknown>=>!!v&&typeof v==="object"&&!Array.isArray(v);
const identifier=(v:unknown):v is string=>typeof v==="string"&&/^[A-Za-z][A-Za-z0-9_.:-]{0,127}$/.test(v);
export type SourceEmbeddingInput={name:string;variable?:string;literal?:string};

/** Validate every source setting before replacing its placeholder presentation. */
export function sourceEmbeddingInputs(widget:SourceModule["widgets"][string]):SourceEmbeddingInput[]|undefined {
 const c=widget.config;
 if(widget.type==="QuiverDashboard")return Object.keys(c).every(k=>k==="title")&&(c.title===undefined||typeof c.title==="string")?[]:undefined;
 const custom=widget.type==="CustomWidget",items=custom?c.params:c.bindings;
 if(Object.keys(c).some(k=>!(custom?["widgetSet","version","params","permissions"]:["moduleId","bindings"]).includes(k))||!identifier(custom?c.widgetSet:c.moduleId)||custom&&(!(typeof c.version==="string"&&/^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(c.version))||!Array.isArray(c.permissions)||c.permissions.some(p=>p!=="read.objects")||new Set(c.permissions).size!==c.permissions.length)||!Array.isArray(items)||items.length>16)return;
 const inputs:SourceEmbeddingInput[]=[];
 for(const item of items) {
  if(!object(item)||Object.keys(item).some(k=>!(custom?["name","varId","value"]:["parentVar","childInterface"]).includes(k)))return;
  const name=custom?item.name:item.childInterface,variable=custom?item.varId:item.parentVar,literal=custom?item.value:undefined;
  if(!identifier(name)||variable!==undefined&&!identifier(variable)||literal!==undefined&&typeof literal!=="string"||(variable!==undefined)===(literal!==undefined)||inputs.some(i=>i.name===name))return;
  inputs.push({name,variable:variable as string|undefined,literal:literal as string|undefined});
 }
 return inputs;
}

/** The member-projected descriptor guides mapping; Go verifies original bytes. */
export function embeddedImportTarget(type:string,b:EmbeddingImportBinding|undefined,target:ImportTarget):Api.Definition|undefined {
 if(!b||Object.keys(b).some(k=>!["migration","page","contentVersion","interfaceVersion","ports","results"].includes(k))||!object(b.page)||Object.keys(b.page).some(k=>!["ref","sourceVersion"].includes(k))||!object(b.page.ref)||Object.keys(b.page.ref).some(k=>!["app","kind","name"].includes(k))||!object(b.ports)||b.results!==undefined&&!object(b.results)||!/^page\.sha256\.[0-9a-f]{64}$/.test(b.contentVersion)||!Number.isInteger(b.interfaceVersion)||b.migration!==(type==="EmbeddedModule"?"original-page":type==="CustomWidget"?"registered-page":"actual-analysis-page"))return;
 const d=target.definitions?.find(d=>d.ref.kind==="page"&&d.ref.app===b.page.ref.app&&d.ref.name===b.page.ref.name&&d.version===b.page.sourceVersion&&d.contentVersion===b.contentVersion);
 if(!d?.page||b.page.ref.kind!=="page"||(d.page.document?.interface?.version??0)!==b.interfaceVersion)return;
 if(type==="QuiverDashboard"&&!d.page.sections?.some(s=>["metric","chart","pivot","gauge","summary-stats","collection-analysis","observation"].includes(s.widget)))return;
 return d;
}
