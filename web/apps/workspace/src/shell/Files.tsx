import { RequestAccessDialog, resourceEntryRoute, useHost, useNewRecord, useReadQuery } from "@platform/app";
import type { Api } from "@platform/api";
import { Button, DataTable, Dialog, Input, Notice, PageHeader, Select, Tag, Tree, t, useCanonicalRoute, useWorkspace } from "@platform/ui";
import { Icon, type IconName } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { File, Folder, FolderPlus, FolderTree, MoveRight, Plus } from "lucide-react";
import { useState, type ReactNode } from "react";

type NavNode = { kind: "all" } | { kind: "project"; resource: Api.ResourceEntry } | { kind: "folder"; folder: Api.Folder };

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
  const [requesting, setRequesting] = useState<Api.ResourceEntry>();
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const create = useNewRecord("build.app", target => open({ view: "project", params: { id: target.id } }), { essential: true });
  const select = (selected?: Api.ResourceEntry, folder = "") => open({ view: "files", params: { ...(selected ? { id: selected.id } : {}), ...(folder ? { folder } : {}) } });
  const folderNodes = (parent: string): NavNode[] => folders.filter(item => item.project === project && (item.parent ?? "") === parent).map(folder => ({ kind: "folder", folder }));
  const navRoots: NavNode[] = [{ kind: "all" }, ...projects.map(resource => ({ kind: "project" as const, resource }))];
  const navChildren = (node: NavNode): NavNode[] => node.kind === "project" && project === node.resource.draft ? folderNodes("") : [];
  const navId = (node: NavNode) => node.kind === "all" ? "all" : node.kind === "folder" ? `folder:${node.folder.id}` : `project:${node.resource.id}`;
  const navRow = (node: NavNode): ReactNode => node.kind === "all"
    ? <HStack gap={2}><Icon icon={FolderTree} /><Text maxLines={1}>{t("All projects")}</Text></HStack>
    : node.kind === "folder"
      ? <HStack gap={2}><Icon icon={Folder} /><Text maxLines={1}>{node.folder.name}</Text></HStack>
      : <HStack gap={2}>
        <Icon icon={(project === node.resource.draft ? "chevronDown" : "chevronRight") as IconName} />
        <Icon icon={Folder} />
        <Text maxLines={1}>{node.resource.title}</Text>
      </HStack>;
  const shown = resources.filter(item => (!project ? item.ref.kind === "app" : item.project === project && item.ref.kind !== "app" && (search || (item.folder ?? "") === initialFolder))
    && (state === "archived" ? item.state === "archived" : item.state !== "archived") && `${item.title} ${item.ref.name} ${item.ref.kind}`.toLocaleLowerCase().includes(search.trim().toLocaleLowerCase()));
  const visit = (resource: Api.ResourceEntry) => {
    if (resource.ref.kind === "app") { select(resource); return; }
    if (!resource.canOpen) { setError(t("Request project access to open this resource.")); setRequesting(resource); return; }
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
  return <VStack gap={3} height="100%" minHeight={0}>
    <PageHeader title={projectResource?.title || t("Files")} description={project ? t("Resources in this project") : t("Projects and their resources")} actions={<>
      {project && projectResource?.canEdit && <Button size="sm" onClick={() => { setNewFolder(true); setName(""); }}><Icon icon={FolderPlus} />{t("New folder")}</Button>}
      {create.available && <Button size="sm" onClick={() => create.take()}><Icon icon={Plus} />{t("New project")}</Button>}
      {projectResource?.canOpen && host.opens.has("build.app") && <Button size="sm" onClick={() => open(resourceEntryRoute(projectResource, host.opens))}>{t("Open project")}</Button>}
    </>} />
    {error && <Notice tone="warning" message={error} />}
    <RequestAccessDialog project={requesting?.project} open={!!requesting} onOpenChange={open => { if (!open) setRequesting(undefined); }} onRequested={() => setError("")} />
    {query.isError && <HStack gap={2}><Notice tone="danger" message={t("Resource directory could not be loaded.")} /><Button onClick={() => void query.refetch()}>{t("Retry")}</Button></HStack>}
    <StackItem size="fill"><HStack gap={0} height="100%" minHeight={0}>
      <VStack width={224} gap={1} padding={2} isScrollable>
        <Tree roots={navRoots} children={navChildren} row={navRow} id={navId}
          selected={initialFolder ? `folder:${initialFolder}` : projectResource ? `project:${projectResource.id}` : "all"}
          onSelect={node => {
            if (node.kind === "all") select();
            else if (node.kind === "folder") select(projectResource, node.folder.id);
            else select(node.resource);
          }} />
      </VStack>
      <StackItem size="fill"><VStack gap={2} padding={3} minHeight={0} height="100%">
        <HStack gap={2}>
          <Input aria-label={t("Search resources")} placeholder={t("Search resources")} value={search} onChange={event => setSearch(event.target.value)} />
          <Select aria-label={t("Status")} value={state} onChange={event => setState(event.target.value)}><option value="current">{t("Current")}</option><option value="archived">{t("Archived")}</option></Select>
        </HStack>
        <DataTable storageKey="files:resources" data={shown} getRowId={item => item.id} loading={query.isLoading} searchable={false} height="100%" onRowClick={visit} empty={t("This folder is empty.")} columns={[
          { id: "title", header: t("Name"), accessorFn: item => item.title, cell: ({ row: { original } }) => <HStack gap={2}><Icon icon={File} color="secondary" /><Text maxLines={1}>{original.title}</Text></HStack> },
          { id: "type", header: t("Type"), accessorFn: item => item.ref.kind },
          { id: "version", header: t("Version"), accessorFn: item => item.version },
          { id: "status", header: t("Status"), cell: ({ row: { original } }) => <Tag label={t(original.state)} /> },
          { id: "move", header: "", meta: { width: 72 }, cell: ({ row: { original } }) => original.canEdit && original.ref.kind !== "app" ? <Button size="icon" variant="ghost" title={t("Move")} onClick={event => { event.stopPropagation(); setMoving(original); setDestination(original.project); setDestinationFolder(original.folder ?? ""); }}><Icon icon={MoveRight} /></Button> : null },
        ]} />
      </VStack></StackItem>
    </HStack></StackItem>
    <Dialog open={newFolder} onOpenChange={setNewFolder} title={t("New folder")}>
      <VStack gap={3}><Input autoFocus aria-label={t("Folder name")} value={name} onChange={event => setName(event.target.value)} /><Button variant="primary" disabled={busy || !name.trim()} onClick={() => void saveFolder()}>{t("Create folder")}</Button></VStack>
    </Dialog>
    <Dialog open={!!moving} onOpenChange={open => { if (!open) setMoving(undefined); }} title={t("Move resource")}>
      <VStack gap={3}>
        <Select aria-label={t("Project")} value={destination} onChange={event => { setDestination(event.target.value); setDestinationFolder(""); }}>{projects.filter(item => item.canEdit).map(item => <option key={item.id} value={item.draft}>{item.title}</option>)}</Select>
        <Select aria-label={t("Folder")} value={destinationFolder} onChange={event => setDestinationFolder(event.target.value)}><option value="">{t("Project root")}</option>{folders.filter(item => item.project === destination).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</Select>
        <Button variant="primary" disabled={busy || !destination} onClick={() => void move()}>{t("Move")}</Button>
      </VStack>
    </Dialog>
    {create.dialog}
  </VStack>;
}