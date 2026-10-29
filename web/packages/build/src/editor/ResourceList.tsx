import { useHost, useRecordInventory } from "@platform/app";
import { Button, GroupedList, RecordList, Tag, Panel, t, type EntityRecord, type RecordSource } from "@platform/ui";
import { ListTree, Table2 } from "lucide-react";
import { useState } from "react";
import { kindOfType } from "../projects/resources";
import { projectGroupOfRecord, useProjectIndex } from "../projects/membership";
import { DraftStatus } from "./workbench";
import { SystemDefinitionReferences } from "./SystemDefinitions";

type Row = EntityRecord & { name?: string; title?: string; state?: string; object?: string; archived?: boolean; version?: number };

const stateOf = (r: Row) => r.state ?? (typeof (r as Record<string, unknown>).active === "boolean" ? ((r as Record<string, unknown>).active ? "active" : "inactive") : undefined);

export function ResourceList({ source, type, fields = [], onOpen }: { source: RecordSource; type: string; fields?: string[]; onOpen?: (record: EntityRecord) => void }) {
  const host = useHost();
  const [mode, setMode] = useState<"tree" | "table">("tree");
  const inventory = useRecordInventory<Row>(type);
  const projects = useProjectIndex();
  const info = source.entity(type);
  const title = (field: string) => info?.fields.find((f) => f.name === field)?.title ?? field;
  const objectTitle = (name?: string) => name ? host.entities.find((e) => e.type === name)?.title ?? name : t("No object");
  const rows = (inventory.data?.records ?? []).filter((r) => !r.archived);
  const inProject = !!kindOfType(type);
  const byObject = rows.some((r) => r.object);
  const extras = fields.filter((f) => !["title", "name", "state", "object", "version", "active"].includes(f));
  const groupings = [
    ...(inProject ? [{ id: "project", label: t("Project"), of: (r: Row) => projectGroupOfRecord(projects, type, r.name ?? "") }] : []),
    ...(byObject ? [{ id: "object", label: t("Object type"), of: (r: Row) => ({ id: r.object ?? "_", label: objectTitle(r.object), hint: r.object }) }] : []),
    { id: "status", label: t("Status"), of: (r: Row) => { const s = stateOf(r) ?? "—"; return { id: s, label: t(s === "published" ? "Published" : s === "draft" ? "Draft" : s) }; } },
    { id: "none", label: t("No grouping"), of: () => ({ id: "", label: "" }) },
  ];
  const toggle = <span className="ml-auto flex gap-1">
    <Button size="sm" variant={mode === "tree" ? "primary" : "ghost"} aria-pressed={mode === "tree"} onClick={() => setMode("tree")}><ListTree />{t("Tree")}</Button>
    <Button size="sm" variant={mode === "table" ? "primary" : "ghost"} aria-pressed={mode === "table"} onClick={() => setMode("table")}><Table2 />{t("Table")}</Button>
  </span>;
  const content = mode === "table" ? <div className="grid gap-2"><div className="flex">{toggle}</div><RecordList source={source} type={type} fields={fields} onOpen={onOpen} /></div>
    : inventory.isError ? <Panel role="alert" className="grid gap-2 text-sm"><p>{t("The resource inventory is unavailable. This does not mean the application has no assets.")}</p>
      <div>{toggle}<Button size="sm" onClick={() => void inventory.refetch()}>{t("Refresh")}</Button></div></Panel>
    : <GroupedList<Row> items={rows} id={(r) => r.id} text={(r) => `${r.title ?? ""} ${r.name ?? ""} ${r.object ?? ""} ${extras.map((f) => String((r as Record<string, unknown>)[f] ?? "")).join(" ")}`}
    onSelect={onOpen} groupings={groupings} toolbar={toggle}
    row={(r) => <>
      <span className="min-w-0 flex-1 truncate">{r.title || r.name || r.id}</span>
      {r.name && <span className="hidden shrink-0 font-mono text-2xs text-muted lg:inline">{r.name}</span>}
      {extras.map((f) => { const v = (r as Record<string, unknown>)[f]; return v === undefined || v === null || v === "" ? null : <span key={f} className="hidden shrink-0 text-muted md:inline">{title(f)}: {String(v)}</span>; })}
      {r.version !== undefined && <span className="shrink-0 text-muted">v{r.version}</span>}
      {stateOf(r) && (["draft", "published", "archived"].includes(stateOf(r)!) ? <DraftStatus state={stateOf(r)} /> : <Tag label={t(stateOf(r)!)} tone={stateOf(r) === "active" ? "success" : "neutral"} />)}
    </>}
    empty={inventory.isLoading ? t("Loading…") : t("Nothing here yet.")} />;
  return <div className="grid min-w-0 gap-3"><SystemDefinitionReferences type={type} />{content}</div>;
}
