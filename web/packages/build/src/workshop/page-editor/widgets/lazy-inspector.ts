import {lazy,type ComponentType} from "react";

export class InspectorLoadError extends Error {
 constructor(cause:unknown){super("Inspector module could not be loaded",{cause});this.name="InspectorLoadError";}
}

/** Keep registration cold; loading never reads or replaces the page draft. */
export function lazyInspector<Props extends object>(load:()=>Promise<ComponentType<Props>>){
 return lazy(async()=>{
  try{return {default:await load()};}
  catch(cause){throw new InspectorLoadError(cause);}
 });
}
