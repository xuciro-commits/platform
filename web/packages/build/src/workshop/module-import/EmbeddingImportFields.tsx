import {useHost} from "@platform/app";
import {Input,Select,t} from "@platform/ui";
import type {SourceModule} from "./compile";
import {sourceEmbeddingInputs,type EmbeddingImportBinding,type ExternalFrameImportBinding} from "./embedding";

export function EmbeddingImportFields({widget,module,value,onChange}:{widget:SourceModule["widgets"][string];module:SourceModule;value?:EmbeddingImportBinding;onChange:(value:EmbeddingImportBinding)=>void}){
 const {definitions}=useHost(),pages=definitions.filter(d=>d.page&&d.contentVersion),child=pages.find(d=>JSON.stringify(d.ref)===JSON.stringify(value?.page.ref)),inputs=sourceEmbeddingInputs(widget),migration=widget.type==="EmbeddedModule"?"original-page":widget.type==="CustomWidget"?"registered-page":"actual-analysis-page";
 const update=(patch:Partial<EmbeddingImportBinding>)=>onChange({migration:"",page:{ref:{app:"",kind:"page",name:""},sourceVersion:""},contentVersion:"",interfaceVersion:0,ports:{},...value,...patch});
 return <fieldset className="grid gap-2"><legend>{t("Map embedded source {widget}",{widget:widget.name})}</legend>
  <label className="grid gap-1 text-xs">{t("Embedded source interpretation")}<Select value={value?.migration??""} onChange={e=>update({migration:e.target.value as EmbeddingImportBinding["migration"]})}><option value="">{t("Choose an explicit migration")}</option><option value={migration}>{t(widget.type==="CustomWidget"?"Use a registered original page":widget.type==="QuiverDashboard"?"Use an original analysis page":"Use an original child page")}</option></Select></label>
  <label className="grid gap-1 text-xs">{t("Fixed original embedded page")}<Select value={child?JSON.stringify(child.ref):""} onChange={e=>{const d=pages.find(d=>JSON.stringify(d.ref)===e.target.value);if(d?.page&&d.contentVersion)update({page:{ref:d.ref,sourceVersion:d.version},contentVersion:d.contentVersion,interfaceVersion:d.page.document?.interface?.version??0,ports:{},results:{}});}}><option value="">{t("Choose a published page")}</option>{pages.map(d=><option key={JSON.stringify(d.ref)} value={JSON.stringify(d.ref)}>{d.page?.title||d.ref.name}</option>)}</Select></label>
  {child&&<p className="break-all text-xs text-muted">{value?.contentVersion}</p>}
  {inputs?.map(input=><label key={input.name} className="grid gap-1 text-xs">{t("Map embedded input {input}",{input:input.name})}<Select value={value?.ports[input.name]??""} onChange={e=>update({ports:{...value?.ports,[input.name]:e.target.value}})}><option value="">{t("Choose an original interface port")}</option>{Object.entries(child?.page?.document?.interface?.inputs??{}).map(([id,port])=><option key={id} value={id}>{id} · {port.type}</option>)}</Select></label>)}
  {widget.type==="EmbeddedModule"&&Object.entries(child?.page?.document?.interface?.outputs??{}).filter(([,port])=>port.type!=="record").map(([id,port])=><label key={id} className="grid gap-1 text-xs">{t("Map embedded result {port}",{port:id})}<Select value={value?.results?.[id]??""} onChange={e=>{const results={...value?.results};if(e.target.value)results[id]=e.target.value;else delete results[id];update({results});}}><option value="">{t("None")}</option>{module.variables.filter(v=>v.definitionKind==="static"&&v.type===port.type).map(v=><option key={v.id} value={v.id}>{v.name}</option>)}</Select></label>)}
  <p className="text-xs text-muted">{t("The mapped page supplies real UI, current permissions and fixed dependencies. Vendor code and decorative KPI values do not execute. Collection inputs require the original collection interface.")}</p>
 </fieldset>;
}

export function ExternalFrameImportFields({widget,value,onChange}:{widget:SourceModule["widgets"][string];value?:ExternalFrameImportBinding;onChange:(value:ExternalFrameImportBinding)=>void}){
 let origin="";try{origin=new URL(String(widget.config.url)).origin;}catch{}
 return <fieldset className="grid gap-2"><legend>{t("Map external document {widget}",{widget:widget.name})}</legend>
 <label className="grid gap-1 text-xs">{t("External document interpretation")}<Select value={value?.migration??""} onChange={e=>onChange({origin:value?.origin??origin,migration:e.target.value as ExternalFrameImportBinding["migration"]})}><option value="">{t("Choose an explicit migration")}</option><option value="sandboxed-document">{t("Use an isolated external document")}</option></Select></label>
 <label className="grid gap-1 text-xs">{t("Reviewed external origin")}<Input value={value?.origin??""} onChange={e=>onChange({migration:value?.migration??"",origin:e.target.value})}/></label>
 <p className="text-xs text-muted">{t("Isolated external document. Scripts, forms and platform data access are disabled.")}</p></fieldset>;
}
