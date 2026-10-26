import { useEffect, useMemo, useRef, useState } from "react";
import { t, useUnsavedChanges, useViewVisible } from "@platform/ui";
import { useHost } from "../index";
import { createWorkViews } from "../work/service";

/** Presentation state for original Work/Flow/Console runtime commands. The
 * existing work service gates duplicate decisions; durable unknown requests
 * remain solely in the kernel outbox, never in a second retry queue. */
export function useRunControl(type: "flow.instance" | "agent.run" | "platform.effect" | "build.dispatch", id: string) {
  const host = useHost(), visible = useViewVisible();
  const scope = JSON.stringify([host.client.connection.token, host.client.connection.tenant, host.me.principalId, type, id]);
  const current = useRef({ scope, visible, mounted: true });
  current.current.scope = scope; current.current.visible = visible;
  useEffect(() => { current.current.mounted = true; return () => { current.current.mounted = false; }; }, []);
  const active = () => current.current.mounted && current.current.scope === scope;
  const service = useMemo(() => createWorkViews(host, active), [host, scope]);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [pending, setPending] = useState<{ schema: string; payload: Record<string, unknown> }>();
  const lock = useRef(false);
  useUnsavedChanges(!!pending, () => setPending(undefined), busy);
  const control = async (schema: string, payload: Record<string, unknown> = {}) => {
    if (lock.current || !active() || !current.current.visible) return false;
    if (pending && (pending.schema !== schema || JSON.stringify(pending.payload) !== JSON.stringify(payload))) {
      setError(t("An earlier runtime decision is unconfirmed. Retry it before changing the operation.")); return false;
    }
    const original = pending ?? { schema, payload: structuredClone(payload) };
    lock.current = true; setBusy(true); setError("");
    try {
      const accepted = await service.controlRun(original.schema, { type, id }, original.payload);
      if (active()) { setPending(accepted ? undefined : original); if (!accepted) setError(t("The runtime decision is unconfirmed. Retry the original operation.")); }
      return accepted;
    } catch (failure) {
      if (active()) { setPending(failure instanceof Error && (failure.cause === "rejected" || failure.cause === "busy" || failure.cause === "blocked") ? undefined : original); setError(failure instanceof Error ? failure.message : t("The runtime decision is unconfirmed. Retry the original operation.")); }
      return false;
    } finally { lock.current = false; if (current.current.mounted) setBusy(false); }
  };
  return { busy, error, pending, control, retry: () => pending ? control(pending.schema, pending.payload) : Promise.resolve(false) };
}
