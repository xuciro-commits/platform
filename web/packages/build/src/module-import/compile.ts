import {pageUIManifest,type Api} from "@platform/kernel";
import type {PageDraft,AuthoringSection} from "../page-editor/draft";
import {workshopMapping,workshopMigrationCatalog} from "./catalog";

type Obj=Record<string,unknown>;
type SourceWidget=Obj&{id:string;type:string;name:string;config:Obj};
type SourceSection=Obj&{id:string;name:string;layout:string;children:{kind:string;id:string}[]};
type SourceVariable=Obj&{id:string;name:string;type:string;definitionKind:string};
export type SourceModule=Obj&{id:string;name:string;description:string;pages:{id:string;name:string;rootSectionId:string}[];sections:Record<string,SourceSection>;widgets:Record<string,SourceWidget>;variables:SourceVariable[];overlays:(Obj&{id:string;name:string;kind:string;rootSectionId:string;openVariableId?:string})[];unusedWidgetIds:string[]};
export type ImportDiagnostic={path:string;code:string;blocking:boolean};
export type ImportBindings={aggregates?:Record<string,{measure:string;conditions?:Api.PageQueryCondition[]}>;states?:Record<string,Record<string,string>>;links?:Record<string,Api.PageRecordLink[]>;edits?:Record<string,{action:string;fields:string[]}>;objects:Record<string,string>;fields:Record<string,Record<string,string>>;actions:Record<string,string>;queries:Record<string,Api.AssetBinding|undefined>};
export type ImportEntity={type:string;app:string;lifecycle?:Api.LifecycleInfo;fields:{name:string;type:string;ref?:string;inverse?:string}[]};
export type ImportTarget={object:string;profile:string;entities:ImportEntity[];actions:{schema:string;target:string;new?:boolean;needsApproval?:boolean;payload?:{name:string;required?:boolean}[]}[];definitions?:Api.Definition[]};
export type ImportReport={formatVersion:1;source:string;sourceModule:string;selectedPage:string;inventoryRevision:string;diagnostics:ImportDiagnostic[];ids:{nodes:Record<string,string>;widgets:Record<string,string>;variables:Record<string,string>;overlays:Record<string,string>};draft?:PageDraft};
const obj=(v:unknown):v is Obj=>!!v&&typeof v==="object"&&!Array.isArray(v);
const arr=(v:unknown):unknown[]=>Array.isArray(v)?v:[];
const text=(v:unknown)=>typeof v==="string"?v:"";
const own=(o:Obj,key:string)=>Object.hasOwn(o,key)?o[key]:undefined;
const pointer=(value:string)=>value.replaceAll("~","~0").replaceAll("/","~1");

export function parseWorkshopModule(source:string):{module?:SourceModule;diagnostics:ImportDiagnostic[]} {
 if(new TextEncoder().encode(source).length>1_048_576)return {diagnostics:[{path:"/",code:"source-size",blocking:true}]};
 try {
  const m:unknown=JSON.parse(source);
  if(!obj(m)||!text(m.id)||!text(m.name)||m.description!==undefined&&typeof m.description!=="string"||!Array.isArray(m.pages)||!obj(m.sections)||!obj(m.widgets)||!Array.isArray(m.variables)||!Array.isArray(m.overlays)||!Array.isArray(m.unusedWidgetIds))throw Error();
  if(m.pages.length>32||Object.keys(m.sections).length>256||Object.keys(m.widgets).length>128||m.variables.length>256||m.overlays.length>16) return {diagnostics:[{path:"/",code:"source-budget",blocking:true}]};
  const validID=(v:unknown)=>typeof v==="string"&&!!v&&v.length<=128&&!Object.hasOwn(Object.prototype,v)&&v!=="__proto__"&&v!=="prototype";
  const ids=new Set<string>();
  for(const p of m.pages){if(!obj(p)||!validID(p.id)||!text(p.name)||!validID(p.rootSectionId)||ids.has(p.id as string))throw Error();ids.add(p.id as string);}
  for(const [id,s] of Object.entries(m.sections)){if(!validID(id)||!obj(s)||s.id!==id||!text(s.layout)||!Array.isArray(s.children)||s.children.some(c=>!obj(c)||!["section","widget"].includes(text(c.kind))||!validID(c.id)))throw Error();}
  for(const [id,w] of Object.entries(m.widgets)){if(!validID(id)||!obj(w)||w.id!==id||!text(w.type)||!text(w.name)||!obj(w.config))throw Error();}
  ids.clear();for(const v of m.variables){if(!obj(v)||!validID(v.id)||!text(v.type)||!text(v.definitionKind)||ids.has(v.id as string))throw Error();ids.add(v.id as string);}
  ids.clear();for(const o of m.overlays){if(!obj(o)||!validID(o.id)||!validID(o.rootSectionId)||ids.has(o.id as string)||!["modal","drawer"].includes(text(o.kind)))throw Error();ids.add(o.id as string);}
  if(m.unusedWidgetIds.some(id=>!validID(id)))throw Error();
  return {module:m as SourceModule,diagnostics:[]};
 }catch{return {diagnostics:[{path:"/",code:"module-shape",blocking:true}]};}
}

/** One-way authoring conversion. No source expression, Action or Flow executes. */
export function compileWorkshopModule(source:string,pageID:string,bindings:ImportBindings,target:ImportTarget):ImportReport {
 const parsed=parseWorkshopModule(source),diagnostics=[...parsed.diagnostics],m=parsed.module;
 const ids:ImportReport["ids"]={nodes:{},widgets:{},variables:{},overlays:{}};
 const report:ImportReport={formatVersion:1,source,sourceModule:m?.id??"",selectedPage:pageID,inventoryRevision:workshopMigrationCatalog.source.typesSha256,diagnostics,ids};
 const issue=(path:string,code:string,blocking=true)=>{if(!diagnostics.some(d=>d.path===path&&d.code===code))diagnostics.push({path,code,blocking});};
 if(!m)return report;
 const page=m.pages.find(p=>p.id===pageID);if(!page){issue("/pages","page-required");return report;}
 // The selected-page boundary is explicit; the complete original module stays in the report.
 issue("/header","native-shell",false);if(m.pages.length>1)issue("/pages","other-pages-retained",false);
 for(const key of Object.keys(m))if(!["id","name","description","header","pages","sections","widgets","variables","overlays","unusedWidgetIds","flows","usedObjectTypes","usedActions","usedFunctions","moduleInterface","savedState"].includes(key))issue(`/${pointer(key)}`,"unsupported-setting");
 if(arr(m.flows).length)issue("/flows","flows-retained-not-executed",false);
 if(arr(m.moduleInterface).length)issue("/moduleInterface","interface-profile");
 const document:Api.PageDocument={formatVersion:2,uiProfile:target.profile,root:"",nodes:{},variables:{},queries:{},overlays:{},events:[]},sections:AuthoringSection[]=[];
 const draft:PageDraft={document,sections,selections:[],title:page.name,description:m.description||""};
 const allocated=new Map<string,string>(),scope=new Map<string,{kind:"page"|"overlay";owner?:string}>(),widgetOwners=new Map<string,string|undefined>(),usedWidgets=new Set<string>(),seenSections=new Set<string>(),vars=new Map(m.variables.map(v=>[v.id,v])),queryObjects=new Map<string,string>(),recordObjects=new Map<string,string>();
 let counter=0;const id=(space:string,sourceID:string)=>{const key=`${space}/${sourceID}`;if(!allocated.has(key))allocated.set(key,`import_${space}_${++counter}`);return allocated.get(key)!;};
 const variablePath=(sourceID:string)=>`/variables/${m.variables.findIndex(v=>v.id===sourceID)}`;
 const overlayPath=(sourceID:string)=>`/overlays/${m.overlays.findIndex(o=>o.id===sourceID)}`;
 const safeKeys=(value:Obj,keys:string[],path:string)=>{for(const key of Object.keys(value))if(!keys.includes(key))issue(`${path}/${pointer(key)}`,"unsupported-setting");};
 const presentation=(value:Obj,path:string,defaults:Obj)=>{for(const [key,expected] of Object.entries(defaults))if(own(value,key)!==undefined){if(JSON.stringify(value[key])!==JSON.stringify(expected))issue(`${path}/${pointer(key)}`,"presentation-profile");else issue(`${path}/${pointer(key)}`,"native-presentation",false);}};
 const entity=(external:string,path:string)=>{const mapped=own(bindings.objects,external),found=target.entities.find(e=>e.type===mapped);if(!found)issue(path,"object-binding");return found;};
 const field=(external:string,property:string,path:string)=>{const e=entity(external,path),fields=own(bindings.fields,external),mapped=obj(fields)?text(own(fields,property)):"";if(!mapped||mapped!=="id"&&!e?.fields.some(f=>f.name===mapped)||mapped==="id"&&property!=="id")issue(path,"field-binding");return mapped||"";};
 const variable=(sourceID:string,context:{kind:"page"|"overlay";owner?:string},path:string):string=>{
  const v=vars.get(sourceID);if(!v){issue(path,"variable-reference");return "";}
  safeKeys(v,["id","name","type","description","definitionKind","recompute","staticValue","sourceObjectType","transformations","dependsOn","widgetId","widgetOutputKey","isInterface","externalId","interfaceDirection","inputVarId","objectSet","functionApiName"],variablePath(sourceID));
  if(v.isInterface||v.interfaceDirection||v.externalId)issue(variablePath(sourceID),"interface-profile");
  if(v.recompute!==undefined&&v.recompute!=="automatic")issue(`${variablePath(sourceID)}/recompute`,"recompute-profile");
  if(arr(v.dependsOn).length||arr(v.transformations).length||v.inputVarId||v.functionApiName)issue(variablePath(sourceID),"variable-profile");
  if(v.definitionKind==="widgetOutput"&&widgetOwners.has(text(v.widgetId))){const producerOwner=widgetOwners.get(text(v.widgetId));if(producerOwner&&producerOwner!==context.owner){issue(path,"variable-scope");return "";}context={kind:producerOwner?"overlay":"page",owner:producerOwner};}
  if(context.kind==="overlay")issue(variablePath(sourceID),"native-overlay-lifetime",false);
  const opened=m.overlays.find(o=>o.openVariableId===sourceID);
  if(opened){const mapped=id("open",opened.id);ids.variables[sourceID]=mapped;document.variables![mapped]={scope:"page",type:"boolean",mode:"state",initial:false};if(v.definitionKind!=="static"||v.type!=="boolean"||v.staticValue!==false)issue(path,"overlay-open-profile");return mapped;}
  const previous=scope.get(sourceID);if(previous&&(previous.kind!==context.kind||previous.owner!==context.owner)){issue(path,"variable-scope");return "";}
  scope.set(sourceID,context);const mapped=id("variable",sourceID);ids.variables[sourceID]=mapped;
  if(document.variables![mapped])return mapped;
  // Reserve the identity before traversing dependencies; cycles remain diagnostics.
  document.variables![mapped]={scope:context.kind,owner:context.owner,type:"string",mode:"constant",initial:""};
  if(v.definitionKind==="static"&&["string","boolean"].includes(v.type)&&typeof v.staticValue===(v.type==="string"?"string":"boolean"))document.variables![mapped]={title:v.name,scope:context.kind,owner:context.owner,type:v.type,mode:"state",initial:v.staticValue};
  else if(v.definitionKind==="static"&&v.type==="array"&&Array.isArray(v.staticValue)&&v.staticValue.length<=64&&v.staticValue.every(item=>typeof item==="string")&&new Set(v.staticValue).size===v.staticValue.length)document.variables![mapped]={title:v.name,scope:context.kind,owner:context.owner,type:"string-set",mode:"state",initial:{kind:"string-set",values:v.staticValue}};
  else if(v.definitionKind==="static"&&v.type==="numeric"&&(v.staticValue===""||typeof v.staticValue==="number"&&Number.isFinite(v.staticValue)))document.variables![mapped]={title:v.name,scope:context.kind,owner:context.owner,type:"string",mode:"state",initial:String(v.staticValue)};
  else if(v.definitionKind==="widgetOutput"&&v.type==="array"&&v.widgetOutputKey==="selectedObjects"){document.variables![mapped]={title:v.name,scope:context.kind,owner:context.owner,type:"record-set",mode:"resource",source:{kind:"records",section:id("widget",text(v.widgetId))}};}
  else if(v.definitionKind==="widgetOutput"&&v.type==="object"&&v.widgetOutputKey==="activeObject"){
   const producer=text(v.widgetId);
   document.variables![mapped]={title:v.name,scope:context.kind,owner:context.owner,type:"record",mode:"resource",source:{kind:"record",section:id("widget",producer)}};
  }else if(v.definitionKind==="objectSetDefinition"&&v.type==="objectSet"){
   const set=obj(v.objectSet)?v.objectSet:undefined,external=text(set?.objectType||v.sourceObjectType),e=entity(external,`${variablePath(sourceID)}/objectSet/objectType`),query=id("query",sourceID),conditions:Api.PageQueryCondition[]= [];let search:Api.PageValue|undefined;
   if(set){safeKeys(set,["objectType","steps"],`${variablePath(sourceID)}/objectSet`);for(const [index,step] of arr(set.steps).entries()){
    const base=`${variablePath(sourceID)}/objectSet/steps/${index}`;
    if(!obj(step)||step.op!=="where"||!Array.isArray(step.clauses)){issue(base,"query-profile");continue;}safeKeys(step,["op","clauses"],base);
    for(const [n,clause] of step.clauses.entries()){
     const p=`${base}/clauses/${n}`;if(!obj(clause)){issue(p,"query-profile");continue;}safeKeys(clause,["id","property","op","value","varId"],p);
     const operators:Record<string,string>={is:"=",isNot:"!=",isGreaterThan:">",isLessThan:"<",isGreaterThanOrEqualTo:">=",isLessThanOrEqualTo:"<="};
     if(clause.varId){
      const input=variable(text(clause.varId),context,`${p}/varId`),type=document.variables![input]?.type,property=text(clause.property),op=text(clause.op);
      if(op==="fullTextSearch"&&property==="*"&&type==="string"){if(search)issue(p,"query-profile");search={variable:input};issue(p,"native-search-scope",false);continue;}
      const mapped=field(external,property,`${p}/property`),descriptor=e?.fields.find(f=>f.name===mapped),nativeOp=({isOneOf:"in",isNotOneOf:"not in",contains:"like",...operators} as Record<string,string>)[op];
      const decimal=["integer","decimal"].includes(descriptor?.type??"");
      if(!nativeOp||(["in","not in"].includes(nativeOp)?type!=="string-set"||!["text","choice"].includes(descriptor?.type??""):decimal?type!=="string":type!=="string"&&type!=="boolean")){issue(p,"query-profile");continue;}
      conditions.push({field:mapped,op:nativeOp,value:{variable:input},optional:true,...(decimal?{asDecimal:true}:{})});continue;
     }
     if(!operators[text(clause.op)]||!["string","number","boolean"].includes(typeof clause.value)||clause.value===""){issue(p,"query-profile");continue;}
     conditions.push({field:field(external,text(clause.property),`${p}/property`),op:operators[text(clause.op)]!,value:{literal:clause.value}});
    }
   }}
   queryObjects.set(sourceID,external);const binding=bindings.queries[sourceID];if(binding){
    const definition=target.definitions?.find(d=>d.ref.kind==="query"&&binding.ref.kind==="query"&&d.ref.app===binding.ref.app&&d.ref.name===binding.ref.name),query=definition?.queryVersions?.[binding.sourceVersion]??(definition?.version===binding.sourceVersion?definition.query:undefined);
    if(!query||query.object!==e?.type||query.by)issue(variablePath(sourceID),"query-binding");else issue(variablePath(sourceID),"explicit-query-binding",false);
   }
   document.queries![query]={owner:context.owner,object:{app:e?.app??"",kind:"object",name:e?.type??""},query:binding,conditions,search,limit:100};
   document.variables![mapped]={title:v.name,scope:context.kind,owner:context.owner,type:"object-set",mode:"resource",source:{kind:"plan",query}};
  }else issue(variablePath(sourceID),"variable-profile");
  return mapped;
 };
 const scopeOf=(owner?:string)=>({kind:owner?"overlay":"page",owner} as const);
 const metric=(sourceID:string,context:{kind:"page"|"overlay";owner?:string},path:string)=>{
  const v=vars.get(sourceID),p=variablePath(sourceID);if(!v||v.type!=="numeric"){issue(path,"metric-aggregate-profile");return {};}
  safeKeys(v,["id","name","type","description","definitionKind","recompute","inputVarId","dependsOn","transformations"],p);
  const input=text(v.inputVarId),steps=arr(v.transformations),step=steps[0],cardinality=v.definitionKind==="variableTransformation"&&obj(step)&&step.op==="cardinality",aggregate=v.definitionKind==="objectSetAggregation"&&obj(step)&&step.op==="aggregate";
  if(!input||steps.length!==1||!cardinality&&!aggregate||v.recompute!==undefined&&v.recompute!=="automatic"||arr(v.dependsOn).some(id=>id!==input))issue(p,"metric-aggregate-profile");
  if(obj(step)){safeKeys(step,cardinality?["op"]:["op","args"],`${p}/transformations/0`);if(aggregate){const args=step.args;if(!obj(args)||!text(args.by))issue(p,"metric-aggregate-profile");else safeKeys(args,["by"],`${p}/transformations/0/args`);}}
  const collection=variable(input,context,`${p}/inputVarId`),resource=document.variables![collection],external=queryObjects.get(input)||"",e=entity(external,path),q=resource?.source?.query,base=q?document.queries![q]:undefined,binding=own(bindings.aggregates??{},sourceID),measure=cardinality?"count":obj(binding)?text(binding.measure):"",conditions=obj(binding)?arr(binding.conditions):[];
  if(!base||resource?.type!=="object-set"||!measure||cardinality&&obj(binding)&&(binding.measure!=="count"||conditions.length))issue(p,"metric-aggregate-binding");
  const [op,name]=measure.split(":"),field=e?.fields.find(f=>f.name===name);if(measure!=="count"&&(!["sum","avg","min","max"].includes(op??"")||measure.split(":").length!==2||!["integer","decimal","money"].includes(field?.type??"")))issue(p,"metric-aggregate-binding");
  if(obj(binding))safeKeys(binding,["measure","conditions"],p);if(conditions.length>1||obj(binding)&&binding.conditions!==undefined&&!Array.isArray(binding.conditions))issue(p,"metric-aggregate-binding");
  const predicates=conditions.flatMap((raw,index)=>{const at=`${p}/nativeConditions/${index}`;if(!obj(raw)||!obj(raw.value)||Object.keys(raw.value).length!==1||!("literal" in raw.value)||raw.op!=="="||!e?.fields.some(f=>f.name===raw.field)||!["string","number","boolean"].includes(typeof raw.value.literal)||typeof raw.value.literal==="number"&&!Number.isFinite(raw.value.literal)){issue(at,"metric-aggregate-binding");return [];}const field=e.fields.find(f=>f.name===raw.field)!,type=field.type,literal=raw.value.literal;if(!["text","choice","boolean","integer","decimal"].includes(type)||typeof literal!==(["integer","decimal"].includes(type)?"number":type==="boolean"?"boolean":"string"))issue(at,"metric-aggregate-binding");safeKeys(raw,["field","op","value"],at);return [raw as unknown as Api.PageQueryCondition];});
  if(!base)return {};let output=collection;if(predicates.length){const query=id("metric_query",sourceID),value=id("metric_set",sourceID);document.queries![query]={...base,conditions:[...(base.conditions??[]),...predicates]};document.variables![value]={scope:context.kind,owner:context.owner,type:"object-set",mode:"resource",source:{kind:"plan",query}};output=value;}
  issue(path,"native-metric-aggregate",false);return {collectionVariable:output,measure,object:e?.type===target.object?undefined:e?.type};
 };
 const widget=(sourceID:string,owner?:string)=>{
  const w=m.widgets[sourceID];if(!w){issue(`/widgets/${pointer(sourceID)}`,"widget-reference");return "";}
  if(usedWidgets.has(sourceID)){issue(`/widgets/${pointer(sourceID)}`,"multiple-parents");return id("leaf",sourceID);}usedWidgets.add(sourceID);
  const path=`/widgets/${pointer(sourceID)}`,mapping=workshopMapping(w.type),sectionID=id("widget",sourceID),leaf=id("leaf",sourceID),config=w.config,context=scopeOf(owner);ids.widgets[sourceID]=sectionID;ids.nodes[sourceID]=leaf;
  if(mapping?.status!=="profile"){issue(`${path}/type`,mapping?"widget-profile":"unknown-widget");return leaf;}
  const section:AuthoringSection={id:sectionID,widget:mapping.target!,configVersion:1,title:w.name};sections.push(section);document.nodes[leaf]={kind:"widget",section:sectionID};
  safeKeys(w,["id","type","name","config","events","height","flex","style","emptyMessage","readOnly","hidden","mountBehavior","unmountBehavior"],path);
  presentation(w,path,{mountBehavior:"normal",unmountBehavior:"normal",hidden:false,readOnly:false});if(w.style!==undefined)issue(`${path}/style`,"presentation-profile");if(w.emptyMessage!==undefined)issue(`${path}/emptyMessage`,"presentation-profile");
  if(w.height!==undefined)document.nodes[leaf]!.size={height:Number(w.height)};if(w.flex!==undefined)document.nodes[leaf]!.size={...document.nodes[leaf]!.size,weight:Number(w.flex)};
  if(w.events!==undefined&&!Array.isArray(w.events))issue(`${path}/events`,"event-profile");
  if(arr(w.events).length){const events=arr(w.events),event=events[0],eventPath=`${path}/events/0`;
   if(w.type!=="ObjectTable"||events.length!==1||!obj(event)||event.trigger!=="onSelect"||arr(event.actions).length!==1)issue(`${path}/events`,"event-profile");
   else{safeKeys(event,["id","trigger","actions"],eventPath);const action=arr(event.actions)[0];if(!obj(action)||action.kind!=="setVariable")issue(`${eventPath}/actions/0`,"event-profile");else{safeKeys(action,["kind","variableId","valueExpr"],`${eventPath}/actions/0`);let literal:unknown;try{literal=JSON.parse(text(action.valueExpr));}catch{issue(`${eventPath}/actions/0/valueExpr`,"expression-profile");}const targetID=variable(text(action.variableId),context,`${eventPath}/actions/0/variableId`),v=document.variables![targetID];if(!["string","boolean"].includes(typeof literal)||v?.mode!=="state"||v.type!==typeof literal||m.overlays.some(o=>o.openVariableId===action.variableId))issue(`${eventPath}/actions/0`,"event-profile");if(Number(target.profile.split(".").at(-1))<33)issue(eventPath,"event-profile");document.events!.push({source:sectionID,event:"select",target:targetID,value:literal});}}
  }
  if(w.type==="Markdown"){safeKeys(config,["text"],`${path}/config`);section.text=text(config.text);}
  if(w.type==="TextInput"||w.type==="NumericInput"){if(w.type==="NumericInput")issue(`${path}/config`,"native-number-syntax",false);safeKeys(config,["variableId","label","placeholder"],`${path}/config`);document.nodes[leaf]!.valueVariable=variable(text(config.variableId),context,`${path}/config/variableId`);if(document.variables![document.nodes[leaf]!.valueVariable!]?.type!=="string")issue(`${path}/config/variableId`,"input-profile");if(config.label!==undefined)section.title=text(config.label);if(config.placeholder!==undefined)issue(`${path}/config/placeholder`,"native-presentation",false);}
  if(w.type==="MetricCard"){
   safeKeys(config,["label","variableId","variant","prefix","suffix","color","formatter"],`${path}/config`);Object.assign(section,metric(text(config.variableId),context,`${path}/config/variableId`));section.title=text(config.label)||w.name;const limits=pageUIManifest.runtime.metricPresentation,formatter=config.formatter===undefined?"number":config.formatter==="currencyShort"?"short":"",variant=text(config.variant)||"card",tone=text(config.color)||"neutral";
   if(config.label!==undefined&&typeof config.label!=="string"||config.variant!==undefined&&typeof config.variant!=="string"||config.color!==undefined&&typeof config.color!=="string"||!formatter||!(limits.variants as readonly string[]).includes(variant)||!(limits.tones as readonly string[]).includes(tone)||config.prefix!==undefined&&typeof config.prefix!=="string"||config.suffix!==undefined&&typeof config.suffix!=="string"||new TextEncoder().encode(text(config.prefix)).length>limits.maxUnitBytes||new TextEncoder().encode(text(config.suffix)).length>limits.maxUnitBytes||Number(target.profile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))issue(`${path}/config`,"metric-presentation-profile");
   if(formatter==="short"&&target.entities.find(e=>e.type===(section.object||target.object))?.fields.find(f=>f.name===section.measure?.split(":")[1])?.type==="money")issue(`${path}/config`,"metric-presentation-profile");
   section.metricPresentation={prefix:text(config.prefix)||undefined,suffix:text(config.suffix)||undefined,formatter,variant,tone};
  }
  if(w.type==="FilterList"){
   safeKeys(config,["objectSetVarId","outputFilterVarId","facets","searchVarId"],`${path}/config`);
   const source=text(config.objectSetVarId);section.collectionVariable=variable(source,context,`${path}/config/objectSetVarId`);const external=queryObjects.get(source)??"",e=entity(external,`${path}/config/objectSetVarId`);section.object=e?.type===target.object?undefined:e?.type;
   section.facets=arr(config.facets).flatMap((value,index)=>{const p=`${path}/config/facets/${index}`;if(!obj(value)){issue(p,"facet-profile");return [];}safeKeys(value,["property","label","type","varId"],p);const fieldID=field(external,text(value.property),`${p}/property`),input=variable(text(value.varId),context,`${p}/varId`),type=document.variables![input]?.type,kind=text(value.type);if(!["checkbox","histogram","search"].includes(kind)||type!==(kind==="search"?"string":"string-set")||!["text","choice"].includes(e?.fields.find(f=>f.name===fieldID)?.type??""))issue(p,"facet-profile");if(value.label!==undefined)issue(`${p}/label`,"native-presentation",false);return [{field:fieldID,variable:input,kind}];});
   if(config.searchVarId)section.filterSearchVariable=variable(text(config.searchVarId),context,`${path}/config/searchVarId`);
   if(config.outputFilterVarId){const output=text(config.outputFilterVarId),definition=vars.get(output);if(definition?.definitionKind!=="objectSetDefinition")issue(`${path}/config/outputFilterVarId`,"query-profile");else variable(output,context,`${path}/config/outputFilterVarId`);}
  }
  if(w.type==="ObjectTable"){
   safeKeys(config,["objectSetVarId","activeVarId","selectedVarId","columns","density","enableSelection","selectionMode","showToolbar","showSearch","enableInlineEdit","titleTemplate"],`${path}/config`);
   const source=text(config.objectSetVarId);section.collectionVariable=variable(source,context,`${path}/config/objectSetVarId`);const external=queryObjects.get(source)||"",e=entity(external,`${path}/config/objectSetVarId`);section.object=e?.type===target.object?undefined:e?.type;
   const limits=pageUIManifest.runtime.tablePresentation,seenColumns=new Set<string>();
   if(!Array.isArray(config.columns)||arr(config.columns).length>limits.maxColumns)issue(`${path}/config/columns`,"table-presentation-profile");
   section.tableColumns=arr(config.columns).flatMap((c,n)=>{const p=`${path}/config/columns/${n}`;if(!obj(c)){issue(p,"field-binding");return [];}safeKeys(c,["key","label","width","formatter"],p);const name=field(external,text(c.key),`${p}/key`),type=e?.fields.find(f=>f.name===name)?.type,format=c.formatter===undefined?undefined:({none:"text",numeric:"numeric",date:"date",status:"badge",priority:"badge"} as Record<string,string>)[text(c.formatter)],definition=limits.formatters.find(f=>f.id===format);
    if(seenColumns.has(name))issue(p,"table-presentation-profile");seenColumns.add(name);
    if(c.label!==undefined&&(typeof c.label!=="string"||new TextEncoder().encode(c.label).length>limits.maxTitleBytes)||c.width!==undefined&&(typeof c.width!=="number"||!Number.isInteger(c.width)||c.width<limits.minWidth||c.width>limits.maxWidth)||c.formatter!==undefined&&(!format||name==="id"&&format!=="text"||name!=="id"&&!(definition?.fieldTypes as readonly string[]|undefined)?.includes(type??"")))issue(p,"table-presentation-profile");
    if(format==="badge")issue(`${p}/formatter`,"native-badge-tones",false);
    return [{field:name,title:typeof c.label==="string"?c.label:undefined,width:typeof c.width==="number"?c.width:undefined,formatter:format}];});
   section.fields=section.tableColumns.map(c=>c.field).filter(name=>{if(name==="id")issue(`${path}/config/columns`,"native-system-id",false);return name!=="id";});
   if(config.showSearch!==undefined){if(typeof config.showSearch!=="boolean")issue(`${path}/config/showSearch`,"table-presentation-profile");else section.showSearch=config.showSearch;}
   if(Number(target.profile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))issue(`${path}/config/columns`,"table-presentation-profile");
   if(config.selectionMode!==undefined&&!["single","multiple"].includes(text(config.selectionMode))||config.enableSelection===false)issue(`${path}/config`,"table-interaction-profile");
   if(config.selectionMode==="multiple"&&config.selectedVarId){const source=text(config.selectedVarId),definition=vars.get(source);section.selectionSetVariable=variable(source,context,`${path}/config/selectedVarId`);if(definition?.type!=="array"||definition.definitionKind!=="widgetOutput"||definition.widgetId!==sourceID||definition.widgetOutputKey!=="selectedObjects")issue(`${path}/config/selectedVarId`,"selection-producer");}else if(config.selectionMode==="multiple"||config.selectedVarId)issue(`${path}/config/selectedVarId`,"selection-profile");
   if(config.enableInlineEdit===true){const edit=bindings.edits?.[sourceID],action=target.actions.find(a=>a.schema===edit?.action&&a.schema===`${e?.type}.edit`&&a.target===e?.type&&!a.new&&!a.needsApproval&&!a.payload?.some(f=>f.required));if(!edit||!action||!edit.fields.length||edit.fields.length>16||new Set(edit.fields).size!==edit.fields.length||edit.fields.some(name=>!section.fields?.includes(name)||!action.payload?.some(f=>f.name===name)))issue(`${path}/config/enableInlineEdit`,"edit-binding");else section.inlineEdit={action:action.schema,fields:[...edit.fields]};}
   for(const key of ["density","showToolbar","titleTemplate"])if(config[key]!==undefined)issue(`${path}/config/${key}`,"native-presentation",false);
   if(config.activeVarId){const active=vars.get(text(config.activeVarId));if(active?.type!=="object"||active.definitionKind!=="widgetOutput"||active.widgetId!==sourceID||active.widgetOutputKey!=="activeObject")issue(`${path}/config/activeVarId`,"selection-producer");recordObjects.set(text(config.activeVarId),external);if(e){section.selection=id("selection",sourceID);draft.selections.push({name:section.selection,object:{app:e.app,kind:"object",name:e.type}});}}
  }
  if(w.type==="PropertyList"||w.type==="InlineAction"||w.type==="ObjectView"||w.type==="Links"||w.type==="StatusTracker"){
   const source=text(config.objectVarId);section.recordVariable=variable(source,context,`${path}/config/objectVarId`);const v=vars.get(source),producer=m.widgets[text(v?.widgetId)],setID=text(producer?.config.objectSetVarId),producerOwner=widgetOwners.get(text(v?.widgetId));if(producer&&producer.type==="ObjectTable")variable(setID,scopeOf(producerOwner),`${path}/config/objectVarId`);const external=recordObjects.get(source)||queryObjects.get(setID)||"",e=entity(external,`${path}/config/objectVarId`);section.object=e?.type===target.object?undefined:e?.type;
if(w.type==="PropertyList"){safeKeys(config,["objectVarId","properties","columns","hideNull","inlineEdit","style"],`${path}/config`);section.fields=arr(config.properties).map((p,n)=>field(external,text(p),`${path}/config/properties/${n}`)).filter(name=>{if(name==="id")issue(`${path}/config/properties`,"native-system-id",false);return name!=="id";});if(config.inlineEdit===true)issue(`${path}/config`,"detail-interaction-profile");const limits=pageUIManifest.runtime.detailPresentation,columns=config.columns??2;if(typeof columns!=="number"||!Number.isInteger(columns)||columns<1||columns>limits.maxColumns||config.hideNull!==undefined&&typeof config.hideNull!=="boolean"||Number(target.profile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))issue(`${path}/config`,"detail-interaction-profile");section.detailPresentation={columns:typeof columns==="number"?columns:2,hideNull:config.hideNull===true};for(const key of ["style"])if(config[key]!==undefined)issue(`${path}/config/${key}`,"native-presentation",false);}
   else if(w.type==="StatusTracker"){
    safeKeys(config,["objectVarId","activeProp","stages"],`${path}/config`);const limits=pageUIManifest.runtime.statusTracker,l=e?.lifecycle,mapped=own(bindings.states??{},sourceID),stages=arr(config.stages),status=field(external,text(config.activeProp),`${path}/config/activeProp`);
    if(!l||status!==l.field||!Array.isArray(config.stages)||!stages.length||stages.length>limits.maxStages||stages.some(v=>!text(v))||new Set(stages).size!==stages.length||Number(target.profile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))issue(`${path}/config`,"status-tracker-profile");
    const native=stages.map((value,index)=>{const state=obj(mapped)?text(own(mapped,text(value))):"";if(!state||!l?.states.some(s=>s.name===state))issue(`${path}/config/stages/${index}`,"status-tracker-binding");return state;});if(new Set(native).size!==native.length)issue(`${path}/config/stages`,"status-tracker-binding");section.statusTracker={field:status,stages:native};issue(`${path}/config`,"native-lifecycle-status",false);
   }
   else if(w.type==="Links"){
    safeKeys(config,["objectVarId","linkTypes","linkTypeApiNames","outputVarId"],`${path}/config`);const limits=pageUIManifest.runtime.recordLinks,legacy=config.linkTypes!==undefined,groups=arr(legacy?config.linkTypes:config.linkTypeApiNames),mapped=own(bindings.links??{},sourceID);
    if(config.outputVarId||legacy&&config.linkTypeApiNames!==undefined||!Array.isArray(legacy?config.linkTypes:config.linkTypeApiNames)||!groups.length||groups.length>limits.maxGroups||Number(target.profile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))issue(`${path}/config`,"record-links-profile");
    if(!Array.isArray(mapped)||mapped.length!==groups.length)issue(`${path}/config`,"record-links-binding");
    const seen=new Set<string>();section.recordLinks=groups.flatMap((raw,index)=>{const p=`${path}/config/${legacy?"linkTypes":"linkTypeApiNames"}/${index}`,binding=Array.isArray(mapped)?mapped[index] as Api.PageRecordLink|undefined:undefined;
     if(legacy){if(!obj(raw)||!text(raw.target)||!text(raw.linkField)||raw.label!==undefined&&typeof raw.label!=="string")issue(p,"record-links-profile");else safeKeys(raw,["label","target","linkField"],p);}else if(!text(raw))issue(p,"record-links-profile");
     const related=target.entities.find(candidate=>candidate.type===binding?.object?.name&&candidate.app===binding?.object?.app),reference=related?.fields.find(f=>f.name===binding?.field&&f.type==="reference"&&f.ref===e?.type&&!!f.inverse),key=`${binding?.object?.name}/${binding?.field}`,title=legacy&&obj(raw)?text(raw.label):undefined;
     if(!binding||binding.object?.kind!=="object"||!reference||seen.has(key)||new TextEncoder().encode(title??"").length>limits.maxTitleBytes){issue(p,"record-links-binding");return [];}seen.add(key);return [{object:binding.object,field:binding.field,title:title||undefined}];
    });issue(`${path}/config`,"native-related-navigation",false);
   }
   else if(w.type==="ObjectView"){safeKeys(config,["objectVarId","formFactor","viewMode","tabs"],`${path}/config`);if(config.formFactor!==undefined&&config.formFactor!=="panel"||config.viewMode!==undefined&&config.viewMode!=="configured")issue(`${path}/config`,"record-view-profile");const names:Record<string,string>={Overview:"overview",Properties:"properties",Links:"links",History:"history"},tabs=config.tabs===undefined?[...pageUIManifest.runtime.recordView.tabs]:arr(config.tabs).map(t=>names[text(t)]??"");if(!tabs.length||tabs.length>4||new Set(tabs).size!==tabs.length||tabs.some(t=>!t)||Number(target.profile.split(".").at(-1))<Number(pageUIManifest.runtime.recordView.requiredUIProfile.split(".").at(-1)))issue(`${path}/config`,"record-view-profile");section.recordView={tabs};section.fields=e?.fields.map(f=>f.name)??[];section.actions=Object.values(bindings.actions).filter(schema=>target.actions.some(a=>a.schema===schema&&a.target===e?.type&&!a.new));issue(`${path}/config`,"native-record-work",false);}
   else{safeKeys(config,["actionId","objectVarId","mode"],`${path}/config`);const schema=text(own(bindings.actions,text(config.actionId))),action=target.actions.find(a=>a.schema===schema&&a.target===e?.type&&!a.new);if(!action)issue(`${path}/config/actionId`,"action-binding");section.actions=action?[action.schema]:[];if(config.mode!==undefined&&config.mode!=="form")issue(`${path}/config/mode`,"action-profile");}
  }
  if(w.type==="ButtonGroup"){safeKeys(config,["buttons"],`${path}/config`);const limits=pageUIManifest.runtime.buttonGroup,buttons=arr(config.buttons);if(!buttons.length||buttons.length>limits.maxButtons||Number(target.profile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))issue(`${path}/config/buttons`,"button-group-profile");section.buttons=buttons.flatMap((raw,index)=>{const p=`${path}/config/buttons/${index}`;if(!obj(raw)){issue(p,"button-group-profile");return [];}safeKeys(raw,["label","variant","icon","eventActions"],p);const variant=raw.variant===undefined?"default":({primary:"primary",secondary:"default",minimal:"ghost",danger:"danger"} as Record<string,string>)[text(raw.variant)],icon=raw.icon===undefined?undefined:text(raw.icon);if(!text(raw.label)||new TextEncoder().encode(text(raw.label)).length>limits.maxTitleBytes||!variant||icon!==undefined&&!(limits.icons as readonly string[]).includes(icon))issue(p,"button-group-profile");const control=id("control",`${sourceID}/${index}`),actions=arr(raw.eventActions),a=actions[0];if(actions.length!==1||!obj(a))issue(`${p}/eventActions`,"event-profile");else if(a.kind==="openOverlay"||a.kind==="closeOverlay"){safeKeys(a,["kind","overlayId"],`${p}/eventActions/0`);const overlay=text(a.overlayId);if(!m.overlays.some(o=>o.id===overlay))issue(`${p}/eventActions/0`,"overlay-reference");else document.events!.push({source:sectionID,control,event:"click",target:id("open",overlay),value:a.kind==="openOverlay"});}else if(a.kind==="setVariable"){safeKeys(a,["kind","variableId","valueExpr"],`${p}/eventActions/0`);let literal:unknown;try{literal=JSON.parse(text(a.valueExpr));}catch{issue(`${p}/eventActions/0/valueExpr`,"expression-profile");}if(!["string","boolean"].includes(typeof literal))issue(`${p}/eventActions/0/valueExpr`,"expression-profile");document.events!.push({source:sectionID,control,event:"click",target:variable(text(a.variableId),context,`${p}/eventActions/0/variableId`),value:literal});}else issue(`${p}/eventActions/0`,"event-profile");return [{id:control,title:text(raw.label),variant,icon}];});}
  if(w.type==="SingleButton"){safeKeys(config,["label","variant","eventActions"],`${path}/config`);section.title=text(config.label)||w.name;if(config.variant!==undefined)issue(`${path}/config/variant`,"native-presentation",false);const actions=arr(config.eventActions);if(actions.length!==1||!obj(actions[0]))issue(`${path}/config/eventActions`,"event-profile");else{
   const event=actions[0];if(event.kind==="openOverlay"||event.kind==="closeOverlay"){safeKeys(event,["kind","overlayId"],`${path}/config/eventActions/0`);const overlayID=text(event.overlayId),o=m.overlays.find(o=>o.id===overlayID);if(!o)issue(`${path}/config/eventActions/0`,"overlay-reference");else document.events!.push({source:sectionID,event:"click",target:id("open",overlayID),value:event.kind==="openOverlay"});}
   else if(event.kind==="setVariable"){safeKeys(event,["kind","variableId","valueExpr"],`${path}/config/eventActions/0`);let literal:unknown;try{literal=JSON.parse(text(event.valueExpr));}catch{issue(`${path}/config/eventActions/0/valueExpr`,"expression-profile");}if(!["string","boolean"].includes(typeof literal))issue(`${path}/config/eventActions/0/valueExpr`,"expression-profile");document.events!.push({source:sectionID,event:"click",target:variable(text(event.variableId),context,`${path}/config/eventActions/0/variableId`),value:literal});}
   else issue(`${path}/config/eventActions/0`,"event-profile");
  }}
  if(["table","detail"].includes(section.widget)&&!section.fields?.length)issue(`${path}/config`,"field-binding");
  return leaf;
 };
 const visit=(sourceID:string,owner?:string,ancestors=new Set<string>()):string=>{
  const s=m.sections[sourceID],path=`/sections/${pointer(sourceID)}`;if(!s||ancestors.has(sourceID)){issue(path,s?"layout-cycle":"section-reference");return "";}if(seenSections.has(sourceID)){issue(path,"multiple-parents");return id("node",sourceID);}seenSections.add(sourceID);
  const node=id("node",sourceID);ids.nodes[sourceID]=node;safeKeys(s,["id","name","layout","children","showHeader","title","description","direction","width","height","minWidth","maxWidth","padding","gap","background","border","elevation","divider","scroll","collapsible","defaultCollapsed","visibleVariableId","tabs","loopVariableId","loopItemVarId","flex","auto","align","justify","wrap","stackBelow","noStack"],path);
  const next=new Set(ancestors);next.add(sourceID);if(!["rows","columns","tabs","flow","toolbar"].includes(s.layout)){issue(`${path}/layout`,"layout-profile");return node;}
  if(s.loopVariableId||s.loopItemVarId||s.layout!=="tabs"&&arr(s.tabs).length)issue(path,"layout-profile");
  document.nodes[node]={kind:s.layout,title:text(s.title)||s.name,children:s.children.map(child=>child.kind==="widget"?widget(child.id,owner):visit(child.id,owner,next))};const n=document.nodes[node]!;
  if(s.gap!==undefined)n.gap=Number(s.gap);if(s.justify!==undefined&&["flow","toolbar"].includes(s.layout))n.align=text(s.justify);
  for(const key of ["width","height","minWidth","maxWidth"])if(s[key]!==undefined){const value=s[key];if(typeof value==="string"&&/^\d+px$/.test(value))n.size={...n.size,[key]:Number.parseInt(value)};else issue(`${path}/${pointer(key)}`,"dimension-profile");}if(s.flex!==undefined)n.size={...n.size,weight:Number(s.flex)};
  if(s.visibleVariableId)n.visibleWhen=variable(text(s.visibleVariableId),scopeOf(owner),`${path}/visibleVariableId`);
  presentation(s,path,{showHeader:false,padding:8,background:"default",border:false,elevation:0,divider:false,scroll:false,collapsible:false,defaultCollapsed:false,auto:false,align:"stretch",wrap:true,noStack:false,stackBelow:640});if(s.description)issue(`${path}/description`,"section-header-profile");
  if(s.direction!==undefined&&s.direction!==(s.layout==="columns"?"horizontal":"vertical"))issue(`${path}/direction`,"layout-profile");
  if(s.layout==="tabs"){const tabs=arr(s.tabs);if(tabs.length!==s.children.length)issue(`${path}/tabs`,"tabs-profile");tabs.forEach((tab,i)=>{if(!obj(tab)||tab.childIndex!==i)issue(`${path}/tabs/${i}`,"tabs-profile");else if(n.children?.[i]&&document.nodes[n.children[i]!])document.nodes[n.children[i]!]!.title=text(tab.label);});const active=id("tab",sourceID);n.activeVariable=active;document.variables![active]={scope:owner?"overlay":"page",owner,type:"string",mode:"state",initial:n.children?.[0]??""};}
  return node;
 };
 // Allocate every reachable producer before interpreting selection variables.
 const collect=(root:string,owner?:string,seen=new Set<string>())=>{if(seen.has(root))return;seen.add(root);for(const child of m.sections[root]?.children??[]){if(child.kind==="widget"){usedWidgets.add(child.id);widgetOwners.set(child.id,owner);}else collect(child.id,owner,seen);}};collect(page.rootSectionId);m.overlays.forEach(o=>collect(o.rootSectionId,id("overlay",o.id)));m.unusedWidgetIds.forEach(w=>{usedWidgets.add(w);widgetOwners.set(w,undefined);});const expectedWidgets=new Set(usedWidgets);usedWidgets.clear();
 document.root=visit(page.rootSectionId);
 for(const o of m.overlays){const mapped=id("overlay",o.id),open=id("open",o.id);ids.overlays[o.id]=mapped;safeKeys(o,["id","name","kind","rootSectionId","side","size","customWidth","backdrop","closeOnBackdrop","closeOnEsc","openVariableId","onCloseActions","title"],overlayPath(o.id));presentation(o,overlayPath(o.id),{side:"right",size:"medium",backdrop:true,closeOnBackdrop:true,closeOnEsc:true});if(o.customWidth!==undefined||arr(o.onCloseActions).length)issue(overlayPath(o.id),"overlay-profile");document.variables![open]={scope:"page",type:"boolean",mode:"state",initial:false};document.overlays![mapped]={root:visit(o.rootSectionId,mapped),kind:o.kind,title:text(o.title)||o.name,openVariable:open};if(o.openVariableId){ids.variables[o.openVariableId]=open;const v=vars.get(o.openVariableId);if(v?.definitionKind!=="static"||v.type!=="boolean"||v.staticValue!==false)issue(`${overlayPath(o.id)}/openVariableId`,"overlay-open-profile");}}
 for(const sourceID of m.unusedWidgetIds){if(usedWidgets.has(sourceID)){issue(`/unusedWidgetIds/${m.unusedWidgetIds.indexOf(sourceID)}`,"multiple-parents");continue;}const leaf=widget(sourceID);document.unusedWidgets=[...(document.unusedWidgets??[]),{node:leaf,parent:document.root}];issue(`/unusedWidgetIds/${m.unusedWidgetIds.indexOf(sourceID)}`,"unused-page-placement",false);}
 // Check selection producer after the full tree traversal, so order does not grant meaning.
 for(const [sourceID,mapped] of Object.entries(ids.variables)){const v=vars.get(sourceID);if(v?.definitionKind==="widgetOutput"&&!expectedWidgets.has(text(v.widgetId)))issue(`${variablePath(sourceID)}/widgetId`,"selection-producer");const value=document.variables![mapped];if((value?.source?.kind==="record"||value?.source?.kind==="records")&&!sections.some(s=>s.id===value.source!.section&&s.widget==="table"))issue(`${variablePath(sourceID)}/widgetId`,"selection-producer");}
 if(Object.keys(m.widgets).some(w=>!usedWidgets.has(w))||m.variables.some(v=>!ids.variables[v.id]))issue("/","unreferenced-content-retained",false);
 if(Object.keys(document.queries!).length>8||Object.values(document.queries!).reduce((s,q)=>s+q.limit,0)>512||Object.keys(document.variables!).length>64||sections.length>128||Object.keys(document.nodes).length>256)issue("/","target-budget");
 if(!diagnostics.some(d=>d.blocking))report.draft=draft;
 return report;
}
