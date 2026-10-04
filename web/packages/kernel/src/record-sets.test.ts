import {afterEach,expect,test,vi} from "vitest";
import {EdgeClient} from "./client";

afterEach(()=>vi.unstubAllGlobals());
const client=()=>new EdgeClient({server:"https://test.invalid",token:"test",tenant:"t",principal:"m"});

test("set reads carry one complete typed expression and caller, with no operand downloads",async()=>{
 const query={set:{op:"subtract",inputs:[{search:"part"},{domain:[["qty","<",{kind:"decimal",value:"0.1"}]]}]},sort:["id"],offset:501,limit:2};
 const fetcher=vi.fn(async(url:string,options:RequestInit)=>{
  expect(url).toBe("https://test.invalid/v1/records/sample.note/query");expect(options.method).toBe("POST");expect(options.headers).toMatchObject({Authorization:"Bearer test","Platform-Tenant":"t","Content-Type":"application/json"});expect(JSON.parse(String(options.body))).toEqual(query);
  return new Response(JSON.stringify({records:[{id:"late"}],total:620}));
 });vi.stubGlobal("fetch",fetcher);
 expect(await client().records("sample.note",query)).toEqual({records:[{id:"late"}],total:620});expect(fetcher).toHaveBeenCalledTimes(1);
});

test("a refused set read does not retry as an ordinary or unconstrained query",async()=>{
 const fetcher=vi.fn().mockResolvedValue(new Response("Denied",{status:403}));vi.stubGlobal("fetch",fetcher);
 await expect(client().records("sample.note",{set:{op:"union",inputs:[{},{}]}})).rejects.toThrow("HTTP 403");expect(fetcher).toHaveBeenCalledTimes(1);
});

test("ordinary reads keep their original GET transport",async()=>{
 const fetcher=vi.fn(async(url:string,options:RequestInit)=>{expect(options.method).toBeUndefined();const parsed=new URL(url);expect(parsed.pathname).toBe("/v1/records/sample.note");expect(parsed.searchParams.get("search")).toBe("part");expect(parsed.searchParams.get("limit")).toBe("1");return new Response(JSON.stringify({records:[],total:0}));});vi.stubGlobal("fetch",fetcher);
 await client().records("sample.note",{search:"part",limit:1});expect(fetcher).toHaveBeenCalledTimes(1);
});

test("set aggregates post predicates and measures without a record window or operand downloads",async()=>{
 const query={set:{op:"intersect",inputs:[{domain:[["active","=",true]]},{search:"current"}]},groups:["bucket"],measures:["count"]};
 const fetcher=vi.fn(async(url:string,options:RequestInit)=>{expect(url).toBe("https://test.invalid/v1/aggregates/sample.note/query");expect(options.method).toBe("POST");expect(JSON.parse(String(options.body))).toEqual(query);expect(options.headers).toMatchObject({Authorization:"Bearer test","Platform-Tenant":"t"});return new Response(JSON.stringify({columns:[],rows:[]}));});vi.stubGlobal("fetch",fetcher);await client().aggregate("sample.note",query);expect(fetcher).toHaveBeenCalledTimes(1);
});

test("retained link traversal carries its exact identity and query once, and rejection never broadens the read",async()=>{
 const binding={ref:{app:"build",kind:"link-type" as const,name:"children"},sourceVersion:"1.link-1"},query={domain:[["state","=","open"]],limit:1,offset:1};
 const fetcher=vi.fn(async(url:string,options:RequestInit)=>{expect(url).toBe("https://test.invalid/v1/link-types/build/children/1.link-1/forward/parent%20one");expect(options.method).toBe("POST");expect(JSON.parse(String(options.body))).toEqual(query);return new Response("{}",{status:403});});vi.stubGlobal("fetch",fetcher);
 await expect(client().traverseLink(binding,"forward","parent one",query)).rejects.toThrow("403");expect(fetcher).toHaveBeenCalledTimes(1);
 await expect(client().traverseLink({...binding,sourceVersion:""},"forward","parent one",query)).rejects.toThrow("exact version");expect(fetcher).toHaveBeenCalledTimes(1);
});

test("a bounded direct aggregate posts its row budget and never retries as an unbounded GET",async()=>{
 const query={groups:["bucket"],measures:["count"],maxRows:4096};
 const fetcher=vi.fn(async(url:string,options:RequestInit)=>{expect(url).toBe("https://test.invalid/v1/aggregates/sample.note/query");expect(options.method).toBe("POST");expect(JSON.parse(String(options.body))).toEqual(query);return new Response("Budget refused",{status:400});});vi.stubGlobal("fetch",fetcher);
 await expect(client().aggregate("sample.note",query)).rejects.toThrow("HTTP 400");expect(fetcher).toHaveBeenCalledTimes(1);
});

test("a histogram request always posts its complete predicates and bin contract",async()=>{
 const query={histogram:{field:"pressure",bins:12},domain:[["status","=","active"]],search:"asset"};const fetcher=vi.fn(async(url:string,options:RequestInit)=>{expect(url).toBe("https://test.invalid/v1/aggregates/sample.note/query");expect(options.method).toBe("POST");expect(JSON.parse(String(options.body))).toEqual(query);return new Response(JSON.stringify({columns:[],rows:[],histogram:{}}));});vi.stubGlobal("fetch",fetcher);await client().aggregate("sample.note",query);expect(fetcher).toHaveBeenCalledTimes(1);
});
test("event-time window statistics post their explicit contract and never retry as a complete GET or record page",async()=>{
 const query={window:{timeField:"at",field:"value",rows:100000,threshold:"11.5"},domain:[["status","=","ready"]],search:"original"};
 const fetcher=vi.fn(async(url:string,options:RequestInit)=>{expect(url).toBe("https://test.invalid/v1/aggregates/sample.note/query");expect(options.method).toBe("POST");expect(JSON.parse(String(options.body))).toEqual(query);return new Response("Denied window",{status:403});});vi.stubGlobal("fetch",fetcher);await expect(client().aggregate("sample.note",query)).rejects.toThrow("403");expect(fetcher).toHaveBeenCalledTimes(1);
});
