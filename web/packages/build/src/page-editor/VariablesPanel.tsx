import type { Api } from "@platform/kernel";
import { pageUIProfile, pageVariableContract, useHost, type PageVariableValue } from "@platform/app";
import { Button, Card, Checkbox, Input, PropertyList, Select, t } from "@platform/ui";
import { useState } from "react";
import { layoutID, loopOwner, overlayOwner, variableAccessible } from "../page-layout";

type Variables = NonNullable<Api.PageDocument["variables"]>;
const initial = (type: string) => type === "boolean" ? false : "";
const expression = (op: string): Api.PageExpression => {
  const contract = pageVariableContract.operators.find((item) => item.id === op)!;
  return { op, args: Array.from({ length: contract.minArgs }, () => ({ literal: initial(contract.input) })) };
};

export function VariablesPanel({ document, sections, values, onChange, application = false }: { application?: boolean; document: Api.PageDocument; sections: { id?: string; widget: string; title?: string }[]; values: Record<string, PageVariableValue>; onChange: (document: Api.PageDocument) => void }) {
  const { definitions } = useHost();
  const shared = Object.fromEntries(definitions.flatMap((d) => Object.entries(d.application?.variables ?? {})));
  const sharedObjects=Object.fromEntries(definitions.flatMap((d)=>Object.entries(d.application?.variables??{}).flatMap(([id,v])=>v.source?.kind==="plan"&&d.application?.queries?.[v.source.query??""]?[[id,d.application.queries[v.source.query??""]!.object]]:[])));
  const sectionOwner=(section?:string)=>{const node=Object.entries(document.nodes).find(([,n])=>n.section===section)?.[0];return node?overlayOwner(document,node):undefined;};
  const sourceSections=(scope:string,owner?:string)=>sections.filter((s)=>sectionOwner(s.id)===(scope==="overlay"?owner:undefined));
  const sourcePlans=(scope:string,owner?:string)=>Object.entries(document.queries??{}).filter(([,q])=>(q.owner??undefined)===(scope==="overlay"?owner:undefined));
  const variables = document.variables ?? {}, [chosen, choose] = useState("");
  const id = variables[chosen] ? chosen : Object.keys(variables)[0] ?? "", variable = variables[id];
  const update = (next: Variables) => onChange({ ...document, uiProfile: pageUIProfile, variables: next });
  const patch = (change: Partial<Api.PageVariable>) => update({ ...variables, [id]: { ...variable!, ...change } });
  const targets = Object.values(document.nodes).filter((node) => node.activeVariable === id).flatMap((node) => node.children ?? []);
  const expr = variable?.expression;
  const result = values[id], current = result && (result.status === "value" || result.status === "empty") ? result.value : undefined;
  return <Card className="grid content-start gap-3 p-3">
    <h3 className="text-sm font-semibold">{t(application ? "Application variables" : "Page variables")}</h3>
    <p className="text-xs text-muted">{t(application ? "These values are shared by pages of one application instance. Refresh restores initial values." : "Variables belong to this page session. Refresh restores their initial values.")}</p>
    <Select aria-label={t(application ? "Choose application variable" : "Choose page variable")} value={id} onChange={(event) => choose(event.target.value)}>
      {!id && <option value="">{t("No page variables")}</option>}
      {Object.entries(variables).map(([key, value]) => <option key={key} value={key}>{value.title || key}</option>)}
    </Select>
    <Button disabled={Object.keys(variables).length >= pageVariableContract.maxVariables} onClick={() => {
      const key = layoutID("value"); update({ ...variables, [key]: { title: t("Variable {n}", { n: Object.keys(variables).length + 1 }), scope: application ? "application" : "page", type: "string", mode: "state", initial: "" } }); choose(key);
    }}>{t("Add variable")}</Button>
    {variable && <>
      <label className="grid gap-1 text-xs">{t("Variable label")}<Input value={variable.title ?? ""} onChange={(event) => patch({ title: event.target.value })} /></label>
      <p className="break-all font-mono text-[10px] text-muted">{id}</p>
      <label className="grid gap-1 text-xs">{t("Variable scope")}<Select value={variable.scope === "page" || variable.scope === "application" ? variable.scope : `${variable.scope}:${variable.owner}`} disabled={application || variable.source?.kind === "item" || variable.mode === "input" || variable.mode === "shared"} onChange={(event) => {
        const value = event.target.value, at = value.indexOf(":");
        const scope = value === "page" ? "page" : value.slice(0, at), owner = value === "page" ? undefined : value.slice(at + 1);
        patch({ scope, owner, ...(variable.mode === "resource" ? { mode: "state", type: "string", initial: "", source: undefined, expression: undefined } : {}) });
      }}><option value="page">{t("Page")}</option>{(application || variable.mode === "shared") && <option value="application">{t("Application")}</option>}{Object.entries(document.overlays ?? {}).map(([id, overlay]) => <option key={id} value={`overlay:${id}`}>{t("Overlay")}: {overlay.title}</option>)}{Object.entries(document.nodes).filter(([, node]) => node.kind === "loop").map(([id, node], at) => <option key={id} value={`loop-item:${id}`}>{node.title || t("Loop {n}", { n: at + 1 })}</option>)}</Select></label>
      {variable.scope === "overlay" && <p className="text-xs text-muted">{t("Values belong to this overlay and reset when it closes.")}</p>}
      {variable.scope === "loop-item" && <p className="text-xs text-muted">{t("Values are local to each loop record; the page inspector has no active item.")}</p>}
      <label className="grid gap-1 text-xs">{t("Variable mode")}<Select value={variable.mode} disabled={variable.source?.kind === "item" || variable.mode === "input"} onChange={(event) => {
        const mode = event.target.value;
        if (mode === "shared") {
          const [key, declaration] = Object.entries(shared)[0] ?? [];
          patch({ scope:"application", owner:undefined, mode, type:declaration?.type ?? "string", writable:declaration?.mode === "state", source:{kind:"application",variable:key ?? "",...(declaration?.type==="object-set"?{object:sharedObjects[key??""]}: {})}, initial:undefined, expression:undefined });
        } else if (mode === "resource") {
          if(application){patch({mode,type:"object-set",initial:undefined,expression:undefined,source:{kind:"plan",query:Object.keys(document.queries??{})[0]??""}});return;}
          const resource = pageVariableContract.resources[0];
          patch({ mode, type: resource.type, initial: undefined, expression: undefined, source: { kind: resource.kind, section: sourceSections(variable.scope,variable.owner).find((section) => section.widget === resource.widget)?.id ?? "" } });
        } else {
          const type = variable.type === "string" ? "string" : "boolean";
          patch({ mode, scope: variable.mode === "shared" ? "page" : variable.scope, writable:undefined, type, source: undefined, initial: mode === "derived" ? undefined : initial(type), expression: mode === "derived" ? expression(type === "string" ? "concat" : "equal") : undefined });
        }
      }}>{variable.mode === "input" && <option value="input">{t("Page input")}</option>}{!application && (Object.keys(shared).length > 0 || variable.mode === "shared") && <option value="shared">{t("Application binding")}</option>}<option value="state">{t("State")}</option><option value="constant">{t("Constant")}</option><option value="derived">{t("Derived")}</option>{application || variable.scope === "page" || variable.scope === "overlay" || variable.source?.kind === "item" ? <option value="resource">{t("Resource output")}</option> : null}</Select></label>
      <label className="grid gap-1 text-xs">{t("Value type")}<Select value={variable.type} disabled={variable.mode === "derived" || variable.mode === "resource" || variable.mode === "input" || variable.mode === "shared"} onChange={(event) => patch({ type: event.target.value, initial: initial(event.target.value) })}>
        <option value="string">{t("Text")}</option><option value="boolean">{t("Boolean")}</option>{variable.mode === "resource" && <option value={variable.type}>{t(variable.type)}</option>}</Select></label>
      {(variable.mode === "state" || variable.mode === "constant") && (targets.length ? <label className="grid gap-1 text-xs">{t("Initial tab")}<Select value={String(variable.initial)} onChange={(event) => patch({ initial: event.target.value })}>
        {[...new Set(targets)].map((child, i) => <option key={child} value={child}>{document.nodes[child]?.title || t("Tab {n}", { n: i + 1 })}</option>)}</Select></label>
        : variable.type === "boolean" ? <Checkbox checked={variable.initial === true} onChange={(value) => patch({ initial: value })}>{t("Initial value")}</Checkbox>
        : <label className="grid gap-1 text-xs">{t("Initial value")}<Input value={String(variable.initial ?? "")} onChange={(event) => patch({ initial: event.target.value })} /></label>)}
      {variable.mode === "shared" && <label className="grid gap-1 text-xs">{t("Application variable")}<Select value={variable.source?.variable ?? ""} onChange={(event) => { const source = shared[event.target.value]; if (source) patch({ type:source.type, writable:source.mode === "state", source:{kind:"application",variable:event.target.value,...(source.type==="object-set"?{object:sharedObjects[event.target.value]}:{})} }); }}><option value="">{t("Choose an application variable")}</option>{Object.entries(shared).map(([id,v]) => <option key={id} value={id}>{v.title || id}</option>)}</Select></label>}
      {variable.mode==="resource"&&application&&<label className="grid gap-1 text-xs">{t("Query plan")}<Select value={variable.source?.query??""} onChange={(e)=>patch({source:{kind:"plan",query:e.target.value}})}><option value="">{t("Choose query plan")}</option>{Object.entries(document.queries??{}).map(([id,q])=><option key={id} value={id}>{q.title||id}</option>)}</Select></label>}
      {variable.mode === "resource" && (variable.scope === "page" || variable.scope === "overlay") && <>
        <label className="grid gap-1 text-xs">{t("Resource output kind")}<Select value={variable.source?.kind ?? "record"} onChange={(event) => {
          if (event.target.value === "plan") { patch({type:"object-set",source:{kind:"plan",query:sourcePlans(variable.scope,variable.owner)[0]?.[0]??""}});return; }
          const resource = pageVariableContract.resources.find((resource) => resource.kind === event.target.value)!;
          patch({ type: resource.type, source: { kind: resource.kind, section: sourceSections(variable.scope,variable.owner).find((section) => section.widget === resource.widget)?.id ?? "" } });
        }}><option value="record">{t("Record selection")}</option>{variable.scope==="page"&&<option value="filter">{t("Filter values")}</option>}<option value="query">{t("Query window")}</option>{sourcePlans(variable.scope,variable.owner).length>0&&<option value="plan">{t("Query plan")}</option>}</Select></label>
        {variable.source?.kind === "plan" ? <label className="grid gap-1 text-xs">{t("Query plan")}<Select value={variable.source.query??""} onChange={(event)=>patch({source:{kind:"plan",query:event.target.value}})}>{sourcePlans(variable.scope,variable.owner).map(([id,query])=><option key={id} value={id}>{query.title||id}</option>)}</Select></label> : <label className="grid gap-1 text-xs">{t("Source widget")}<Select value={variable.source?.section ?? ""} onChange={(event) => patch({ source: { kind: variable.source!.kind, section: event.target.value } })}>
          <option value="">{t("Choose a source widget")}</option>{sourceSections(variable.scope,variable.owner).filter((section) => section.widget === pageVariableContract.resources.find((resource) => resource.kind === variable.source?.kind)?.widget).map((section) => <option key={section.id} value={section.id}>{section.title || section.widget}</option>)}
        </Select></label>}
        <p className="text-xs text-muted">{t("Uses the widget's original binding and read permissions. A query window is not the full object set.")}</p>
      </>}
      {expr && <>
        <label className="grid gap-1 text-xs">{t("Operator")}<Select value={expr.op} onChange={(event) => patch({ expression: expression(event.target.value), type: pageVariableContract.operators.find((op) => op.id === event.target.value)!.output })}>
          {pageVariableContract.operators.map((op) => <option key={op.id} value={op.id}>{t(op.id)}</option>)}</Select></label>
        {expr.args.map((arg, at) => <fieldset key={at} className="grid min-w-0 gap-2 border-t border-border pt-2"><legend className="text-xs">{t("Argument {n}", { n: at + 1 })}</legend>
          <Argument value={arg} variables={Object.fromEntries(Object.entries(variables).filter(([, value]) => value.scope === "page" || value.scope === "application" || value.scope === variable.scope && value.owner === variable.owner))} document={document} onChange={(value) => patch({ expression: { ...expr, args: expr.args.map((old, i) => i === at ? value : old) } })} />
          {expr.args.length > (pageVariableContract.operators.find((op) => op.id === expr.op)?.minArgs ?? 0) && <Button onClick={() => patch({ expression: { ...expr, args: expr.args.filter((_, i) => i !== at) } })}>{t("Remove argument")}</Button>}
        </fieldset>)}
        <Button disabled={expr.args.length >= (pageVariableContract.operators.find((op) => op.id === expr.op)?.maxArgs ?? 0)} onClick={() => patch({ expression: { ...expr, args: [...expr.args, { literal: initial(variable.type) }] } })}>{t("Add argument")}</Button>
        <p className="text-xs text-muted">{t("Dependencies")}: {expr.args.flatMap((arg) => arg.variable ? [variables[arg.variable]?.title || arg.variable] : []).join(", ") || "—"}</p>
      </>}
      <div role="region" aria-label={t("Current variable value")} className="grid gap-2 border-t border-border pt-2 text-xs">
        <strong>{t("Current variable value")}</strong><span role="status">{t(result?.status === "pending" ? "Loading value" : result?.status === "value" ? "Value available" : result?.status === "error" ? "Value error" : "No value")}</span>
        {result?.status === "error" && <p role="alert">{t(result.code)}</p>}
        {current !== undefined && (typeof current === "object" ? current.kind === "record" ? <PropertyList items={[[t("Object"), current.reference.object], [t("Record"), current.reference.id]]} />
          : current.kind === "filter" ? <PropertyList items={Object.entries(current.fields).map(([field, value]) => [field, String(value)])} />
          : <PropertyList items={[[t("Object"), current.window.object], [t("Records in window"), String(current.window.records.length)], [t("Total matching records"), String(current.window.total)], [t("Offset"), String(current.window.query.offset ?? 0)], [t("Window coverage"), t(current.window.complete ? "Complete for this read" : "Partial window")]]} />
          : <p className="break-all">{String(current)}</p>)}
      </div>
      <Button onClick={() => { const next = { ...variables }; delete next[id]; update(next); choose(""); }}>{t("Remove variable")}</Button>
    </>}
  </Card>;
}

function Argument({ value, variables, document, onChange }: { value: Api.PageValue; variables: Variables; document: Api.PageDocument; onChange: (value: Api.PageValue) => void }) {
  const text = typeof value.literal !== "boolean";
  const children = Object.values(document.nodes).filter((node) => node.kind === "tabs").flatMap((node) => node.children ?? []);
  return <>
    <Select aria-label={t("Argument source")} value={value.variable ?? ""} onChange={(event) => onChange(event.target.value ? { variable: event.target.value } : { literal: "" })}>
      <option value="">{t("Literal")}</option>{Object.entries(variables).map(([id, variable]) => <option key={id} value={id}>{variable.title || id} · {t(variable.type)}</option>)}
    </Select>
    {!value.variable && <><Select aria-label={t("Literal type")} value={text ? "string" : "boolean"} onChange={(event) => onChange({ literal: initial(event.target.value) })}>
      <option value="string">{t("Text")}</option><option value="boolean">{t("Boolean")}</option></Select>
      {text ? <><Input aria-label={t("Literal value")} value={String(value.literal ?? "")} onChange={(event) => onChange({ literal: event.target.value })} />
        {!!children.length && <Select aria-label={t("Use tab identity")} value={children.includes(String(value.literal)) ? String(value.literal) : ""} onChange={(event) => { if (event.target.value) onChange({ literal: event.target.value }); }}>
          <option value="">{t("Use tab identity")}</option>{[...new Set(children)].map((id, i) => <option key={id} value={id}>{document.nodes[id]?.title || t("Tab {n}", { n: i + 1 })}</option>)}
        </Select>}</> : <Checkbox checked={value.literal === true} onChange={(literal) => onChange({ literal })}>{t("Literal value")}</Checkbox>}
    </>}
  </>;
}

export function NodeBindings({ document, id, button, input, onChange }: { document: Api.PageDocument; id: string; button?: boolean; input?: boolean; onChange: (patch: Partial<Api.PageLayoutNode>) => void }) {
  const node = document.nodes[id], owner = loopOwner(document, id), overlay = overlayOwner(document, id);
  const visible = Object.entries(document.variables ?? {}).filter(([, variable]) => variableAccessible(variable, owner, overlay));
  const eligible = visible.filter(([, variable]) => variable.type === "boolean");
  return <Card className="grid gap-2 p-3"><label className="grid gap-1 text-xs">{t("Visible when")}<Select value={node?.visibleWhen ?? ""} onChange={(event) => onChange({ visibleWhen: event.target.value || undefined })}>
    <option value="">{t("Always visible")}</option>{eligible.map(([key, variable]) => <option key={key} value={key}>{variable.title || key}</option>)}
  </Select></label>{(button || input) && <label className="grid gap-1 text-xs">{t("Enabled when")}<Select value={node?.enabledWhen ?? ""} onChange={(event) => onChange({ enabledWhen: event.target.value || undefined })}>
    <option value="">{t("Always enabled")}</option>{eligible.map(([key, variable]) => <option key={key} value={key}>{variable.title || key}</option>)}
  </Select></label>}{input && <label className="grid gap-1 text-xs">{t("Input state variable")}<Select value={node?.valueVariable ?? ""} onChange={(event) => onChange({ valueVariable: event.target.value || undefined })}><option value="">{t("Choose a text state variable")}</option>{visible.filter(([, variable]) => variable.type === "string" && (variable.mode === "state" || variable.mode === "shared" && variable.writable)).map(([id, variable]) => <option key={id} value={id}>{variable.title || id}</option>)}</Select></label>}</Card>;
}
