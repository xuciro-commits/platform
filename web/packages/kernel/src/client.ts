import type { SubmissionJson } from "./gen/platform/kernel/v1alpha1/change_pb";
import type { Action, ActionCheckResult, AssetBinding, AssetRef, CompositeAnswer, Definition, SchemaVersion, ImportRow, ObjectSet, ObjectSetCount } from "./gen/host";
import { aggregateRequest, countRequest, interfaceRequest, loadRequest, objectSetPath, type AggregateQueryInput, type RecordQueryInput } from "./object-set";
import { Authorities, type Entry } from "./outbox";
import { LiveReads } from "./live";
import { PreparedWorkspace, preparedIdentity, rememberPreparedIdentity, preparedMetadataPaths, readPath, type PreparedApplication } from "./prepared";

export type ActionDeclaration = Action;

export type Connection = { readonly server: string; token: string; readonly tenant: string; readonly principal: string };
export class OfflineReadError extends Error { readonly name="OfflineReadError"; }
export class CompositeRefusalError extends Error { readonly name="CompositeRefusalError"; }

export function apiErrorMessage(body: unknown): string | undefined {
  if (body instanceof Error) return body.message.trim() ? body.message : undefined;
  if (!body || typeof body !== "object" || !("error" in body)) return undefined;
  const error = body.error;
  const message = typeof error === "string" ? error
    : error && typeof error === "object" && "message" in error ? error.message : undefined;
  return typeof message === "string" && message.trim() ? message : undefined;
}

export class EdgeClient {
  readonly authorities: Authorities;
  prepared?: PreparedWorkspace;
  usingPrepared = false;
  preparePaths() { return [...new Set([...preparedMetadataPaths, ...this.live.paths().filter(readPath)])]; }
  async prepareApplication(application:Omit<PreparedApplication,"at"|"reads">,paths:string[]=[]) {
    if(!this.prepared)throw Error("The workspace identity is not ready.");
    const generation=this.generation;
    const current=()=>{if(this.closed||this.generation!==generation)throw Error("The workspace identity changed.");};
    const selected=[...new Set([...this.preparePaths(),...paths])];
    if(selected.length>128||selected.some(path=>!readPath(path)))throw Error("Choose at most 128 readable task inputs for preparation.");
    const reads:Record<string,unknown>={};
    for(const path of selected){current();reads[path]=await this.get(path,true);}
    current();
    const definitions=reads["/v1/definitions"] as Definition[],ref=application.binding.ref;
    const definition=definitions.find(item=>item.ref.app===ref.app&&item.ref.kind===ref.kind&&item.ref.name===ref.name);
    const release=await this.get<{id:string}>("/v1/releases/active",true);
    current();
    if(definition?.version!==application.binding.sourceVersion||(reads["/v1/releases/active"] as {id:string}).id!==application.release||release.id!==application.release)throw Error("The application release changed during preparation. Refresh it and prepare again.");
    this.prepared.prepare({...application,at:new Date().toISOString(),reads});
    await rememberPreparedIdentity(this.connection);
    this.usingPrepared=false;
    return this.prepared.current()!;
  }
  private live = new LiveReads((paths, signal) => fetch(this.connection.server + "/v1/changes?" + new URLSearchParams({ watch: JSON.stringify(paths) }), { headers: this.headers(), signal }));
  enableLiveReads() { return this.live.enable(); }
  subscribeRead(path: string, changed: () => void) { return this.live.subscribe(path, changed); }
  refreshLiveReads() { this.readRevision++; this.live.refresh(); }
  subscribeInventory(type: string, limit: number, changed: () => void) {
    const path = (offset: number) => `/v1/records/${encodeURIComponent(type)}?limit=500&offset=${offset}`;
    const stops = new Map<number, () => void>();
    const update = () => {
      let total: number | undefined;
      try { total = this.live.read<{ total: number }>(path(0))?.value.total; } catch {   }
      const pages = total !== undefined && Number.isInteger(total) && total >= 0 && total <= limit ? Math.max(1, Math.ceil(total / 500)) : 1;
      for (const [offset, stop] of stops) if (offset >= pages * 500) { stop(); stops.delete(offset); }
      for (let page = 0; page < pages; page++) if (!stops.has(page * 500)) stops.set(page * 500, this.subscribeRead(path(page * 500), update));
      changed();
    };
    stops.set(0, this.subscribeRead(path(0), update));
    return () => { stops.forEach(stop => stop()); stops.clear(); };
  }
  recordsPath(type: string, q: RecordQueryInput = {}) {
    if (q.set || q.traversal || q.businessTime || q.format) return objectSetPath("load", loadRequest(type, q));
    return `/v1/records/${encodeURIComponent(type)}?${this.readParams(q)}`;
  }
  interfaceRecordsPath(name:string,q:RecordQueryInput={}) {
    return objectSetPath("load", interfaceRequest(name, q));
  }
  namedQueryPath(binding:AssetBinding,q:{search?:string;offset?:number;limit?:number}={}) {
    if(binding.ref.kind!=="query"||!binding.sourceVersion)throw Error("Invalid named query binding");
    const p=this.readParams(q);p.set("version",binding.sourceVersion);
    return `/v1/queries/${encodeURIComponent(binding.ref.app)}/${encodeURIComponent(binding.ref.name)}?${p}`;
  }
  aggregatePath(type: string, q: AggregateQueryInput = {}) {
    return objectSetPath("aggregate", aggregateRequest(type, q));
  }
  countPath(type: string, q: Omit<RecordQueryInput, "sort" | "offset" | "limit"> = {}) {
    return objectSetPath("count", countRequest(type, q));
  }
  private readParams(q: {domain?:unknown;search?:string;sort?:string[];offset?:number;limit?:number;archived?:boolean}) {
    const p = new URLSearchParams();
    if (Array.isArray(q.domain) && q.domain.length) p.set("domain", JSON.stringify(q.domain));
    if (q.search) p.set("search", q.search);
    if (q.sort?.length) p.set("sort", q.sort.join(","));
    if (q.offset) p.set("offset", String(q.offset));
    if (q.limit) p.set("limit", String(q.limit));
    if (q.archived) p.set("archived", "true");
    return p;
  }
  async pageContent(ref:AssetRef,contentVersion:string):Promise<Definition>{
    if(ref.kind!=="page"||!ref.app||!ref.name||!/^[A-Za-z0-9._:-]+$/.test(ref.app)||ref.app.length>256||ref.name.length>256||ref.name.includes("/")||!/^page\.sha256\.[0-9a-f]{64}$/.test(contentVersion))throw Error("Invalid page content identity");
    const path=`/v1/pages/${encodeURIComponent(ref.app)}/${encodeURIComponent(ref.name)}/${encodeURIComponent(contentVersion)}`;
    const result=await this.get<Definition>(path);if(!result?.page||result.ref?.app!==ref.app||result.ref.kind!==ref.kind||result.ref.name!==ref.name||result.contentVersion!==contentVersion)throw Error("Page content identity differs from its original binding");return result;
  }
  private storageKey?: string;
  private storageLoadError?: Error;
  private inFlightReads = new Map<string, Promise<unknown>>();
  private readRevision = 0;
  private generation = 0;
  private closed = false;
  private sendQueue: Promise<void> = Promise.resolve();
  private sending = new AbortController();
  private currentConnection: Connection;
  get connection(): Connection { return this.currentConnection; }

  constructor(connection: Connection, edge = "browser") {
    this.currentConnection = { ...connection };
    this.authorities = new Authorities(edge);
    if (connection.tenant && connection.principal) this.bindIdentity(connection.tenant, connection.principal);
  }

  bindIdentity(tenant: string, principal: string): void {
    if (!tenant || !principal) throw Error("The workspace identity is not ready.");
    const server = this.connection.server || globalThis.location?.origin;
    if (!server) throw Error("The workspace host is not ready.");
    const key = `outbox:${server.replace(/\/$/, "")}:${tenant}:${principal}`;
    if (this.storageKey && key !== this.storageKey) throw Error("Open a new client for another workspace identity.");
    this.closed = false;
    if (key === this.storageKey) return;
    this.currentConnection = { ...this.connection, tenant, principal };
    if(!this.prepared||this.prepared.scope.tenant!==tenant||this.prepared.scope.principal!==principal)this.prepared=new PreparedWorkspace(this.connection);
    this.storageKey = key;
    this.storageLoadError = undefined;
    this.authorities.refresh([]);
    this.authorities.outbox = [];
    try {
      const saved: unknown = JSON.parse(localStorage.getItem(key) ?? "[]");
      const states = ["SUBMISSION_STATE_PENDING", "SUBMISSION_STATE_SENDING", "SUBMISSION_STATE_UNKNOWN", "SUBMISSION_STATE_CONFIRMED", "SUBMISSION_STATE_CONFLICT", "SUBMISSION_STATE_REJECTED"];
      if (!Array.isArray(saved) || saved.some(entry => !entry?.submission?.idempotencyKey || entry.submission.tenantId !== tenant || entry.submission.principalId !== principal || !states.includes(entry.state))
        || new Set(saved.map(entry => entry.submission.idempotencyKey)).size !== saved.length) throw Error("Invalid saved requests");
      this.authorities.outbox = saved as Entry[];
      let interrupted = false;
      for (const entry of this.authorities.outbox) if (entry.state === "SUBMISSION_STATE_SENDING") {
        this.authorities.transition(tenant, entry.submission.idempotencyKey!, "timeout");
        interrupted = true;
      }
      if (interrupted) this.save();
    } catch {
      this.storageLoadError = Error("Saved requests could not be opened. Local storage was not replaced.");
    }
  }

  close(): void {
    this.closed = true;
    this.generation++;
    this.sending.abort();
    this.sending = new AbortController();
    this.inFlightReads.clear();
    this.live.dispose();
  }

  static problem(error: unknown): string {
    return /HTTP 401/.test(String(error)) ? "sign-in required: this host accepts identity-provider tokens only" : "host unreachable";
  }

  private headers(json = false, connection = this.connection): Record<string, string> {
    const lang = typeof document === "undefined" ? "" : document.documentElement.lang;
    return { Authorization: `Bearer ${connection.token}`, ...(connection.tenant ? { "Platform-Tenant": connection.tenant } : {}),
      ...(lang ? { "Accept-Language": lang } : {}), ...(json ? { "Content-Type": "application/json" } : {}) };
  }

  async get<T>(key: string, fresh = false): Promise<T> {
    if (this.closed && this.connection.principal) throw Error("The workspace identity changed.");
    const generation = this.generation;
    const prepared=async():Promise<T>=>{
      try{
        if(fresh)throw Error("Connect to prepare current application content.");
        this.prepared??=await preparedIdentity(this.connection);
        if(!this.prepared)throw Error("This task was not prepared for offline use. Connect to continue.");
        const value=this.prepared.read<T>(key);this.usingPrepared=true;return value;
      }catch(error){throw new OfflineReadError(error instanceof Error?error.message:String(error));}
    };
    if(typeof navigator!=="undefined"&&navigator.onLine===false)return prepared();
    let cached:{value:T}|undefined;
    try{cached=fresh?undefined:this.live.read<T>(key);}catch(error){if(String(error).includes("live connection unavailable"))return prepared();throw error;}
    if(cached)return cached.value;
    const [path, body] = key.split("\n");
    const headers=this.headers(body !== undefined),signal=this.sending.signal;
    const read=async():Promise<T>=>{
      let response:Response;
      try{response=await fetch(this.connection.server + path, { headers, signal, ...(body !== undefined ? { method: "POST", body } : {}) });}
      catch{if(signal.aborted)throw Error("The workspace identity changed.");return prepared();}
      if(generation!==this.generation)throw Error("The workspace identity changed.");
      if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`);
      const current = fresh ? undefined : this.live.read<T>(key); if (current) return current.value;
      const value = await response.json() as T;
      if(generation!==this.generation)throw Error("The workspace identity changed.");
      return fresh ? value : this.live.read<T>(key)?.value ?? value;
    };
    if(fresh||!readPath(key))return read();
    const scope=JSON.stringify([key,this.connection.tenant,this.connection.principal,headers,this.readRevision]);
    const pending=this.inFlightReads.get(scope);if(pending)return pending as Promise<T>;
    const request=read();this.inFlightReads.set(scope,request);
    try{return await request;}finally{if(this.inFlightReads.get(scope)===request)this.inFlightReads.delete(scope);}
  }

  async inventory<T>(type: string, limit = 1000): Promise<{ records: T[] }> {
    const path = `/v1/records/${encodeURIComponent(type)}`;
    const records: T[] = [];
    let total: number | undefined;
    do {
      const page = await this.get<{ records: T[]; total: number }>(`${path}?limit=500&offset=${records.length}`);
      total ??= page.total;
      if (!Number.isInteger(total) || total < 0 || total > limit || total !== page.total || records.length + page.records.length > total
        || (page.records.length === 0 && records.length < total)) throw new Error("Incomplete or oversized record inventory");
      records.push(...page.records);
    } while (records.length < total);
    return { records };
  }

  async follow(onChange: (kind: "changed" | "operations") => void, signal: AbortSignal): Promise<void> {
    for (let wait = 1000; !signal.aborted; wait = Math.min(wait * 2, 30000)) {
      try {
        const response = await fetch(this.connection.server + "/v1/changes", { headers: this.headers(), signal });
        if (!response.ok || !response.body) throw new Error(`/v1/changes: HTTP ${response.status}`);
        const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
        let buffer = "";
        for (;;) {
          const { value, done } = await reader.read();
          if (done) break;
          wait = 1000;
          buffer += value;
          const events = buffer.split("\n\n");
          buffer = events.pop() ?? "";
          if (events.some((e) => e.startsWith("event: changed\n"))) onChange("changed");
          else if (events.some((e) => e.startsWith("event: operations\n"))) onChange("operations");
        }
      } catch {
        if (signal.aborted) return;
      }
      await new Promise((resolve) => setTimeout(resolve, wait));
    }
  }

  async upload(file: Blob, name: string, options:{signal?:AbortSignal}={}): Promise<{ hash: string; size: number; contentType: string; name: string }> {
    const response = await fetch(`${this.connection.server}/v1/files?name=${encodeURIComponent(name)}`, {
      method: "POST", headers: { ...this.headers(), "Content-Type": file.type || "application/octet-stream" }, body: file, signal:options.signal,
    });
    if (!response.ok) throw new Error(await response.text() || `upload failed: ${response.status}`);
    return response.json();
  }

  async importCSV(type: string, file: Blob, preview: boolean): Promise<ImportRow[]> {
    const response = await fetch(`${this.connection.server}/v1/import/${encodeURIComponent(type)}?preview=${preview}`, {
      method: "POST", headers: { ...this.headers(), "Content-Type": "text/csv" }, body: file,
    });
    if (!response.ok) {
      const refused = await response.json().catch(() => undefined) as { error?: { detail?: string; message?: string } } | undefined;
      throw new Error(refused?.error?.detail ?? refused?.error?.message ?? `import failed: ${response.status}`);
    }
    return response.json();
  }

  async composite(key: string, edits: { schema: string; target: { type: string; id: string }; payload?: unknown }[]): Promise<CompositeAnswer> {
    const answer = await this.call<CompositeAnswer & { error?: string | { detail?: string; message?: string }; outcome?: "refused" }>("POST", "/v1/composites", { key, edits });
    if (!answer.ok) {
      const detail = answer.body.error;
      const message = (typeof detail === "object" ? detail.detail ?? detail.message : detail) ?? `composite failed: ${answer.status}`;
      throw answer.body.outcome === "refused" ? new CompositeRefusalError(message) : new Error(message);
    }
    return answer.body;
  }

  async exportCSV(type: string, query: RecordQueryInput): Promise<Blob> {
    const response = await fetch(`${this.connection.server}/v1/export/${encodeURIComponent(type)}`, {
      method: "POST", headers: { ...this.headers(), "Content-Type": "application/json" }, body: JSON.stringify(query),
    });
    if (!response.ok) {
      const text = await response.text();
      let reason: unknown;
      try { reason = JSON.parse(text); } catch { reason = undefined; }
      throw new Error(apiErrorMessage(reason) ?? (text || `export failed: ${response.status}`));
    }
    return response.blob();
  }


  async download(id: string, options:{signal?:AbortSignal;maxBytes?:number}={}): Promise<Blob> {
    const limit=options.maxBytes;
    if(limit!==undefined&&(!Number.isSafeInteger(limit)||limit<0))throw new Error("Invalid download byte budget");
    const response = await fetch(`${this.connection.server}/v1/files/${encodeURIComponent(id)}`, { headers: this.headers(),signal:options.signal });
    if (!response.ok) throw new Error(`download failed: ${response.status}`);
    if(limit===undefined)return response.blob();
    if(Number(response.headers.get("Content-Length"))>limit){await response.body?.cancel();throw new Error("The file exceeds the preview byte budget.");}
    if(!response.body){const blob=await response.blob();if(blob.size>limit)throw new Error("The file exceeds the preview byte budget.");return blob;}
    const reader=response.body.getReader(),parts:Uint8Array<ArrayBuffer>[]=[];let size=0;
    try {while(true){const next=await reader.read();if(next.done)break;size+=next.value.byteLength;if(size>limit){await reader.cancel();throw new Error("The file exceeds the preview byte budget.");}parts.push(next.value);}}
    finally {reader.releaseLock();}
    return new Blob(parts,{type:response.headers.get("Content-Type")??"application/octet-stream"});
  }

  async call<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<{ ok: boolean; status: number; body: T }> {
    const response = await fetch(this.connection.server + path, {
      method, headers: this.headers(body !== undefined),
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    return { ok: response.ok, status: response.status, body: (await response.json().catch(() => ({}))) as T };
  }

  async stream<T>(path: string, body: unknown, onEvent: (event: string, data: unknown) => void): Promise<{ ok: boolean; status: number; body?: T }> {
    const response = await fetch(this.connection.server + path, { method: "POST", headers: this.headers(true), body: JSON.stringify(body) });
    if (!response.headers.get("Content-Type")?.startsWith("text/event-stream") || !response.body) {
      return { ok: response.ok, status: response.status, body: (await response.json().catch(() => ({}))) as T };
    }
    const reader = response.body.pipeThrough(new TextDecoderStream()).getReader();
    let buffer = "";
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      buffer += value;
      let end: number;
      while ((end = buffer.indexOf("\n\n")) >= 0) {
        const block = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        const name = /^event: (.*)$/m.exec(block)?.[1] ?? "message";
        const data = /^data: (.*)$/m.exec(block)?.[1];
        onEvent(name, data === undefined ? undefined : JSON.parse(data));
      }
    }
    return { ok: true, status: response.status };
  }

  async records<T = unknown>(type: string, q: RecordQueryInput = {}): Promise<T> {
    return this.get<T>(this.recordsPath(type, q));
  }

  async aggregate<T = unknown>(type: string, q: AggregateQueryInput = {}): Promise<T> {
    return this.get<T>(this.aggregatePath(type, q));
  }

  async count(type: string, q: Omit<RecordQueryInput, "sort" | "offset" | "limit"> = {}): Promise<ObjectSetCount> {
    return this.get<ObjectSetCount>(this.countPath(type, q));
  }

  objectSetCount(set: ObjectSet): Promise<ObjectSetCount> {
    return this.get<ObjectSetCount>(objectSetPath("count", { set }), true);
  }

  actionCheck(schema: string, target: { type: string; id: string }, inputs?: unknown): Promise<ActionCheckResult> {
    return this.get<ActionCheckResult>(`/v1/actions/${encodeURIComponent(schema)}/check\n${JSON.stringify({ target: { object: target.type, id: target.id }, ...(inputs === undefined ? {} : { inputs }) })}`, true);
  }

  record<T = unknown>(type: string, id: string): Promise<T> {
    return this.get<T>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
  }

  async refreshDeclarations(): Promise<void> {
    this.authorities.refresh(await this.get("/v1/declarations"));
  }

  private save(): void {
    if (this.closed) throw Error("The workspace identity changed.");
    if (this.storageLoadError) throw this.storageLoadError;
    if (!this.storageKey) throw Error("The workspace identity is not ready.");
    try { localStorage.setItem(this.storageKey, JSON.stringify(this.authorities.outbox)); }
    catch { throw Error("Local request storage is unavailable. Retry after storage is restored."); }
  }

  draft(schema: string, target: { type: string; id: string }, payload: unknown, evidenceFactIds: string[] = [], expectedRevision?: number, schemaVersion?: SchemaVersion, correctionOf?: string, requestKey?: string): string {
    if (this.storageLoadError) throw this.storageLoadError;
    if (!this.storageKey) throw Error("The workspace identity is not ready.");
    const key = requestKey ?? crypto.randomUUID();
    const submission: SubmissionJson = {
      tenantId: this.connection.tenant, principalId: this.connection.principal,
      authority: this.authorities.authorityOf(this.connection.tenant, target.type) ?? "",
      target, schema: { name: schema, version: schemaVersion?.family ?? 1 }, idempotencyKey: key,
      payload: btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(payload)))),
      ...(evidenceFactIds.length ? { evidenceFactIds } : {}),
      ...(expectedRevision === undefined ? {} : { expectedRevision }),
      ...(correctionOf ? { correlationId: `correction:${correctionOf}` } : {}),
    };
    this.authorities.enqueue(submission);
    const entry = this.authorities.outbox.find(entry => entry.submission.idempotencyKey === key);
    if (entry && schemaVersion) entry.schemaVersion = { ...schemaVersion };
    if (entry && correctionOf) entry.correctionOf = correctionOf;
    try { this.save(); }
    catch (error) {
      this.authorities.outbox = this.authorities.outbox.filter(item => item !== entry);
      throw error;
    }
    return key;
  }

  private async transmit(entry: Entry, timeoutMs: number) {
    const connection = this.connection;
    const response = await fetch(`${connection.server}/v1/submissions`, {
      method: "POST", body: JSON.stringify(entry.submission), signal: AbortSignal.any([this.sending.signal, AbortSignal.timeout(timeoutMs)]),
      headers: { ...this.headers(true, connection), ...(entry.schemaVersion ? { "Platform-Schema-Version": entry.schemaVersion.version } : {}), ...(entry.correctionOf ? { "Platform-Correction-Of": entry.correctionOf } : {}) },
    });
    const body = await response.json().catch(() => ({})) as { record?: { changeId?: string }; error?: { code?: string; message?: string; issues?: import("./gen/host").FieldIssue[] } };
    return response.ok && body.record?.changeId || body.error?.code ? body : undefined;
  }

  async repeat(key: string, timeoutMs = 5000): Promise<Entry> {
    this.save();
    const generation = this.generation;
    const entry = this.authorities.outbox.find(item => item.submission.tenantId === this.connection.tenant && item.submission.principalId === this.connection.principal && item.submission.idempotencyKey === key);
    if (!entry || !["SUBMISSION_STATE_CONFIRMED", "SUBMISSION_STATE_REJECTED", "SUBMISSION_STATE_CONFLICT"].includes(entry.state)) throw Error("The original answered request is unavailable.");
    const body = await this.transmit(entry, timeoutMs);
    if (generation !== this.generation) throw Error("The workspace identity changed.");
    if (!body) throw Error("The original answer could not be confirmed.");
    const outcome = body.error?.code ?? body.record?.changeId ?? "";
    if (outcome !== entry.outcome) throw Error("The repeated answer differs from the original request.");
    entry.reason = body.error?.message;
    entry.issues = body.error?.issues;
    this.save();
    return entry;
  }

  async send(key?: string, timeoutMs = 5000): Promise<Entry[]> {
    this.save();
    const generation = this.generation, answered: Entry[] = [];
    for (const entry of this.authorities.outbox.filter(e => (!key || e.submission.idempotencyKey === key)
      && (e.state === "SUBMISSION_STATE_PENDING" || e.state === "SUBMISSION_STATE_UNKNOWN"))) {
      if (generation !== this.generation) break;
      const sent = this.sendQueue.then(() => this.sendEntry(entry, timeoutMs));
      this.sendQueue = sent.then(() => undefined, () => undefined);
      const answer = await sent;
      if (answer) answered.push(answer);
    }
    return answered;
  }

  private async sendEntry(entry: Entry, timeoutMs: number): Promise<Entry | undefined> {
    this.save();
    const generation = this.generation;
    if ((entry.state === "SUBMISSION_STATE_PENDING" || entry.state === "SUBMISSION_STATE_UNKNOWN") && navigator.onLine) {
      const { tenantId = "", idempotencyKey = "" } = entry.submission;
      if (entry.submission.tenantId !== this.connection.tenant || entry.submission.principalId !== this.connection.principal) throw Error("Saved requests belong to another workspace identity.");
      const previous = entry.state;
      this.authorities.transition(tenantId, idempotencyKey, entry.state === "SUBMISSION_STATE_PENDING" ? "send" : "retry");
      try { this.save(); }
      catch (error) {
        this.authorities.transition(tenantId, idempotencyKey, previous === "SUBMISSION_STATE_PENDING" ? "undelivered" : "timeout");
        throw error;
      }
      try {
        const body = await this.transmit(entry, timeoutMs);
        if (generation !== this.generation) return;
        if (body) {
          this.authorities.answer(tenantId, idempotencyKey, body.error?.code);
          entry.outcome = body.error?.code ?? body.record?.changeId ?? "";
          entry.reason = body.error?.message;
          entry.issues = body.error?.issues;
          if (body.error?.code === "ERROR_CODE_NOT_AUTHORITY") await this.refreshDeclarations().catch(() => undefined);
        } else {
          this.authorities.transition(tenantId, idempotencyKey, "timeout");
        }
      } catch {
        if (generation !== this.generation) return;
        this.authorities.transition(tenantId, idempotencyKey, "timeout");
      }
      this.save();
      if (entry.state !== "SUBMISSION_STATE_UNKNOWN") return entry;
    }
  }
}
