import { afterEach, expect, test, vi } from "vitest";
import { LiveReads } from "./live";

afterEach(() => vi.useRealTimers());

test("adding a detail lease preserves unchanged windows while changed data and denied reads still notify", async () => {
  vi.useFakeTimers();
  const streams: ReadableStreamDefaultController<Uint8Array>[] = [];
  const live = new LiveReads(async (_, signal) => new Response(new ReadableStream({ start(stream) {
    streams.push(stream); signal.addEventListener("abort", () => stream.error(Error("aborted")));
  } })));
  const stop = live.enable(), changed = vi.fn(), detail = vi.fn();
  const release = live.subscribe("/v1/records/sample.note", changed);
  const push = (results: unknown[]) => streams.at(-1)!.enqueue(new TextEncoder().encode(`event: snapshot\ndata: ${JSON.stringify({sequence:1,results})}\n\n`));
  const original = {path:"/v1/records/sample.note",status:200,body:{records:[{id:"A",revision:1}]}};
  await vi.advanceTimersByTimeAsync(60); push([original]); await vi.advanceTimersByTimeAsync(0);
  expect(changed).toHaveBeenCalledTimes(1);
  const releaseDetail = live.subscribe("/v1/records/sample.note/A", detail);
  await vi.advanceTimersByTimeAsync(60); push([original,{path:"/v1/records/sample.note/A",status:200,body:{record:{id:"A",revision:1}}}]); await vi.advanceTimersByTimeAsync(0);
  expect(changed).toHaveBeenCalledTimes(1); expect(detail).toHaveBeenCalledTimes(1);
  push([{...original,body:{records:[{id:"A",revision:2}]}}]); await vi.advanceTimersByTimeAsync(0);
  expect(changed).toHaveBeenCalledTimes(2);
  push([{...original,status:403,body:{error:"denied"}}]); await vi.advanceTimersByTimeAsync(0);
  expect(changed).toHaveBeenCalledTimes(3); expect(()=>live.read(original.path)).toThrow("HTTP 403");
  releaseDetail(); release(); stop();
});
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
 const changed=vi.fn(),stop=live.enable();const release=live.subscribe("/v1/me",changed);
 const push=(n:number)=>streams.at(-1)!.enqueue(new TextEncoder().encode(`event: snapshot\ndata: {"sequence":${n},"results":[{"path":"/v1/me","status":200,"body":{"revision":${n}}}]}\n\n`));
 await vi.advanceTimersByTimeAsync(60);push(1);await vi.advanceTimersByTimeAsync(0);expect(live.read("/v1/me")?.value).toEqual({revision:1});
 streams[0]!.error(Error("offline"));await vi.advanceTimersByTimeAsync(0);expect(()=>live.read("/v1/me")).toThrow("live connection unavailable");
 expect(changed).toHaveBeenCalledTimes(2);
 await vi.advanceTimersByTimeAsync(1000);expect(streams).toHaveLength(2);
 streams[1]!.error(Error("still offline"));await vi.advanceTimersByTimeAsync(0);expect(changed).toHaveBeenCalledTimes(2);
 await vi.advanceTimersByTimeAsync(2000);expect(streams).toHaveLength(3);push(2);await vi.advanceTimersByTimeAsync(0);expect(live.read("/v1/me")?.value).toEqual({revision:2});expect(changed).toHaveBeenCalledTimes(3);
 release();stop();
});

test("large workspaces split live registrations by the host's path and byte limits", async () => {
  vi.useFakeTimers();
  const connections: { paths: string[]; signal: AbortSignal; stream: ReadableStreamDefaultController<Uint8Array> }[] = [];
  const encoder = new TextEncoder();
  const live = new LiveReads(async (paths, signal) => {
    if (paths.length > 32 || encoder.encode(JSON.stringify(paths)).length > (64 << 10)) return new Response(null, { status: 400 });
    return new Response(new ReadableStream({ start(stream) {
      connections.push({ paths, signal, stream });
      signal.addEventListener("abort", () => stream.error(Error("aborted")));
    } }));
  });
  const paths = [
    ...Array.from({ length: 40 }, (_, n) => `/v1/records/build.asset${n}`),
    ...Array.from({ length: 12 }, (_, n) => `/v1/records/build.zone${n}?search=${"温".repeat(2300)}`),
  ].sort();
  const stop = live.enable(), changed = vi.fn();
  const releases = paths.map(path => live.subscribe(path, changed));
  await vi.advanceTimersByTimeAsync(60);
  expect(connections.length).toBe(3);
  expect(connections.flatMap(connection => connection.paths)).toEqual(paths);
  for (const connection of connections) {
    connection.stream.enqueue(encoder.encode(`event: snapshot\ndata: ${JSON.stringify({ sequence: 1, results: connection.paths.map(path => ({ path, status: 200, body: { value: path } })) })}\n\n`));
  }
  await vi.advanceTimersByTimeAsync(0);
  expect(changed).toHaveBeenCalledTimes(paths.length);
  expect(live.read(paths.at(-1)!)?.value).toEqual({ value: paths.at(-1) });
  releases.forEach(release => release());
  stop();
  expect(connections.every(connection => connection.signal.aborted)).toBe(true);
});

test("record lease changes retain metadata streams and still apply permission withdrawal", async () => {
  vi.useFakeTimers();
  const connections: { paths: string[]; signal: AbortSignal; stream: ReadableStreamDefaultController<Uint8Array> }[] = [];
  const live = new LiveReads(async (paths, signal) => new Response(new ReadableStream({ start(stream) {
    connections.push({ paths, signal, stream });
    signal.addEventListener("abort", () => stream.error(Error("aborted")));
  } })));
  const stop = live.enable(), changed = vi.fn();
  const releaseMetadata = live.subscribe("/v1/definitions", changed);
  let releaseRecord = live.subscribe("/v1/records/sample.note/A", () => {});
  await vi.advanceTimersByTimeAsync(60);
  const metadata = connections.find(connection => connection.paths.includes("/v1/definitions"))!;
  const push = (status: number, body: unknown) => metadata.stream.enqueue(new TextEncoder().encode(`event: snapshot\ndata: ${JSON.stringify({ sequence: 1, results: [{ path: "/v1/definitions", status, body }] })}\n\n`));
  push(200, [{ ref: "original" }]);
  await vi.advanceTimersByTimeAsync(0);
  for (const id of ["B", "C", "D"]) {
    releaseRecord();
    releaseRecord = live.subscribe(`/v1/records/sample.note/${id}`, () => {});
    await vi.advanceTimersByTimeAsync(60);
    expect(metadata.signal.aborted).toBe(false);
    expect(connections.filter(connection => !connection.signal.aborted)).toHaveLength(2);
  }
  expect(connections.filter(connection => connection.paths.includes("/v1/definitions"))).toHaveLength(1);
  expect(live.read("/v1/definitions")?.value).toEqual([{ ref: "original" }]);
  push(403, { error: "denied" });
  await vi.advanceTimersByTimeAsync(0);
  expect(() => live.read("/v1/definitions")).toThrow("HTTP 403");
  expect(changed).toHaveBeenCalledTimes(2);
  releaseRecord(); releaseMetadata(); stop();
  expect(connections.every(connection => connection.signal.aborted)).toBe(true);
});

test("an oversized read fails independently while valid metadata stays connected", async () => {
  vi.useFakeTimers();
  const connect = vi.fn(async (_paths: string[], signal: AbortSignal) => new Response(new ReadableStream({ start(stream) {
    signal.addEventListener("abort", () => stream.error(Error("aborted")));
  } })));
  const live = new LiveReads(connect), stop = live.enable();
  const releaseMetadata = live.subscribe("/v1/definitions", () => {});
  const oversized = `/v1/records/sample.note?search=${"温".repeat(23000)}`;
  const releaseOversized = live.subscribe(oversized, () => {});
  await vi.advanceTimersByTimeAsync(60);
  expect(connect).toHaveBeenCalledTimes(1);
  expect(connect.mock.calls[0]![0]).toEqual(["/v1/definitions"]);
  expect(connect.mock.calls[0]![1].aborted).toBe(false);
  expect(() => live.read(oversized)).toThrow("HTTP 413");
  releaseOversized(); releaseMetadata(); stop();
});

test("explicit refresh clears snapshots and replaces retained connections", async () => {
  vi.useFakeTimers();
  const connections: { signal: AbortSignal; stream: ReadableStreamDefaultController<Uint8Array> }[] = [];
  const live = new LiveReads(async (_paths, signal) => new Response(new ReadableStream({ start(stream) {
    connections.push({ signal, stream });
    signal.addEventListener("abort", () => stream.error(Error("aborted")));
  } })));
  const stop = live.enable(), changed = vi.fn();
  const release = live.subscribe("/v1/definitions", changed);
  const push = (revision: number) => connections.at(-1)!.stream.enqueue(new TextEncoder().encode(`event: snapshot\ndata: ${JSON.stringify({ sequence: revision, results: [{ path: "/v1/definitions", status: 200, body: { revision } }] })}\n\n`));
  await vi.advanceTimersByTimeAsync(60); push(1); await vi.advanceTimersByTimeAsync(0);
  live.refresh();
  expect(connections[0]!.signal.aborted).toBe(true);
  expect(live.read("/v1/definitions")).toBeUndefined();
  await vi.advanceTimersByTimeAsync(60); push(2); await vi.advanceTimersByTimeAsync(0);
  expect(connections).toHaveLength(2);
  expect(live.read("/v1/definitions")?.value).toEqual({ revision: 2 });
  expect(changed).toHaveBeenCalledTimes(2);
  release(); stop();
  expect(connections.every(connection => connection.signal.aborted)).toBe(true);
});

test("a retired connection cannot replace the refreshed snapshot with a late refusal", async () => {
  vi.useFakeTimers();
  let finishRetired!: (response: Response) => void;
  let stream!: ReadableStreamDefaultController<Uint8Array>;
  let calls = 0;
  const live = new LiveReads(async (_paths, signal) => {
    if (++calls === 1) return new Promise<Response>(resolve => { finishRetired = resolve; });
    return new Response(new ReadableStream({ start(controller) {
      stream = controller;
      signal.addEventListener("abort", () => stream.error(Error("aborted")));
    } }));
  });
  const stop = live.enable(), release = live.subscribe("/v1/definitions", () => {});
  await vi.advanceTimersByTimeAsync(60);
  live.refresh();
  await vi.advanceTimersByTimeAsync(60);
  stream.enqueue(new TextEncoder().encode('event: snapshot\ndata: {"sequence":2,"results":[{"path":"/v1/definitions","status":200,"body":{"revision":2}}]}\n\n'));
  await vi.advanceTimersByTimeAsync(0);
  finishRetired(new Response(null, { status: 403 }));
  await vi.advanceTimersByTimeAsync(0);
  expect(live.read("/v1/definitions")?.value).toEqual({ revision: 2 });
  release(); stop();
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
