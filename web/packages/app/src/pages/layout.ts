import {pageUIManifest,type Api} from "@platform/kernel";

/** Application API diagnostics; the host remains the save/publish authority. */
export function pageLayoutDiagnostics(document:Api.PageDocument,sections:Pick<Api.Section,"id"|"widget"|"configVersion">[]=[]):{node:string;code:string}[] {
 const limits=pageUIManifest.layout,issues:{node:string;code:string}[]=[];
 const parents=new Map<string,string>();
 for(const [id,n]of Object.entries(document.nodes))for(const child of n.children??[]){if(parents.has(child))issues.push({node:child,code:"Slot and layout nodes need one parent."});parents.set(child,id);}
 const entries=document.unusedWidgets??[];
 if(entries.length>limits.maxUnused||entries.length&&Number(document.uiProfile.split(".").at(-1))<Number(limits.unusedProfile.split(".").at(-1)))issues.push({node:document.root,code:"Unused widgets need a newer profile and a bounded inventory."});
 const registered=new Set<string>();for(const entry of entries){if(registered.has(entry.node)||parents.has(entry.node)||document.nodes[entry.node]?.kind!=="widget"||!document.nodes[entry.parent]||document.nodes[entry.parent]?.kind==="widget")issues.push({node:entry.node,code:"Unused widget needs one original leaf and a layout parent."});registered.add(entry.node);}
 const unused=new Set((document.unusedWidgets??[]).map(entry=>entry.node));
 for(const entry of document.unusedWidgets??[])parents.set(entry.node,entry.parent);
 const bounded=(id:string,seen=new Set<string>()):boolean=>{
  if(seen.has(id))return false;seen.add(id);
  const n=document.nodes[id],p=parents.get(id),parent=p&&document.nodes[p];
  return !!n&&(n.size?.height!==undefined||!!parent&&!!p&&(parent.kind==="columns"||parent.kind==="rows"&&n.size?.weight!==undefined)&&bounded(p,seen));
 };
 for(const [id,n]of Object.entries(document.nodes)){
  const ownerID=parents.get(id),owner=ownerID?document.nodes[ownerID]:undefined;
  if(n.slot&&owner?.kind!=="widget")issues.push({node:id,code:"A widget slot needs its registered widget parent."});
  if(n.kind==="widget"&&(n.children?.length??0)>0){
   const section=sections.find(s=>s.id===n.section),contract=pageUIManifest.widgets.find(w=>w.componentID===section?.widget),slots=contract&&"slots" in contract?contract.slots:[],used=new Set<string>();
   for(const child of n.children??[]){const root=document.nodes[child],slot=slots.find(s=>s.id===root?.slot);if(!slot||used.has(slot.id)||Number(document.uiProfile.split('.').at(-1))<Number(slot.requiredUIProfile.split('.').at(-1))||!(slot.allowedLayouts as readonly string[]).includes(root?.kind??""))issues.push({node:child,code:"Widget slots need unique declared layouts and a supported page profile."});if(slot)used.add(slot.id);}
  }
  const s=n.size,p=parents.get(id),parent=p?document.nodes[p]:undefined;
  const fail=(code:string)=>issues.push({node:id,code});
  const r=n.presentation;
  if(r&&(Number(document.uiProfile.split('.').at(-1))<Number(limits.presentationProfile.split('.').at(-1))||!['rows','columns'].includes(n.kind)||r.padding!==undefined&&(!Number.isInteger(r.padding)||r.padding<0||r.padding>limits.maxPadding)||r.background!==undefined&&!['default','panel'].includes(r.background)||(['border','showHeader','collapsible','defaultCollapsed']as const).some(k=>r[k]!==undefined&&typeof r[k]!=='boolean')||r.showHeader&&!n.title||r.collapsible&&(!r.showHeader||!n.title)||r.defaultCollapsed&&!r.collapsible))fail('Region presentation needs a supported container, bounded padding and a visible collapse title.');
  if((s||n.gap!==undefined)&&Number(document.uiProfile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))fail("Layout sizing needs a newer page profile.");
  if(n.gap!==undefined&&(!["rows","columns"].includes(n.kind)||!Number.isInteger(n.gap)||n.gap<0||n.gap>limits.maxGap))fail("Layout gap is outside its container budget.");
  if(!s)continue;
  for(const key of ["width","height","minWidth","maxWidth","minHeight","maxHeight"]as const){const v=s[key];if(v!==undefined&&(!Number.isInteger(v)||v<limits.minSize||v>limits.maxSize))fail("Layout dimension is outside its size budget.");}
  for(const [fixed,min,max]of [[s.width,s.minWidth,s.maxWidth],[s.height,s.minHeight,s.maxHeight]])if(min!==undefined&&max!==undefined&&min>max||fixed!==undefined&&(min!==undefined&&fixed<min||max!==undefined&&fixed>max))fail("Fixed, minimum and maximum dimensions disagree.");
  if(s.weight!==undefined){
   if(!unused.has(id)&&(!parent||!["rows","columns"].includes(parent.kind))||!Number.isInteger(s.weight)||s.weight<1||s.weight>limits.maxWeight)fail("Layout weight needs a Rows or Columns parent.");
   if(!unused.has(id)&&(parent?.kind==="columns"&&s.width!==undefined||parent?.kind==="rows"&&s.height!==undefined))fail("Layout weight conflicts with a fixed main-axis size.");
   if(!unused.has(id)&&parent?.kind==="rows"&&(!p||!bounded(p)))fail("Row weight needs a parent with a definite height.");
  }
  if(s.scroll&&!["visible","auto"].includes(s.scroll))fail("Layout scroll must be visible or auto.");
  if(s.scroll==="auto"&&!unused.has(id)&&!bounded(id)&&s.maxHeight===undefined)fail("Layout scroll needs a definite or maximum height.");
 }
 return issues;
}
