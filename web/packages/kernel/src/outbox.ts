// K5 Authority and sync for a browser edge (contract/spec/K5-authority.md):
// declarations, the authority check and the outbox. Runs the contract's K5 vectors.
import type { AuthorityDeclarationJson, SubmissionStateJson } from "./gen/platform/kernel/v1alpha1/authority_pb";
import type { ErrorCodeJson } from "./gen/platform/kernel/v1alpha1/error_pb";
import type { SubmissionJson } from "./gen/platform/kernel/v1alpha1/change_pb";

export type State = Exclude<SubmissionStateJson, "SUBMISSION_STATE_UNSPECIFIED">;
export type Entry = { submission: SubmissionJson; state: State; outcome?: string };
export type Event = "send" | "confirm" | "conflict" | "reject" | "timeout" | "retry" | "undelivered";

export class KernelError extends Error {
  constructor(readonly code: ErrorCodeJson) { super(code); }
}

const transitions: Record<Event, Partial<Record<State, State>>> = {
  send: { SUBMISSION_STATE_PENDING: "SUBMISSION_STATE_SENDING" },
  confirm: { SUBMISSION_STATE_SENDING: "SUBMISSION_STATE_CONFIRMED" },
  conflict: { SUBMISSION_STATE_SENDING: "SUBMISSION_STATE_CONFLICT" },
  reject: { SUBMISSION_STATE_SENDING: "SUBMISSION_STATE_REJECTED" },
  timeout: { SUBMISSION_STATE_SENDING: "SUBMISSION_STATE_UNKNOWN" },
  undelivered: { SUBMISSION_STATE_SENDING: "SUBMISSION_STATE_PENDING" },
  retry: { SUBMISSION_STATE_UNKNOWN: "SUBMISSION_STATE_SENDING" },
};

/** Stable JSON: identical submissions compare equal whatever their key order. */
function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.entries(value).filter(([, v]) => v !== undefined).sort(([a], [b]) => a.localeCompare(b))
      .map(([k, v]) => `${JSON.stringify(k)}:${canonical(v)}`).join(",")}}`;
  }
  return JSON.stringify(value);
}

export class Authorities {
  private current = new Map<string, AuthorityDeclarationJson>();
  outbox: Entry[] = [];

  constructor(readonly edge: string) {}

  declare(d: AuthorityDeclarationJson): void {
    if (!d.tenantId || !d.dataClass || !d.authorityId || !d.kind || d.kind === "AUTHORITY_KIND_UNSPECIFIED") {
      throw new KernelError("ERROR_CODE_INVALID_ARGUMENT"); // A1
    }
    const key = `${d.tenantId}\u0000${d.dataClass}`;
    if ((d.epoch ?? 0) !== (this.current.get(key)?.epoch ?? 0) + 1) throw new KernelError("ERROR_CODE_CONFLICT"); // A2
    this.current.set(key, d);
  }

  /** Replaces the declarations with the authority's current ones (A9). */
  refresh(declarations: AuthorityDeclarationJson[]): void {
    this.current = new Map(declarations.map((d) => [`${d.tenantId}\u0000${d.dataClass}`, d]));
  }

  authorityOf(tenant: string, dataClass: string): string | undefined {
    return this.current.get(`${tenant}\u0000${dataClass}`)?.authorityId;
  }

  private declaration(s: SubmissionJson): AuthorityDeclarationJson {
    const d = this.current.get(`${s.tenantId}\u0000${s.target?.type}`);
    if (!d) throw new KernelError("ERROR_CODE_NOT_FOUND");
    return d;
  }

  authorize(s: SubmissionJson): void {
    if (this.declaration(s).authorityId !== s.authority) throw new KernelError("ERROR_CODE_NOT_AUTHORITY"); // A3
  }

  private entry(tenant: string, key: string): Entry | undefined {
    return this.outbox.find((e) => e.submission.tenantId === tenant && e.submission.idempotencyKey === key);
  }

  enqueue(s: SubmissionJson): State {
    const authority = this.declaration(s).authorityId;
    const existing = this.entry(s.tenantId ?? "", s.idempotencyKey ?? "");
    if (existing) {
      if (canonical(existing.submission) !== canonical(s)) throw new KernelError("ERROR_CODE_IDEMPOTENCY_CONFLICT"); // A6
      return existing.state;
    }
    const state: State = authority === this.edge ? "SUBMISSION_STATE_CONFIRMED" : "SUBMISSION_STATE_PENDING"; // A4
    this.outbox.push({ submission: s, state });
    return state;
  }

  transition(tenant: string, key: string, event: string): State {
    const entry = this.entry(tenant, key);
    if (!entry) throw new KernelError("ERROR_CODE_NOT_FOUND");
    const next = transitions[event as Event]?.[entry.state];
    if (!next) throw new KernelError("ERROR_CODE_INVALID_ARGUMENT"); // A5
    entry.state = next;
    return next;
  }

  /** The authority's answer (A8): no code confirms, a conflict code conflicts, any other code rejects. */
  answer(tenant: string, key: string, code?: string): State {
    const event = !code ? "confirm" : code === "ERROR_CODE_CONFLICT" || code === "ERROR_CODE_IDEMPOTENCY_CONFLICT" ? "conflict" : "reject";
    return this.transition(tenant, key, event);
  }
}
