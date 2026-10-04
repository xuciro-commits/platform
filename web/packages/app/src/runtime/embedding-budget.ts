import type {Api} from "@platform/kernel";
export type PageCost={instances:number;queries:number;records:number;sections:number};
export function pageEmbeddingCost(page:Api.Page):PageCost {
 const d=page.document,owners=new Map<string,string|undefined>();const walk=(id:string,parent?:string)=>{if(owners.has(id))return;owners.set(id,parent);const node=d?.nodes[id],children=[...(node?.children??[]),...(d?.unusedWidgets??[]).filter(entry=>entry.parent===id).map(entry=>entry.node)];for(const child of children)walk(child,node?.kind==="loop"?id:parent);};if(d){walk(d.root);for(const o of Object.values(d.overlays??{}))walk(o.root);}
 const factor=(owner?:string)=>{let n=1,depth=0;while(owner){const node=d?.nodes[owner];if(!node?.loop||++depth>2)return Infinity;n*=node.loop.limit;owner=owners.get(owner);}return n;};
 return {instances:1,queries:Object.keys(d?.queries??{}).length,records:Object.values(d?.queries??{}).reduce((total,q)=>total+q.limit*factor(q.itemOwner),0),sections:page.sections?.length??0};
}
/** Reservations belong to mounted page instances, including repeated copies. */
export class PageEmbeddingBudget {
 private slots=new Map<object,PageCost>();
 private readonly limits:PageCost;
 constructor(limits:PageCost,root:PageCost){this.limits=limits;this.slots.set({},root);}
 reserve(owner:object,cost:PageCost):boolean {if(this.slots.has(owner))return true;const total={instances:0,queries:0,records:0,sections:0};for(const entry of [...this.slots.values(),cost])for(const key of Object.keys(total) as (keyof PageCost)[])total[key]+=entry[key];if((Object.keys(total) as (keyof PageCost)[]).some(key=>!Number.isSafeInteger(total[key])||total[key]>this.limits[key]))return false;this.slots.set(owner,cost);return true;}
 release(owner:object){this.slots.delete(owner);}
}
