// A browser edge of a server-authoritative domain: a persisted K5 outbox and an
// HTTP transport to the tenant's authority.
import type { SubmissionJson } from "./gen/platform/kernel/v1alpha1/change_pb";
import type { Action, Query, AggregateQuery, AssetBinding } from "./gen/host";
import { Authorities, type Entry } from "./outbox";

/** One action a server offers to this caller (ADR-0008): render from it, never re-check roles. Generated from the host (ADR-0023). */
export type ActionDeclaration = Action;

export type Connection = { server: string; token: string; tenant: string; principal: string };

/** The HTTP edge accepts both the host's structured refusal and a plain
 * diagnostic. Callers render only text, never the error object itself. */
export function apiErrorMessage(body: unknown): string | undefined {
  if (!body || typeof body !== "object" || !("error" in body)) return undefined;
  const error = body.error;
  const message = typeof error === "string" ? error
    : error && typeof error === "object" && "message" in error ? error.message : undefined;
  return typeof message === "string" && message.trim() ? message : undefined;
}

export class EdgeClient {
  readonly authorities: Authorities;
  private readonly storageKey: string;

  constructor(readonly connection: Connection, edge = "browser") {
    this.storageKey = `outbox:${connection.server}:${connection.tenant}:${connection.principal}`;
    this.authorities = new Authorities(edge);
    try {
      this.authorities.outbox = JSON.parse(localStorage.getItem(this.storageKey) ?? "[]") as Entry[];
    } catch { /* unreadable storage starts empty; the server keeps what was confirmed */ }
  }

  /** Why a read failed, for a status line: a host on its production path refuses development tokens. */
  static problem(error: unknown): string {
    return /HTTP 401/.test(String(error)) ? "sign-in required: this host accepts identity-provider tokens only" : "host unreachable";
  }

  /** The bearer token, the tenant once known (a person may be a member of several on one host, ADR-0018), and the page's language, in which the host serves declarations (ADR-0023). */
  private headers(json = false): Record<string, string> {
    const lang = typeof document === "undefined" ? "" : document.documentElement.lang;
    return { Authorization: `Bearer ${this.connection.token}`, ...(this.connection.tenant ? { "Platform-Tenant": this.connection.tenant } : {}),
      ...(lang ? { "Accept-Language": lang } : {}), ...(json ? { "Content-Type": "application/json" } : {}) };
  }

  async get<T>(path: string): Promise<T> {
    const response = await fetch(this.connection.server + path, { headers: this.headers() });
    if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`);
    return response.json() as Promise<T>;
  }

  /** Read all pages of a bounded record inventory. Never silently return the
   * first 500 rows or a partial read after a failed page. */
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

  /**
   * Follows the tenant's changes (`GET /v1/changes`, server-sent events): calls
   * `onChange` each time the host took inputs, until `signal` aborts; reconnects
   * after a dropped stream, waiting longer each time up to 30 s (F-32). Read
   * with fetch, as EventSource sends no Authorization header.
   */
  async follow(onChange: () => void, signal: AbortSignal): Promise<void> {
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
          if (events.some((e) => e.startsWith("event: changed"))) onChange();
        }
      } catch {
        if (signal.aborted) return;
      }
      await new Promise((resolve) => setTimeout(resolve, wait));
    }
  }

  /** Calls a host service that is not a submission, such as a model call (ADR-0015): the answer's JSON comes back whatever its status. */
  /** Uploads a file's bytes (ADR-0028); answers the hash to attach. */
  async upload(file: Blob, name: string, options:{signal?:AbortSignal}={}): Promise<{ hash: string; size: number; contentType: string; name: string }> {
    const response = await fetch(`${this.connection.server}/v1/files?name=${encodeURIComponent(name)}`, {
      method: "POST", headers: { ...this.headers(), "Content-Type": file.type || "application/octet-stream" }, body: file, signal:options.signal,
    });
    if (!response.ok) throw new Error(await response.text() || `upload failed: ${response.status}`);
    return response.json();
  }

  /** Imports records of a type from CSV, or previews what each row would do (ADR-0028). */
  async importCSV(type: string, file: Blob, preview: boolean): Promise<{ row: number; id: string; action: string; outcome: string }[]> {
    const response = await fetch(`${this.connection.server}/v1/import/${encodeURIComponent(type)}?preview=${preview}`, {
      method: "POST", headers: { ...this.headers(), "Content-Type": "text/csv" }, body: file,
    });
    if (!response.ok) throw new Error(`import failed: ${response.status}`);
    return response.json();
  }

  /** The records a list shows the member, as CSV (ADR-0028). */
  async exportCSV(type: string, query: string): Promise<Blob> {
    const response = await fetch(`${this.connection.server}/v1/export/${encodeURIComponent(type)}${query}`, { headers: this.headers() });
    if (!response.ok) throw new Error(`export failed: ${response.status}`);
    return response.blob();
  }

  /** A file's bytes, as the member may read them (ADR-0028). */
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

  /**
   * A POST answered as server-sent events (ADR-0029 D2): each event's name and
   * data as it comes; an answer that is not a stream (a refusal) is returned whole.
   */
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

  /** Records of an entity type (ADR-0016): a domain, a search, sort fields and a page. */
  async records<T = unknown>(type: string, q: Omit<Query,"domain"> & {domain?:unknown[]} = {}): Promise<T> {
    if(q.set||q.traversal){
      const path=`/v1/records/${encodeURIComponent(type)}/query`;
      const response=await fetch(this.connection.server+path,{method:"POST",headers:this.headers(true),body:JSON.stringify(q)});
      if(!response.ok)throw new Error(`${path}: HTTP ${response.status}`);
      return response.json() as Promise<T>;
    }
    const p = new URLSearchParams();
    if (q.domain?.length) p.set("domain", JSON.stringify(q.domain));
    if (q.search) p.set("search", q.search);
    if (q.sort?.length) p.set("sort", q.sort.join(","));
    if (q.offset) p.set("offset", String(q.offset));
    if (q.limit) p.set("limit", String(q.limit));
    if (q.archived) p.set("archived", "true");
    return this.get<T>(`/v1/records/${encodeURIComponent(type)}?${p}`);
  }

  /** Follow an exact relationship version; the server applies its typed reference and member scope. */
  async traverseLink<T=unknown>(binding:AssetBinding,direction:"forward"|"reverse",id:string,query:Omit<Query,"domain">&{domain?:unknown[]}={}):Promise<T>{
    if(binding.ref.kind!=="link-type"||!binding.sourceVersion||!id)throw new Error("Link traversal needs an exact version and start record");
    const path=`/v1/link-types/${[binding.ref.app,binding.ref.name,binding.sourceVersion,direction,id].map(encodeURIComponent).join("/")}`;
    const response=await fetch(this.connection.server+path,{method:"POST",headers:this.headers(true),body:JSON.stringify(query)});
    if(!response.ok)throw new Error(`${path}: HTTP ${response.status}`);return response.json() as Promise<T>;
  }

  /** Groups and measures of an entity type's records within the caller's scope (ADR-0019): `groups` like "stage" or "checkIn:month", `measures` like "count" or "sum:amount". */
  async aggregate<T = unknown>(type: string, q: Omit<AggregateQuery,"domain"> & {domain?:unknown[]} = {}): Promise<T> {
    if(q.histogram||q.set||q.traversal||q.maxRows){const path=`/v1/aggregates/${encodeURIComponent(type)}/query`;const response=await fetch(this.connection.server+path,{method:"POST",headers:this.headers(true),body:JSON.stringify(q)});if(!response.ok)throw new Error(`${path}: HTTP ${response.status}`);return response.json() as Promise<T>;}
    const p = new URLSearchParams();
    if (q.domain?.length) p.set("domain", JSON.stringify(q.domain));
    if (q.search) p.set("search", q.search);
    if (q.archived) p.set("archived", "true");
    if (q.groups?.length) p.set("group", q.groups.join(","));
    if (q.measures?.length) p.set("measure", q.measures.join(","));
    return this.get<T>(`/v1/aggregates/${encodeURIComponent(type)}?${p}`);
  }

  /** One record with its history and related records. */
  record<T = unknown>(type: string, id: string): Promise<T> {
    return this.get<T>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
  }

  /** Takes the tenant's declarations from its authority (A9). */
  async refreshDeclarations(): Promise<void> {
    this.authorities.refresh(await this.get("/v1/declarations"));
  }

  private save(): void {
    try { localStorage.setItem(this.storageKey, JSON.stringify(this.authorities.outbox)); } catch { /* storage unavailable */ }
  }

  /** Builds and queues a decision about `target`; returns its idempotency key. `expectedRevision`
   *  is the target's revision the user saw (K4 C12): a stale screen is refused, not applied. */
  draft(schema: string, target: { type: string; id: string }, payload: unknown, evidenceFactIds: string[] = [], expectedRevision?: number): string {
    const key = crypto.randomUUID();
    const submission: SubmissionJson = {
      tenantId: this.connection.tenant, principalId: this.connection.principal,
      authority: this.authorities.authorityOf(this.connection.tenant, target.type) ?? "",
      target, schema: { name: schema, version: 1 }, idempotencyKey: key,
      payload: btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(payload)))),
      ...(evidenceFactIds.length ? { evidenceFactIds } : {}),
      ...(expectedRevision === undefined ? {} : { expectedRevision }),
    };
    this.authorities.enqueue(submission);
    this.save();
    return key;
  }

  /** Sends pending entries and retries unknown ones with the same key; returns entries that got an answer. */
  async send(timeoutMs = 5000): Promise<Entry[]> {
    const answered: Entry[] = [];
    for (const entry of this.authorities.outbox.filter((e) => e.state === "SUBMISSION_STATE_PENDING" || e.state === "SUBMISSION_STATE_UNKNOWN")) {
      const { tenantId = "", idempotencyKey = "" } = entry.submission;
      if (entry.state === "SUBMISSION_STATE_PENDING" && !navigator.onLine) continue; // stays pending offline
      this.authorities.transition(tenantId, idempotencyKey, entry.state === "SUBMISSION_STATE_PENDING" ? "send" : "retry");
      this.save();
      try {
        const response = await fetch(`${this.connection.server}/v1/submissions`, {
          method: "POST", body: JSON.stringify(entry.submission), signal: AbortSignal.timeout(timeoutMs),
          headers: this.headers(true),
        });
        const body = await response.json().catch(() => ({})) as { record?: { changeId?: string }; error?: { code?: string; message?: string } };
        if (response.ok || body.error?.code) {
          this.authorities.answer(tenantId, idempotencyKey, body.error?.code);
          entry.outcome = body.error?.code ?? body.record?.changeId ?? "";
          entry.reason = body.error?.message;
          answered.push(entry);
          if (body.error?.code === "ERROR_CODE_NOT_AUTHORITY") await this.refreshDeclarations().catch(() => undefined);
        } else {
          this.authorities.transition(tenantId, idempotencyKey, "timeout");
        }
      } catch {
        // A browser cannot tell whether a failed request reached the server: unknown, retried with the same key.
        this.authorities.transition(tenantId, idempotencyKey, "timeout");
      }
      this.save();
    }
    return answered;
  }
}
