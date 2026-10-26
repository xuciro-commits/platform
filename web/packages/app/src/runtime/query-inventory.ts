import type {Api} from '@platform/kernel';
/** Same conservative declaration graph as the application API. Unknown IDs have
 * no edge; matching literals retain a read rather than suppressing one. */
export function activeQueryPlans(document:Api.PageDocument,sections:Api.Section[],inventoryProfile:string):Set<string>{
 const queries=document.queries??{};
 if(!document.root||!(Number((document.uiProfile??'').split('.').at(-1))>=Number(inventoryProfile.split('.').at(-1))))return new Set(Object.keys(queries));
 let active=new Set<string>();const seen=new Set<string>(),widgets=new Map(sections.map(s=>[s.id,s]));
 const scan=(value:unknown):void=>{if(typeof value==='string')visit(value);else if(Array.isArray(value))value.forEach(scan);else if(value&&typeof value==='object')Object.values(value).forEach(scan);};
 const visit=(id:string):void=>{if(seen.has(id))return;seen.add(id);if(queries[id]){active.add(id);scan(queries[id]);}if(document.variables?.[id])scan(document.variables[id]);if(document.nodes[id])scan(document.nodes[id]);if(widgets.has(id))scan(widgets.get(id));};
 visit(document.root);scan(document.overlays);scan(document.interface);scan(document.events);const main=active;active=new Set<string>();seen.clear();for(const entry of document.unusedWidgets??[])visit(entry.node);for(const id of Object.keys(queries))if(!active.has(id))main.add(id);return main;
}
export function queryInventoryBudget(document:Api.PageDocument,sections:Api.Section[],limits:{maxPlans:number;maxTotalLimit:number;inventoryUIProfile:string;maxDeclaredPlans:number;maxDeclaredTotalLimit:number}){
 const active=activeQueryPlans(document,sections,limits.inventoryUIProfile),queries=document.queries??{},modern=!!document.root&&Number((document.uiProfile??'').split('.').at(-1))>=Number(limits.inventoryUIProfile.split('.').at(-1));
 const parents=new Map<string,string>();for(const [id,node] of Object.entries(document.nodes))for(const child of node.children??[])parents.set(child,id);for(const entry of document.unusedWidgets??[])parents.set(entry.node,entry.parent);
 const factor=(id:string)=>{let n=1,owner=queries[id]?.itemOwner;const seen=new Set<string>();while(owner){if(seen.has(owner))return Infinity;seen.add(owner);const node=document.nodes[owner];if(node?.kind==='loop'){if(!node.loop?.limit)return Infinity;n*=node.loop.limit;}owner=parents.get(owner);}return n;};
 const total=Object.entries(queries).reduce((n,[id,q])=>n+(active.has(id)?q.limit*factor(id):0),0),declared=Object.entries(queries).reduce((n,[id,q])=>n+q.limit*factor(id),0);
 return {active,total,declared,valid:active.size<=limits.maxPlans&&total<=limits.maxTotalLimit&&Object.keys(queries).length<=(modern?limits.maxDeclaredPlans:limits.maxPlans)&&declared<=(modern?limits.maxDeclaredTotalLimit:limits.maxTotalLimit)};
}
