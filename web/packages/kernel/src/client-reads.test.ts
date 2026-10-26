import {afterEach,expect,test,vi} from "vitest";
import {EdgeClient} from "./client";

const connection={server:"https://host.invalid",token:"session",tenant:"tenant",principal:"member"},path="/v1/records/sample.item/A";
const deferred=<T>()=>{let resolve!:(value:T)=>void;const promise=new Promise<T>(done=>{resolve=done;});return {promise,resolve};};
afterEach(()=>vi.unstubAllGlobals());

test("concurrent authorized reads share transport only while in flight",async()=>{
 const client=new EdgeClient(connection),first=deferred<Response>(),fetcher=vi.fn().mockReturnValueOnce(first.promise).mockResolvedValueOnce(new Response('{"revision":2}'));vi.stubGlobal("fetch",fetcher);
 const readers=Array.from({length:20},()=>client.get(path));expect(fetcher).toHaveBeenCalledTimes(1);
 first.resolve(new Response('{"revision":1}'));expect(await Promise.all(readers)).toEqual(Array(20).fill({revision:1}));
 expect(await client.get(path)).toEqual({revision:2});expect(fetcher).toHaveBeenCalledTimes(2);
});

test("a shared refusal is released and never becomes a permanent cached title",async()=>{
 const client=new EdgeClient(connection),first=deferred<Response>(),fetcher=vi.fn().mockReturnValueOnce(first.promise).mockResolvedValueOnce(new Response('{"name":"Current"}'));vi.stubGlobal("fetch",fetcher);
 const readers=[client.get(path),client.get(path)],answers=Promise.allSettled(readers);first.resolve(new Response("denied",{status:403}));
 expect((await answers).every(answer=>answer.status==="rejected"&&String(answer.reason).includes("HTTP 403"))).toBe(true);expect(fetcher).toHaveBeenCalledTimes(1);
 expect(await client.get(path)).toEqual({name:"Current"});expect(fetcher).toHaveBeenCalledTimes(2);
});

test("another identity, token, language or read revision never joins an older request",async()=>{
 const client=new EdgeClient(connection),pending:ReturnType<typeof deferred<Response>>[]=[],fetcher=vi.fn(()=>{const request=deferred<Response>();pending.push(request);return request.promise;});vi.stubGlobal("fetch",fetcher);vi.stubGlobal("document",{documentElement:{lang:"en"}});
 const readers=[client.get(path),new EdgeClient({...connection,principal:"other"}).get(path)];
 client.connection.token="renewed";readers.push(client.get(path));
 vi.stubGlobal("document",{documentElement:{lang:"zh-CN"}});readers.push(client.get(path));
 client.refreshLiveReads();readers.push(client.get(path));
 expect(fetcher).toHaveBeenCalledTimes(5);pending.forEach((request,index)=>request.resolve(new Response(JSON.stringify({index}))));
 expect(await Promise.all(readers)).toEqual(Array.from({length:5},(_,index)=>({index})));
});

test("fresh reads remain independent and closing an identity aborts its shared read",async()=>{
 const client=new EdgeClient(connection),signals:AbortSignal[]=[],fetcher=vi.fn((_url:string,options:RequestInit)=>new Promise<Response>((_resolve,reject)=>{signals.push(options.signal!);options.signal!.addEventListener("abort",()=>reject(Error("aborted")),{once:true});}));vi.stubGlobal("fetch",fetcher);
 const readers=[client.get(path),client.get(path),client.get(path,true)],answers=Promise.allSettled(readers);expect(fetcher).toHaveBeenCalledTimes(2);
 client.close();expect(signals.every(signal=>signal.aborted)).toBe(true);expect((await answers).every(answer=>answer.status==="rejected"&&String(answer.reason).includes("identity changed"))).toBe(true);
 await expect(client.get(path)).rejects.toThrow("identity changed");expect(fetcher).toHaveBeenCalledTimes(2);
});

test("anonymous discovery can restart after a development effect cleanup",async()=>{
 const client=new EdgeClient({...connection,tenant:"",principal:""});client.close();vi.stubGlobal("fetch",vi.fn(async()=>new Response('{"principalId":"member","tenantId":"tenant"}')));
 expect(await client.get("/v1/me")).toEqual({principalId:"member",tenantId:"tenant"});client.bindIdentity("tenant","member");
});

test("an unanswered save survives reopening and retries the original payload and key",async()=>{
 const saved=new Map<string,string>(),writes=vi.fn((key:string,value:string)=>saved.set(key,value));
 vi.stubGlobal("localStorage",{getItem:(key:string)=>saved.get(key)??null,setItem:writes});vi.stubGlobal("navigator",{onLine:true});
 const client=new EdgeClient(connection),declaration={tenantId:"tenant",dataClass:"sample.item",authorityId:"sample",kind:"AUTHORITY_KIND_TENANT_SERVER" as const};
 client.authorities.refresh([declaration]);
 const key=client.draft("sample.item.edit",{type:"sample.item",id:"A"},{contact:null},[],4);
 const transport=vi.fn().mockRejectedValueOnce(Error("lost reply")).mockResolvedValueOnce(new Response('{"record":{"changeId":"chg-1"}}'));vi.stubGlobal("fetch",transport);
 await client.send(key);
 expect(client.authorities.outbox[0]?.state).toBe("SUBMISSION_STATE_UNKNOWN");
 const reopened=new EdgeClient(connection);await reopened.send(key);
 expect(reopened.authorities.outbox).toHaveLength(1);expect(reopened.authorities.outbox[0]?.state).toBe("SUBMISSION_STATE_CONFIRMED");
 expect(transport.mock.calls[0]?.[1].body).toBe(transport.mock.calls[1]?.[1].body);
 expect(JSON.parse(transport.mock.calls[1]?.[1].body).idempotencyKey).toBe(key);
 expect(saved.size).toBe(1);
});
