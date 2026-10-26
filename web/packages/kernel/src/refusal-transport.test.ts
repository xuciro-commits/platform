import {afterEach,expect,test,vi} from "vitest";
import {CompositeRefusalError,EdgeClient} from "./client";

afterEach(()=>vi.unstubAllGlobals());

test("composite transport distinguishes saved refusals from unknown outcomes without losing the original message",async()=>{
 const client=setup(),edits=[{schema:"build.object.archive",target:{type:"build.object",id:"draft"},payload:{}}];
 vi.stubGlobal("fetch",async()=>new Response(JSON.stringify({error:"edit refused",outcome:"refused"}),{status:409}));
 await expect(client.composite("original",edits)).rejects.toBeInstanceOf(CompositeRefusalError);
 await expect(client.composite("original",edits)).rejects.toThrow("edit refused");
 for(const response of [{error:"application answer unavailable"},{error:{message:"legacy conflict"}}]){
  vi.stubGlobal("fetch",async()=>new Response(JSON.stringify(response),{status:409}));
  try{await client.composite("original",edits);throw Error("composite unexpectedly succeeded");}catch(error){
   expect(error).toBeInstanceOf(Error);expect(error).not.toBeInstanceOf(CompositeRefusalError);
   expect((error as Error).message).toBe(typeof response.error==="string"?response.error:response.error.message);
  }
 }
 vi.stubGlobal("fetch",async()=>{throw TypeError("connection lost");});
 await expect(client.composite("original",edits)).rejects.toThrow("connection lost");
});

function setup() {
 const saved=new Map<string,string>();
 vi.stubGlobal("navigator",{onLine:true});
 vi.stubGlobal("localStorage",{getItem:(key:string)=>saved.get(key)??null,setItem:(key:string,value:string)=>saved.set(key,value)});
 const client=new EdgeClient({server:"https://test.invalid",token:"caller",tenant:"tenant",principal:"member"});
 client.authorities.declare({tenantId:"tenant",dataClass:"build.batch",authorityId:"build",kind:"AUTHORITY_KIND_TENANT_SERVER",epoch:1});
 return client;
}

test("original retries keep the saved key and changed inputs carry a durable correction link",async()=>{
 const client=setup(),bodies:string[]=[],headers:Headers[]=[];
 vi.stubGlobal("fetch",async(_url:string,options:RequestInit)=>{
  bodies.push(String(options.body));headers.push(new Headers(options.headers));
  return new Response(JSON.stringify({error:{code:"ERROR_CODE_INVALID_ARGUMENT",message:"Equipment is unavailable"}}),{status:400});
 });
 const original=client.draft("build.batch.start",{type:"build.batch",id:"B1"},{equipment:"E1"},[],2);
 await client.send();
 expect(client.authorities.outbox[0]?.state).toBe("SUBMISSION_STATE_REJECTED");
 await client.repeat(original);
 expect(bodies[1]).toBe(bodies[0]);
 expect(client.authorities.outbox).toHaveLength(1);
 expect(client.authorities.outbox[0]?.state).toBe("SUBMISSION_STATE_REJECTED");
 const correction=client.draft("build.batch.start",{type:"build.batch",id:"B1"},{equipment:"E2"},[],2,undefined,original);
 expect(correction).not.toBe(original);
 await client.send();
 expect(headers[2]?.get("Platform-Correction-Of")).toBe(original);
 expect(client.authorities.outbox[1]?.correctionOf).toBe(original);
 const restored=new EdgeClient(client.connection);
 expect(restored.authorities.outbox[1]?.correctionOf).toBe(original);
 expect(JSON.parse(bodies[2]!).idempotencyKey).toBe(correction);
});

test("failed or contradictory re-reads preserve the original terminal answer",async()=>{
 const client=setup();
 vi.stubGlobal("fetch",async()=>new Response(JSON.stringify({error:{code:"ERROR_CODE_INVALID_ARGUMENT",message:"Original refusal"}}),{status:400}));
 const original=client.draft("build.batch.start",{type:"build.batch",id:"B1"},{equipment:"E1"});
 await client.send();
 vi.stubGlobal("fetch",async()=>{throw Error("connection lost");});
 await expect(client.repeat(original)).rejects.toThrow("connection lost");
 expect(client.authorities.outbox[0]).toMatchObject({state:"SUBMISSION_STATE_REJECTED",outcome:"ERROR_CODE_INVALID_ARGUMENT",reason:"Original refusal"});
 vi.stubGlobal("fetch",async()=>new Response(JSON.stringify({record:{changeId:"unexpected-acceptance"}})));
 await expect(client.repeat(original)).rejects.toThrow("differs");
 expect(client.authorities.outbox[0]).toMatchObject({state:"SUBMISSION_STATE_REJECTED",outcome:"ERROR_CODE_INVALID_ARGUMENT",reason:"Original refusal"});
 expect(client.authorities.outbox).toHaveLength(1);
});
