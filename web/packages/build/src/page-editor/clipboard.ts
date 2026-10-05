import {queryInventoryBudget} from "@platform/app/query-inventory";
import type {Api} from "@platform/kernel";

type Section = {tablePresentation?:Api.PageTablePresentation;collectionBuilder?:Api.PageCollectionBuilder;collectionOutputVariable?:string;map?:Api.PageRecordMap;scene?:Api.PageSceneConfig;sceneSampleCollectionVariable?:string;sceneSampleVariable?:string;scenePartVariable?:string;ai?:Api.PageAI;externalFrame?:Api.PageExternalFrame;embedding?:Api.PageEmbedding;observation?:Api.PageObservation;observationHistoryVariable?:string;observationContextVariable?:string;observationSignalVariable?:string;observationThresholdVariable?:string;observationRowsVariable?:string;observationCountVariable?:string;observationMeanVariable?:string;notepadVariable?:string;analysisXVariable?:string;analysisYVariable?:string;analysisCountVariable?:string;analysisMeanVariable?:string;graphExplorer?:Api.PageGraphExplorer;avatar?:{contextVariable?:string;contextCollectionVariable?:string};commentDraftVariable?:string;fileVariable?:string;pdfPageVariable?:string;recordSetVariable?:string;sparklineDecimalVariable?:string;sparklineNumberVariable?:string;groupValueVariable?:string;groupSetVariable?:string;rowValueVariable?:string;rowSetVariable?:string;columnValueVariable?:string;columnSetVariable?:string;pickerValueVariable?:string;alertValueVariable?:string;dateVariable?:string;choiceSetVariable?:string;choiceVariable?:string;booleanVariable?:string;rangeMinVariable?:string;rangeMaxVariable?:string;statisticsVariable?:string;gaugeValueVariable?:string;progressValueVariable?:string;progressTotalVariable?:string;countVariable?:string;selectionSetVariable?:string;facets?:Api.PageFacet[];filterSearchVariable?:string;id?:string;widget:string;object?:string;selection?:string;parentSelection?:string;relation?:string;inputs?:Record<string,Api.Binding>;recordVariable?:string;selectionVariable?:string;collectionVariable?:string;filterVariable?:string};
type Draft<S extends Section> = {document:Api.PageDocument;sections:S[];selections:Api.SelectionVariable[]};
export type LayoutClipboard<S extends Section> = {draft:Draft<S>;root:string;object:string;overlay?:string};
export type ClipboardIssue = "unsupported" | "scope" | "invalid" | "dependencies" | "budget" | "tab-binding" | "overlay-entry";
type Result<T> = {value:T;issue?:never} | {issue:ClipboardIssue;value?:never};
type Limits = {maxVariables:number;query:{maxPlans:number;maxTotalLimit:number;inventoryUIProfile?:string;maxDeclaredPlans?:number;maxDeclaredTotalLimit?:number};loop:{maxContainers:number;maxItems:number;maxTotalItems:number;maxDepth:number};aggregate:{maxVariables:number;maxExpandedReads:number};selectionWriters:readonly string[];selectionWidgets:readonly string[];references:Readonly<Record<string,readonly string[]>>};
const nodeFields=["valueVariable","activeVariable","visibleWhen","enabledWhen"] as const;
const sectionFields=["collectionOutputVariable","sceneSampleCollectionVariable","sceneSampleVariable","scenePartVariable","observationHistoryVariable","observationContextVariable","observationSignalVariable","observationThresholdVariable","observationRowsVariable","observationCountVariable","observationMeanVariable","notepadVariable","analysisXVariable","analysisYVariable","analysisCountVariable","analysisMeanVariable","commentDraftVariable","fileVariable","pdfPageVariable","recordSetVariable","sparklineDecimalVariable","sparklineNumberVariable","groupValueVariable","groupSetVariable","rowValueVariable","rowSetVariable","columnValueVariable","columnSetVariable","pickerValueVariable","alertValueVariable","dateVariable","choiceSetVariable","choiceVariable","booleanVariable","rangeMinVariable","rangeMaxVariable","statisticsVariable","gaugeValueVariable","progressValueVariable","progressTotalVariable","countVariable","recordVariable","selectionVariable","collectionVariable","filterVariable","filterSearchVariable","selectionSetVariable"] as const;
const same=(a:unknown,b:unknown)=>JSON.stringify(a)===JSON.stringify(b);
const layoutKinds=["rows","columns","tabs","flow","toolbar","loop"];
function inMainPage(document:Api.PageDocument,id:string,wholeLoop=false):boolean {
 const seen=new Set<string>();let at:string|undefined=id;
 while(at&&!seen.has(at)){
  if(document.nodes[at]?.kind==="loop"&&!(wholeLoop&&at===id))return false;
  if(at===document.root)return true;
  seen.add(at);at=document.unusedWidgets?.find(e=>e.node===at)?.parent??Object.entries(document.nodes).find(([,n])=>n.children?.includes(at!))?.[0];
 }
 return false;
}

/** Capture original draft bytes, never runtime records or an alternate page format. */
export function copyLayout<S extends Section>(draft:Draft<S>,root:string,object:string):Result<LayoutClipboard<S>> {
 const document=draft.document,seen=new Set<string>();
 const walk=(id:string):boolean=>{
  const node=document.nodes[id];if(!node||seen.has(id)||![...layoutKinds,"widget"].includes(node.kind))return false;
  seen.add(id);return [...(node.children??[]),...(document.unusedWidgets??[]).filter(e=>e.parent===id).map(e=>e.node)].every(walk);
 };
 if(!document.nodes[root]||!layoutKinds.includes(document.nodes[root]!.kind)&&!(document.nodes[root]!.kind==="widget"&&document.nodes[root]!.children?.length))return {issue:"unsupported"};
 if(document.nodes[root]!.slot)return {issue:"scope"};
 if(!walk(root))return {issue:"unsupported"};
 // A complete Loop brings its owner. Fragments cannot lift item state to page.
 const overlay=Object.entries(document.overlays??{}).find(([,o])=>o.root===root)?.[0];
 if(!overlay&&!inMainPage(document,root,true))return {issue:"scope"};
 const ownedSections=new Set<string>();
 for(const id of seen){const node=document.nodes[id]!,section=node.section;if(node.kind==="loop"&&!node.loop)return {issue:"invalid"};if(node.kind==="widget"){if(!section||ownedSections.has(section)||draft.sections.filter(s=>s.id===section).length!==1)return {issue:"invalid"};ownedSections.add(section);}}
 return {value:{draft:structuredClone(draft),root,object,overlay}};
}

/** One atomic edit: copy the owned graph, preserve explicit external bindings,
 * and reject stale external declarations before changing the current draft. */
export function pasteLayout<S extends Section>(current:Draft<S>,clip:LayoutClipboard<S>,target:string,object:string,limits:Limits,options?:{entry:S;title:string}):Result<{draft:Draft<S>;root:string;shared:string[]}> {
 if(object!==clip.object)return {issue:"scope"};
 if(!layoutKinds.includes(current.document.nodes[target]?.kind??"")||!inMainPage(current.document,target))return {issue:"scope"};
 const original=clip.draft.document,document=structuredClone(current.document);
 const overlay=clip.overlay?original.overlays?.[clip.overlay]:undefined;
 if(clip.overlay&&(!overlay||overlay.root!==clip.root))return {issue:"invalid"};
 if(overlay&&(!options||options.entry.widget!=="button"||!options.title.trim()))return {issue:"overlay-entry"};
 const nodes=new Set<string>(),walk=(id:string)=>{nodes.add(id);for(const child of [...(original.nodes[id]?.children??[]),...(original.unusedWidgets??[]).filter(e=>e.parent===id).map(e=>e.node)])walk(child);};walk(clip.root);
 const sections=clip.draft.sections.filter(s=>nodes.has(Object.keys(original.nodes).find(id=>original.nodes[id]?.section===s.id)??""));
 const loops=new Set([...nodes].filter(id=>original.nodes[id]?.kind==="loop"));
 const sectionIDs=new Set(sections.map(s=>s.id!));
 const events=(original.events??[]).filter(e=>sectionIDs.has(e.source));
 const variables=new Set<string>(),queries=new Set<string>(),missing={value:false};
 const valueRefs=(value:Api.PageValue|undefined)=>value?.variable?[value.variable]:[];
 const queryRefs=(query:Api.PageQuery)=>[...query.input?[query.input]:[],...valueRefs(query.search),...valueRefs(query.for),...(query.conditions??[]).flatMap(c=>valueRefs(c.value))];
 const addQuery=(id:string)=>{if(queries.has(id))return;const q=original.queries?.[id];if(!q){missing.value=true;return;}queries.add(id);queryRefs(q).forEach(addVariable);q.set?.inputs.forEach(addQuery);};
 const addVariable=(id:string)=>{if(variables.has(id))return;const v=original.variables?.[id];if(!v){missing.value=true;return;}variables.add(id);v.expression?.args.flatMap(valueRefs).forEach(addVariable);if(v.source?.compute)addVariable(v.source.compute.recordVariable);if(v.mode!=="shared"&&v.source?.variable)addVariable(v.source.variable);if(["plan","count","aggregate","statistics"].includes(v.source?.kind??"")&&v.source?.query)addQuery(v.source.query);};
 for(const id of nodes)for(const key of nodeFields){const value=original.nodes[id]?.[key];if(value)addVariable(value);}
 for(const id of loops){const loop=original.nodes[id]!.loop!;addVariable(loop.collection);addVariable(loop.itemVariable);}
 // Owned declarations remain owned even if their consumer is dormant.
 const ownedScope=(v:Api.PageVariable)=>v.scope==="loop-item"&&loops.has(v.owner??"")||!!overlay&&v.scope==="overlay"&&v.owner===clip.overlay;
 for(const [id,v] of Object.entries(original.variables??{}))if(ownedScope(v))addVariable(id);
 for(const [id,q] of Object.entries(original.queries??{}))if(loops.has(q.itemOwner??"")||overlay&&q.owner===clip.overlay)addQuery(id);
 if(overlay)addVariable(overlay.openVariable);
 for(const section of sections)for(const facet of section.facets??[])addVariable(facet.variable);
 for(const section of sections)for(const key of sectionFields)if(section[key])addVariable(section[key]!);
 for(const section of sections){if(section.ai?.questionVariable)addVariable(section.ai.questionVariable);for(const value of Object.values(section.embedding?.inputs??{}))if(value.variable)addVariable(value.variable);for(const value of Object.values(section.embedding?.results??{}))addVariable(value);}
 for(const section of sections)for(const value of [section.avatar?.contextVariable,section.avatar?.contextCollectionVariable])if(value)addVariable(value);
 for(const section of sections){for(const output of section.graphExplorer?.outputs??[])addVariable(output.variable);for(const id of [section.observation?.rowOutput,section.observation?.assetOutput])if(id)addVariable(id);}
 const locallyRead=new Set(variables);
 for(const event of events){if(event.target)addVariable(event.target);Object.values(event.navigate?.inputs??{}).flatMap(valueRefs).forEach(addVariable);Object.values(event.navigate?.results??{}).forEach(addVariable);}
 if(missing.value)return {issue:"invalid"};
 const externalPorts=new Set([...Object.values(original.interface?.inputs??{}),...Object.values(original.interface?.outputs??{})].map(p=>p.variable));
 const overlayOpen=new Set(Object.values(original.overlays??{}).map(o=>o.openVariable));
 // Tab values name layout nodes. A selector exposed outside this subtree
 // cannot keep naming the old identities while selecting the new children.
 const tabSelectors=new Map<string,Set<string>>();
 for(const id of nodes){const node=original.nodes[id]!;if(node.kind!=="tabs")continue;
  const variable=node.activeVariable,v=original.variables?.[variable??""];
  if(!variable||!v||!(ownedScope(v)||!overlay&&v.scope==="page")||v.mode!=="state"||v.type!=="string"||externalPorts.has(variable)||overlayOpen.has(variable))return {issue:"tab-binding"};
  if(!node.children?.includes(String(v.initial)))return {issue:"invalid"};
  const children=tabSelectors.get(variable)??new Set<string>();node.children.forEach(child=>children.add(child));tabSelectors.set(variable,children);
 }
 const clonedVariables=new Set<string>(),clonedQueries=new Set<string>();
 for(const id of variables)if(ownedScope(original.variables![id]!))clonedVariables.add(id);
 for(const id of queries){const q=original.queries![id]!;if(loops.has(q.itemOwner??"")||overlay&&q.owner===clip.overlay)clonedQueries.add(id);}
 if(overlay){const open=original.variables?.[overlay.openVariable];if(!open||open.scope!=="page"||open.type!=="boolean"||open.mode!=="state"||open.initial!==false)return {issue:"invalid"};clonedVariables.add(overlay.openVariable);}
 const cloneState=(id:string)=>{const v=original.variables?.[id];if(!overlay&&v?.scope==="page"&&v.mode==="state"&&!externalPorts.has(id)&&!overlayOpen.has(id))clonedVariables.add(id);};
 for(const section of sections)for(const id of [section.fileVariable,section.commentDraftVariable,section.pdfPageVariable])if(id)cloneState(id);
 for(const id of nodes){const v=original.nodes[id]?.valueVariable;if(v)cloneState(v);}
 for(const section of sections){if(section.ai?.questionVariable)cloneState(section.ai.questionVariable);if(section.scenePartVariable)cloneState(section.scenePartVariable);}
 for(const section of sections)for(const id of Object.values(section.embedding?.results??{}))cloneState(id);
 for(const section of sections){for(const id of [section.observationSignalVariable,section.observationThresholdVariable,section.observationRowsVariable])if(id)cloneState(id);if(section.notepadVariable)cloneState(section.notepadVariable);if(section.analysisXVariable)cloneState(section.analysisXVariable);if(section.analysisYVariable)cloneState(section.analysisYVariable);for(const facet of section.facets??[])cloneState(facet.variable);if(section.filterSearchVariable)cloneState(section.filterSearchVariable);}
 for(const id of tabSelectors.keys())cloneState(id);
 for(const event of events)for(const id of [event.target,...Object.values(event.navigate?.results??{})])if(locallyRead.has(id))cloneState(id);
 for(const id of variables){const v=original.variables![id]!;if(v.source?.section&&sectionIDs.has(v.source.section))clonedVariables.add(id);}
 let changed=true;
 while(changed){changed=false;
  for(const id of queries){const q=original.queries![id]!;if(!clonedQueries.has(id)&&(queryRefs(q).some(v=>clonedVariables.has(v))||q.set?.inputs.some(q=>clonedQueries.has(q)))){clonedQueries.add(id);changed=true;}}
  for(const id of variables){const v=original.variables![id]!;if(!clonedVariables.has(id)&&(v.expression?.args.some(a=>a.variable&&clonedVariables.has(a.variable))||v.mode!=="shared"&&v.source?.variable&&clonedVariables.has(v.source.variable)||["plan","count","aggregate","statistics"].includes(v.source?.kind??"")&&clonedQueries.has(v.source?.query??""))){clonedVariables.add(id);changed=true;}}
 }
 for(const id of variables){const v=original.variables![id]!;if(!["page","application"].includes(v.scope)&&!ownedScope(v))return {issue:"scope"};}
 for(const id of queries){const q=original.queries![id]!;if(q.owner&&q.owner!==clip.overlay||q.itemOwner&&!loops.has(q.itemOwner))return {issue:"scope"};}
 const shared:string[]=[];
 for(const id of variables)if(!clonedVariables.has(id)){if(!same(original.variables?.[id],document.variables?.[id]))return {issue:"dependencies"};shared.push(id);}
 for(const id of queries)if(!clonedQueries.has(id)&&!same(original.queries?.[id],document.queries?.[id]))return {issue:"dependencies"};
 // Resource outputs supplied by widgets outside the subtree keep those producers.
 for(const id of variables){const producer=original.variables![id]!.source?.section;if(producer&&!sectionIDs.has(producer)&&!same(clip.draft.sections.find(s=>s.id===producer),current.sections.find(s=>s.id===producer)))return {issue:"dependencies"};}
 const used=new Set([...Object.keys(document.nodes),...Object.keys(document.overlays??{}),...current.sections.map(s=>s.id!),...Object.keys(document.variables??{}),...Object.keys(document.queries??{}),...current.selections.map(s=>s.name)]);
 const newID=(prefix:string)=>{let id:string;do{id=prefix+crypto.randomUUID();}while(used.has(id));used.add(id);return id;};
 const nodeMap=new Map([...nodes].map(id=>[id,newID("node")])),sectionMap=new Map([...sectionIDs].map(id=>[id,newID("section")]));
 const variableMap=new Map([...clonedVariables].map(id=>[id,newID("value")])),queryMap=new Map([...clonedQueries].map(id=>[id,newID("query")]));
 const overlayID=overlay?newID("overlay"):undefined;
 const remapValue=(value:Api.PageValue):Api.PageValue=>({...value,...(value.variable?{variable:variableMap.get(value.variable)??value.variable}:{})});
 const tabValue=(variable:string,value:unknown)=>typeof value==="string"&&tabSelectors.get(variable)?.has(value)?nodeMap.get(value)!:value;
 const selectionMap=new Map<string,string>(),addedSelections:Api.SelectionVariable[]=[];
 const slot=(type:string,name?:string)=>JSON.stringify([type,name??""]);
 for(const s of sections)if(limits.selectionWriters.includes(s.widget)&&!s.selectionVariable){const type=s.object||object,key=slot(type,s.selection);if(!selectionMap.has(key)){const name=newID("selection");selectionMap.set(key,name);addedSelections.push({name,object:{app:type.split(".")[0]!,kind:"object",name:type}});}}
 const rewritten=sections.map(source=>{const s=structuredClone(source);s.id=sectionMap.get(s.id!)!;
  for(const key of sectionFields)if(s[key])s[key]=variableMap.get(s[key]!)??s[key];
  if(s.observation){for(const port of ["rowOutput","assetOutput"] as const)if(s.observation[port])s.observation[port]=variableMap.get(s.observation[port]!)??s.observation[port];}
  if(s.graphExplorer)s.graphExplorer.outputs=s.graphExplorer.outputs?.map(o=>({...o,variable:variableMap.get(o.variable)??o.variable}));
  if(s.ai?.questionVariable)s.ai.questionVariable=variableMap.get(s.ai.questionVariable)??s.ai.questionVariable;
  if(s.embedding){s.embedding.inputs=Object.fromEntries(Object.entries(s.embedding.inputs??{}).map(([id,value])=>[id,remapValue(value)]));s.embedding.results=Object.fromEntries(Object.entries(s.embedding.results??{}).map(([id,value])=>[id,variableMap.get(value)??value]));}
  if(s.avatar){if(s.avatar.contextVariable)s.avatar.contextVariable=variableMap.get(s.avatar.contextVariable)??s.avatar.contextVariable;if(s.avatar.contextCollectionVariable)s.avatar.contextCollectionVariable=variableMap.get(s.avatar.contextCollectionVariable)??s.avatar.contextCollectionVariable;}
  if(s.facets)s.facets=s.facets.map(f=>({...f,variable:variableMap.get(f.variable)??f.variable}));
  const own=selectionMap.get(slot(s.object||object,s.selection));if(own&&limits.selectionWidgets.includes(s.widget)&&!s.recordVariable&&!s.selectionVariable)s.selection=own;
  const parentType=s.parentSelection?clip.draft.selections.find(v=>v.name===s.parentSelection)?.object.name:object;
  const parent=parentType&&selectionMap.get(slot(parentType,s.parentSelection));if(parent&&(s.relation||s.parentSelection||s.widget==="table"&&(s.object||object)!==object&&(limits.references[s.object||object]??[]).includes(parentType)||Object.values(s.inputs??{}).some(b=>b.source==="subject")))s.parentSelection=parent;
  return s;
 });
 for(const s of sections)for(const name of [s.selection,s.parentSelection])if(name&&!selectionMap.has(slot(clip.draft.selections.find(v=>v.name===name)?.object.name??"",name))){if(!same(clip.draft.selections.find(v=>v.name===name),current.selections.find(v=>v.name===name)))return {issue:"dependencies"};shared.push(name);}
 for(const [id,mapped] of nodeMap){const n=structuredClone(original.nodes[id]!);if(n.children)n.children=n.children.map(child=>nodeMap.get(child)!);if(n.section)n.section=sectionMap.get(n.section)!;for(const key of nodeFields)if(n[key])n[key]=variableMap.get(n[key]!)??n[key];if(n.loop){n.loop.collection=variableMap.get(n.loop.collection)??n.loop.collection;n.loop.itemVariable=variableMap.get(n.loop.itemVariable)!;}document.nodes[mapped]=n;}
 const entries:S[]=[];
 if(overlay&&overlayID&&options){
  document.overlays={...document.overlays,[overlayID]:{...structuredClone(overlay),title:options.title,root:nodeMap.get(clip.root)!,openVariable:variableMap.get(overlay.openVariable)!}};
  const entry={...structuredClone(options.entry),id:newID("section")},leaf=newID("node");entries.push(entry);document.nodes[leaf]={kind:"widget",section:entry.id};
  document.nodes[target]!.children=[...(document.nodes[target]!.children??[]),leaf];
 }else document.nodes[target]!.children=[...(document.nodes[target]!.children??[]),nodeMap.get(clip.root)!];
 const dormant=(original.unusedWidgets??[]).filter(e=>nodes.has(e.parent)).map(e=>({node:nodeMap.get(e.node)!,parent:nodeMap.get(e.parent)!}));
 if(dormant.length)document.unusedWidgets=[...(document.unusedWidgets??[]),...dormant];
 for(const [id,mapped] of variableMap){
  const v=structuredClone(original.variables![id]!);
  if(v.owner)v.owner=v.scope==="overlay"&&v.owner===clip.overlay?overlayID!:nodeMap.get(v.owner)??v.owner;
  if(tabSelectors.has(id))v.initial=tabValue(id,v.initial);
  if(v.expression){
   const selector=v.expression.op==="equal"?v.expression.args.find(a=>a.variable&&tabSelectors.has(a.variable))?.variable:undefined;
   v.expression.args=v.expression.args.map(a=>({...remapValue(a),...(selector&&typeof a.literal==="string"?{literal:tabValue(selector,a.literal)}:{})}));
  }
  if(v.source){
   if(v.source.section)v.source.section=sectionMap.get(v.source.section)??v.source.section;
   if(v.source.node)v.source.node=nodeMap.get(v.source.node)??v.source.node;
   if(v.source.compute)v.source.compute.recordVariable=variableMap.get(v.source.compute.recordVariable)??v.source.compute.recordVariable;
   if(v.mode!=="shared"&&v.source.variable)v.source.variable=variableMap.get(v.source.variable)??v.source.variable;
   if(v.source.query)v.source.query=queryMap.get(v.source.query)??v.source.query;
  }
  document.variables={...document.variables,[mapped]:v};
 }
 for(const [id,mapped] of queryMap){const q=structuredClone(original.queries![id]!);if(q.owner===clip.overlay&&overlayID)q.owner=overlayID;if(q.itemOwner)q.itemOwner=nodeMap.get(q.itemOwner)!;q.input=q.input?variableMap.get(q.input)??q.input:undefined;if(q.search)q.search=remapValue(q.search);if(q.for)q.for=remapValue(q.for);if(q.conditions)q.conditions=q.conditions.map(c=>({...c,value:remapValue(c.value)}));if(q.set)q.set.inputs=q.set.inputs.map(id=>queryMap.get(id)??id);document.queries={...document.queries,[mapped]:q};}
 document.events=[...(document.events??[]),...events.map(source=>{const e=structuredClone(source);e.source=sectionMap.get(e.source)!;if(e.value!==undefined)e.value=tabValue(e.target,e.value);e.target=variableMap.get(e.target)??e.target;if(e.navigate){if(e.navigate.inputs)e.navigate.inputs=Object.fromEntries(Object.entries(e.navigate.inputs).map(([key,value])=>[key,remapValue(value)]));if(e.navigate.results)e.navigate.results=Object.fromEntries(Object.entries(e.navigate.results).map(([key,value])=>[key,variableMap.get(value)??value]));}return e;})];
 if(overlay&&entries[0])document.events.push({source:entries[0].id!,event:"click",target:variableMap.get(overlay.openVariable)!,value:true});
 const next={...current,document,sections:[...current.sections,...rewritten,...entries],selections:[...current.selections,...addedSelections]};
 if(Object.keys(document.nodes).length>256||Object.keys(document.overlays??{}).length>16||document.events.length>256||next.sections.length>128||(document.unusedWidgets?.length??0)>128||Object.keys(document.variables??{}).length>limits.maxVariables||Object.keys(document.queries??{}).length>(Number(document.uiProfile.split(".").at(-1))>=97?(limits.query.maxDeclaredPlans??limits.query.maxPlans):limits.query.maxPlans)||!withinLoopBudgets(document,limits,next.sections))return {issue:"budget"};
 return {value:{draft:next,root:nodeMap.get(clip.root)!,shared:[...new Set(shared)]}};
}

function withinLoopBudgets(document:Api.PageDocument,limits:Limits,sections:Section[]):boolean {
 const owners=new Map<string,string|undefined>();
 const visit=(id:string,owner?:string)=>{if(owners.has(id))return;owners.set(id,owner);const n=document.nodes[id];for(const child of [...(n?.children??[]),...(document.unusedWidgets??[]).filter(e=>e.parent===id).map(e=>e.node)])visit(child,n?.kind==="loop"?id:owner);};
 visit(document.root);Object.values(document.overlays??{}).forEach(o=>visit(o.root));
 const factor=(owner?:string)=>{let result=1,depth=0;while(owner){if(++depth>limits.loop.maxDepth)return Infinity;const loop=document.nodes[owner]?.loop;if(!loop||!Number.isInteger(loop.limit)||loop.limit<1||loop.limit>limits.loop.maxItems)return Infinity;result*=loop.limit;owner=owners.get(owner);}return result;};
 const loops=Object.entries(document.nodes).filter(([,n])=>n.kind==="loop"),items=loops.reduce((sum,[id,n])=>sum+(n.loop?.limit??Infinity)*factor(owners.get(id)),0);
 if(loops.length>limits.loop.maxContainers||items>limits.loop.maxTotalItems||loops.some(([id])=>!Number.isFinite(factor(id))))return false;
 const reads=Object.values(document.queries??{}).reduce((sum,q)=>sum+q.limit*factor(q.itemOwner),0),aggregates=Object.values(document.variables??{}).filter(v=>v.mode==="aggregate"),expanded=[...new Set(aggregates.map(v=>JSON.stringify([v.source?.query??"",["aggregate","statistics"].includes(v.source?.kind??"")?`${v.source?.kind}:${v.source?.measure}`:"count"])))].reduce((sum,key)=>sum+factor(document.queries?.[JSON.parse(key)[0]]?.itemOwner),0);
 return (limits.query.inventoryUIProfile?queryInventoryBudget(document,sections as unknown as Api.Section[],limits.query as Required<typeof limits.query>).valid:reads<=limits.query.maxTotalLimit)&&aggregates.length<=limits.aggregate.maxVariables&&expanded<=limits.aggregate.maxExpandedReads;
}
