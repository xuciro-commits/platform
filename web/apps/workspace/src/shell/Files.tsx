import { resourceEntryRoute, useHost, useNewRecord, useReadQuery } from "@platform/app";
import type { Api } from "@platform/api";
import { Button, DataTable, Dialog, Input, Notice, PageHeader, Select, Tag, t, useCanonicalRoute, useWorkspace } from "@platform/ui";
import { ChevronDown, File, Folder, FolderPlus, FolderTree, MoveRight, Plus } from "lucide-react";
import { useState, type ReactNode } from "react";

export function Files({ project: initialProject = "", folder: initialFolder = "" }: { project?: string; folder?: string }) {
  const host = useHost(), { open } = useWorkspace();
  const query = useReadQuery<Api.ResourceDirectory>("/v1/resources");
  const directory = query.data;
  const resources = directory?.resources ?? [], folders = directory?.folders ?? [];
  const projects = resources.filter(item => item.ref.kind === "app" && item.state !== "archived");
  const projectResource = projects.find(item => item.id === initialProject || item.draft === initialProject);
  const project = projectResource?.draft ?? "";
  useCanonicalRoute(projectResource ? { view: "files", params: { id: projectResource.id, ...(initialFolder ? { folder: initialFolder } : {}) } } : undefined);
  const [search, setSearch] = useState(""), [state, setState] = useState("current");
  const [newFolder, setNewFolder] = useState(false), [name, setName] = useState("");
  const [moving, setMoving] = useState<Api.ResourceEntry>(), [destination, setDestination] = useState(""), [destinationFolder, setDestinationFolder] = useState("");
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const create = useNewRecord("build.app", target => open({ view: "project", params: { id: target.id } }), { essential: true });
  const select = (selected?: Api.ResourceEntry, folder = "") => open({ view: "files", params: { ...(selected ? { id: selected.id } : {}), ...(folder ? { folder } : {}) } });
  const folderRows = (parent: string, depth = 0): ReactNode => folders.filter(item => item.project === project && (item.parent ?? "") === parent).map(item => <div key={item.id}>
    <button className={`flex w-full items-center gap-2 py-1.5 text-left text-sm hover:bg-row-hover ${initialFolder === item.id ? "bg-row-hover font-medium" : ""}`} style={{ paddingLeft: 12 + depth * 16 }} onClick={() => select(projectResource, item.id)}><Folder className="size-4 shrink-0" /><span className="truncate">{item.name}</span></button>
    {folderRows(item.id, depth + 1)}
  </div>);
  const shown = resources.filter(item => (!project ? item.ref.kind === "app" : item.project === project && item.ref.kind !== "app" && (search || (item.folder ?? "") === initialFolder))
    && (state === "archived" ? item.state === "archived" : item.state !== "archived") && `${item.title} ${item.ref.name} ${item.ref.kind}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const visit = (resource: Api.ResourceEntry) => {
    if (resource.ref.kind === "app") { select(resource); return; }
    if (!resource.canOpen) { setError(t("Request project access to open this resource.")); return; }
    open(resourceEntryRoute(resource, host.opens));
  };
  const saveFolder = async () => {
    setBusy(true); setError("");
    if (await host.decide("build.folder.create", { type: "build.folder", id: crypto.randomUUID() }, { name: name.trim(), project, parent: initialFolder }, { quiet: true, onRefused: setError })) { setNewFolder(false); setName(""); await query.refetch(); }
    setBusy(false);
  };
  const move = async () => {
    if (!moving) return;
    setBusy(true); setError("");
    if (await host.decide("build.resource.move", { type: "build.resource", id: moving.id }, { project: destination, folder: destinationFolder }, { quiet: true, onRefused: setError })) { setMoving(undefined); await query.refetch(); }
    setBusy(false);
  };
  return <div className="flex h-full min-h-0 flex-col gap-3">
    <PageHeader title={projectResource?.title || t("Files")} description={project ? t("Resources in this project") : t("Projects and their resources")} actions={<>
      {project && projectResource?.canEdit && <Button size="sm" onClick={() => { setNewFolder(true); setName(""); }}><FolderPlus />{t("New folder")}</Button>}
      {create.available && <Button size="sm" onClick={() => create.take()}><Plus />{t("New project")}</Button>}
      {projectResource?.canOpen && host.opens.has("build.app") && <Button size="sm" onClick={() => open(resourceEntryRoute(projectResource, host.opens))}>{t("Open project")}</Button>}
    </>} />
    {error && <Notice tone="warning" message={error} />}
    {query.isError && <><Notice tone="danger" message={t("Resource directory could not be loaded.")} /><Button onClick={() => void query.refetch()}>{t("Retry")}</Button></>}
    <div className="grid min-h-0 flex-1 grid-cols-[14rem_minmax(0,1fr)] border border-border">
      <nav aria-label={t("Folders")} className="min-h-0 overflow-auto border-r border-border bg-surface">
        <button className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-row-hover" onClick={() => select()}><FolderTree className="size-4" />{t("All projects")}</button>
        {projects.map(item => <div key={item.id}>
          <button className={`flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-row-hover ${project === item.draft && !initialFolder ? "bg-row-hover font-medium" : ""}`} onClick={() => select(item)}><ChevronDown className={`size-3 ${project === item.draft ? "" : "-rotate-90"}`} /><Folder className="size-4" /><span className="truncate">{item.title}</span></button>
          {project === item.draft && folderRows("", 1)}
        </div>)}
      </nav>
      <div className="flex min-h-0 min-w-0 flex-col gap-2 p-3">
        <div className="flex gap-2"><Input aria-label={t("Search resources")} placeholder={t("Search resources")} value={search} onChange={event => setSearch(event.target.value)} /><Select aria-label={t("Status")} value={state} onChange={event => setState(event.target.value)}><option value="current">{t("Current")}</option><option value="archived">{t("Archived")}</option></Select></div>
        <DataTable storageKey="files:resources" data={shown} getRowId={item => item.id} loading={query.isLoading} searchable={false} height="100%" onRowClick={visit} empty={t("This folder is empty.")} columns={[
          { id: "title", header: t("Name"), accessorFn: item => item.title, cell: ({ row: { original } }) => <span className="flex items-center gap-2"><File className="size-4 text-muted" />{original.title}</span> },
          { id: "type", header: t("Type"), accessorFn: item => item.ref.kind },
          { id: "version", header: t("Version"), accessorFn: item => item.version },
          { id: "status", header: t("Status"), cell: ({ row: { original } }) => <Tag label={t(original.state)} /> },
          { id: "move", header: "", meta: { width: 72 }, cell: ({ row: { original } }) => original.canEdit && original.ref.kind !== "app" ? <Button size="icon" variant="ghost" title={t("Move")} onClick={event => { event.stopPropagation(); setMoving(original); setDestination(original.project); setDestinationFolder(original.folder ?? ""); }}><MoveRight /></Button> : null },
        ]} />
      </div>
    </div>
    <Dialog open={newFolder} onOpenChange={setNewFolder} title={t("New folder")}>
      <div className="grid gap-3"><Input autoFocus aria-label={t("Folder name")} value={name} onChange={event => setName(event.target.value)} /><Button variant="primary" disabled={busy || !name.trim()} onClick={() => void saveFolder()}>{t("Create folder")}</Button></div>
    </Dialog>
    <Dialog open={!!moving} onOpenChange={open => { if (!open) setMoving(undefined); }} title={t("Move resource")}>
      <div className="grid gap-3"><Select aria-label={t("Project")} value={destination} onChange={event => { setDestination(event.target.value); setDestinationFolder(""); }}>{projects.filter(item => item.canEdit).map(item => <option key={item.id} value={item.draft}>{item.title}</option>)}</Select><Select aria-label={t("Folder")} value={destinationFolder} onChange={event => setDestinationFolder(event.target.value)}><option value="">{t("Project root")}</option>{folders.filter(item => item.project === destination).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</Select><Button variant="primary" disabled={busy || !destination} onClick={() => void move()}>{t("Move")}</Button></div>
    </Dialog>
    {create.dialog}
  </div>;
}
