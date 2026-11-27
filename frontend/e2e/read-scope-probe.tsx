import { useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import { useRead, useReads } from "../src/host/read";
import { currentSession, startSession, switchAccount } from "../src/host/session";
import { request } from "../src/host/request";
import { send } from "../src/host/write";
import type { Api } from "../src/host/contract";

type Answer = { value: string };
export function mountReadScopeProbe() {
  const root = createRoot(document.body.appendChild(document.createElement("div")));
  const observations: { single: string; many: string }[] = [];
  // Stable component identity across path changes.
  let path = "/v1/read-scope-probe/one";
  function Probe() {
    const single = useRead<Answer>(path);
    const many = useReads<Answer>([path]);
    useLayoutEffect(() => {
      observations.push({
        single: single.status === "ready" ? single.data.value : single.status,
        many: many.status === "ready" ? many.data.map(x => x.value).join(",") : many.status,
      });
    });
    return null;
  }
  startSession({ token: "probe-a", tenant: "audit-a" });
  flushSync(() => root.render(<Probe />));
  return {
    latest: () => observations.at(-1),
    switchTenant: () => {
      observations.length = 0;
      flushSync(() => switchAccount("probe-b", "audit-b"));
      return observations.slice();
    },
    changePath: () => {
      observations.length = 0;
      path = "/v1/read-scope-probe/two";
      flushSync(() => root.render(<Probe />));
      return observations.slice();
    },
  };
}


export async function probeSessionRequests() {
  const originalFetch = window.fetch;
  let release!: (response: Response) => void;
  const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { "Content-Type": "application/json" } });
  const wait = () => new Promise<Response>(resolve => { release = resolve; });
  const seat = (name: string) => startSession({ token: "probe-" + name, tenant: "audit-" + name });
  const submitted: string[] = [];
  try {
    seat("a");
    window.fetch = () => wait();
    const stale = request("/v1/probe").catch(() => undefined);
    seat("b");
    release(json({ error: "expired" }, 401));
    await stale;
    const afterOld401 = currentSession()?.tenant;

    seat("a");
    window.fetch = async (path, options) => {
      const tenant = new Headers(options?.headers).get("Platform-Tenant");
      if (path === "/v1/me") return wait();
      if (path === "/v1/declarations") return json([{ dataClass: "probe", authorityId: "test" }]);
      if (path === "/v1/submissions") {
        submitted.push(tenant ?? "");
        return json({ record: { changeId: "change", revision: 1 } });
      }
      throw Error(String(path));
    };
    const action = { target: "probe", schema: "probe.edit" } as Api.Action;
    const oldWrite = send(action, "one", {}).then(() => "accepted", () => "refused");
    seat("b");
    release(json({ tenantId: "audit-a", principalId: "a" }));
    const write = await oldWrite;

    seat("b");
    window.fetch = async () => json({ error: "expired" }, 401);
    await request("/v1/probe-current").catch(() => undefined);
    const current401Ended = currentSession()?.tenant !== "audit-b";
    return { afterOld401, write, submitted, current401Ended };
  } finally {
    window.fetch = originalFetch;
  }
}
