import { afterEach, expect, test, vi } from "vitest";
import { LiveReads } from "./live";

afterEach(() => vi.useRealTimers());
test("visible leases share one stream, release hidden queries and install current snapshots before callbacks", async () => {
  vi.useFakeTimers();
  const connections: { paths: string[]; signal: AbortSignal; stream: ReadableStreamDefaultController<Uint8Array> }[] = [];
  const live = new LiveReads(async (paths, signal) => new Response(new ReadableStream({ start(stream) {
    connections.push({ paths, signal, stream });
    signal.addEventListener("abort", () => stream.error(Error("aborted")));
  } })));
  const stop = live.enable(), changed = vi.fn();
  const first = live.subscribe("/v1/records/sample.note", changed);
  const shared = live.subscribe("/v1/records/sample.note", changed);
  const second = live.subscribe("/v1/records/other.note", changed);
  await vi.advanceTimersByTimeAsync(60);
  expect(connections).toHaveLength(1);
  expect(connections[0]!.paths).toHaveLength(2);
  connections[0]!.stream.enqueue(new TextEncoder().encode('event: snapshot\ndata: {"sequence":1,"results":[{"path":"/v1/records/sample.note","status":200,"body":{"records":["original"]}}]}\n\n'));
  await vi.advanceTimersByTimeAsync(0);
  expect(live.read("/v1/records/sample.note")?.value).toEqual({ records: ["original"] });
  expect(changed).toHaveBeenCalledTimes(2);
  first(); expect(connections[0]!.signal.aborted).toBe(false);
  second(); await vi.advanceTimersByTimeAsync(60);
  expect(connections[0]!.signal.aborted).toBe(true);
  expect(connections[1]!.paths).toEqual(["/v1/records/sample.note"]);
  connections[1]!.stream.enqueue(new TextEncoder().encode('event: snapshot\ndata: {"sequence":2,"results":[{"path":"/v1/records/sample.note","status":403,"body":{"error":"denied"}}]}\n\n'));
  await vi.advanceTimersByTimeAsync(0);
  expect(() => live.read("/v1/records/sample.note")).toThrow("HTTP 403");
  shared(); expect(connections[1]!.signal.aborted).toBe(true);
  expect(live.read("/v1/records/sample.note")).toBeUndefined(); stop();
});

test("connection loss invalidates cached values and reconnect installs the current snapshot", async () => {
 vi.useFakeTimers();
 const streams: ReadableStreamDefaultController<Uint8Array>[]=[];
 const live=new LiveReads(async(_,signal)=>new Response(new ReadableStream({start(stream){streams.push(stream);signal.addEventListener("abort",()=>stream.error(Error("aborted")));}})));
 const stop=live.enable();const release=live.subscribe("/v1/me",()=>{});
 const push=(n:number)=>streams.at(-1)!.enqueue(new TextEncoder().encode(`event: snapshot\ndata: {"sequence":${n},"results":[{"path":"/v1/me","status":200,"body":{"revision":${n}}}]}\n\n`));
 await vi.advanceTimersByTimeAsync(60);push(1);await vi.advanceTimersByTimeAsync(0);expect(live.read("/v1/me")?.value).toEqual({revision:1});
 streams[0]!.error(Error("offline"));await vi.advanceTimersByTimeAsync(0);expect(()=>live.read("/v1/me")).toThrow("live connection unavailable");
 await vi.advanceTimersByTimeAsync(1000);expect(streams).toHaveLength(2);push(2);await vi.advanceTimersByTimeAsync(0);expect(live.read("/v1/me")?.value).toEqual({revision:2});
 release();stop();
});

test("inventory leases follow actual page membership and never return a partial inventory after a denied page", async () => {
 vi.useFakeTimers();
 const connections:{paths:string[];stream:ReadableStreamDefaultController<Uint8Array>;signal:AbortSignal}[]=[];
 const fetcher=vi.fn(async(url:string,init:RequestInit)=>new Response(new ReadableStream({start(stream){const paths=JSON.parse(new URL(url,"https://test.invalid").searchParams.get("watch")!);const signal=init.signal!;connections.push({paths,stream,signal});signal.addEventListener("abort",()=>stream.error(Error("aborted")));}})));
 vi.stubGlobal("fetch",fetcher);
 const {EdgeClient}=await import("./client");const client=new EdgeClient({server:"https://test.invalid",token:"token",tenant:"tenant",principal:"member"});
 const stop=client.enableLiveReads(),release=client.subscribeInventory("sample.note",1000,()=>{});
 const first="/v1/records/sample.note?limit=500&offset=0",second="/v1/records/sample.note?limit=500&offset=500";
 const push=(results:unknown[])=>connections.at(-1)!.stream.enqueue(new TextEncoder().encode(`event: snapshot\ndata: ${JSON.stringify({sequence:1,results})}\n\n`));
 await vi.advanceTimersByTimeAsync(60);expect(connections[0]!.paths).toEqual([first]);
 const rows=Array.from({length:500},(_,id)=>({id}));push([{path:first,status:200,body:{records:rows,total:600}}]);await vi.advanceTimersByTimeAsync(60);
 expect(connections.at(-1)!.paths).toEqual([first,second]);
 push([{path:first,status:200,body:{records:rows,total:600}},{path:second,status:403,body:{error:"denied"}}]);await vi.advanceTimersByTimeAsync(0);
 await expect(client.inventory("sample.note")).rejects.toThrow("HTTP 403");
 push([{path:first,status:200,body:{records:[{id:"retained"}],total:1}},{path:second,status:200,body:{records:[],total:1}}]);await vi.advanceTimersByTimeAsync(60);
 expect(connections.at(-1)!.paths).toEqual([first]);expect(await client.inventory("sample.note")).toEqual({records:[{id:"retained"}]});
 release();stop();vi.unstubAllGlobals();
});
