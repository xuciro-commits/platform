import { useEffect, useState, type ReactNode } from "react";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, Input, Select, humanizeKernelError, t, type EntityInfo } from "@platform/ui";
import { useHost, useReadQuery } from "../index";

type Term = [string, string, string | number | boolean];
type FieldLike = { name: string; title: string; type: string };
type Mode = "object" | "interface" | "around" | "union" | "intersect" | "subtract";

export type SetFilterSlot = (props: {
  draftKey: string;
  object?: string;
  fields: Api.FieldInfo[];
  lookup: (type: string) => EntityInfo | undefined;
  value?: Api.Expr;
  onChange: (value?: Api.Expr) => void;
}) => ReactNode;

export type SetBuilderProps = {
  value: Api.ObjectSet;
  onChange: (set: Api.ObjectSet) => void;
  filter?: SetFilterSlot;
  search?: boolean;
  combine?: boolean;
  around?: boolean;
  locked?: boolean;
  count?: boolean;
  interfaces?: Api.Interface[];
  draftKey?: string;
};

const supported = ["text", "choice", "reference", "boolean", "integer", "decimal", "date", "datetime"];
const maxTerms = 16;
const maxOperands = 16;
const maxDepth = 4;
const combinators = ["union", "intersect", "subtract"] as const;
const operators = (type?: string) => type && ["boolean", "reference"].includes(type) ? ["=", "!="] : ["=", "!=", "<", "<=", ">", ">=", ...(type === "text" || type === "choice" ? ["like"] : [])];

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

const terms = (set: Api.ObjectSet): Term[] | undefined => {
  if (set.domain === undefined) return [];
  return Array.isArray(set.domain) && set.domain.every(term => Array.isArray(term) && term.length === 3 && typeof term[0] === "string" && typeof term[1] === "string") ? set.domain as Term[] : undefined;
};

const withTerms = (set: Api.ObjectSet, next: Term[]): Api.ObjectSet => {
  const { domain: _, ...rest } = set;
  return next.length ? { ...rest, domain: next } : rest;
};

function useSetContext(extra?: Api.Interface[]) {
  const { entities, definitions, source } = useHost();
  const apps = useReadQuery<Api.AppInfo[]>("/v1/apps").data ?? [];
  const interfaces = [...apps.flatMap(app => app.interfaces ?? []), ...(extra ?? [])].filter((item, at, all) => all.findIndex(other => other.name === item.name) === at);
  const links = definitions.filter(definition => definition.linkType);
  return { entities, interfaces, links, lookup: (type: string) => source.entity(type) };
}

export function SetBuilder({ value, onChange, filter, search = true, combine = true, around = true, locked = false, count = true, interfaces, draftKey = "set" }: SetBuilderProps) {
  const context = useSetContext(interfaces);
  return <div className="grid min-w-0 gap-2 text-xs" aria-label={t("Object set builder")}>
    <SetNode value={value} onChange={onChange} depth={0} path={draftKey} context={context} filter={filter} search={search} combine={combine} around={around} locked={locked} />
    {count && <SetCount set={value} />}
  </div>;
}

type Context = ReturnType<typeof useSetContext>;
type NodeProps = { value: Api.ObjectSet; onChange: (set: Api.ObjectSet) => void; depth: number; path: string; context: Context; filter?: SetFilterSlot; search: boolean; combine: boolean; around: boolean; locked: boolean };

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
  </div>;
}

function BaseSet({ value, onChange, path, context, filter, search, locked }: Pick<NodeProps, "value" | "onChange" | "path" | "context" | "filter" | "search" | "locked">) {
  const isInterface = value.interface !== undefined;
  const entity = isInterface ? undefined : context.entities.find(item => item.type === value.object);
  const shape = isInterface ? context.interfaces.find(item => item.name === value.interface) : undefined;
  const fields: FieldLike[] = isInterface ? shape?.fields ?? [] : entity?.fields.map(field => ({ name: field.name, title: field.title, type: field.type })) ?? [];
  const list = terms(value);
  const setTerm = (index: number, term: Term) => onChange(withTerms(value, list!.map((entry, at) => at === index ? term : entry)));
  const chooseField = (index: number, name: string) => {
    const type = fields.find(field => field.name === name)?.type;
    setTerm(index, [name, "=", type === "boolean" ? false : type === "integer" || type === "decimal" ? 0 : ""]);
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
      <div className="grid gap-1">
        <span className="font-medium">{t("Conditions")}</span>
        {list === undefined
          ? <p className="text-muted">{t("These conditions use logical operators and stay as they are.")}</p>
          : <>
            {list.map((term, index) => {
              const field = fields.find(item => item.name === term[0]);
              return <div key={index} role="group" aria-label={t("Condition {n}", { n: index + 1 })} className="grid grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)_auto] items-center gap-1">
                <Select aria-label={t("Query condition field")} value={term[0]} onChange={event => chooseField(index, event.target.value)}>
                  <option value="">{t("Choose a field")}</option>
                  {fields.filter(item => supported.includes(item.type)).map(item => <option key={item.name} value={item.name}>{item.title}</option>)}
                </Select>
                <Select aria-label={t("Query condition operator")} value={term[1]} onChange={event => setTerm(index, [term[0], event.target.value, term[2]])}>
                  {operators(field?.type).map(op => <option key={op}>{op}</option>)}
                </Select>
                {typeof term[2] === "boolean"
                  ? <Checkbox checked={term[2]} onChange={checked => setTerm(index, [term[0], term[1], checked])}>{t("Query literal value")}</Checkbox>
                  : <Input aria-label={t("Query literal value")} draftKey={`${path}:term:${index}`} type={typeof term[2] === "number" ? "number" : "text"} value={String(term[2])} onChange={event => setTerm(index, [term[0], term[1], typeof term[2] === "number" ? Number(event.target.value) : event.target.value])} />}
                <Button size="sm" variant="ghost" aria-label={t("Remove query condition")} onClick={() => onChange(withTerms(value, list.filter((_, at) => at !== index)))}>{t("Remove")}</Button>
              </div>;
            })}
            <div><Button size="sm" disabled={!fields.length || list.length >= maxTerms} onClick={() => onChange(withTerms(value, [...list, ["", "=", ""]]))}>{t("Add query condition")}</Button></div>
          </>}
      </div>
      {search && <label className="grid gap-1"><span className="font-medium">{t("Search text")}</span>
        <Input aria-label={t("Search text")} draftKey={`${path}:search`} value={value.search ?? ""} onChange={event => { const { search: _, ...rest } = value; onChange(event.target.value ? { ...rest, search: event.target.value } : rest); }} />
      </label>}
      {filter && !isInterface && value.object && <div className="grid gap-1"><span className="font-medium">{t("Filter expression")}</span>
        {filter({ draftKey: `${path}:filter`, object: value.object, fields: entity?.fields ?? [], lookup: context.lookup, value: value.filter as Api.Expr | undefined, onChange: next => { const { filter: _, ...rest } = value; onChange(next ? { ...rest, filter: next } : rest); } })}
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
  const titleOf = (name: string) => context.entities.find(item => item.type === name)?.title ?? name;
  const options = context.links.flatMap(definition => {
    const type = definition.linkType!;
    return (["forward", "reverse"] as const).flatMap(direction => {
      const from = direction === "forward" ? type.parent.name : type.child.name, to = direction === "forward" ? type.child.name : type.parent.name;
      if (start && from !== start) return [];
      return [{ key: JSON.stringify([definition.ref.app, definition.ref.name, direction]), definition, direction, label: `${direction === "forward" ? type.forward : type.reverse} · ${titleOf(from)} → ${titleOf(to)}` }];
    });
  });
  const current = JSON.stringify([link.link.ref.app, link.link.ref.name, link.direction]);
  const choose = (key: string) => {
    const option = options.find(item => item.key === key);
    if (option) onChange({ ...value, around: { ...link, link: { ref: option.definition.ref, sourceVersion: option.definition.version }, direction: option.direction } });
  };
  return <div className="grid min-w-0 gap-2 border-l border-border pl-3">
    <span className="font-medium">{t("Start from")}</span>
    <SetNode value={link.from} onChange={from => onChange({ ...value, around: { ...link, from } })} depth={depth + 1} path={`${path}:from`} context={context} filter={filter} search={search} combine={combine} around={around} locked={locked} />
    <label className="grid gap-1"><span className="font-medium">{t("Link type")}</span>
      <Select aria-label={t("Link type")} value={options.some(item => item.key === current) ? current : ""} onChange={event => choose(event.target.value)}>
        <option value="">{t("Choose a link type")}</option>
        {options.map(item => <option key={item.key} value={item.key}>{item.label}</option>)}
      </Select>
    </label>
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

function SetCount({ set }: { set: Api.ObjectSet }) {
  const { client, source } = useHost();
  const key = JSON.stringify(set), complete = objectSetComplete(set);
  const [answer, setAnswer] = useState<{ key: string; scope: unknown; count?: number; partial?: boolean; error?: string }>();
  useEffect(() => {
    if (!complete) return;
    let active = true;
    const handle = setTimeout(() => {
      client.objectSetCount(set).then(
        result => { if (active) setAnswer({ key, scope: source.scope, count: result.count, partial: result.partial }); },
        error => { if (active) setAnswer({ key, scope: source.scope, error: humanizeKernelError(error instanceof Error ? error.message : String(error)) }); },
      );
    }, 300);
    return () => { active = false; clearTimeout(handle); };
  }, [key, complete, source.scope]);
  if (!complete) return <p role="status" className="text-muted">{t("Complete the set to count its records.")}</p>;
  const current = answer?.key === key && answer.scope === source.scope ? answer : undefined;
  if (!current) return <p role="status" className="text-muted">{t("Counting…")}</p>;
  if (current.error) return <p role="alert" className="text-danger">{current.error}</p>;
  return <p role="status" className="tabular-nums text-muted">{current.partial ? t("At least {count} records", { count: current.count!.toLocaleString() }) : t("{count} records", { count: current.count!.toLocaleString() })}</p>;
}
