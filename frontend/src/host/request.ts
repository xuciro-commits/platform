import { t } from "../i18n";
import { currentSession, endSessionIfCurrent, requireSession, type Session } from "./session";

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

type Options = { signal?: AbortSignal; body?: unknown; headers?: Record<string, string>; cache?: RequestCache };
type Failure = { error?: string | { code?: string; message?: string; reason?: string; reasonCode?: string; currentRevision?: number } };

export function refusal(status: number, body: Failure | null) {
  const error = body?.error;
  if (typeof error === "string") return new HostRefusal(status, "", error);
  const code = error?.code ?? "";
  const said = error?.message ?? error?.reason ?? "";
  return new HostRefusal(status, code, said === code ? "" : said, error?.reasonCode, error?.currentRevision);
}

const inflight = new Map<string, Promise<unknown>>();
let inflightSession: Session | null = null;

export function invalidateRequests() {
  inflight.clear();
}

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

async function respond(path: string, options: Options, session: Session | null) {
  requireSession(session);
  const headers: Record<string, string> = { ...options.headers };
  if (session) Object.assign(headers, { authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant });
  const raw = options.body instanceof Blob ? options.body : undefined;
  const sending = options.body === undefined ? {} : { method: "POST", body: raw ?? JSON.stringify(options.body) };
  if (options.body !== undefined) headers["Content-Type"] = raw ? raw.type || "application/octet-stream" : "application/json";
  const response = await fetch(path, { signal: options.signal, cache: options.cache, headers, ...sending });
  requireSession(session);
  if (response.ok) return response;
  if (response.status === 401) endSessionIfCurrent(session);
  throw refusal(response.status, await response.json().catch(() => null));
}

export async function request<Answer>(path: string, options: Options = {}): Promise<Answer> {
  const session = currentSession();
  if (inflightSession !== session) {
    invalidateRequests();
    inflightSession = session;
  }
  const decode = async (response: Response) => {
    const value = await response.json() as Answer;
    requireSession(session);
    return value;
  };
  if (options.body === undefined && !options.headers) {
    const key = path;
    let shared = inflight.get(key) as Promise<Answer> | undefined;
    if (!shared) {
      shared = respond(path, {}, session).then(decode).finally(() => {
        if (inflight.get(key) === shared) inflight.delete(key);
      });
      inflight.set(key, shared);
    }
    return withAbort(shared, options.signal);
  }
  return decode(await respond(path, options, session));
}

export async function requestBlob(path: string, signal?: AbortSignal) {
  const session = currentSession();
  const blob = await (await respond(path, { signal, cache: "no-store" }, session)).blob();
  requireSession(session);
  return blob;
}

export function reasonOf(problem: unknown) {
  return (problem instanceof Error && problem.message) || t("The platform did not accept this change.");
}
