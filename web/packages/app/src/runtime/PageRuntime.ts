import {scalarAssignable,type ScalarValue} from "./decimal";
import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import type { RecordSource } from "@platform/ui";
import { PageSessionStore, type SelectionPlan } from "./Session";
import { pageUIManifest, type Api } from "@platform/kernel";
import { compileVariables, evaluateVariables, type VariableResult } from "./variables";

export const pageVariableContract = pageUIManifest.runtime;
export const pageVariableValues = (variables: Record<string,Api.PageVariable>, state:Record<string,unknown> = {}) => evaluateVariables(variables,state,pageVariableContract);
export const pageVariableDiagnostics = (variables: Record<string, Api.PageVariable>) => compileVariables(variables, pageVariableContract).issues;

/** Component-local state. Its containing page session is keyed by member and
 * definition, so identity, publication and document edits dispose this state.
 */
export function usePageVariables(variables: Record<string, Api.PageVariable>, state: Record<string, unknown>, session: PageSessionStore, resources: Record<string, VariableResult>) {
  const values = useMemo(() => evaluateVariables(variables, state, pageVariableContract, resources), [variables, state, resources]);
  const setMany = (changes: Record<string, ScalarValue>) => {
    if (Object.entries(changes).some(([id, value]) => {
      const variable = variables[id];
      return variable?.scope !== "page" || variable?.mode !== "state" || !scalarAssignable(variable.type,value,pageVariableContract.maxStringBytes,pageVariableContract.decimal.maxBytes);
    })) return;
    session.setScalars(changes);
  };
  return { values, set: (id: string, value: ScalarValue) => setMany({ [id]: value }), setMany };
}

export function usePageSession(source: RecordSource, plan: SelectionPlan) {
  const [session] = useState(() => new PageSessionStore(source, plan));
  const snapshot = useSyncExternalStore(session.subscribe, session.snapshot, session.snapshot);
  useEffect(() => { session.activate(); return () => session.dispose(); }, [session]);
  useEffect(() => { session.updateSource(source); }, [session, source, source.scope, source.revision]);
  return { session, snapshot };
}
