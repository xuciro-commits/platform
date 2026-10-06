import type {WorkflowDraft} from "./workflow-model";

/** The exact owner inputs; record stamps and published history are not edits. */
export function workflowInputs(draft:WorkflowDraft){
 const {name,title,kind,object,when,manual,input,inputSchema,steps,layout}=draft;
 return {name,title,...(kind?{kind}:{}),object,when,manual:!!manual,input:input??{},inputSchema,steps,layout:layout??{}};
}

function canonical(value:unknown):unknown{
 if(Array.isArray(value))return value.map(canonical);
 if(value&&typeof value==="object")return Object.fromEntries(Object.entries(value).filter(([,v])=>v!==undefined).sort(([a],[b])=>a.localeCompare(b)).map(([k,v])=>[k,canonical(v)]));
 return value;
}

/** Saving a changed draft does not turn it into the installed flow version. */
export function workflowRunMatches(draft:WorkflowDraft,installed:WorkflowDraft|undefined,run:{flow:string;version:number;withheld?:boolean}|undefined,dirty:boolean){
 return !!installed&&!!run&&!dirty&&!run.withheld&&run.flow===`build.${installed.name}`&&run.version===installed.version&&JSON.stringify(canonical(workflowInputs(draft)))===JSON.stringify(canonical(workflowInputs(installed)));
}
