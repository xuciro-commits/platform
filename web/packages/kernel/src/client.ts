// A browser edge of a server-authoritative domain: a persisted K5 outbox and an
// HTTP transport to the tenant's authority.
import type { SubmissionJson } from "./gen/platform/kernel/v1alpha1/change_pb";
import type { Action } from "./gen/host";
import { Authorities, type Entry } from "./outbox";

/** One action a server offers to this caller (ADR-0008): render from it, never re-check roles. Generated from the host (ADR-0023). */
export type ActionDeclaration = Action;

export type Connection = { server: string; token: string; tenant: string; principal: string };

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

  /** Calls a host service that is not a submission, such as a model call (ADR-0015): the answer's JSON comes back whatever its status. */
  async call<T>(method: "GET" | "POST", path: string, body?: unknown): Promise<{ ok: boolean; status: number; body: T }> {
    const response = await fetch(this.connection.server + path, {
      method, headers: this.headers(body !== undefined),
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    return { ok: response.ok, status: response.status, body: (await response.json().catch(() => ({}))) as T };
  }

  /** Records of an entity type (ADR-0016): a domain, a search, sort fields and a page. */
  records<T = unknown>(type: string, q: { domain?: unknown[]; search?: string; sort?: string[]; offset?: number; limit?: number; archived?: boolean } = {}): Promise<T> {
    const p = new URLSearchParams();
    if (q.domain?.length) p.set("domain", JSON.stringify(q.domain));
    if (q.search) p.set("search", q.search);
    if (q.sort?.length) p.set("sort", q.sort.join(","));
    if (q.offset) p.set("offset", String(q.offset));
    if (q.limit) p.set("limit", String(q.limit));
    if (q.archived) p.set("archived", "true");
    return this.get<T>(`/v1/records/${encodeURIComponent(type)}?${p}`);
  }

  /** Groups and measures of an entity type's records within the caller's scope (ADR-0019): `groups` like "stage" or "checkIn:month", `measures` like "count" or "sum:amount". */
  aggregate<T = unknown>(type: string, q: { domain?: unknown[]; search?: string; archived?: boolean; groups?: string[]; measures?: string[] } = {}): Promise<T> {
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
        const body = await response.json().catch(() => ({})) as { record?: { changeId?: string }; error?: { code?: string } };
        if (response.ok || body.error?.code) {
          this.authorities.answer(tenantId, idempotencyKey, body.error?.code);
          entry.outcome = body.error?.code ?? body.record?.changeId ?? "";
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
