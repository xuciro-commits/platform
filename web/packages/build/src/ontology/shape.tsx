// The object's shape (ADR-0058 A2, A3): the interfaces it implements, so pages
// and queries written against the interface work for its records too, and the
// installed type it extends, one extension record per base record through the
// `base` reference. The host checks both again when the object is published.
import { useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, Select, t, type EntityInfo } from "@platform/ui";
import { baseField, missingInterfaceFields, type Field, type Process } from "./object-model";

/** Every interface the tenant's apps declare, with the app declaring it. */
export function useInterfaces() {
  const apps = useReadQuery<Api.AppInfo[]>("/v1/apps");
  return (apps.data ?? []).flatMap((app) => (app.interfaces ?? []).map((shape) => ({ ...shape, app: app.id })));
}

export function ShapeEditor({ process, parent, entities, onChange }: { process: Process; parent: string; entities: EntityInfo[]; onChange: (next: Process) => void }) {
  const interfaces = useInterfaces();
  const implemented = process.implements ?? [];
  const bases = entities.filter((entity) => entity.type !== parent);
  const toggle = (name: string, on: boolean) => onChange({ ...process, implements: on ? [...implemented, name] : implemented.filter((x) => x !== name) });
  const complete = (fields: { name: string; type: string; title: string }[]) =>
    onChange({ ...process, fields: [...process.fields, ...fields.map((f): Field => ({ name: f.name, title: f.title || f.name, type: f.type, search: f.name === "code" || f.name === "name" }))] });
  const extend = (type: string) => {
    const fields = process.fields.filter((f) => f.name !== baseField);
    if (type) fields.unshift({ name: baseField, title: entities.find((e) => e.type === type)?.title ?? t("Base record"), type: "reference", ref: type, required: true, inverse: parent.split(".").pop() });
    onChange({ ...process, extends: type, fields, states: type ? [] : process.states, actions: type ? [] : process.actions });
  };
  return <div className="grid gap-3 rounded-md border border-border p-3">
    <h3 className="text-sm font-semibold">{t("Shape")}</h3>
    <div className="grid gap-2">
      <p className="text-xs text-muted">{t("Interfaces this object implements. Pages and queries written against an interface work for every object implementing it.")}</p>
      {interfaces.length ? interfaces.map((shape) => {
        const on = implemented.includes(shape.name), missing = missingInterfaceFields(process.fields, shape);
        return <div key={shape.name} className="flex flex-wrap items-center gap-2 text-sm">
          <Checkbox checked={on} onChange={(checked) => toggle(shape.name, checked)}>{shape.title} <span className="font-mono text-xs text-muted">{shape.name}</span></Checkbox>
          <span className="text-xs text-muted">{shape.fields.map((f) => `${f.name}: ${t(f.type)}`).join(", ")}</span>
          {on && missing.length > 0 && <Button size="sm" onClick={() => complete(missing)}>{t("Add the {n} missing fields", { n: missing.length })}</Button>}
        </div>;
      }) : <p className="text-xs text-muted">{t("No app declares an interface yet.")}</p>}
    </div>
    <label className="grid gap-1 text-xs">{t("Extends")}
      <Select value={process.extends ?? ""} onChange={(e) => extend(e.target.value)}>
        <option value="">{t("Nothing: a type of its own")}</option>
        {bases.map((entity) => <option key={entity.type} value={entity.type}>{entity.title} · {entity.type}</option>)}
      </Select>
      <span className="text-muted">{t("An extension adds fields to an installed object: one record per base record through its required base reference. Its lifecycle stays the base type's, so it has no states or actions of its own.")}</span>
    </label>
  </div>;
}
