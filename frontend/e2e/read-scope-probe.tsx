import { useLayoutEffect } from "react";
import { createRoot } from "react-dom/client";
import { flushSync } from "react-dom";
import { useRead, useReads } from "../src/host/read";
import { startSession, switchAccount } from "../src/host/session";

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
