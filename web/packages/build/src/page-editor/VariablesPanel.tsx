import type { Api } from "@platform/kernel";
import { pageUIProfile, pageVariableContract, type PageVariableValue } from "@platform/app";
import { Button, Card, Checkbox, Input, PropertyList, Select, t } from "@platform/ui";
import { useState } from "react";
import { layoutID } from "../page-layout";

type Variables = NonNullable<Api.PageDocument["variables"]>;
const initial = (type: string) => type === "boolean" ? false : "";
const expression = (op: string): Api.PageExpression => {
  const contract = pageVariableContract.operators.find((item) => item.id === op)!;
  return { op, args: Array.from({ length: contract.minArgs }, () => ({ literal: initial(contract.input) })) };
};

export function VariablesPanel({ document, sections, values, onChange }: { document: Api.PageDocument; sections: { id?: string; widget: string; title?: string }[]; values: Record<string, PageVariableValue>; onChange: (document: Api.PageDocument) => void }) {
  const variables = document.variables ?? {}, [chosen, choose] = useState("");
  const id = variables[chosen] ? chosen : Object.keys(variables)[0] ?? "", variable = variables[id];
  const update = (next: Variables) => onChange({ ...document, uiProfile: pageUIProfile, variables: next });
  const patch = (change: Partial<Api.PageVariable>) => update({ ...variables, [id]: { ...variable!, ...change } });
  const targets = Object.values(document.nodes).filter((node) => node.activeVariable === id).flatMap((node) => node.children ?? []);
  const expr = variable?.expression;
  const result = values[id], current = result && (result.status === "value" || result.status === "empty") ? result.value : undefined;
  return <Card className="grid content-start gap-3 p-3">
    <h3 className="text-sm font-semibold">{t("Page variables")}</h3>
    <p className="text-xs text-muted">{t("Variables belong to this page session. Refresh restores their initial values.")}</p>
    <Select aria-label={t("Choose page variable")} value={id} onChange={(event) => choose(event.target.value)}>
      {!id && <option value="">{t("No page variables")}</option>}
      {Object.entries(variables).map(([key, value]) => <option key={key} value={key}>{value.title || key}</option>)}
    </Select>
    <Button disabled={Object.keys(variables).length >= pageVariableContract.maxVariables} onClick={() => {
      const key = layoutID("value"); update({ ...variables, [key]: { title: t("Variable {n}", { n: Object.keys(variables).length + 1 }), scope: "page", type: "string", mode: "state", initial: "" } }); choose(key);
    }}>{t("Add variable")}</Button>
    {variable && <>
      <label className="grid gap-1 text-xs">{t("Variable label")}<Input value={variable.title ?? ""} onChange={(event) => patch({ title: event.target.value })} /></label>
      <p className="break-all font-mono text-[10px] text-muted">{id}</p>
      <label className="grid gap-1 text-xs">{t("Variable mode")}<Select value={variable.mode} onChange={(event) => {
        const mode = event.target.value;
        if (mode === "resource") {
          const resource = pageVariableContract.resources[0];
          patch({ mode, type: resource.type, initial: undefined, expression: undefined, source: { kind: resource.kind, section: sections.find((section) => section.widget === resource.widget)?.id ?? "" } });
        } else {
          const type = variable.type === "string" ? "string" : "boolean";
          patch({ mode, type, source: undefined, initial: mode === "derived" ? undefined : initial(type), expression: mode === "derived" ? expression(type === "string" ? "concat" : "equal") : undefined });
        }
      }}><option value="state">{t("State")}</option><option value="constant">{t("Constant")}</option><option value="derived">{t("Derived")}</option><option value="resource">{t("Resource output")}</option></Select></label>
      <label className="grid gap-1 text-xs">{t("Value type")}<Select value={variable.type} disabled={variable.mode === "derived" || variable.mode === "resource"} onChange={(event) => patch({ type: event.target.value, initial: initial(event.target.value) })}>
        <option value="string">{t("Text")}</option><option value="boolean">{t("Boolean")}</option>{variable.mode === "resource" && <option value={variable.type}>{t(variable.type)}</option>}</Select></label>
      {(variable.mode === "state" || variable.mode === "constant") && (targets.length ? <label className="grid gap-1 text-xs">{t("Initial tab")}<Select value={String(variable.initial)} onChange={(event) => patch({ initial: event.target.value })}>
        {[...new Set(targets)].map((child, i) => <option key={child} value={child}>{document.nodes[child]?.title || t("Tab {n}", { n: i + 1 })}</option>)}</Select></label>
        : variable.type === "boolean" ? <Checkbox checked={variable.initial === true} onChange={(value) => patch({ initial: value })}>{t("Initial value")}</Checkbox>
        : <label className="grid gap-1 text-xs">{t("Initial value")}<Input value={String(variable.initial ?? "")} onChange={(event) => patch({ initial: event.target.value })} /></label>)}
      {variable.mode === "resource" && <>
        <label className="grid gap-1 text-xs">{t("Resource output kind")}<Select value={variable.source?.kind ?? "record"} onChange={(event) => {
          const resource = pageVariableContract.resources.find((resource) => resource.kind === event.target.value)!;
          patch({ type: resource.type, source: { kind: resource.kind, section: sections.find((section) => section.widget === resource.widget)?.id ?? "" } });
        }}><option value="record">{t("Record selection")}</option><option value="filter">{t("Filter values")}</option><option value="query">{t("Query window")}</option></Select></label>
        <label className="grid gap-1 text-xs">{t("Source widget")}<Select value={variable.source?.section ?? ""} onChange={(event) => patch({ source: { kind: variable.source!.kind, section: event.target.value } })}>
          <option value="">{t("Choose a source widget")}</option>{sections.filter((section) => section.widget === pageVariableContract.resources.find((resource) => resource.kind === variable.source?.kind)?.widget).map((section) => <option key={section.id} value={section.id}>{section.title || section.widget}</option>)}
        </Select></label>
        <p className="text-xs text-muted">{t("Uses the widget's original binding and read permissions. A query window is not the full object set.")}</p>
      </>}
      {expr && <>
        <label className="grid gap-1 text-xs">{t("Operator")}<Select value={expr.op} onChange={(event) => patch({ expression: expression(event.target.value), type: pageVariableContract.operators.find((op) => op.id === event.target.value)!.output })}>
          {pageVariableContract.operators.map((op) => <option key={op.id} value={op.id}>{t(op.id)}</option>)}</Select></label>
        {expr.args.map((arg, at) => <fieldset key={at} className="grid min-w-0 gap-2 border-t border-border pt-2"><legend className="text-xs">{t("Argument {n}", { n: at + 1 })}</legend>
          <Argument value={arg} variables={variables} document={document} onChange={(value) => patch({ expression: { ...expr, args: expr.args.map((old, i) => i === at ? value : old) } })} />
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

export function NodeBindings({ document, id, onChange }: { document: Api.PageDocument; id: string; onChange: (patch: Partial<Api.PageLayoutNode>) => void }) {
  const node = document.nodes[id];
  return <Card className="grid gap-2 p-3"><label className="grid gap-1 text-xs">{t("Visible when")}<Select value={node?.visibleWhen ?? ""} onChange={(event) => onChange({ visibleWhen: event.target.value || undefined })}>
    <option value="">{t("Always visible")}</option>{Object.entries(document.variables ?? {}).filter(([, variable]) => variable.type === "boolean").map(([key, variable]) => <option key={key} value={key}>{variable.title || key}</option>)}
  </Select></label></Card>;
}
