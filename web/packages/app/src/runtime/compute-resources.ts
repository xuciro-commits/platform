import type {Api} from '@platform/kernel';
import {parseDecimal} from './decimal';
import type {VariableResult} from './variables';

export function computeScalar(output:unknown,field:string|undefined,type:string):VariableResult {
 const value=field&&output&&typeof output==='object'&&!Array.isArray(output)?(output as Record<string,unknown>)[field]:field?undefined:output;
 if(typeof value!=='number'||!Number.isFinite(value)||Number.isInteger(value)&&!Number.isSafeInteger(value))return {status:'error',code:'The calculation result could not be read.'};
 if(type==='number')return {status:'value',value:{kind:'number',value}};
 const decimal=Number.isSafeInteger(value)?parseDecimal(String(value)):undefined;
 return decimal?{status:'value',value:decimal}:{status:'error',code:'The calculation result could not be read.'};
}
/** IDs, bindings and keys remain stable across an unanswered invocation. */
export function createComputeResourceReader(port:{describe:(binding:Api.AssetBinding)=>Promise<Api.CapabilityDescriptor>;invoke:(request:Api.CapabilityInvocation)=>Promise<Api.CapabilityResult>;read:(call:string)=>Promise<Api.OperationResult>},active:()=>boolean){
 const check=()=>{if(!active())throw Error('The calculation result could not be read.');};
 return {
  async start(c:Api.PageComputeResource,record:string,key:string){
   check();const descriptor=await port.describe(c.operation);check();
   if(descriptor.ref.app!==c.operation.ref.app||descriptor.ref.kind!=='compute'||descriptor.ref.name!==c.operation.ref.name||descriptor.version!==c.operation.sourceVersion)throw Error('The published calculation is unavailable.');
   const request={ref:c.operation.ref,version:descriptor.revision??0,key,inputs:{},bindings:c.inputs,record};
   const answer=await port.invoke(request);check();if(!answer.call||answer.ref.app!==request.ref.app||answer.ref.name!==request.ref.name||answer.ref.kind!=='compute')throw Error('The calculation result could not be read.');return answer.call;
  },
  async read(call:string){check();const result=await port.read(call);check();if(result.id!==call||!['pending','running','completed','failed','cancelled'].includes(result.state))throw Error('The calculation result could not be read.');return result;}
 };
}

type Plan={identity:string;ready:boolean;compute:Api.PageComputeResource;record:string};
type Entry={plan:Plan;key:string;started:boolean;state:'pending'|'value'|'error';output?:unknown;cancelWait?:()=>void};
/** Data re-confirmation hides fields without restarting an accepted operation. */
export class PageComputeStore {
 private entries=new Map<string,Entry>();private listeners=new Set<()=>void>();private version=0;private disposed=false;
 private port:(active:()=>boolean,identity:string)=>ReturnType<typeof createComputeResourceReader>;private makeKey:()=>string;
 constructor(port:(active:()=>boolean,identity:string)=>ReturnType<typeof createComputeResourceReader>,makeKey:()=>string=()=>crypto.randomUUID()){this.port=port;this.makeKey=makeKey;}
 subscribe=(listener:()=>void)=>{this.listeners.add(listener);return ()=>{this.listeners.delete(listener);}};
 snapshot=()=>this.version;
 private notify(){this.version++;for(const listener of this.listeners)listener();}
 reconcile(plans:Plan[]){this.disposed=false;const wanted=new Set(plans.map(p=>p.identity));for(const [id,e]of this.entries)if(!wanted.has(id)){this.entries.delete(id);e.cancelWait?.();}
  for(const p of plans){let e=this.entries.get(p.identity);if(!e){e={plan:p,key:this.makeKey(),started:false,state:'pending'};this.entries.set(p.identity,e);}e.plan=p;if(p.ready&&!e.started){e.started=true;void this.run(e);}}
 }
 private async run(e:Entry){const active=()=>!this.disposed&&this.entries.get(e.plan.identity)===e,reader=this.port(active,e.plan.identity);
  try{let call:string;try{call=await reader.start(e.plan.compute,e.plan.record,e.key);}catch{if(!active())return;call=await reader.start(e.plan.compute,e.plan.record,e.key);}while(active()){const result=await reader.read(call);if(result.state==='completed'){e.output=result.output;e.state='value';this.notify();return;}if(result.state==='failed'||result.state==='cancelled')throw Error('Failed calculation');await new Promise<void>(resolve=>{const timer=setTimeout(()=>{e.cancelWait=undefined;resolve();},1000);e.cancelWait=()=>{clearTimeout(timer);resolve();};});}}catch{if(active()){e.state='error';this.notify();}}
 }
 value(identity:string,field:string|undefined,type:string):VariableResult{const e=this.entries.get(identity);return !e||e.state==='pending'?{status:'pending'}:e.state==='error'?{status:'error',code:'The calculation result could not be read.'}:computeScalar(e.output,field,type);}
 dispose(){this.disposed=true;for(const e of this.entries.values())e.cancelWait?.();this.entries.clear();}
}
