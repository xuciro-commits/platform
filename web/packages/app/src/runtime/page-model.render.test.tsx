import {act,cleanup,renderHook} from '@testing-library/react';
import {afterEach,expect,test,vi} from 'vitest';
import type {Api} from '@platform/kernel';
import type {RecordSource,RecordView} from '@platform/ui';
import type {WidgetBindingContext} from '../widgets/bindings';
import {PageSessionStore} from './Session';
import {usePageModel} from './PageModel';

const host=vi.hoisted(()=>({client:{get:vi.fn()},definitions:[] as Api.Definition[]}));
vi.mock('../index',()=>({useHost:()=>host}));
afterEach(()=>{cleanup();vi.useRealTimers();host.client.get.mockReset();});
const stamp={by:'member',at:'2026-10-26T00:00:00Z'};
const view=(record:Record<string,unknown>&{id:string;revision:number}):RecordView=>({record:{...record,created:stamp,changed:stamp},history:[],following:false,tasks:[],approvals:[],files:[],comments:[],related:[],linked:[],activity:[],processes:[]});

function fixture(){
 const source:RecordSource={scope:'member',entity:type=>({type,app:'sample',title:type,plural:type,display:'id',standard:[],fields:type==='sample.order'?[{name:'supplier',title:'Supplier',type:'reference',ref:'sample.supplier'}]:type==='sample.supplier'?[{name:'name',title:'Name',type:'text'},{name:'country',title:'Country',type:'reference',ref:'sample.country'}]:[{name:'title',title:'Title',type:'text'}]}),get:vi.fn(async(type,id)=>view({id,revision:1,...type==='sample.supplier'?{name:'Supplier',country:'country'}:{title:'Country'}})),list:vi.fn()};
 const session=new PageSessionStore(source,{objects:new Map(),children:new Map(),queryParents:new Map()});
 const section={id:'orders',widget:'table',configVersion:1,model:{type:'sample.order',fields:[{path:['supplier','name']},{path:['supplier','country','title']}]}};
 const page={name:'orders',title:'Orders',object:{app:'sample',kind:'object',name:'sample.order'},sections:[section]};
 const bound={section,page,session,narrowed:{},modelDraft:true} as unknown as WidgetBindingContext;
 const answer={records:[{id:'one',revision:1,supplier:'supplier'},{id:'two',revision:1,supplier:'supplier'}],total:2,fields:[{name:'supplier.name'},{name:'supplier.country.title'}]};
 return {source,session,bound,answer};
}

test('rapid model searches send only the latest query and retain pending feedback',async()=>{
 vi.useFakeTimers();const {source,session,bound,answer}=fixture();host.client.get.mockResolvedValue(answer);
 const {result}=renderHook(()=>usePageModel(bound,source));
 await act(()=>vi.advanceTimersByTimeAsync(0));expect(host.client.get).toHaveBeenCalledTimes(1);host.client.get.mockClear();
 act(()=>result.current.bound.window!.onChange({search:'a',offset:0}));
 await act(()=>vi.advanceTimersByTimeAsync(100));
 act(()=>result.current.bound.window!.onChange({search:'ab',offset:0}));
 await act(()=>vi.advanceTimersByTimeAsync(100));
 expect(host.client.get).not.toHaveBeenCalled();expect(result.current.status.status).toBe('pending');
 await act(()=>vi.advanceTimersByTimeAsync(50));
 expect(host.client.get).toHaveBeenCalledTimes(1);
 expect(JSON.parse(host.client.get.mock.calls[0]![0].split('\n')[1]).search).toBe('ab');
 expect(result.current.data?.records).toHaveLength(2);session.dispose();
});

test('continuous live revisions refresh model windows without waiting for a typing pause',async()=>{
 vi.useFakeTimers();const {source,session,bound,answer}=fixture();host.client.get.mockResolvedValue(answer);
 const {result,rerender}=renderHook(({source})=>usePageModel(bound,source),{initialProps:{source}});
 await act(()=>vi.advanceTimersByTimeAsync(0));
 for(let revision=1;revision<=3;revision++){
  const next={...source,revision};act(()=>session.updateSource(next));rerender({source:next});
  await act(()=>vi.advanceTimersByTimeAsync(50));expect(result.current.data?.records).toHaveLength(2);
 }
 expect(host.client.get).toHaveBeenCalledTimes(4);session.dispose();
});

test('model relationship fields share reads within one window and refresh under a new identity',async()=>{
 vi.useFakeTimers();const {source,session,bound,answer}=fixture();host.client.get.mockResolvedValue(answer);
 const {result,rerender}=renderHook(({source})=>usePageModel(bound,source),{initialProps:{source}});
 await act(()=>vi.advanceTimersByTimeAsync(150));
 expect(source.get).toHaveBeenCalledTimes(2);
 expect(result.current.data?.records[0]).toMatchObject({'supplier.name':'Supplier','supplier.country.title':'Country'});
 const next={...source,scope:'next-member',get:vi.fn(async(type:string,id:string)=>view({id,revision:1,...type==='sample.supplier'?{name:'Next supplier',country:'country'}:{title:'Next country'}}))};
 act(()=>session.updateSource(next));rerender({source:next});
 expect(result.current.data).toBeUndefined();
 await act(()=>vi.advanceTimersByTimeAsync(150));
 expect(next.get).toHaveBeenCalledTimes(2);
 expect(result.current.data?.records[0]).toMatchObject({'supplier.name':'Next supplier','supplier.country.title':'Next country'});session.dispose();
});

test('a failed relationship read does not display a partial model result',async()=>{
 vi.useFakeTimers();const {source,session,bound,answer}=fixture();host.client.get.mockResolvedValue(answer);
 vi.mocked(source.get).mockRejectedValue(Error('Denied'));
 const {result}=renderHook(()=>usePageModel(bound,source));
 await act(()=>vi.advanceTimersByTimeAsync(150));
 expect(result.current.status.status).toBe('error');expect(result.current.data).toBeUndefined();session.dispose();
});
