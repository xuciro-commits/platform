import {useEffect,useRef,useState,useSyncExternalStore} from 'react';
import type {Api} from '@platform/kernel';
import {useHost,useInvokeCapability} from '../index';
import {recordResourceSlot} from './resources';
import type {PageSessionStore,PageSessionSnapshot} from './Session';
import type {VariableResult} from './variables';
import {PageComputeStore,createComputeResourceReader} from './compute-resources';
type Application={identity?:string;readScope?:string;resources:Record<string,VariableResult>;recordReferences:Record<string,{object:string;id:string}|undefined>};
export function usePageComputations(page:Api.Page,session:PageSessionStore,snapshot:PageSessionSnapshot,application:Application,live:boolean){
 const {client,source}=useHost(),invoke=useInvokeCapability(true),original=useRef({client,invoke}),admitted=useRef(new Set<string>());original.current={client,invoke};
 const [store]=useState(()=>new PageComputeStore((active,identity)=>createComputeResourceReader({describe:binding=>original.current.client.get<Api.CapabilityDescriptor>(`/v1/capabilities/${encodeURIComponent(binding.ref.app)}/compute/${encodeURIComponent(binding.ref.name)}?version=${Number(binding.sourceVersion.match(/\.compute-(\d+)$/)?.[1]??0)}`),invoke:request=>original.current.invoke(request),read:call=>original.current.client.get<Api.OperationResult>(`/v1/capabilities/calls/compute/${encodeURIComponent(call)}`)},()=>active()&&admitted.current.has(identity))));
 useSyncExternalStore(store.subscribe,store.snapshot,store.snapshot);
 const mounted=new Set<string>();const collect=(id:string)=>{const n=page.document?.nodes[id];if(n?.section)mounted.add(n.section);n?.children?.forEach(collect);};collect(page.document?.root??'');for(const overlay of Object.values(page.document?.overlays??{}))if(snapshot.scalars[overlay.openVariable]===true)collect(overlay.root);
 const used=new Set<string>();const add=(id:string)=>{if(used.has(id))return;used.add(id);const v=page.document?.variables?.[id];v?.expression?.args.forEach(a=>a.variable&&add(a.variable));if(v?.source?.variable)add(v.source.variable);};for(const s of page.sections??[])if(mounted.has(s.id??''))for(const value of Object.values(s))if(typeof value==='string')add(value);
 const plans=Object.entries(page.document?.variables??{}).filter(([,v])=>v.source?.kind==='compute'&&!!v.source.compute).map(([id,v])=>{
  const c=v.source!.compute!,slot=recordResourceSlot(page,c.recordVariable),read=slot?snapshot.records[slot]:undefined,record=slot?session.selected(slot):undefined,parent=page.document?.variables?.[c.recordVariable],shared=parent?.mode==='shared',appRef=shared?application.recordReferences[c.recordVariable]:undefined;
  const reference=read&&(read.status==='value'||read.status==='pending')?read.value:undefined;
  const eligible=live&&used.has(id)&&Number(page.document?.uiProfile.split('.').at(-1))>=100&&source.scope===session.snapshotScope()&&!!record&&!!reference&&(!shared||application.resources[c.recordVariable]?.status!=='error'&&appRef?.object===reference.object&&appRef?.id===reference.id);
  const ready=eligible&&!!slot&&!!session.confirmedSelected(slot)&&(!shared||application.resources[c.recordVariable]?.status==='value');
  const identity=JSON.stringify([source.scope,c.operation,c.inputs,c.recordVariable,reference,record?.revision,slot?session.recordBindingEpoch(slot):undefined,v.owner,v.owner?session.overlayEpoch(v.owner):undefined,shared?[application.identity,application.readScope]:undefined]);
  return {id,v,c,identity,record:reference?`${reference.object}/${reference.id}`:'',eligible,ready};});
 admitted.current=new Set(plans.filter(p=>p.eligible).map(p=>p.identity));
 const signature=JSON.stringify(plans.map(p=>[p.id,p.identity,p.eligible,p.ready]));
 useEffect(()=>{store.reconcile(plans.filter(p=>p.eligible).map(p=>({identity:p.identity,ready:p.ready,compute:p.c,record:p.record})));},[store,signature]);
 useEffect(()=>()=>store.dispose(),[store]);
 return Object.fromEntries(plans.map(p=>[p.id,p.ready?store.value(p.identity,p.c.outputField,p.v.type):p.eligible?{status:'pending'} as VariableResult:{status:'empty'} as VariableResult]));
}
