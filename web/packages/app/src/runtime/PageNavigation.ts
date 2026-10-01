import { useEffect, useMemo, useRef, useState } from "react";
import { useViewCall, useWorkspace } from "@platform/ui";
import { findDefinition, useHost } from "../index";
import type { Api } from "@platform/kernel";
import type { PageSessionStore, PageSessionSnapshot } from "./Session";
import type { VariableResult } from "./variables";
import type { LoopContext } from "./LoopRuntime";
import { checkPortValues, navigationValues, portValues, readPageEnvelope } from "./page-values";

export const inputSlot = (variable: string) => `input/${variable}`;
export function usePageInputs(page: Api.Page, session: PageSessionStore, snapshot: PageSessionSnapshot) {
  const call = useViewCall();
  const iface = page.document?.interface, envelope = useMemo(() => readPageEnvelope(call?.input), [call?.input]);
  const inputValues = envelope?.values ?? {};
  const inputError = call?.expired ? "The page navigation context expired. Open it again from its caller."
    : envelope && envelope.version !== (iface?.version ?? 0) ? "The page interface version changed. Open it again from its caller."
    : checkPortValues(iface?.inputs ?? {}, inputValues);
  const inputKey = JSON.stringify([iface?.inputs, inputValues, inputError]);
  useEffect(() => {
    if (inputError) return;
    for (const [id, port] of Object.entries(iface?.inputs ?? {})) {
      if (port.type !== "record") continue;
      const reference = inputValues[id] as { object: string; id: string } | undefined;
      session.selectReference(inputSlot(port.variable), reference);
    }
  }, [session, inputKey]);
  const inputs: Record<string, VariableResult> = {};
  let recordError = false;
  for (const [id, port] of Object.entries(iface?.inputs ?? {})) {
    if (port.type === "record") {
      const state = snapshot.records[inputSlot(port.variable)];
      recordError ||= state?.status === "error";
      inputs[port.variable] = state?.status === "value" ? { status: "value", value: { kind: "record", reference: state.value } }
        : state?.status === "pending" ? { status: "pending" } : state?.status === "error" ? { status: "error", code: "Resource read failed" } : { status: "empty" };
    } else if (Object.hasOwn(inputValues, id)) inputs[port.variable] = { status: "value", value: inputValues[id] as string | boolean };
  }
  return { inputs, error: inputError ?? (recordError ? "A page record input is unavailable." : undefined) };
}

export function usePageNavigation(page: Api.Page, live: boolean, session: PageSessionStore, values: Record<string, VariableResult>, set: (id: string, value: string | boolean) => void) {
  const { definitions, source } = useHost(), workspace = useWorkspace(), call = useViewCall();
  const [error, setError] = useState<string>();
  const active = useRef(true); useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);
  const currentScope = useRef(source.scope); currentScope.current = source.scope;
  const iface = page.document?.interface;
  const emit = (event: Api.PageEventBinding, context?: LoopContext) => {
    try {
      setError(undefined);
      if (event.return) {
        if (!call?.returnValue) throw new Error("This page has no active caller.");
        const result = portValues(iface?.outputs ?? {}, values), problem = checkPortValues(iface?.outputs ?? {}, result);
        if (problem) throw new Error(problem);
        call.returnValue({ version: iface?.version ?? 0, values: result }); return;
      }
      const navigation = event.navigate;
      if (!navigation || !workspace.transfer) throw new Error("This workspace cannot transfer page context.");
      const target = findDefinition(definitions, navigation.page);
      if (!target?.page) throw new Error("The target page is unavailable.");
      const targetInterface = target.page.document?.interface;
      if ((targetInterface?.version ?? 0) !== navigation.interfaceVersion) throw new Error("The target page interface changed.");
      const input = navigationValues(navigation.inputs ?? {}, context?.values ?? values), problem = checkPortValues(targetInterface?.inputs ?? {}, input);
      if (problem) throw new Error(problem);
      const scope = source.scope;
      workspace.transfer("", { view: live ? "page" : "page-preview", params: { app: navigation.page.app, kind: "page", name: navigation.page.name } }, { version: navigation.interfaceVersion, values: input }, (raw) => {
        if (!active.current || currentScope.current !== scope || context && !session.hasLoopItem(context.owner, context.key)) return;
        const returned = readPageEnvelope(raw);
        if (!returned || returned.version !== navigation.interfaceVersion || checkPortValues(targetInterface?.outputs ?? {}, returned.values)) { setError("The returned page values do not match the interface."); return; }
        for (const [output, id] of Object.entries(navigation.results ?? {})) {
          const value = returned.values[output];
          if (typeof value !== "string" && typeof value !== "boolean") continue;
          page.document?.variables?.[id]?.scope === "loop-item" && context ? context.set(id, value) : set(id, value);
        }
      });
    } catch (failure) { setError(failure instanceof Error ? failure.message : "Page navigation failed."); }
  };
  return { emit, error };
}
