import { currentSession, endSession } from "./session";

export class HostRefusal extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    reason: string,
    readonly reasonCode = "",
    readonly currentRevision?: number,
  ) {
    super(reason);
  }
}

type Options = { signal?: AbortSignal; body?: unknown; headers?: Record<string, string> };
type Failure = { error?: { code?: string; message?: string; reason?: string; reasonCode?: string; currentRevision?: number } };

export function refusal(status: number, body: Failure | null) {
  const code = body?.error?.code ?? "";
  const said = body?.error?.message ?? body?.error?.reason ?? "";
  return new HostRefusal(status, code, said === code ? "" : said, body?.error?.reasonCode, body?.error?.currentRevision);
}

async function respond(path: string, options: Options) {
  const session = currentSession();
  const headers: Record<string, string> = { ...options.headers };
  if (session) Object.assign(headers, { authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant });
  const raw = options.body instanceof Blob ? options.body : undefined;
  const sending = options.body === undefined ? {} : { method: "POST", body: raw ?? JSON.stringify(options.body) };
  if (options.body !== undefined) headers["Content-Type"] = raw ? raw.type || "application/octet-stream" : "application/json";
  const response = await fetch(path, { signal: options.signal, headers, ...sending });
  if (response.ok) return response;
  if (response.status === 401) endSession();
  throw refusal(response.status, await response.json().catch(() => null));
}

export async function request<Answer>(path: string, options: Options = {}): Promise<Answer> {
  return (await respond(path, options)).json();
}

export async function requestBlob(path: string, signal?: AbortSignal) {
  return (await respond(path, { signal })).blob();
}
