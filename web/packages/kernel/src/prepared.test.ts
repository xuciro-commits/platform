import {afterEach,expect,test,vi} from "vitest";
import {EdgeClient,type Connection} from "./client";
import {PreparedWorkspace,preparedIdentity,rememberPreparedIdentity,type PreparedApplication} from "./prepared";

const connection:Connection={server:"https://workspace.invalid",token:"fixture-session",tenant:"hotel-next",principal:"desk"};
function setup(){
 const saved=new Map<string,string>();
 const storage={getItem:vi.fn((key:string)=>saved.get(key)??null),setItem:vi.fn((key:string,value:string)=>{saved.set(key,value);})};
 vi.stubGlobal("localStorage",storage);vi.stubGlobal("navigator",{onLine:true});return {saved,storage};
}
function application(id="reservations",connectionScope=connection):PreparedApplication{return {id,title:id,binding:{ref:{app:"build",kind:"app",name:id},sourceVersion:"1.application-1"},release:"release-one",at:"2026-10-22T10:00:00Z",route:`#/page?application=build:${id}`,reads:{"/v1/me":{tenantId:connectionScope.tenant,principalId:connectionScope.principal},"/v1/actions":[{schema:"build.reservation.create",target:"build.reservation",schemaVersion:{family:1,version:"original"}}],"/v1/records/build.reservation?limit=50":{records:[{id:"R1",state:"booked"}],total:1}}};}
afterEach(()=>vi.unstubAllGlobals());

test("prepared applications and local inputs reopen only in their original host tenant and member",()=>{
 setup();const workspace=new PreparedWorkspace(connection),app=application();workspace.prepare(app);
 const form={key:"new-reservation",application:app.id,binding:app.binding,schema:"build.reservation.create",schemaVersion:{family:1,version:"original"},target:{type:"build.reservation",id:"R2"},expectedRevision:0,values:{guest:"Local input"},at:"2026-10-22T10:01:00Z"};workspace.saveForm(form);
 const reopened=new PreparedWorkspace(connection);const {reads:_,...summary}=app;expect(reopened.current()).toEqual(summary);expect(reopened.form(form.key)).toEqual(form);
 const copy=reopened.read<{records:{state:string}[]}>("/v1/records/build.reservation?limit=50");copy.records[0]!.state="invented";
 expect(reopened.read("/v1/records/build.reservation?limit=50")).toEqual(app.reads["/v1/records/build.reservation?limit=50"]);
 for(const other of [{...connection,server:"https://other.invalid"},{...connection,tenant:"plant-next"},{...connection,principal:"another-desk"}]){
  const separate=new PreparedWorkspace(other);expect(separate.forms()).toEqual([]);expect(()=>separate.read("/v1/me")).toThrow("not prepared");
 }
 workspace.prepare(application("sales"));workspace.activate("reservations");expect(workspace.current()?.id).toBe("reservations");
 workspace.activate("unprepared");expect(()=>workspace.read("/v1/records/build.reservation?limit=50")).toThrow("not prepared");
 expect(workspace.form(form.key)).toEqual(form);
});

test("failed durable writes and malformed stored content retain the previous saved bytes",()=>{
 const {saved,storage}=setup(),workspace=new PreparedWorkspace(connection);workspace.prepare(application());const before=[...saved];
 storage.setItem.mockImplementationOnce(()=>{throw Error("disk full");});expect(()=>workspace.prepare(application("sales"))).toThrow("disk full");expect([...saved]).toEqual(before);expect(workspace.current()?.id).toBe("reservations");
 const key=[...saved.keys()][0]!;saved.set(key,'{"version":1,"scope":{}}');expect(()=>workspace.forms()).toThrow("could not be opened");expect(()=>workspace.prepare(application())).toThrow("could not be opened");expect(saved.get(key)).toBe('{"version":1,"scope":{}}');
});

test("preparation rejects another identity, writes, oversized content and a draft from another application version",()=>{
 const {saved}=setup(),workspace=new PreparedWorkspace(connection),app=application();
 expect(()=>workspace.prepare(application("foreign",{...connection,principal:"other"}))).toThrow("another workspace identity");
 expect(()=>workspace.prepare({...app,reads:{...app.reads,"/v1/submissions":{}}})).toThrow("readable task");
 expect(()=>workspace.prepare({...app,reads:{...app.reads,"/v1/records/build.reservation/R1":"x".repeat(2*1024*1024)}})).toThrow("2 MiB");expect(saved.size).toBe(0);
 workspace.prepare(app);expect(()=>workspace.saveForm({key:"wrong",application:app.id,binding:{...app.binding,sourceVersion:"1.application-2"},schema:"build.reservation.create",target:{type:"build.reservation",id:"R2"},values:{},at:app.at})).toThrow("exact application version");
 for(const schemaVersion of [{family:1,version:"changed"},{family:2,version:"original"}])expect(()=>workspace.saveForm({key:"wrong-action",application:app.id,binding:app.binding,schema:"build.reservation.create",schemaVersion,target:{type:"build.reservation",id:"R2"},values:{},at:app.at})).toThrow("exact application version");
 expect(workspace.forms()).toEqual([]);
});

test("the offline bootstrap index stores scope names without retaining a credential",async()=>{
 const {saved}=setup();new PreparedWorkspace(connection).prepare(application());await rememberPreparedIdentity(connection);
 const reopened=await preparedIdentity({...connection,tenant:"",principal:""});expect(reopened?.scope.principal).toBe("desk");
 expect(await preparedIdentity({...connection,token:"another-session",tenant:"",principal:""})).toBeUndefined();
 expect(JSON.stringify([...saved])).not.toContain(connection.token);
 const key=[...saved.keys()].find(key=>key.startsWith("prepared-identity:"))!;saved.set(key,'{"tenants":{}}');await expect(preparedIdentity(connection)).rejects.toThrow("identity could not be opened");expect(saved.get(key)).toBe('{"tenants":{}}');
});

test("the original client serves only prepared reads offline and keeps an actual server refusal authoritative",async()=>{
 setup();const app=application();new PreparedWorkspace(connection).prepare(app);await rememberPreparedIdentity(connection);
 const client=new EdgeClient({...connection,tenant:"",principal:""});vi.stubGlobal("navigator",{onLine:false});
 expect(await client.get("/v1/me")).toEqual(app.reads["/v1/me"]);client.bindIdentity(connection.tenant,connection.principal);
 expect(await client.get("/v1/records/build.reservation?limit=50")).toEqual(app.reads["/v1/records/build.reservation?limit=50"]);expect(client.usingPrepared).toBe(true);
 await expect(client.get("/v1/records/build.reservation/R2")).rejects.toThrow("not prepared");
 vi.stubGlobal("navigator",{onLine:true});vi.stubGlobal("fetch",vi.fn(async()=>new Response('{"error":"revoked"}',{status:403})));
 await expect(client.get("/v1/me")).rejects.toThrow("HTTP 403");
 vi.stubGlobal("fetch",vi.fn(async()=>{throw TypeError("network unavailable");}));expect(await client.get("/v1/me")).toEqual(app.reads["/v1/me"]);
 await expect(client.get("/v1/me",true)).rejects.toThrow("Connect to prepare");
});

test("a release change or identity switch during preparation cannot replace its previous context",async()=>{
 setup();const client=new EdgeClient(connection),app=application();client.prepared!.prepare(app);
 const base={...app,id:"sales",binding:{ref:{app:"build",kind:"app" as const,name:"sales"},sourceVersion:"1.application-1"}};
 vi.stubGlobal("fetch",vi.fn(async(url:string)=>new Response(JSON.stringify(url.endsWith("/v1/me")?app.reads["/v1/me"]:url.endsWith("/v1/definitions")?[{ref:base.binding.ref,version:base.binding.sourceVersion}]:url.endsWith("/v1/releases/active")?{id:"new-release"}:[]))));
 await expect(client.prepareApplication(base)).rejects.toThrow("release changed");expect(client.prepared!.current()?.id).toBe(app.id);
 vi.stubGlobal("fetch",vi.fn(async()=>{client.close();return new Response(JSON.stringify(app.reads["/v1/me"]));}));
 await expect(client.prepareApplication(base)).rejects.toThrow("identity changed");expect(client.prepared!.current()?.id).toBe(app.id);
});
