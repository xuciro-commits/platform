import {snapDimension} from '@platform/ui/canvas-snapping';
import {pageLayoutDiagnostics} from '@platform/app';
import {pageUIManifest,type Api} from '@platform/kernel';
import type {CanvasDrop, CanvasModel, CanvasRect} from '@platform/ui';
import {layoutID, loopOwner, overlayOwner, repairTabs, setLayoutKind, ungroup, type LayoutKind} from '../page-layout';

type Document=Api.PageDocument;
type Section=Pick<Api.Section,'id'|'widget'|'configVersion'|'title'>;
export function canvasModel(document:Document,sections:Section[],labels:Record<string,string>={}):CanvasModel {
 const parents=new Map(Object.entries(document.nodes).flatMap(([id,n])=>(n.children??[]).map(child=>[child,id]as const))),indexed=new Map(sections.map(section=>[section.id,section]));
 return Object.fromEntries(Object.entries(document.nodes).map(([id,node])=>[id,{
  label:node.title||indexed.get(node.section)?.title||labels[indexed.get(node.section)?.widget??node.kind]||node.kind,
  kind:node.kind==='widget'?'widget':'container',children:node.children??[],
  parent:parents.get(id),
  horizontal:node.kind==='columns'||node.kind==='toolbar',layout:node.kind,fixed:!!node.slot,
 }]));
}
const parent=(d:Document,id:string)=>Object.entries(d.nodes).find(([,n])=>n.children?.includes(id))?.[0];
const subtree=(d:Document,id:string,seen=new Set<string>()):Set<string>=>{if(seen.has(id))return seen;seen.add(id);for(const c of [...d.nodes[id]?.children??[],...(d.unusedWidgets??[]).filter(e=>e.parent===id).map(e=>e.node)])subtree(d,c,seen);return seen;};
const scope=(d:Document,id:string)=>JSON.stringify([overlayOwner(d,id),loopOwner(d,id)]);
export function canvasUngroup(document:Document,id:string,sections:Section[]):Document|undefined {
 const n=document.nodes[id];if(!n||n.kind==='widget'||n.kind==='loop'||n.slot||!parent(document,id))return;
 const next=ungroup(document,id);if(pageLayoutDiagnostics(next,sections).length)return;return next;
}
/** Remove one owned fragment; outside references remain visible draft diagnostics. */
export function canvasRemove(document:Document,id:string):{document:Document;sections:Set<string>}|undefined {
 const n=document.nodes[id],p=parent(document,id)??document.unusedWidgets?.find(e=>e.node===id)?.parent;
 if(!n||!p||n.slot||id===document.root||Object.values(document.overlays??{}).some(o=>o.root===id))return;
 const next=structuredClone(document),nodes=subtree(next,id),sections=new Set<string>();
 for(const key of nodes){if(next.nodes[key]?.section)sections.add(next.nodes[key]!.section!);delete next.nodes[key];}
 next.nodes[p]!.children=next.nodes[p]!.children?.filter(child=>child!==id);
 next.unusedWidgets=next.unusedWidgets?.filter(e=>!nodes.has(e.node)&&!nodes.has(e.parent));
 next.events=next.events?.filter(e=>!sections.has(e.source));
 for(const [key,v]of Object.entries(next.variables??{}))if(v.scope==='loop-item'&&nodes.has(v.owner??''))delete next.variables![key];
 for(const [key,q]of Object.entries(next.queries??{}))if(nodes.has(q.itemOwner??''))delete next.queries![key];
 return {document:clean(next),sections};
}
function clean(d:Document){
 const roots=new Set([d.root,...Object.values(d.overlays??{}).map(o=>o.root)]);
 let changed=true;
 while(changed){changed=false;for(const [id,n]of Object.entries(d.nodes)){
  if(roots.has(id)||n.kind==='widget'||n.kind==='loop'||n.slot||n.children?.length||d.unusedWidgets?.some(e=>e.parent===id))continue;
  const p=parent(d,id);if(!p)continue;d.nodes[p]!.children=d.nodes[p]!.children!.filter(c=>c!==id);delete d.nodes[id];changed=true;
 }}
 return repairTabs(d);
}
/** Source insert/into/swap/wrap semantics, applied to the original PageDocument.
 * Scoped fragments and declared slot roots never escape their existing owner. */
export function canvasDrop(document:Document,id:string,target:CanvasDrop,sections:Section[]):Document|undefined {
 const node=document.nodes[id],from=parent(document,id);
 if(!node||!from||node.slot)return;
 const next=structuredClone(document),owned=subtree(document,id);
 const detach=()=>{next.nodes[from]!.children=next.nodes[from]!.children!.filter(c=>c!==id);};
 if(target.kind==='insert'||target.kind==='into'){
  const to=target.parent;if(!next.nodes[to]||next.nodes[to]!.kind==='widget'||owned.has(to))return;
  const oldIndex=next.nodes[from]!.children!.indexOf(id),index=target.kind==='into'?next.nodes[to]!.children?.length??0:target.index;
  if(!Number.isInteger(index)||index<0||index>(next.nodes[to]!.children?.length??0))return;
  detach();const children=next.nodes[to]!.children??[];children.splice(from===to&&oldIndex<index?index-1:index,0,id);next.nodes[to]!.children=children;
 }else{
  const other=document.nodes[target.target],to=parent(document,target.target);
  if(!other||!to||other.slot||owned.has(target.target)||subtree(document,target.target).has(id))return;
  if(target.kind==='swap'){
   const ai=next.nodes[from]!.children!.indexOf(id),bi=next.nodes[to]!.children!.indexOf(target.target);
   next.nodes[from]!.children![ai]=target.target;next.nodes[to]!.children![bi]=id;
  }else{
   detach();const group=layoutID('group'),horizontal=target.side==='left'||target.side==='right',before=target.side==='left'||target.side==='top';
   next.nodes[group]={kind:horizontal?'columns':'rows',size:structuredClone(other.size),children:before?[id,target.target]:[target.target,id]};
   const at=next.nodes[to]!.children!.indexOf(target.target);next.nodes[to]!.children![at]=group;
   // The new group occupies the former target region; its children now have a new main axis.
   next.nodes[target.target]!.size=undefined;
   if(next.nodes[id]!.size?.weight!==undefined){const size={...next.nodes[id]!.size};delete size.weight;next.nodes[id]!.size=size;}
  }
 }
 for(const candidate of [id,...(target.kind==='swap'?[target.target]:[])])if(scope(document,candidate)!==scope(next,candidate))return;
 const result=clean(next);
 if(Object.keys(result.nodes).length>256||pageLayoutDiagnostics(result,sections).length)return;
 return result;
}
export function canvasMove(document:Document,id:string,delta:-1|1,sections:Section[]):Document|undefined {
 const p=parent(document,id);if(!p)return;const children=document.nodes[p]!.children??[],at=children.indexOf(id),to=at+delta;
 if(at<0||to<0||to>=children.length)return;
 return canvasDrop(document,id,{kind:'swap',target:children[to]!},sections);
}
/** Pixel resize preserves the measured adjacent pair and original size bounds. */
export function canvasResize(document:Document,id:string,axis:'width'|'height',pixels:number,rects:Record<string,CanvasRect>):Record<string,Api.PageLayoutSize>|undefined {
 const n=document.nodes[id],p=parent(document,id);if(!n||!Number.isFinite(pixels))return;
 const min=axis==='width'?'minWidth':'minHeight',max=axis==='width'?'maxWidth':'maxHeight';
 const bound=(node:Api.PageLayoutNode)=>[node.size?.[min]??pageUIManifest.layout.minSize,node.size?.[max]??pageUIManifest.layout.maxSize];
 const [lo,hi]=bound(n);
 if(!Number.isFinite(lo)||!Number.isFinite(hi)||lo!>hi!)return;
 const candidates=Object.entries(rects).filter(([key])=>key!==id&&parent(document,key)===p).map(([,r])=>r[axis]);
 const size={...n.size,[axis]:snapDimension(pixels,lo!,hi!,8,candidates).value};
 if(axis==='height'){size.scroll='auto';if(document.nodes[p??'']?.kind==='rows')delete size.weight;return {[id]:size};}
 if(document.nodes[p??'']?.kind==='columns')delete size.weight;
 const children=document.nodes[p??'']?.children??[],at=children.indexOf(id),other=children[at+1]??children[at-1],a=rects[id],b=rects[other??''];
 if(document.nodes[p??'']?.kind==='columns'&&a&&b&&Math.abs(a.y-b.y)<4&&Math.abs(a.x-b.x)>4){
  const pair=a.width+b.width,[blo,bhi]=bound(document.nodes[other!]!),low=Math.max(lo!,pair-bhi!),high=Math.min(hi!,pair-blo!);
  if(!Number.isFinite(low)||!Number.isFinite(high)||low>high)return;size.width=snapDimension(pixels,low,high,8,candidates).value;const sibling={...document.nodes[other!]!.size,width:Math.round(pair-size.width)};delete sibling.weight;
  return {[id]:size,[other!]:sibling};
 }
 return {[id]:size};
}
export function canvasResetSize(document:Document,id:string,axis:'width'|'height'):Record<string,Api.PageLayoutSize>|undefined {
 if(!document.nodes[id])return;const size={...document.nodes[id]!.size};delete size[axis];
 if(axis==='height'&&size.maxHeight===undefined)delete size.scroll;
 const result:Record<string,Api.PageLayoutSize>={[id]:size};
 if(axis==='width'){
  const p=parent(document,id),children=document.nodes[p??'']?.children??[],at=children.indexOf(id),other=children[at+1]??children[at-1];
  if(document.nodes[p??'']?.kind==='columns'&&other){size.weight=1;const sibling={...document.nodes[other]!.size,weight:1};delete sibling.width;result[other]=sibling;}
 }
 return result;
}

/** Wrap exactly the selected node; preserve its occupied region on the group. */
export function canvasGroup(document:Document,id:string,kind:LayoutKind,sections:Section[]):{document:Document;id:string}|undefined {
 const n=document.nodes[id],p=parent(document,id);if(!n||!p||n.slot)return;
 const next=structuredClone(document),group=layoutID('group');
 next.nodes[group]={kind:'rows',children:[id],size:structuredClone(n.size)};next.nodes[id]!.size=undefined;
 next.nodes[p]!.children=next.nodes[p]!.children!.map(child=>child===id?group:child);
 const converted=setLayoutKind(next,group,kind);converted.uiProfile=document.uiProfile;
 if(pageLayoutDiagnostics(converted,sections).length)return;
 return {document:converted,id:group};
}
/** Equal shares are an explicit layout command; historical sizing stays untouched. */
export function canvasEqualize(document:Document,id:string,sections:Section[]):Document|undefined {
 const n=document.nodes[id];if(!n||!['rows','columns'].includes(n.kind)||!n.children?.length)return;
 const next=structuredClone(document);
 for(const child of n.children){const size={...next.nodes[child]!.size};size.weight=1;if(n.kind==='columns')delete size.width;else delete size.height;next.nodes[child]!.size=Object.keys(size).length?size:undefined;}
 if(pageLayoutDiagnostics(next,sections).length)return;return next;
}
