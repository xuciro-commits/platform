import type {Api} from "@platform/kernel";
import type * as UI from "./types";
import {pageSafetyContract} from "./safety";

type ObjectValue=Record<string,unknown>;
type Envelope=Api.Page;
const object=(value:unknown)=>value as ObjectValue;
const fieldName=(field:Api.FieldRef)=>field.path?.length?field.path.join("."):field.name??"";
const arrayFields=new Set<string>(pageSafetyContract.arrayFieldPaths);

function clearPath(value:unknown,path:string[]):void{
 if(!value||typeof value!=="object")return;
 if(Array.isArray(value)){for(const item of value)clearPath(item,path);return;}
 const part=path[0]!,rest=path.slice(1),key=part.replace(/\[\]$/,"");
 if(!rest.length)delete object(value)[key];else clearPath(object(value)[key],rest);
}
function writePath(value:ObjectValue,path:string[],values:unknown[],array=false):void{
 const part=path[0]!,rest=path.slice(1),key=part.replace(/\[\]$/,"");
 if(part.endsWith("[]")||rest.length&&Array.isArray(value[key])){
  const items=Array.isArray(value[key])?value[key] as ObjectValue[]:values.map(()=>({}));value[key]=items;
  items.forEach((item,index)=>{if(rest.length)writePath(item,rest,[values[index]]);else items[index]=values[index]as ObjectValue;});
 }else if(rest.length){const child=value[key]??{};value[key]=child;writePath(object(child),rest,values);}
 else value[key]=array?values:values.length===1?values[0]:values;
}
function stripBindings(section:ObjectValue):void{
 const paths=[...pageSafetyContract.section.bindingPaths,...pageSafetyContract.section.resourcePaths,...pageSafetyContract.scalarFieldPaths,...pageSafetyContract.arrayFieldPaths,...pageSafetyContract.repeatedFieldPaths,...pageSafetyContract.retainedContract.widgets.flatMap(widget=>[...[...widget.inputPorts,...widget.outputPorts].map(port=>port.bindingField),...("fieldRoles"in widget?widget.fieldRoles.map(role=>role.path):[])])];
 for(const path of paths)clearPath(section,path.split("."));
}

export function decodePage(page:Envelope|UI.Page):UI.Page{
 if(!("format"in page)||page.format!==3)return page as UI.Page;
 const declaration=page as Envelope,bindings=declaration.dataBindings??[],triggers=declaration.actionTriggers??[],inputs=declaration.inputSchema??{},scopes=declaration.scopeVisibility??{root:"",scopes:[],owners:{}};
 const raw=declaration.widgetSpec?.body as {sections?:unknown;document?:UI.PageDocument;layout?:string}|null;
 if(declaration.widgetSpec?.format!=="platform.page.web.v1"||!raw||typeof raw!=="object"||!Array.isArray(raw.sections)||raw.sections.some(section=>!section||typeof section!=="object"||Array.isArray(section))||raw.document&&(!raw.document.nodes||typeof raw.document.nodes!=="object"))return {name:declaration.name,title:declaration.title,object:declaration.object,layout:"composed",listFields:[],detailFields:[],actions:[],sections:[],document:{formatVersion:0,uiProfile:declaration.widgetSpec?.format??"",root:"",nodes:{}}};
 const body=structuredClone(declaration.widgetSpec.body) as {sections?:ObjectValue[];document?:UI.PageDocument};
 const scopeIDs=new Set((scopes.scopes??[]).map(scope=>scope.id));
 const allowedBindings=new Map(bindings.filter(binding=>!binding.owner||scopeIDs.has(binding.owner)).map(binding=>[binding.id,binding]));
 const sections=(body.sections??[]).filter(section=>allowedBindings.has(String(section.binding))).map(section=>{stripBindings(section);return section;});
 for(const section of sections){
  const binding=allowedBindings.get(String(section.binding))!;
  section.id=binding.id;section.object=binding.object;section.model=binding.model;section.fields=(binding.fields??[]).map(fieldName);
  if(binding.models)section.composite={...object(section.composite??{}),bindings:binding.models,...binding.value?{value:binding.value}:{},...binding.acyclic!==undefined?{acyclic:binding.acyclic}:{}};
  for(const [path,value]of Object.entries(binding.ports??{}))writePath(section,path.split("."),[value]);
  for(const [path,fields]of Object.entries(binding.fieldSlots??{}))writePath(section,path.split("."),fields.map(field=>field.transform?(path==="measure"?`${field.transform}:${fieldName(field)}`:`${fieldName(field)}:${field.transform}`):fieldName(field)),arrayFields.has(path));
  if(binding.query)section.query={...binding.query.ref,sourceVersion:binding.query.sourceVersion};
  for(const resource of binding.resources??[])restoreResource(section,resource);
  for(const trigger of triggers.filter(trigger=>trigger.binding===binding.id)){
   if(trigger.actions!==undefined)section.actions=trigger.actions;
   for(const key of ["function","operation","inputs","inlineEdit"]as const)if(trigger[key]!==undefined)section[key]=trigger[key];
   if(trigger.defaults?.length){if(section.widget==="action-table")section.actionTable={parameters:trigger.defaults};else section.actionDefaults=trigger.defaults;}
  }
 }
 const visual=body.document,nodes=visual?.nodes??{},overlays=visual?.overlays??{};
 const projected=sections.length!==(body.sections??[]).length||Object.keys(nodes).some(id=>!scopeIDs.has(id))||Object.keys(overlays).some(id=>!scopeIDs.has(id));
 for(const id of Object.keys(nodes))if(!scopeIDs.has(id))delete nodes[id];
 for(const id of Object.keys(overlays))if(!scopeIDs.has(id))delete overlays[id];
 for(const node of Object.values(nodes)){
  for(const key of ["valueVariable","activeVariable","visibleWhen","enabledWhen","loop","section"]as const)delete node[key];
 }
 for(const overlay of Object.values(overlays))delete object(overlay).openVariable;
 for(const scope of scopes.scopes??[]){
  if(scope.kind==="overlay"){
   const overlay=overlays[scope.id];if(overlay){overlay.root=scope.bindings?.root??"";overlay.openVariable=scope.bindings?.openVariable??"";}continue;
  }
  const node=nodes[scope.id];if(!node)continue;
  const children=(scopes.scopes??[]).filter(child=>child.parent===scope.id&&!child.inactive).map(child=>child.id);
  node.children=[...(node.children??[]).filter(child=>children.includes(child)),...children.filter(child=>!node.children?.includes(child))];
  if(scope.binding)node.section=scope.binding;
  for(const [key,value]of Object.entries(scope.bindings??{}))if(["valueVariable","activeVariable","visibleWhen","enabledWhen"].includes(key))object(node)[key]=value;
  if(scope.kind==="loop")node.loop={collection:scope.bindings?.collection??"",itemVariable:scope.bindings?.itemVariable??"",limit:scope.maxIterations??0};
 }
 const unusedWidgets=visual?.unusedWidgets?.filter(unused=>scopeIDs.has(unused.node)&&scopeIDs.has(unused.parent)&&(scopes.scopes??[]).some(scope=>scope.id===unused.node&&scope.inactive));
 if(projected){
  for(let changed=true;changed;){
   changed=false;
   for(const [id,node]of Object.entries(nodes))if(id!==scopes.root&&node.kind!=="widget"&&!(node.children??[]).some(child=>nodes[child])&&!unusedWidgets?.some(unused=>unused.parent===id)){
    delete nodes[id];changed=true;
   }
   for(const node of Object.values(nodes))if(node.children)node.children=node.children.filter(child=>nodes[child]);
  }
  for(const [id,overlay]of Object.entries(overlays))if(!nodes[overlay.root])delete overlays[id];
 }
 const document: UI.PageDocument|undefined=visual&&!(projected&&!sections.length)?{...visual,root:scopes.root,nodes,overlays,unusedWidgets,variables:inputs.variables,queries:inputs.queries,interface:inputs.interface,events:triggers.flatMap(trigger=>trigger.events??[])}:undefined;
 const actions=triggers.find(trigger=>trigger.binding==="page")?.actions??[];
 if(raw.layout==="list-detail"){
  const fieldsOf=(id:string)=>(sections.find(section=>section.id===id)?.fields as string[]|undefined)??[];
  return {name:declaration.name,title:declaration.title,description:declaration.description,object:declaration.object,layout:"list-detail",listFields:fieldsOf("list"),detailFields:fieldsOf("detail"),actions,selections:inputs.selections,presentationWords:declaration.presentationWords} as UI.Page;
 }
 return {name:declaration.name,title:declaration.title,description:declaration.description,object:declaration.object,layout:"composed",listFields:[],detailFields:[],actions,sections:sections as UI.Section[],document,selections:inputs.selections,presentationWords:declaration.presentationWords};
}
function restoreResource(section:ObjectValue,resource:Api.PageResourceBinding):void{
 const merge=(key:string,fields:ObjectValue)=>{section[key]={...object(section[key]??{}),...fields};};
 const append=(key:string,slot:string,entry:ObjectValue)=>{const config=object(section[key]??{});section[key]=config;const rows=(config[slot]??=[])as ObjectValue[];const existing=rows.find(row=>row.id===entry.id)||rows.find(row=>!row.id&&!row.object&&!row.binding&&!row.variable);if(existing)Object.assign(existing,entry);else rows.push(entry);};
 switch(resource.kind){
 case "filter":section.facets=[...section.facets as unknown[]??[],{field:resource.fields?.[0]&&fieldName(resource.fields[0]),kind:resource.source,variable:resource.outputs?.value}];break;
 case "record-mapping":append("scene","mappings",{id:resource.id,source:resource.source,field:resource.fields?.[0]&&fieldName(resource.fields[0])});break;
 case "ai":merge("ai",{kind:resource.source,questionVariable:resource.outputs?.question,replyField:resource.fields?.find(field=>field.slot==="replyField")?.name});break;
 case "incoming-reference":{
  const links=(section.recordLinks??=[])as ObjectValue[],row=links.find(item=>!item.object)??{};if(!links.includes(row))links.push(row);Object.assign(row,{object:resource.object,field:resource.fields?.[0]&&fieldName(resource.fields[0])});break;
 }

 case "page":merge("embedding",{kind:resource.source,readOnly:resource.readOnly,page:resource.asset,contentVersion:resource.contentVersion,interfaceVersion:resource.interfaceVersion??0,inputs:resource.inputs,results:resource.outputs});break;
 case "external-document":merge("externalFrame",{url:resource.url,origin:resource.origin});break;
 case "image":merge("image",{url:resource.url});break;
 case "asset":append("assetDirectory","items",{id:resource.id,asset:resource.asset});break;
 case "graph-object":append("graphExplorer","objects",{id:resource.id,object:resource.object,labelField:resource.fields?.[0]&&fieldName(resource.fields[0])});break;
 case "graph-link":append("graphExplorer","relations",{id:resource.id,binding:resource.asset});break;
 case "record-output":append("graphExplorer","outputs",{id:resource.id,object:resource.object,variable:resource.outputs?.record});break;
 case "neighborhood":append("vertexGraph","groups",{id:resource.id,binding:resource.asset,direction:resource.source,limit:resource.limit});break;
 case "observation":{
  const visual=object(section.observation??{}),signals=(visual.signals??[])as ObjectValue[];
  merge("observation",{kind:resource.source,assetObject:resource.object,rowOutput:resource.outputs?.row,assetOutput:resource.outputs?.asset});
  let index=0;for(const field of resource.fields??[])if(field.slot==="signals"){signals[index]={...signals[index],field:fieldName(field)};index++;}else writePath(object(section.observation),(field.slot??"").split("."),[fieldName(field)]);
  object(section.observation).signals=signals;break;
 }
 }
}

export function decodeDefinitions(definitions:Api.Definition[]):(Omit<Api.Definition,"page">&{page?:UI.Page})[]{return definitions.map(definition=>definition.page?{...definition,page:decodePage(definition.page)}:definition as Omit<Api.Definition,"page">&{page?:UI.Page});}

function readPath(value:unknown,path:string[]):unknown[]{
 if(!path.length)return [value];
 if(Array.isArray(value))return value.flatMap(item=>readPath(item,path));
 if(!value||typeof value!=="object")return [];
 return readPath(object(value)[path[0]!.replace(/\[\]$/,"")],path.slice(1));
}

export function encodePage(page:UI.Page,definitions:readonly {ref:Api.AssetRef;version:string}[]=[]):Envelope{
 const visualSections:ObjectValue[]=[],dataBindings:Api.PageDataBinding[]=[],actionTriggers:Api.PageActionTrigger[]=[{id:"page",binding:"page",actions:page.actions}],owners:Record<string,string>={};
 const document=page.document,scopes:Api.PageScope[]=[],parents:Record<string,string>={};
 for(const [id,node]of Object.entries(document?.nodes??{}))for(const child of node.children??[])parents[child]=id;
 for(const [id,overlay]of Object.entries(document?.overlays??{}))parents[overlay.root]=id;
 for(const unused of document?.unusedWidgets??[])parents[unused.node]=unused.parent;
 for(const [id,node]of Object.entries(document?.nodes??{})){
  if(node.section)owners[node.section]=id;
  scopes.push({id,parent:parents[id],kind:node.loop?"loop":"page",binding:node.section,bindings:{valueVariable:node.valueVariable??"",activeVariable:node.activeVariable??"",visibleWhen:node.visibleWhen??"",enabledWhen:node.enabledWhen??"",...node.loop?{collection:node.loop.collection,itemVariable:node.loop.itemVariable}:{}},maxIterations:node.loop?.limit,inactive:document?.unusedWidgets?.some(item=>item.node===id)});
 }
 for(const [id,overlay]of Object.entries(document?.overlays??{}))scopes.push({id,kind:"overlay",bindings:{root:overlay.root,openVariable:overlay.openVariable}});
 if(!document)scopes.push({id:"page",kind:"page"});
 for(const [index,section]of (page.sections??[]).entries()){
  const id=section.id??`section-${index+1}`,widget=pageSafetyContract.retainedContract.widgets.find(widget=>widget.componentID===section.widget);
  const binding:Api.PageDataBinding={id,owner:owners[id],kind:section.composite?"composite":section.recordVariable?"record":"records",object:section.object??page.object,model:section.model,models:section.composite?.bindings,value:section.composite?.value,acyclic:section.composite?.acyclic,fields:section.fields?.map(name=>({name})),ports:{},fieldSlots:{},resources:[],query:section.query?{ref:{app:section.query.app,kind:section.query.kind,name:section.query.name},sourceVersion:section.query.sourceVersion??definitions.find(item=>item.ref.kind==="query"&&item.ref.app===section.query?.app&&item.ref.name===section.query?.name)?.version??""}:undefined,limit:200};
  for(const path of [...[...widget?.inputPorts??[],...widget?.outputPorts??[]].map(port=>port.bindingField),"avatar.contextVariable","avatar.contextCollectionVariable","ai.questionVariable","selection","parentSelection","relation","filterSearchVariable","collectionOutputVariable"]){const value=readPath(section,path.split("."))[0];if(typeof value==="string"&&value)binding.ports![path]=value;}
  for(const path of [...new Set([...pageSafetyContract.scalarFieldPaths,...(widget&&"fieldRoles"in widget?widget.fieldRoles.map(role=>role.path):[]),...pageSafetyContract.arrayFieldPaths,...pageSafetyContract.repeatedFieldPaths,"map.latitudeField","map.longitudeField","map.labelField","map.colorField","detailPresentation.barcode","scene.sampleTimeField","scene.sampleAssetField"])]){
   const fields=readPath(section,path.split(".")).flatMap(value=>Array.isArray(value)?value:[value]).filter((value):value is string=>typeof value==="string"&&!!value);
   if(fields.length&&!(path==="measure"&&fields[0]==="count"))binding.fieldSlots![path]=fields.map(name=>{const [first,second]=name.split(":")as [string,string?];return second?{name:path==="measure"?second:first,transform:path==="measure"?first:second}:{name};});
   if(path==="measure"&&fields[0]==="count")binding.ports!["measure"]="count";
  }
  const resources=binding.resources!;
  if(widget&&"resources"in widget)for(const resource of widget.resources){
   if("asset"in resource){const {asset,...metadata}=resource;resources.push({...metadata,asset:{ref:asset.ref,sourceVersion:definitions.find(item=>item.ref.app===asset.ref.app&&item.ref.kind===asset.ref.kind&&item.ref.name===asset.ref.name)?.version??""}});}else resources.push(resource);
  }
  for(const [index,facet]of (section.facets??[]).entries())resources.push({id:`facet:${index}`,kind:"filter",source:facet.kind,fields:[{name:facet.field}],outputs:{value:facet.variable}});
  for(const mapping of section.scene?.mappings??[])resources.push({id:mapping.id,kind:"record-mapping",source:mapping.source,fields:[{name:mapping.field}]});
  if(section.ai)resources.push({id:"ai",kind:"ai",source:section.ai.kind,fields:section.ai.replyField?[{slot:"replyField",name:section.ai.replyField}]:[],outputs:{question:section.ai.questionVariable??""}});
  for(const link of section.recordLinks??[])resources.push({id:`${link.object.app}/${link.object.kind}/${link.object.name}/${link.field}`,kind:"incoming-reference",object:link.object,fields:[{name:link.field}]});
  if(section.embedding){const r=section.embedding;resources.push({id:"embedding",kind:"page",asset:r.page,contentVersion:r.contentVersion,interfaceVersion:r.interfaceVersion,inputs:r.inputs,outputs:r.results,source:r.kind,readOnly:r.readOnly});}
  if(section.externalFrame)resources.push({id:"externalFrame",kind:"external-document",url:section.externalFrame.url,origin:section.externalFrame.origin});
  if(section.image)resources.push({id:"image",kind:"image",url:section.image.url});
  for(const item of section.assetDirectory?.items??[])resources.push({id:item.id,kind:"asset",asset:item.asset});
  for(const item of section.graphExplorer?.objects??[])resources.push({id:`${item.object.app}/${item.object.kind}/${item.object.name}`,kind:"graph-object",object:item.object,fields:[{name:item.labelField}]});
  for(const item of section.graphExplorer?.relations??[])resources.push({id:item.id,kind:"graph-link",asset:item.binding});
  for(const item of section.graphExplorer?.outputs??[])resources.push({id:item.id,kind:"record-output",object:item.object,outputs:{record:item.variable}});
  for(const item of section.vertexGraph?.groups??[])resources.push({id:item.id,kind:"neighborhood",asset:item.binding,source:item.direction,limit:item.limit});
  if(section.observation){const r=section.observation;resources.push({id:"observation",kind:"observation",object:r.assetObject,source:r.kind,outputs:{row:r.rowOutput??"",asset:r.assetOutput??""},fields:[...(["timeField","assetField","averageField"]as const).filter(key=>r[key]).map(slot=>({slot,name:r[slot]!})),...r.signals.map(signal=>({slot:"signals",name:signal.field})),...Object.entries(r.metadata??{}).filter(([,name])=>!!name).map(([key,name])=>({slot:`metadata.${key}`,name}))]});}
  dataBindings.push(binding);
  actionTriggers.push({id,binding:id,owner:owners[id],actions:section.actions,function:section.function,operation:section.operation,inputs:section.inputs,inlineEdit:section.inlineEdit,defaults:section.actionTable?.parameters??section.actionDefaults});
  const visual=structuredClone(section)as ObjectValue;stripBindings(visual);Object.assign(visual,{id,binding:id,triggers:[id]});visualSections.push(visual);
 }
 for(const [index,event]of (document?.events??[]).entries())actionTriggers.push({id:`event:${event.source}:${index}`,binding:event.source,events:[event]});
 const visualDocument=document&&structuredClone(document);
 if(visualDocument){
  for(const path of pageSafetyContract.scope.documentPaths)clearPath(visualDocument,path.split("."));
  for(const node of Object.values(visualDocument.nodes))for(const path of [...pageSafetyContract.scope.bindingPaths,"section"])clearPath(node,path.split("."));
  for(const overlay of Object.values(visualDocument.overlays??{}))for(const path of pageSafetyContract.scope.overlayPaths)clearPath(overlay,path.split("."));
 }
 return {format:3,name:page.name,title:page.title,description:page.description,object:page.object,dataBindings,actionTriggers,inputSchema:{variables:document?.variables,queries:document?.queries,interface:document?.interface,selections:page.selections},scopeVisibility:{root:document?.root??"page",scopes,owners},widgetSpec:{format:"platform.page.web.v1",body:{sections:visualSections,document:visualDocument}}}as Envelope;
}
