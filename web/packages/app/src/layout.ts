import {pageUIManifest,type Api} from "@platform/kernel";

/** Application API diagnostics; the host remains the save/publish authority. */
export function pageLayoutDiagnostics(document:Api.PageDocument):{node:string;code:string}[] {
 const limits=pageUIManifest.layout,issues:{node:string;code:string}[]=[];
 const parents=new Map<string,string>();
 for(const [id,n]of Object.entries(document.nodes))for(const child of n.children??[])parents.set(child,id);
 const bounded=(id:string,seen=new Set<string>()):boolean=>{
  if(seen.has(id))return false;seen.add(id);
  const n=document.nodes[id],p=parents.get(id),parent=p&&document.nodes[p];
  return !!n&&(n.size?.height!==undefined||!!parent&&!!p&&(parent.kind==="columns"||parent.kind==="rows"&&n.size?.weight!==undefined)&&bounded(p,seen));
 };
 for(const [id,n]of Object.entries(document.nodes)){
  const s=n.size,p=parents.get(id),parent=p?document.nodes[p]:undefined;
  const fail=(code:string)=>issues.push({node:id,code});
  if((s||n.gap!==undefined)&&Number(document.uiProfile.split(".").at(-1))<Number(limits.requiredUIProfile.split(".").at(-1)))fail("Layout sizing needs a newer page profile.");
  if(n.gap!==undefined&&(!["rows","columns"].includes(n.kind)||!Number.isInteger(n.gap)||n.gap<0||n.gap>limits.maxGap))fail("Layout gap is outside its container budget.");
  if(!s)continue;
  for(const key of ["width","height","minWidth","maxWidth","minHeight","maxHeight"]as const){const v=s[key];if(v!==undefined&&(!Number.isInteger(v)||v<limits.minSize||v>limits.maxSize))fail("Layout dimension is outside its size budget.");}
  for(const [fixed,min,max]of [[s.width,s.minWidth,s.maxWidth],[s.height,s.minHeight,s.maxHeight]])if(min!==undefined&&max!==undefined&&min>max||fixed!==undefined&&(min!==undefined&&fixed<min||max!==undefined&&fixed>max))fail("Fixed, minimum and maximum dimensions disagree.");
  if(s.weight!==undefined){
   if(!parent||!["rows","columns"].includes(parent.kind)||!Number.isInteger(s.weight)||s.weight<1||s.weight>limits.maxWeight)fail("Layout weight needs a Rows or Columns parent.");
   if(parent?.kind==="columns"&&s.width!==undefined||parent?.kind==="rows"&&s.height!==undefined)fail("Layout weight conflicts with a fixed main-axis size.");
   if(parent?.kind==="rows"&&(!p||!bounded(p)))fail("Row weight needs a parent with a definite height.");
  }
  if(s.scroll&&!["visible","auto"].includes(s.scroll))fail("Layout scroll must be visible or auto.");
  if(s.scroll==="auto"&&!bounded(id)&&s.maxHeight===undefined)fail("Layout scroll needs a definite or maximum height.");
 }
 return issues;
}
