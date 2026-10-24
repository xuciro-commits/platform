// The host as this workspace sees it: one client per identity, and the small
// set of reads and decisions every view performs. Nothing here knows about a
// particular application: the host's catalog says what can be done and this
// layer only carries the caller's words to it.
import { EdgeClient, type Api, type Entry } from "@platform/kernel";

export type Me = Api.MeView;
export type EntityInfo = Api.EntityInfo;
export type CatalogAction = Api.Action;
export type Definition = Api.Definition;
export type RecordPage = Api.RecordPage;

/** One record as the host answers it: the values, its identity and history. */
export type RecordDetail = {
  values: Record<string, unknown>;
  record: { id: string; revision: number; name?: string; [field: string]: unknown };
  history?: HistoryEntry[];
};

export type HistoryEntry = { change: string; schema: string; by: string; at: string; fields?: { field: string; before?: unknown; after?: unknown }[] };
export type Group = { entity: EntityInfo; actions: CatalogAction[] };

const json = (path: string) => fetch(path, { headers: { Accept: "application/json" } });

/** The id a page gives a new record: its own prefix, then six characters. The
 * host names the record in the submission, so the page must choose it before
 * the decision and keep it across retries. */
export const newId = (prefix: string): string =>
  `${prefix}-${crypto.randomUUID().replace(/-/g, "").slice(0, 20).toUpperCase()}`;

/** The development identities a host without an identity provider offers. */
export async function signInOptions(): Promise<Api.SignIn> {
  const response = await json("/v1/sign-in").catch(() => undefined);
  return response?.ok ? (await response.json()) as Api.SignIn : {};
}

/** Who this token is on this host, before a tenant is chosen. */
export async function identify(token: string, tenant: string): Promise<Me> {
  const response = await fetch("/v1/me", { headers: { Authorization: `Bearer ${token}`, ...(tenant ? { "Platform-Tenant": tenant } : {}) } });
  if (!response.ok) throw new Error(response.status === 401 ? "This host does not accept this token." : `The host answered ${response.status}.`);
  return await response.json() as Me;
}

/** Opens the tenant's authority: the host declares which authority owns each
 * data class (K5 A9), and until those declarations are read a decision has no
 * authority to name. Every client is opened through here. */
export async function connect(me: Me, token: string): Promise<EdgeClient> {
  const client = new EdgeClient({ server: "", token, tenant: me.tenantId, principal: me.principalId });
  await client.refreshDeclarations();
  return client;
}

/** The host's refusal as one line: it answers `{error:{code,message}}`. */
export function errorOf(body: unknown): string | undefined {
  const error = (body as { error?: { code?: string; message?: string } } | undefined)?.error;
  return error ? [error.code, error.message].filter(Boolean).join(": ") : undefined;
}

export async function catalog(client: EdgeClient): Promise<{ entities: EntityInfo[]; actions: CatalogAction[] }> {
  const [entities, actions] = await Promise.all([client.get<EntityInfo[]>("/v1/entities"), client.get<CatalogAction[]>("/v1/actions")]);
  return { entities, actions };
}

/** The actions a member may perform on a record type, in catalog order. */
export function actionsOf(actions: CatalogAction[], type: string): CatalogAction[] {
  return actions.filter((action) => action.target === type || action.target === "");
}

export async function definitions(client: EdgeClient): Promise<Definition[]> {
  return await client.get<Definition[]>("/v1/definitions");
}

export async function releases(client: EdgeClient): Promise<Api.ReleasePage> {
  return await client.get<Api.ReleasePage>("/v1/releases/candidates?limit=24");
}

export async function activeRelease(client: EdgeClient): Promise<string> {
  return (await client.get<Api.ReleaseActive>("/v1/releases/active")).id;
}

/** A page of a type: the host reads the complete set within the caller's scope,
 * then windows it. The same shape serves search (q). */
export async function page(client: EdgeClient, type: string, offset = 0, limit = 50, search = ""): Promise<RecordPage> {
  return await client.records<RecordPage>(type, { limit, offset, ...(search ? { search } : {}) });
}

/** The whole scope's records of a type as CSV, read the way the page reads it. */
export async function exportCSV(client: EdgeClient, type: string): Promise<Blob> {
  return await client.exportCSV(type, { limit: 5000 });
}

export async function record(client: EdgeClient, type: string, id: string): Promise<RecordDetail> {
  return await client.get<RecordDetail>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
}

export type Outcome = { ok: boolean; code?: string; message?: string; issues?: Api.FieldIssue[]; key: string };

/** One decision: queued through the outbox, then sent once with its own key. */
export async function decide(client: EdgeClient, action: CatalogAction, targetId: string, values: Record<string, unknown>, expectedRevision?: number): Promise<Outcome> {
  const key = client.draft(action.schema, { type: action.target, id: targetId }, values, [], expectedRevision, action.schemaVersion);
  const answers: Entry[] = await client.send();
  const answer = answers.find((entry) => entry.submission.idempotencyKey === key);
  if (!answer) return { ok: false, message: "The decision is saved and waiting for the host.", key };
  const ok = answer.state === "SUBMISSION_STATE_CONFIRMED";
  return { ok, code: ok ? undefined : answer.outcome, message: answer.reason, issues: answer.issues, key };
}

export function activate(client: EdgeClient, candidateId: string): Promise<{ ok: boolean; status: number; body: { id?: string; error?: { message?: string } } }> {
  return client.call("POST", "/v1/releases/active", { candidateId, key: crypto.randomUUID() });
}
