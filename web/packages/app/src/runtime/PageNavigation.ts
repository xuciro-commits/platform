import {isDecimal,type ScalarValue} from "./decimal";
import { useEffect, useMemo, useRef, useState } from "react";
import { useViewCall, useWorkspace } from "@platform/ui";
import { findDefinition, useHost } from "../index";
import type { Api } from "@platform/kernel";
import type { PageSessionStore, PageSessionSnapshot } from "./Session";
import type { VariableResult } from "./variables";
import { useApplicationContext } from "./ApplicationRuntime";
import { checkPortValues, navigationValues, portValues, readPageEnvelope } from "./page-values";

type EventContext = { values: Record<string, VariableResult>; set: (id: string, value: ScalarValue) => void; isActive: () => boolean };

export const inputSlot = (variable: string) => `input/${variable}`;
export type PageCall={input?:unknown;expired?:boolean;returnValue?:(value:unknown)=>void};
export function usePageInputs(page: Api.Page, session: PageSessionStore, snapshot: PageSessionSnapshot,provided?:PageCall) {
  const inherited=useViewCall(),call=provided??inherited;
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
  }, [session, inputKey,call?.input]);
  const inputs: Record<string, VariableResult> = {};
  let recordError = false;
  for (const [id, port] of Object.entries(iface?.inputs ?? {})) {
    if(port.type==="object-set" && Object.hasOwn(inputValues,id)){inputs[port.variable]={status:"value",value:inputValues[id] as import("./collection-input").CollectionInput};}
    else if (port.type === "record") {
      const state = snapshot.records[inputSlot(port.variable)];
      recordError ||= state?.status === "error";
      inputs[port.variable] = state?.status === "value" ? { status: "value", value: { kind: "record", reference: state.value } }
        : state?.status === "pending" ? { status: "pending" } : state?.status === "error" ? { status: "error", code: "Resource read failed" } : { status: "empty" };
    } else if (Object.hasOwn(inputValues, id)) inputs[port.variable] = { status: "value", value: inputValues[id] as ScalarValue };
  }
  return { inputs, error: inputError ?? (recordError ? "A page record input is unavailable." : undefined) };
}

export function usePageNavigation(page: Api.Page, live: boolean, values: Record<string, VariableResult>, set: (id: string, value: ScalarValue) => void,provided?:PageCall) {
  const { definitions, source } = useHost(), workspace = useWorkspace(),inherited=useViewCall(),call=provided??inherited;
  const application = useApplicationContext();
  const [error, setError] = useState<string>();
  const active = useRef(true); useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);
  const currentScope = useRef(source.scope); currentScope.current = source.scope;
  const currentApplication = useRef(application?.identity); currentApplication.current = application?.identity;
  const iface = page.document?.interface;
  const emit = (event: Api.PageEventBinding, context?: EventContext) => {
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
      const scope = source.scope, applicationIdentity = application?.identity;
      workspace.transfer("", { view: live ? "page" : "page-preview", params: { app: navigation.page.app, kind: "page", name: navigation.page.name, ...(application?.definition.ref.app === navigation.page.app && application.definition.application?.pages.includes(navigation.page.name) ? { application: `${application.definition.ref.app}:${application.definition.ref.name}`, instance: application.instance } : {}) } }, { version: navigation.interfaceVersion, values: input }, (raw) => {
        if (!active.current || currentScope.current !== scope || currentApplication.current !== applicationIdentity || context && !context.isActive()) return;
        const returned = readPageEnvelope(raw);
        if (!returned || returned.version !== navigation.interfaceVersion || checkPortValues(targetInterface?.outputs ?? {}, returned.values)) { setError("The returned page values do not match the interface."); return; }
        for (const [output, id] of Object.entries(navigation.results ?? {})) {
          const value = returned.values[output];
          if (typeof value !== "string" && typeof value !== "boolean" && !isDecimal(value)) continue;
          context ? context.set(id, value) : set(id, value);
        }
      });
    } catch (failure) { setError(failure instanceof Error ? failure.message : "Page navigation failed."); }
  };
  return { emit, error };
}
