import { useEffect, useState } from "react";
import { pageUIManifest, type Api } from "@platform/kernel";
import { findDefinition, useHost } from "../index";
import type { PageSessionStore, PageSessionSnapshot, QueryView } from "./Session";
import type { VariableResult } from "./variables";
import { boundQueryDefinition, compileQueryPlan, queryView, variablePlan, planKey } from "./query-plans";

export function usePageQueries(page: Api.Page, values: Record<string, VariableResult>, session: PageSessionStore, snapshot: PageSessionSnapshot) {
  const { source, definitions } = useHost(), plans = page.document?.queries ?? {};
  const base = Object.entries(plans).map(([id, plan]) => [id, compileQueryPlan(plan, page.document?.variables ?? {}, values, source.entity(plan.object.name), plan.query ? findDefinition(definitions, plan.query.ref) : undefined, pageUIManifest.runtime.query,page.sections??[])] as const);
  const compiled = base.map(([id,result]) => [id,queryView(plans[id]!,result,result.status==="value"&&snapshot.views[planKey(id)]?.base===result.signature?snapshot.views[planKey(id)]:undefined,source.entity(plans[id]!.object.name),plans[id]?.query?findDefinition(definitions,plans[id]!.query!.ref):undefined,pageUIManifest.runtime.query)] as const);
  const [round, rerun] = useState(0);
  const requestKey = JSON.stringify(compiled);
  useEffect(() => {
    for (const [id, plan] of compiled) {
      if (plan.status === "value") void session.querySource(planKey(id)).list(plan.object, plan.query).catch(() => {});
      else session.clearQuery(planKey(id));
    }
  }, [session, requestKey, source.scope, source.revision, round]);
  const ids = JSON.stringify(Object.keys(plans));
  useEffect(() => () => session.resetQueries(JSON.parse(ids).map(planKey)), [session, ids]);
  const resources: Record<string, VariableResult> = {}, signatures: Record<string, string> = {};
  for (const id of Object.keys(page.document?.variables ?? {})) {
    const queryID=variablePlan(page,id);if(queryID===undefined)continue;
    const plan = compiled.find(([id]) => id === queryID)?.[1];
    if (!plan) { resources[id] = { status: "error", code: "Query plan is unavailable or exceeds its budget." }; continue; }
    if (plan.status !== "value") { resources[id] = plan; continue; }
    signatures[queryID] = plan.signature;
    const state = snapshot.queries[planKey(queryID)];
    const window = state && "value" in state ? state.value : undefined;
    const matches = window && JSON.stringify([window.object, window.query]) === plan.signature;
    resources[id] = state?.status === "error" && session.querySignature(planKey(queryID)) === plan.signature ? { status: "error", code: "Resource read failed" } : state?.status === "pending" || !matches ? { status: "pending" }
      : window!.records.length ? { status: "value", value: { kind: "object-set", window: window! } } : { status: "empty", value: { kind: "object-set", window: window! } };
  }
  const windows = Object.fromEntries(compiled.map(([id,result]) => [id,result.status==="value" ? {
    query:result.query,page:session.queryPage(planKey(id),result.signature),error:snapshot.queries[planKey(id)]?.status==="error"&&session.querySignature(planKey(id))===result.signature?"Resource read failed":undefined,
    searchLocked:!!plans[id]?.search,sortLocked:!!(plans[id]?.query&&boundQueryDefinition(findDefinition(definitions,plans[id]!.query!.ref),plans[id]!.query)?.query?.sort?.length),maxOffset:pageUIManifest.runtime.query.maxOffset,
    onChange:(change:QueryView)=>{const original=base.find(([key])=>key===id)?.[1];if(original?.status!=="value")return;const next=queryView(plans[id]!,original,{...(snapshot.views[planKey(id)]?.base===original.signature?snapshot.views[planKey(id)]:{}),...change},source.entity(plans[id]!.object.name),plans[id]?.query?findDefinition(definitions,plans[id]!.query!.ref):undefined,pageUIManifest.runtime.query);if(next.status==="value")session.setQueryView(planKey(id),original.signature,change);}
  }:undefined]));
  return { resources, signatures, windows, retry: (id: string) => { session.resetQueries([planKey(id)]); rerun((round) => round + 1); } };
}
