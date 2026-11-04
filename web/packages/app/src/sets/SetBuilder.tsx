import { useEffect, useMemo, useState, type ReactNode } from "react";
import { objectSetPath, type Api } from "@platform/kernel";
import { Button, Checkbox, DataTable, Input, Select, Textarea, columnsFor, entityFrom, humanizeKernelError, t, useDraftInput, type EntityInfo, type EntityRecord } from "@platform/ui";
import { useHost, useReadQuery } from "../index";
import { ObjectSetFilter, type ObjectSetFilterProps } from "./ObjectSetFilter";

type Mode = "object" | "interface" | "around" | "union" | "intersect" | "subtract";
export type LinkDefinition = { ref: Api.AssetRef; version: string; linkType?: Api.LinkType };
type LinkChoice = { binding: Api.AssetBinding; direction: string; object: string; name: string };

export type SetBuilderProps = {
  value: Api.ObjectSet;
  onChange: (set: Api.ObjectSet) => void;
  filter?: (props: ObjectSetFilterProps) => ReactNode;
  search?: boolean;
  combine?: boolean;
  around?: boolean;
  locked?: boolean;
  count?: boolean;
  preview?: boolean;
  previewRows?: number;
  selectedId?: string;
  onRowOpen?: (record: EntityRecord) => void;
  interfaces?: Api.Interface[];
  draftKey?: string;
};

const maxOperands = 16;
const maxDepth = 4;
const combinators = ["union", "intersect", "subtract"] as const;
const parameterKinds = ["text", "boolean", "integer", "decimal", "date", "datetime"];

const modeOf = (set: Api.ObjectSet): Mode => set.around ? "around" : set.union ? "union" : set.intersect ? "intersect" : set.subtract ? "subtract" : set.interface !== undefined ? "interface" : "object";
const operandsOf = (set: Api.ObjectSet) => set.union ?? set.intersect ?? set.subtract ?? [];

export function objectSetComplete(set: Api.ObjectSet): boolean {
  const mode = modeOf(set);
  if (mode === "object") return !!set.object;
  if (mode === "interface") return !!set.interface;
  if (mode === "around") return !!set.around?.link.sourceVersion && !!set.around.link.ref.name && objectSetComplete(set.around.from);
  const operands = operandsOf(set);
  return operands.length >= 2 && operands.every(objectSetComplete);
}

function useSetContext(extra?: Api.Interface[]) {
  const { entities, definitions, source } = useHost();
  const apps = useReadQuery<Api.AppInfo[]>("/v1/apps").data ?? [];
  const interfaces = [...apps.flatMap(app => app.interfaces ?? []), ...(extra ?? [])].filter((item, at, all) => all.findIndex(other => other.name === item.name) === at);
  const links = definitions.filter(definition => definition.linkType);
  return { entities, interfaces, links, lookup: (type: string) => source.entity(type) };
}

export function LinkTypePicker({ links, entities, from, value, direction, onChange }: { links: LinkDefinition[]; entities: EntityInfo[]; from?: string; value?: Api.AssetBinding; direction?: string; onChange: (choice: LinkChoice) => void }) {
  const titleOf = (name: string) => entities.find(item => item.type === name)?.title ?? name;
  const options = links.flatMap(definition => {
    const type = definition.linkType;
    if (!type) return [];
    return (["forward", "reverse"] as const).flatMap(direction => {
      const source = direction === "forward" ? type.parent.name : type.child.name, object = direction === "forward" ? type.child.name : type.parent.name;
      if (from && source !== from) return [];
      const binding = { ref: definition.ref, sourceVersion: definition.version };
      return [{ key: JSON.stringify([definition.ref.app, definition.ref.name, definition.version, direction]), binding, direction, object, name: type.name, label: `${direction === "forward" ? type.forward : type.reverse} · ${titleOf(source)} → ${titleOf(object)}` }];
    });
  });
  const current = JSON.stringify([value?.ref.app, value?.ref.name, value?.sourceVersion, direction]);
  return <label className="grid gap-1"><span className="font-medium">{t("Link type")}</span>
    <Select aria-label={t("Link type")} value={options.some(option => option.key === current) ? current : ""} onChange={event => { const option = options.find(item => item.key === event.target.value); if (option) onChange(option); }}>
      <option value="">{t("Choose a link type")}</option>
      {options.map(option => <option key={option.key} value={option.key}>{option.label}</option>)}
    </Select>
  </label>;
}

export function SetBuilder({ value, onChange, filter = ObjectSetFilter, search = true, combine = true, around = true, locked = false, count = true, preview = count, previewRows = 20, selectedId, onRowOpen, interfaces, draftKey = "set" }: SetBuilderProps) {
  const context = useSetContext(interfaces);
  return <div className="grid min-w-0 gap-2 text-xs" aria-label={t("Object set builder")}>
    <SetNode value={value} onChange={onChange} depth={0} path={draftKey} context={context} filter={filter} search={search} combine={combine} around={around} locked={locked} />
    {count && <SetResult set={value} links={context.links} preview={preview} previewRows={previewRows} selectedId={selectedId} onRowOpen={onRowOpen} />}
  </div>;
}

type Context = ReturnType<typeof useSetContext>;
type NodeProps = { value: Api.ObjectSet; onChange: (set: Api.ObjectSet) => void; depth: number; path: string; context: Context; filter?: (props: ObjectSetFilterProps) => ReactNode; search: boolean; combine: boolean; around: boolean; locked: boolean };

function SetNode({ value, onChange, depth, path, context, filter, search, combine, around, locked }: NodeProps) {
  const mode = modeOf(value);
  const kept = { ...(value.archived ? { archived: true } : {}), ...(value.asOf ? { asOf: value.asOf } : {}), ...(value.parameters ? { parameters: value.parameters } : {}) };
  const switchTo = (next: Mode) => {
    if (next === mode) return;
    if (next === "object") onChange({ ...kept, object: context.entities[0]?.type ?? "" });
    else if (next === "interface") onChange({ ...kept, interface: context.interfaces[0]?.name ?? "" });
    else if (next === "around") onChange({ ...kept, around: { from: {}, link: { ref: { app: "", kind: "link-type", name: "" }, sourceVersion: "" }, direction: "forward" } });
    else onChange({ ...kept, [next]: combinators.includes(mode as never) ? operandsOf(value) : [value, {}] });
  };
  const kinds: [Mode, string][] = [["object", t("Object type")], ["interface", t("Interface")], ...(around ? [["around", t("Search around")] as [Mode, string]] : []), ...(combine ? [["union", t("Union")], ["intersect", t("Intersection")], ["subtract", t("Difference")]] as [Mode, string][] : [])];
  const nested = depth < maxDepth;
  return <div className="grid min-w-0 gap-2">
    {!locked && <label className="grid gap-1"><span className="font-medium">{t("Set kind")}</span>
      <Select aria-label={t("Set kind")} value={mode} onChange={event => switchTo(event.target.value as Mode)}>
        {kinds.filter(([kind]) => nested || kind === "object" || kind === "interface").map(([kind, label]) => <option key={kind} value={kind}>{label}</option>)}
      </Select>
    </label>}
    {(mode === "object" || mode === "interface") && <BaseSet value={value} onChange={onChange} path={path} context={context} filter={filter} search={search} locked={locked} />}
    {mode === "around" && <AroundSet value={value} onChange={onChange} depth={depth} path={path} context={context} filter={filter} search={search} combine={combine} around={around} locked={locked} />}
    {combinators.includes(mode as never) && <CombinedSet mode={mode as (typeof combinators)[number]} value={value} onChange={onChange} depth={depth} path={path} context={context} filter={filter} search={search} combine={combine} around={around} locked={locked} />}
    {!locked && <label className="grid gap-1"><span className="font-medium">{t("Business time")}</span>
      <Select aria-label={t("Business time")} value={!value.asOf ? "current" : value.asOf.all ? "all" : "at"} onChange={event => { const { asOf: _, ...rest } = value; onChange(event.target.value === "current" ? rest : { ...rest, asOf: event.target.value === "all" ? { all: true } : { at: value.asOf?.at ?? "" } }); }}>
        <option value="current">{t("Current business version")}</option><option value="at">{t("At a business time")}</option><option value="all">{t("All business versions")}</option>
      </Select>
    </label>}
    {!locked && value.asOf && !value.asOf.all && <label className="grid gap-1"><span className="font-medium">{t("Business date or timestamp")}</span><Input aria-label={t("Business date or timestamp")} maxLength={64} placeholder={t("YYYY-MM-DD or ISO 8601")} value={value.asOf.at ?? ""} onChange={event => onChange({ ...value, asOf: { at: event.target.value } })} /></label>}
  </div>;
}

const parameterValue = (kind: string): Api.Value => ({ kind, status: "value", value: kind === "boolean" ? false : kind === "json" ? {} : kind === "decimal" || kind === "integer" ? "0" : "" });

function ParameterValue({ name, value, onChange }: { name: string; value: Api.Value; onChange: (value: Api.Value) => void }) {
  if (value.status === "null") return null;
  if (value.kind === "boolean") return <Checkbox checked={value.value === true} onChange={checked => onChange({ ...value, value: checked })}>{t("Parameter value")}</Checkbox>;
  if (value.kind === "json" || !parameterKinds.includes(value.kind)) return <label className="grid gap-1"><span>{t("Parameter value")}</span><Textarea parse="json" draftKey={`parameter:${name}`} aria-label={t("Parameter value")} value={JSON.stringify(value.value ?? null, null, 2)} onChange={event => onChange({ ...value, value: JSON.parse(event.target.value || "null") })} rows={3} /></label>;
  return <label className="grid gap-1"><span>{t("Parameter value")}</span><Input aria-label={t("Parameter value")} inputMode={value.kind === "decimal" || value.kind === "integer" ? "decimal" : undefined} value={String(value.value ?? "")} onChange={event => onChange({ ...value, value: event.target.value })} /></label>;
}

function BaseSet({ value, onChange, path, context, filter, search, locked }: Pick<NodeProps, "value" | "onChange" | "path" | "context" | "filter" | "search" | "locked">) {
  const isInterface = value.interface !== undefined;
  const idsDraft = useDraftInput(`${path}:ids`, (value.ids ?? []).join("\n"), false, 2570000);
  const entity = isInterface ? undefined : context.entities.find(item => item.type === value.object);
  const shape = isInterface ? context.interfaces.find(item => item.name === value.interface) : undefined;
  const fields: Api.FieldInfo[] = isInterface
    ? (shape?.fields ?? []).map(field => ({ name: field.name, title: field.title, type: field.type as Api.FieldInfo["type"] }))
    : entity?.fields ?? [];
  const parameters = Object.entries(value.parameters ?? {});
  const writeParameter = (name: string, next?: Api.Value, rename?: string) => {
    const entries = parameters.flatMap(([key, entry]) => key === name ? next ? [[rename ?? key, next] as const] : [] : [[key, entry] as const]);
    const { parameters: _, ...rest } = value;
    const updated: Api.ObjectSet = entries.length ? { ...rest, parameters: Object.fromEntries(entries) } : rest;
    if (rename && rename !== name && updated.filter) {
      const rewrite = (expression: Api.Expr): Api.Expr => ({ ...expression, ...(expression.ref === name ? { ref: rename } : {}), ...(expression.args ? { args: expression.args.map(rewrite) } : {}), ...(expression.in ? { in: rewrite(expression.in) } : {}) });
      updated.filter = rewrite(updated.filter);
    }
    onChange(updated);
  };
  return <div className="grid min-w-0 gap-2">
    {!locked && (isInterface
      ? <label className="grid gap-1"><span className="font-medium">{t("Source interface")}</span>
        <Select aria-label={t("Source interface")} value={value.interface ?? ""} onChange={event => onChange({ interface: event.target.value, ...(value.archived ? { archived: true } : {}) })}>
          <option value="">{t("Choose an interface")}</option>
          {context.interfaces.map(item => <option key={item.name} value={item.name}>{item.title} · {item.name}</option>)}
        </Select></label>
      : <label className="grid gap-1"><span className="font-medium">{t("Source object")}</span>
        <Select aria-label={t("Source object")} value={value.object ?? ""} onChange={event => onChange({ object: event.target.value, ...(value.archived ? { archived: true } : {}) })}>
          <option value="">{t("Choose an object type")}</option>
          {context.entities.map(item => <option key={item.type} value={item.type}>{item.title} · {item.type}</option>)}
        </Select></label>)}
    {(value.object || value.interface) && <>
      {!locked && <label className="grid gap-1"><span className="font-medium">{t("Record IDs, one per line")}</span><Textarea aria-label={t("Record IDs, one per line")} data-draft-key={idsDraft.path} value={idsDraft.text} onChange={event => { const raw = event.target.value; idsDraft.write(raw); const { ids: _, ...rest } = value; const ids = raw.split(/\r?\n/).filter(id => id !== ""); onChange(ids.length ? { ...rest, ids } : rest); }} onBlur={idsDraft.clear} rows={3} /></label>}
      {search && <label className="grid gap-1"><span className="font-medium">{t("Search text")}</span>
        <Input aria-label={t("Search text")} draftKey={`${path}:search`} value={value.search ?? ""} onChange={event => { const { search: _, ...rest } = value; onChange(event.target.value ? { ...rest, search: event.target.value } : rest); }} />
      </label>}
      {Array.isArray(value.domain) && value.domain.length > 0 && <p className="text-warning">{t("Legacy conditions remain fixed; add an Expr filter for new conditions.")}</p>}
      {filter && (value.object || value.interface) && <div className="grid gap-1"><span className="font-medium">{t("Filter expression")}</span>
        {filter({ draftKey: `${path}:filter`, object: value.object, fields, parameters: value.parameters, lookup: context.lookup, value: value.filter as Api.Expr | undefined, onChange: next => { const { filter: _, ...rest } = value; onChange(next ? { ...rest, filter: next } : rest); } })}
      </div>}
      {!locked && <div className="grid gap-2"><span className="font-medium">{t("Root parameters")}</span>
        {parameters.map(([name, entry], index) => <div key={index} className="grid gap-2 rounded-sm border border-border p-2">
          <label className="grid gap-1"><span>{t("Parameter name")}</span><div className="flex items-center gap-1"><span className="font-mono text-muted">root.</span><Input aria-label={t("Parameter name")} value={name.startsWith("root.") ? name.slice(5) : name} onChange={event => { const next = `root.${event.target.value}`; if (event.target.value && next.length <= 256 && (!value.parameters?.[next] || next === name)) writeParameter(name, entry, next); }} /></div></label>
          <label className="grid gap-1"><span>{t("Parameter type")}</span><Select aria-label={t("Parameter type")} value={entry.kind} onChange={event => writeParameter(name, parameterValue(event.target.value))}>{!parameterKinds.includes(entry.kind) && <option value={entry.kind}>{entry.kind}</option>}{parameterKinds.map(kind => <option key={kind} value={kind}>{t(kind)}</option>)}</Select></label>
          <Checkbox checked={entry.status === "null"} onChange={checked => writeParameter(name, checked ? { kind: entry.kind, status: "null" } : parameterValue(entry.kind))}>{t("Null parameter")}</Checkbox>
          <ParameterValue name={name} value={entry} onChange={next => writeParameter(name, next)} />
          <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => writeParameter(name)}>{t("Remove parameter")}</Button>
        </div>)}
        <Button size="sm" className="justify-self-start" disabled={parameters.length >= 64} onClick={() => { let index = 1; while (value.parameters?.[`root.parameter${index}`]) index++; onChange({ ...value, parameters: { ...value.parameters, [`root.parameter${index}`]: parameterValue("text") } }); }}>{t("Add parameter")}</Button>
      </div>}
      {!locked && <Checkbox checked={!!value.archived} onChange={archived => { const { archived: _, ...rest } = value; onChange(archived ? { ...rest, archived: true } : rest); }}>{t("Include archived records")}</Checkbox>}
    </>}
  </div>;
}

function startObject(set: Api.ObjectSet): string | undefined {
  const mode = modeOf(set);
  return mode === "object" ? set.object || undefined : undefined;
}

function AroundSet({ value, onChange, depth, path, context, filter, search, combine, around, locked }: NodeProps) {
  const link = value.around!;
  const start = startObject(link.from);
  return <div className="grid min-w-0 gap-2 border-l border-border pl-3">
    <span className="font-medium">{t("Start from")}</span>
    <SetNode value={link.from} onChange={from => onChange({ ...value, around: { ...link, from } })} depth={depth + 1} path={`${path}:from`} context={context} filter={filter} search={search} combine={combine} around={around} locked={locked} />
    <LinkTypePicker links={context.links} entities={context.entities} from={start} value={link.link} direction={link.direction} onChange={choice => onChange({ ...value, around: { ...link, link: choice.binding, direction: choice.direction } })} />
  </div>;
}

function CombinedSet({ mode, value, onChange, depth, path, context, filter, search, combine, around, locked }: NodeProps & { mode: (typeof combinators)[number] }) {
  const operands = operandsOf(value);
  const write = (next: Api.ObjectSet[]) => { const { union: _u, intersect: _i, subtract: _s, ...rest } = value; onChange({ ...rest, [mode]: next }); };
  const label = (index: number) => mode === "subtract" ? index === 0 ? t("Start with") : t("Remove records in") : index === 0 ? t("Records in") : mode === "union" ? t("or in") : t("and in");
  return <div className="grid min-w-0 gap-2">
    {operands.map((operand, index) => <div key={index} className="grid min-w-0 gap-2 border-l border-border pl-3">
      <div className="flex items-center gap-1"><span className="min-w-0 flex-1 font-medium">{label(index)}</span>
        {operands.length > 2 && <Button size="sm" variant="ghost" aria-label={t("Remove set operand")} onClick={() => write(operands.filter((_, at) => at !== index))}>{t("Remove")}</Button>}
      </div>
      <SetNode value={operand} onChange={next => write(operands.map((entry, at) => at === index ? next : entry))} depth={depth + 1} path={`${path}:${index}`} context={context} filter={filter} search={search} combine={combine} around={around} locked={locked} />
    </div>)}
    <div><Button size="sm" disabled={operands.length >= maxOperands} onClick={() => write([...operands, {}])}>{t("Add set operand")}</Button></div>
  </div>;
}

export function objectSetResultType(set: Api.ObjectSet, links: readonly LinkDefinition[]): string | undefined {
  if (set.object) return set.object;
  if (set.around) {
    const link = links.find(item => item.ref.app === set.around!.link.ref.app && item.ref.name === set.around!.link.ref.name && item.version === set.around!.link.sourceVersion)?.linkType;
    return link ? set.around.direction === "forward" ? link.child.name : link.parent.name : undefined;
  }
  return (set.union ?? set.intersect ?? set.subtract ?? []).map(operand => objectSetResultType(operand, links)).find(Boolean);
}

function SetResult({ set, links, preview, previewRows, selectedId, onRowOpen }: { set: Api.ObjectSet; links: readonly LinkDefinition[]; preview: boolean; previewRows: number; selectedId?: string; onRowOpen?: (record: EntityRecord) => void }) {
  const { client, source } = useHost();
  const key = JSON.stringify(set), complete = objectSetComplete(set);
  const [answer, setAnswer] = useState<{ key: string; scope: unknown; count?: number; partial?: boolean; page?: Api.RecordPage; error?: string }>();
  const [offset, setOffset] = useState(0);
  useEffect(() => { setOffset(0); }, [key]);
  useEffect(() => {
    if (!complete) return;
    let active = true;
    const handle = setTimeout(() => {
      const page = preview ? client.get<Api.RecordPage>(objectSetPath("load", { set, offset, limit: previewRows }), true).catch(() => undefined) : Promise.resolve(undefined);
      Promise.all([client.objectSetCount(set), page]).then(
        ([result, rows]) => { if (active) setAnswer({ key, scope: source.scope, count: result.count, partial: result.partial, page: rows }); },
        error => { if (active) setAnswer({ key, scope: source.scope, error: humanizeKernelError(error instanceof Error ? error.message : String(error)) }); },
      );
    }, 300);
    return () => { active = false; clearTimeout(handle); };
  }, [key, complete, preview, previewRows, offset, source.scope]);
  const type = objectSetResultType(set, links), info = type ? source.entity(type) : undefined;
  const columns = useMemo(() => {
    const entity = info ? entityFrom(info, {}, source) : undefined;
    return [{ id: "id", header: "ID", accessorKey: "id", meta: { width: 110 } }, ...(entity && info ? columnsFor(entity, info.fields.filter(field => !field.readOnly).slice(0, 3).map(field => field.name)) : [])];
  }, [info, source]);
  if (!complete) return <p role="status" className="text-muted">{t("Complete the set to count its records.")}</p>;
  const current = answer?.key === key && answer.scope === source.scope ? answer : undefined;
  if (!current) return <p role="status" className="text-muted">{t("Counting…")}</p>;
  if (current.error) return <p role="alert" className="text-danger">{current.error}</p>;
  const rows = (current.page?.records ?? []) as EntityRecord[];
  return <div className="grid min-w-0 gap-1" role="region" aria-label={t("Set result")}>
    <p role="status" className="tabular-nums text-muted">{current.partial ? t("At least {count} records", { count: current.count!.toLocaleString() }) : t("{count} records", { count: current.count!.toLocaleString() })}</p>
    {current.partial && <p className="text-warning">{t("Some records are hidden by markings or row policies.")}</p>}
    {preview && <DataTable storageKey={type ? `set-preview:${type}` : undefined} totalRows={current.page?.total ?? (current.partial ? undefined : current.count)} manualSorting data={rows} columns={columns as never} getRowId={(row: EntityRecord) => row.id} selectedId={selectedId} onRowClick={onRowOpen ? row => onRowOpen(row) : undefined} height={Math.min(320, 40 + rows.length * 28)} searchable={false} empty={t("No records match this set.")} />}
    {preview && <div className="flex items-center justify-between gap-2 text-xs text-muted">
      <span>{t("{shown} of {total} records", { shown: rows.length, total: (current.page?.total ?? current.count ?? 0).toLocaleString() })}</span>
      <div className="flex gap-1"><Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - previewRows))}>{t("Previous page")}</Button><Button size="sm" variant="ghost" disabled={offset + previewRows >= (current.page?.total ?? current.count ?? 0)} onClick={() => setOffset(offset + previewRows)}>{t("Next page")}</Button></div>
    </div>}
  </div>;
}
