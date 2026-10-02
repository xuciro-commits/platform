import type {Api} from "@platform/kernel";
import type {PageDraft,AuthoringSection} from "../page-editor/draft";
import {workshopMapping,workshopMigrationCatalog} from "./catalog";

type Obj=Record<string,unknown>;
type SourceWidget=Obj&{id:string;type:string;name:string;config:Obj};
type SourceSection=Obj&{id:string;name:string;layout:string;children:{kind:string;id:string}[]};
type SourceVariable=Obj&{id:string;name:string;type:string;definitionKind:string};
export type SourceModule=Obj&{id:string;name:string;description:string;pages:{id:string;name:string;rootSectionId:string}[];sections:Record<string,SourceSection>;widgets:Record<string,SourceWidget>;variables:SourceVariable[];overlays:(Obj&{id:string;name:string;kind:string;rootSectionId:string;openVariableId?:string})[];unusedWidgetIds:string[]};
export type ImportDiagnostic={path:string;code:string;blocking:boolean};
export type ImportBindings={edits?:Record<string,{action:string;fields:string[]}>;objects:Record<string,string>;fields:Record<string,Record<string,string>>;actions:Record<string,string>;queries:Record<string,Api.AssetBinding|undefined>};
export type ImportEntity={type:string;app:string;fields:{name:string;type:string}[]};
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
 const widget=(sourceID:string,owner?:string)=>{
  const w=m.widgets[sourceID];if(!w){issue(`/widgets/${pointer(sourceID)}`,"widget-reference");return "";}
  if(usedWidgets.has(sourceID)){issue(`/widgets/${pointer(sourceID)}`,"multiple-parents");return id("leaf",sourceID);}usedWidgets.add(sourceID);
  const path=`/widgets/${pointer(sourceID)}`,mapping=workshopMapping(w.type),sectionID=id("widget",sourceID),leaf=id("leaf",sourceID),config=w.config,context=scopeOf(owner);ids.widgets[sourceID]=sectionID;ids.nodes[sourceID]=leaf;
  if(mapping?.status!=="profile"){issue(`${path}/type`,mapping?"widget-profile":"unknown-widget");return leaf;}
  const section:AuthoringSection={id:sectionID,widget:mapping.target!,configVersion:1,title:w.name};sections.push(section);document.nodes[leaf]={kind:"widget",section:sectionID};
  safeKeys(w,["id","type","name","config","events","height","flex","style","emptyMessage","readOnly","hidden","mountBehavior","unmountBehavior"],path);
  presentation(w,path,{mountBehavior:"normal",unmountBehavior:"normal",hidden:false,readOnly:false});if(w.style!==undefined)issue(`${path}/style`,"presentation-profile");if(w.emptyMessage!==undefined)issue(`${path}/emptyMessage`,"presentation-profile");
  if(w.height!==undefined)document.nodes[leaf]!.size={height:Number(w.height)};if(w.flex!==undefined)document.nodes[leaf]!.size={...document.nodes[leaf]!.size,weight:Number(w.flex)};
  if(arr(w.events).length)issue(`${path}/events`,"event-profile");
  if(w.type==="Markdown"){safeKeys(config,["text"],`${path}/config`);section.text=text(config.text);}
  if(w.type==="TextInput"||w.type==="NumericInput"){if(w.type==="NumericInput")issue(`${path}/config`,"native-number-syntax",false);safeKeys(config,["variableId","label","placeholder"],`${path}/config`);document.nodes[leaf]!.valueVariable=variable(text(config.variableId),context,`${path}/config/variableId`);if(document.variables![document.nodes[leaf]!.valueVariable!]?.type!=="string")issue(`${path}/config/variableId`,"input-profile");if(config.label!==undefined)section.title=text(config.label);if(config.placeholder!==undefined)issue(`${path}/config/placeholder`,"native-presentation",false);}
  if(w.type==="FilterList"){
   safeKeys(config,["objectSetVarId","outputFilterVarId","facets","searchVarId"],`${path}/config`);
   const source=text(config.objectSetVarId);section.collectionVariable=variable(source,context,`${path}/config/objectSetVarId`);const external=queryObjects.get(source)??"",e=entity(external,`${path}/config/objectSetVarId`);section.object=e?.type===target.object?undefined:e?.type;
   section.facets=arr(config.facets).flatMap((value,index)=>{const p=`${path}/config/facets/${index}`;if(!obj(value)){issue(p,"facet-profile");return [];}safeKeys(value,["property","label","type","varId"],p);const fieldID=field(external,text(value.property),`${p}/property`),input=variable(text(value.varId),context,`${p}/varId`),type=document.variables![input]?.type,kind=text(value.type);if(!["checkbox","histogram","search"].includes(kind)||type!==(kind==="search"?"string":"string-set")||!["text","choice"].includes(e?.fields.find(f=>f.name===fieldID)?.type??""))issue(p,"facet-profile");if(value.label!==undefined)issue(`${p}/label`,"native-presentation",false);return [{field:fieldID,variable:input,kind}];});
   if(config.searchVarId)section.filterSearchVariable=variable(text(config.searchVarId),context,`${path}/config/searchVarId`);
   if(config.outputFilterVarId){const output=text(config.outputFilterVarId),definition=vars.get(output);if(definition?.definitionKind!=="objectSetDefinition")issue(`${path}/config/outputFilterVarId`,"query-profile");else variable(output,context,`${path}/config/outputFilterVarId`);}
  }
  if(w.type==="ObjectTable"){
   safeKeys(config,["objectSetVarId","activeVarId","columns","density","enableSelection","selectionMode","showToolbar","enableInlineEdit","titleTemplate"],`${path}/config`);
   const source=text(config.objectSetVarId);section.collectionVariable=variable(source,context,`${path}/config/objectSetVarId`);const external=queryObjects.get(source)||"",e=entity(external,`${path}/config/objectSetVarId`);section.object=e?.type===target.object?undefined:e?.type;
   section.fields=arr(config.columns).map((c,n)=>{if(!obj(c)){issue(`${path}/config/columns/${n}`,"field-binding");return "";}safeKeys(c,["key","label","width","formatter"],`${path}/config/columns/${n}`);if(c.width!==undefined||c.label!==undefined)issue(`${path}/config/columns/${n}`,"native-presentation",false);if(c.formatter!==undefined&&c.formatter!=="none")issue(`${path}/config/columns/${n}/formatter`,"presentation-profile");return field(external,text(c.key),`${path}/config/columns/${n}/key`);}).filter(name=>{if(name==="id")issue(`${path}/config/columns`,"native-system-id",false);return name!=="id";});
   if(config.selectionMode!==undefined&&config.selectionMode!=="single"||config.enableSelection===false)issue(`${path}/config`,"table-interaction-profile");
   if(config.enableInlineEdit===true){const edit=bindings.edits?.[sourceID],action=target.actions.find(a=>a.schema===edit?.action&&a.schema===`${e?.type}.edit`&&a.target===e?.type&&!a.new&&!a.needsApproval&&!a.payload?.some(f=>f.required));if(!edit||!action||!edit.fields.length||edit.fields.length>16||new Set(edit.fields).size!==edit.fields.length||edit.fields.some(name=>!section.fields?.includes(name)||!action.payload?.some(f=>f.name===name)))issue(`${path}/config/enableInlineEdit`,"edit-binding");else section.inlineEdit={action:action.schema,fields:[...edit.fields]};}
   for(const key of ["density","showToolbar","titleTemplate"])if(config[key]!==undefined)issue(`${path}/config/${key}`,"native-presentation",false);
   if(config.activeVarId){const active=vars.get(text(config.activeVarId));if(active?.type!=="object"||active.definitionKind!=="widgetOutput"||active.widgetId!==sourceID||active.widgetOutputKey!=="activeObject")issue(`${path}/config/activeVarId`,"selection-producer");recordObjects.set(text(config.activeVarId),external);}
  }
  if(w.type==="PropertyList"||w.type==="InlineAction"){
   const source=text(config.objectVarId);section.recordVariable=variable(source,context,`${path}/config/objectVarId`);const v=vars.get(source),producer=m.widgets[text(v?.widgetId)],setID=text(producer?.config.objectSetVarId),producerOwner=widgetOwners.get(text(v?.widgetId));if(producer&&producer.type==="ObjectTable")variable(setID,scopeOf(producerOwner),`${path}/config/objectVarId`);const external=recordObjects.get(source)||queryObjects.get(setID)||"",e=entity(external,`${path}/config/objectVarId`);section.object=e?.type===target.object?undefined:e?.type;
   if(w.type==="PropertyList"){safeKeys(config,["objectVarId","properties","columns","hideNull","inlineEdit","style"],`${path}/config`);section.fields=arr(config.properties).map((p,n)=>field(external,text(p),`${path}/config/properties/${n}`)).filter(name=>{if(name==="id")issue(`${path}/config/properties`,"native-system-id",false);return name!=="id";});if(config.inlineEdit===true||config.hideNull===true)issue(`${path}/config`,"detail-interaction-profile");for(const key of ["columns","style"])if(config[key]!==undefined)issue(`${path}/config/${key}`,"native-presentation",false);}
   else{safeKeys(config,["actionId","objectVarId","mode"],`${path}/config`);const schema=text(own(bindings.actions,text(config.actionId))),action=target.actions.find(a=>a.schema===schema&&a.target===e?.type&&!a.new);if(!action)issue(`${path}/config/actionId`,"action-binding");section.actions=action?[action.schema]:[];if(config.mode!==undefined&&config.mode!=="form")issue(`${path}/config/mode`,"action-profile");}
  }
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
 for(const [sourceID,mapped] of Object.entries(ids.variables)){const v=vars.get(sourceID);if(v?.definitionKind==="widgetOutput"&&!expectedWidgets.has(text(v.widgetId)))issue(`${variablePath(sourceID)}/widgetId`,"selection-producer");const value=document.variables![mapped];if(value?.source?.kind==="record"&&!sections.some(s=>s.id===value.source!.section&&s.widget==="table"))issue(`${variablePath(sourceID)}/widgetId`,"selection-producer");}
 if(Object.keys(m.widgets).some(w=>!usedWidgets.has(w))||m.variables.some(v=>!ids.variables[v.id]))issue("/","unreferenced-content-retained",false);
 if(Object.keys(document.queries!).length>8||Object.values(document.queries!).reduce((s,q)=>s+q.limit,0)>512||Object.keys(document.variables!).length>64||sections.length>128||Object.keys(document.nodes).length>256)issue("/","target-budget");
 if(!diagnostics.some(d=>d.blocking))report.draft=draft;
 return report;
}
