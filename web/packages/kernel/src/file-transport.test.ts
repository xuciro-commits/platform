import {afterEach,expect,test,vi} from "vitest";
import {EdgeClient} from "./client";
afterEach(()=>vi.unstubAllGlobals());
const client=()=>new EdgeClient({server:"https://test.invalid",token:"caller",tenant:"tenant",principal:"member"});
test("attachment reads keep caller authorization, encoded identity and abort signal within a byte budget",async()=>{
 const stop=new AbortController(),fetcher=vi.fn(async(url:string,options:RequestInit)=>{expect(url).toBe("https://test.invalid/v1/files/FILE%2F%3F");expect(options.headers).toMatchObject({Authorization:"Bearer caller","Platform-Tenant":"tenant"});expect(options.signal).toBe(stop.signal);return new Response("data",{headers:{"Content-Type":"text/plain","Content-Length":"4"}});});vi.stubGlobal("fetch",fetcher);const blob=await client().download("FILE/?",{signal:stop.signal,maxBytes:4});expect(await blob.text()).toBe("data");expect(blob.type).toBe("text/plain");
});
test("declared or streamed excess bytes stop before returning a preview blob",async()=>{
 let cancelled=false;vi.stubGlobal("fetch",async()=>new Response(new ReadableStream({cancel(){cancelled=true;}}),{headers:{"Content-Length":"100"}}));await expect(client().download("FILE",{maxBytes:4})).rejects.toThrow("budget");expect(cancelled).toBe(true);
 cancelled=false;vi.stubGlobal("fetch",async()=>new Response(new ReadableStream({start(controller){controller.enqueue(new Uint8Array([1,2,3]));controller.enqueue(new Uint8Array([4,5,6]));},cancel(){cancelled=true;}})));await expect(client().download("FILE",{maxBytes:4})).rejects.toThrow("budget");expect(cancelled).toBe(true);await expect(client().download("FILE",{maxBytes:-1})).rejects.toThrow("budget");
});
test("refused reads do not become empty files and uploads send the actual blob with cancellation",async()=>{
 vi.stubGlobal("fetch",async()=>new Response("Denied",{status:403}));await expect(client().download("FILE",{maxBytes:4})).rejects.toThrow("403");const file=new Blob(["data"],{type:"text/plain"}),stop=new AbortController();vi.stubGlobal("fetch",async(_url:string,options:RequestInit)=>{expect(options.body).toBe(file);expect(options.signal).toBe(stop.signal);return new Response(JSON.stringify({hash:"hash",size:4,name:"real.txt",contentType:"text/plain"}));});expect((await client().upload(file,"real.txt",{signal:stop.signal})).size).toBe(4);
});
