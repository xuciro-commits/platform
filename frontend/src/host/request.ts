import { currentSession, endSession } from "./session";

export class HostRefusal extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    reason: string,
  ) {
    super(reason);
  }
}

export async function request<Answer>(path: string, signal?: AbortSignal): Promise<Answer> {
  const session = currentSession();
  const headers: Record<string, string> = session ? { authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant } : {};
  const response = await fetch(path, { signal, headers });
  if (response.ok) return response.json();
  if (response.status === 401) endSession();
  const body = await response.json().catch(() => null);
  throw new HostRefusal(response.status, body?.error?.code ?? "", body?.error?.reason ?? "");
}
