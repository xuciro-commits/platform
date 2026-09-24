// A browser edge of a server-authoritative domain: a persisted K5 outbox and an
// HTTP transport to the tenant's authority.
import type { SubmissionJson } from "./gen/platform/kernel/v1alpha1/change_pb";
import { Authorities, type Entry } from "./outbox";

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

  async get<T>(path: string): Promise<T> {
    const response = await fetch(this.connection.server + path, { headers: { Authorization: `Bearer ${this.connection.token}` } });
    if (!response.ok) throw new Error(`${path}: HTTP ${response.status}`);
    return response.json() as Promise<T>;
  }

  /** Takes the tenant's declarations from its authority (A9). */
  async refreshDeclarations(): Promise<void> {
    this.authorities.refresh(await this.get("/v1/declarations"));
  }

  private save(): void {
    try { localStorage.setItem(this.storageKey, JSON.stringify(this.authorities.outbox)); } catch { /* storage unavailable */ }
  }

  /** Builds and queues a decision about `target`; returns its idempotency key. */
  draft(schema: string, target: { type: string; id: string }, payload: unknown, evidenceFactIds: string[] = []): string {
    const key = crypto.randomUUID();
    const submission: SubmissionJson = {
      tenantId: this.connection.tenant, principalId: this.connection.principal,
      authority: this.authorities.authorityOf(this.connection.tenant, target.type) ?? "",
      target, schema: { name: schema, version: 1 }, idempotencyKey: key,
      payload: btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(payload)))),
      ...(evidenceFactIds.length ? { evidenceFactIds } : {}),
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
          headers: { Authorization: `Bearer ${this.connection.token}`, "Content-Type": "application/json" },
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
