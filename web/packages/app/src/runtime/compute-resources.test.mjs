import {registerHooks} from 'node:module';import assert from 'node:assert/strict';import test from 'node:test';
registerHooks({resolve(s,c,next){try{return next(s,c)}catch(e){if(s.startsWith('./')||s.startsWith('../'))return next(s+'.ts',c);throw e;}}});
const {PageComputeStore,computeScalar,createComputeResourceReader}=await import('./compute-resources.ts');
const c={operation:{ref:{app:'sample',kind:'compute',name:'score'},sourceVersion:'1'},recordVariable:'selected',inputs:{qty:{source:'subject',path:['qty']}}},tick=()=>new Promise(r=>setImmediate(r)),defer=()=>{let resolve;return {promise:new Promise(r=>resolve=r),resolve:v=>resolve(v)}};
test('one retained call supplies Gauge, Sparkline and exact integer Alert/Progress values across source re-confirmation',async()=>{
 const result=defer();let calls=0;const store=new PageComputeStore(()=>({start:async()=>{calls++;return 'call'},read:async()=>result.promise}),()=> 'original-key');const plan={identity:'A/revision1',ready:true,compute:c,record:'sample.note/A'};
 store.reconcile([plan,plan]);await tick();assert.equal(calls,1);store.reconcile([{...plan,ready:false}]);store.reconcile([plan]);assert.equal(calls,1);result.resolve({id:'call',state:'completed',output:73});await tick();
 assert.deepEqual(store.value(plan.identity,undefined,'number'),{status:'value',value:{kind:'number',value:73}});assert.deepEqual(store.value(plan.identity,undefined,'decimal'),{status:'value',value:{kind:'decimal',value:'73'}});store.dispose();
});
test('record or instance retirement excludes late values and later source revisions create one fresh invocation',async()=>{
 const held=defer();let calls=0;const store=new PageComputeStore(()=>({start:async()=>String(++calls),read:async id=>id==='1'?held.promise:{id,state:'completed',output:9}}),()=> 'key');store.reconcile([{identity:'A/1',ready:true,compute:c,record:'sample.note/A'}]);await tick();store.reconcile([{identity:'B/1',ready:true,compute:c,record:'sample.note/B'}]);await tick();held.resolve({id:'1',state:'completed',output:80});await tick();assert.equal(store.value('A/1',undefined,'number').status,'pending');assert.equal(store.value('B/1',undefined,'number').value.value,9);store.reconcile([{identity:'B/2',ready:true,compute:c,record:'sample.note/B'}]);await tick();assert.equal(calls,3);store.dispose();
});
test('unanswered invocation retries its exact key and pending unconfirmed records do not invoke',async()=>{
 const keys=[];const store=new PageComputeStore(()=>({start:async(_,__,key)=>{keys.push(key);if(keys.length===1)throw Error('response lost');return 'call'},read:async()=>({id:'call',state:'completed',output:1})}),()=> 'same-key');const p={identity:'A',ready:false,compute:c,record:'sample.note/A'};store.reconcile([p]);await tick();assert.equal(keys.length,0);store.reconcile([{...p,ready:true}]);await tick();assert.deepEqual(keys,['same-key','same-key']);store.dispose();
});
test('fixed version, call identity and current permission scope are checked before exposing an answer',async()=>{
 let active=true,invoked=0;const reader=createComputeResourceReader({describe:async()=>({ref:c.operation.ref,version:'2',revision:2}),invoke:async()=>{invoked++;return {ref:c.operation.ref,call:'call'}},read:async()=>({id:'other',state:'completed',output:1})},()=>active);
 await assert.rejects(reader.start(c,'sample.note/A','key'));assert.equal(invoked,0);await assert.rejects(reader.read('call'));active=false;await assert.rejects(reader.read('call'));
 for(const value of [undefined,null,Infinity,Number.MAX_SAFE_INTEGER+1])assert.equal(computeScalar(value,undefined,'number').status,'error');assert.equal(computeScalar(1.5,undefined,'decimal').status,'error');assert.equal(computeScalar({value:2},'missing','number').status,'error');
});
