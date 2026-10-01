import type { Api } from "@platform/kernel";
import { pageUIProfile, SemanticObjectSelect } from "@platform/app";
import { Button, Card, Checkbox, Input, Select, t } from "@platform/ui";
import { layoutID } from "../page-layout";

export function InterfacePanel({ document, object, onChange }: { document: Api.PageDocument; object: Api.AssetRef; onChange: (document: Api.PageDocument) => void }) {
  const iface = document.interface ?? { version: 1 }, variables = document.variables ?? {};
  const update = (next: Api.PageInterface, values = variables) => onChange({ ...document, uiProfile: pageUIProfile, interface: next, variables: values });
  const patch = (direction: "inputs" | "outputs", id: string, value: Partial<Api.PagePort>) => update({ ...iface, [direction]: { ...iface[direction], [id]: { ...iface[direction]![id]!, ...value } } });
  return <Card className="grid gap-3 p-3"><strong className="text-sm">{t("Page interface")}</strong>
    <label className="grid gap-1 text-xs">{t("Interface version")}<Input type="number" min={1} max={65535} value={iface.version} onChange={(e) => update({ ...iface, version: Number(e.target.value) })} /></label>
    {(["inputs", "outputs"] as const).map((direction) => <div key={direction} className="grid gap-3 border-t border-border pt-3">
      <strong className="text-xs">{t(direction === "inputs" ? "Page inputs" : "Page outputs")}</strong>
      {Object.entries(iface[direction] ?? {}).map(([id, port]) => <div key={id} className="grid gap-2 border border-border p-2">
        <span className="text-xs font-mono">{id}</span>
        <label className="grid gap-1 text-xs">{t("Port name")}<Input defaultValue={id} onBlur={(e) => { const name = e.target.value.trim(); if (!name || name === id || iface[direction]?.[name]) return; const ports = { ...iface[direction] }; delete ports[id]; update({ ...iface, [direction]: { ...ports, [name]: port } }); }} /></label>
        {direction === "inputs" ? <label className="grid gap-1 text-xs">{t("Input type")}<Select value={port.type} onChange={(e) => {
          const type = e.target.value;
          update({ ...iface, inputs: { ...iface.inputs, [id]: { ...port, type, object: type === "record" ? object : undefined } } }, { ...variables, [port.variable]: { ...variables[port.variable]!, type, initial: undefined } });
        }}><option value="string">{t("Text")}</option><option value="boolean">{t("Boolean")}</option><option value="record">{t("Record")}</option></Select></label>
          : <label className="grid gap-1 text-xs">{t("Output variable")}<Select value={port.variable} onChange={(e) => { const variable = variables[e.target.value]; if (variable) patch(direction, id, { variable: e.target.value, type: variable.type, object: variable.type === "record" ? object : undefined }); }}><option value="">{t("Choose a variable")}</option>{Object.entries(variables).filter(([, v]) => v.scope === "page" && ["string", "boolean", "record"].includes(v.type)).map(([key, v]) => <option key={key} value={key}>{v.title || key}</option>)}</Select></label>}
        {port.type === "record" && <SemanticObjectSelect label={t("Port record object")} value={port.object?.name} onChange={(ref) => patch(direction, id, { object: ref })} />}
        <Checkbox checked={port.required === true} onChange={(required) => patch(direction, id, { required })}>{t(direction === "inputs" ? "Required input" : "Required output")}</Checkbox>
        <Button onClick={() => { const ports = { ...iface[direction] }; delete ports[id]; const next = { ...variables }; if (direction === "inputs") delete next[port.variable]; update({ ...iface, [direction]: ports }, next); }}>{t("Remove port")}</Button>
      </div>)}
      <Button disabled={Object.keys(iface[direction] ?? {}).length >= 16} onClick={() => {
        const variable = direction === "inputs" ? layoutID("input") : Object.entries(variables).find(([, v]) => v.scope === "page" && ["string", "boolean", "record"].includes(v.type))?.[0] ?? "";
        let n = 1; while (iface[direction]?.[`${direction === "inputs" ? "input" : "output"}${n}`]) n++;
        const id = `${direction === "inputs" ? "input" : "output"}${n}`, type = direction === "inputs" ? "string" : variables[variable]?.type ?? "string";
        update({ ...iface, [direction]: { ...iface[direction], [id]: { variable, type, object: type === "record" ? object : undefined, required: direction === "inputs" } } }, direction === "inputs" ? { ...variables, [variable]: { title: id, scope: "page", type, mode: "input" } } : variables);
      }}>{t(direction === "inputs" ? "Add input" : "Add output")}</Button>
    </div>)}
    <p className="text-xs text-muted">{t("Inputs carry values or record references. The receiver reads records using the current member's permissions.")}</p>
  </Card>;
}
