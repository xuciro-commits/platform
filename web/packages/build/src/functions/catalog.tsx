import { useHost } from "@platform/app";
import { ActionMenu, DataTable, Input, PageHeader, SegmentedChoice, Tag, t, type ColumnDef, type ContextCommand } from "@platform/ui";
import { Braces, Plus, Sparkles, TableProperties, Workflow } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { useDraftInventory } from "../branches/records";
import { DraftStatus } from "../editor/workbench";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { projectGroupOfRecord, useProjectIndex } from "../projects/membership";
import { matchesResourceKind, resourceKinds, resourceRoute, type ResourceKindInfo, type ResourceRecord } from "../projects/resources";

type Engine = { id: string; kind: ResourceKindInfo; title: string; icon: ReactNode };
const kind = (id: string) => resourceKinds.find(item => item.kind === id)!;
const engines: Engine[] = [
  { id: "code", kind: kind("compute"), title: "Code", icon: <Braces /> },
  { id: "table", kind: kind("table"), title: "Decision table", icon: <TableProperties /> },
  { id: "ai", kind: kind("function"), title: "AI", icon: <Sparkles /> },
  { id: "flow", kind: kind("flow"), title: "Flow logic", icon: <Workflow /> },
];

type Row = { key: string; engine: Engine; record: ResourceRecord };

export function FunctionCatalog() {
  const { holds } = useHost();
  const { open } = useApplicationWorkspace();
  const application = useApplicationScope();
  const builder = holds("build", "builder");
  const inventories = engines.map(engine => useDraftInventory<ResourceRecord>(engine.kind.type, 1000, builder));
  const projects = useProjectIndex();
  const [engine, setEngine] = useState("all"), [search, setSearch] = useState("");
  const rows = useMemo(() => engines.flatMap((engine, index) => (inventories[index]?.data?.records ?? [])
    .filter(record => !record.archived && matchesResourceKind(engine.kind, record))
    .map(record => ({ key: `${engine.id}:${record.id}`, engine, record }))), inventories.map(inventory => inventory.data));
  const text = search.trim().toLocaleLowerCase();
  const visible = rows.filter(row => (engine === "all" || row.engine.id === engine) && (!text || `${row.record.title} ${row.record.name}`.toLocaleLowerCase().includes(text)))
    .sort((a, b) => (a.record.title || a.record.name).localeCompare(b.record.title || b.record.name));
  if (!builder) return <PageHeader title={t("Functions")} description={t("Only a builder can edit functions.")} />;
  const commands: ContextCommand[] = engines.map(item => ({ id: item.id, label: t(item.title), icon: item.icon, run: () => open(resourceRoute(item.kind, "new", application)) }));
  const columns: ColumnDef<Row>[] = [
    { id: "title", header: t("Function"), meta: { width: 320, pin: "left" }, accessorFn: row => row.record.title || row.record.name, cell: ({ row }) => <span className="flex min-w-0 items-center gap-2">
      <span className="grid size-6 shrink-0 place-items-center rounded-sm bg-row-selected text-primary [&_svg]:size-3.5">{row.original.engine.icon}</span>
      <span className="grid min-w-0"><span className="truncate font-medium">{row.original.record.title || row.original.record.name}</span><span className="truncate font-mono text-2xs text-muted">{row.original.record.name}</span></span>
    </span> },
    { id: "engine", header: t("Engine"), meta: { width: 140 }, accessorFn: row => t(row.engine.title), cell: ({ row }) => <Tag label={t(row.original.engine.title)} /> },
    { id: "state", header: t("Status"), meta: { width: 120 }, accessorFn: row => row.record.state ?? "", cell: ({ row }) => <DraftStatus state={row.original.record.state} /> },
    { id: "version", header: t("Version"), meta: { width: 90 }, accessorFn: row => row.record.version ?? 0, cell: ({ row }) => <span className="tabular-nums text-muted">{row.original.record.version ?? "—"}</span> },
    { id: "project", header: t("Project"), meta: { width: 220 }, accessorFn: row => projectGroupOfRecord(projects, row.engine.kind.type, row.record.name).label },
  ];
  return <div className="flex min-h-full flex-col gap-3">
    <PageHeader title={t("Functions")} meta={t("{count} functions", { count: visible.length })} help={t("A function computes; it never writes. Pick the engine that fits the logic: code, a decision table, an AI call or a flow. Actions call functions to change data.")}
      actions={<>
        <Input type="search" aria-label={t("Search functions")} placeholder={t("Search functions")} value={search} onChange={event => setSearch(event.target.value)} className="w-56" />
        <ActionMenu label={t("New function")} icon={<Plus />} variant="primary" size="md" commands={commands}>{t("New function")}</ActionMenu>
      </>} />
    <SegmentedChoice label={t("Engine")} value={engine} onChange={setEngine} options={[{ value: "all", label: t("All engines") }, ...engines.map(item => ({ value: item.id, label: t(item.title) }))]} />
    {inventories.some(inventory => inventory.isError) && <p role="alert" className="text-xs text-danger">{t("Some functions could not be loaded.")}</p>}
    <div className="min-h-[24rem] flex-1">
      <DataTable data={visible} columns={columns} getRowId={row => row.key} height="100%" searchable={false} loading={inventories.some(inventory => inventory.isLoading)}
        onRowClick={row => open(resourceRoute(row.engine.kind, row.record.id, application))} empty={text ? t("No function matches.") : t("No functions yet. Choose New function and an engine.")} />
    </div>
  </div>;
}
