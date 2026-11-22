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
type Failure = { error?: string | { code?: string; message?: string; reason?: string; reasonCode?: string; currentRevision?: number } };

export function refusal(status: number, body: Failure | null) {
  const error = body?.error;
  if (typeof error === "string") return new HostRefusal(status, "", error);
  const code = error?.code ?? "";
  const said = error?.message ?? error?.reason ?? "";
  return new HostRefusal(status, code, said === code ? "" : said, error?.reasonCode, error?.currentRevision);
}

const inflight = new Map<string, Promise<unknown>>();

function withAbort<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise;
  if (signal.aborted) return Promise.reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
  return new Promise<T>((resolve, reject) => {
    const onAbort = () => reject(signal.reason ?? new DOMException("Aborted", "AbortError"));
    signal.addEventListener("abort", onAbort, { once: true });
    promise.then(
      (value) => { signal.removeEventListener("abort", onAbort); resolve(value); },
      (error) => { signal.removeEventListener("abort", onAbort); reject(error); },
    );
  });
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
  if (options.body === undefined && !options.headers) {
    const session = currentSession();
    const key = `${session?.tenant ?? ""} ${session?.token ?? ""} ${path}`;
    let shared = inflight.get(key) as Promise<Answer> | undefined;
    if (!shared) {
      shared = respond(path, {}).then((res) => res.json() as Promise<Answer>).finally(() => { inflight.delete(key); });
      inflight.set(key, shared);
    }
    return withAbort(shared, options.signal);
  }
  return (await respond(path, options)).json();
}

export async function requestBlob(path: string, signal?: AbortSignal) {
  return (await respond(path, { signal })).blob();
}
