import { ApplicationRuntimePanel, useDependencies, useHost, useNewRecord, useReadQuery, useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, ContextHelp, DataTable, Disclosure, Input, Notice, PanelSection, PropertyList, Select, Workbench, t, useWorkspace, type ColumnDef, type NavSection, type Route } from "@platform/ui";
import { useRef, useState } from "react";
import type { Project } from "./project";
import { assetRoute, resourceInArea, studioArea, studioAreas } from "./studio-model";
import { installedStudioRef, pageDraftFromDefinition } from "./studio-applications";
import { useStudioApplication } from "./use-studio-application";
import { useProjectResources, type OwnedResource } from "./resource-inventory";
import { joinProjectResource } from "./membership";
import { refKey } from "./resources";

function viewLinks(items: NavSection["items"]): { label: string; route: Route }[] {
  return items.flatMap(item => "route" in item ? [item] : viewLinks(item.items));
}

export function InstalledApplicationHome({ id, area, selected }: { id: string; area?: string; selected?: string }) {
  const host = useHost(), { open } = useWorkspace();
  const ref = installedStudioRef(id)!;
  const { application, definition } = useStudioApplication(id);
  const installedApps = useReadQuery<Api.AppInfo[]>("/v1/apps");
  const title = application?.title || installedApps.data?.find(app => app.id === ref.app)?.title || ref.app;
  const { owned, unavailable } = useProjectResources(application, ref.app, false);
  const activeArea = studioArea(area, selected?.split("/")[1]);
  const inventory = useRecordInventory<Project>("build.app", 1000, host.holds("build", "builder"));
  const extensions = (inventory.data?.records ?? []).filter(project => !project.archived && project.extends && refKey(project.extends) === refKey(ref));
  const [targetID, setTargetID] = useState("");
  const target = extensions.find(project => project.id === targetID) ?? extensions[0];
  const entries = (host.applications ?? []).filter(entry => entry.id === ref.app || entry.serves.includes(ref.app));
  const [entryID, setEntryID] = useState("");
  const entry = entryID || entries.find(entry => entry.id === ref.app)?.id || (entries.length === 1 ? entries[0]!.id : "");
  const [filter, setFilter] = useState("");
  const [chosen, setChosen] = useState<string>();
  const visible = owned.filter(item => resourceInArea(item, activeArea) && `${item.title} ${item.ref.name}`.toLocaleLowerCase().includes(filter.toLocaleLowerCase()));
  const current = visible.find(item => refKey(item.ref) === (chosen ?? selected)) ?? visible[0];
  const currentDefinition = host.definitions.find(item => current && refKey(item.ref) === refKey(current.ref));
  const dependencies = useDependencies(current?.ref, undefined, !!current?.installed);
  const [error, setError] = useState("");
  const [joining, setJoining] = useState(false);
  const pageApplication = useRef<string | undefined>(undefined);
  const [pendingPage, setPendingPage] = useState<{ id: string; application: string }>();
  const joinPage = async (pending: { id: string; application: string }) => {
    setPendingPage(pending); setJoining(true); setError("");
    try {
      const { record } = await host.client.get<{ record: { name: string } }>(`/v1/records/build.page/${encodeURIComponent(pending.id)}`);
      await joinProjectResource(host, pending.application, { app: "build", kind: "page", name: record.name });
      setPendingPage(undefined);
      open({ view: "module", params: { id: pending.application, application: pending.application, page: pending.id } }, { ifCurrent: { view: "project", params: { id, ...(area ? { area } : {}), ...(selected ? { asset: selected } : {}) } } });
    } catch (cause) { setError(cause instanceof Error ? cause.message : t("The page could not be added to the extension.")); }
    finally { setJoining(false); }
  };
  const createExtension = useNewRecord("build.app", target => open({ view: "project", params: { id: target.id } }), { essential: true });
  const createPage = useNewRecord("build.page", target => { if (pageApplication.current) void joinPage({ id: target.id, application: pageApplication.current }); }, { essential: true });
  let importedPage: ReturnType<typeof pageDraftFromDefinition> | undefined;
  if (currentDefinition?.page) {
    try { importedPage = pageDraftFromDefinition(currentDefinition.page); } catch {   }
  }
  const columns: ColumnDef<OwnedResource>[] = [
    { id: "title", header: t("Resource"), accessorFn: item => item.title },
    { id: "kind", header: t("Type"), accessorFn: item => t(item.kind.label) },
    { id: "identity", header: t("Identity"), accessorFn: item => refKey(item.ref) },
  ];
  return <Workbench storageKey="project" title={title} crumbs={[{ label: t("Files"), onClick: () => open({ view: "projects" }) }]}
    status={<span className="text-xs text-muted">{t(definition?.source === "code" ? "Native code" : "Published application")} · {definition?.version ?? installedApps.data?.find(app => app.id === ref.app)?.version}</span>}
    actions={<>
      <ContextHelp title={title}>{t("Inspect the declared assets and extend them without changing their owner or permissions.")}</ContextHelp>
      {entries.length > 0 && <Select aria-label={t("Application entry")} value={entry} onChange={event => setEntryID(event.target.value)}><option value="">{t("Choose an application entry")}</option>{entries.map(item => <option key={item.id} value={item.id}>{item.title}</option>)}</Select>}
      {createExtension.available && definition?.source === "code" && <Button disabled={!entry} onClick={() => createExtension.take(undefined, { extends: ref, entry, pages: [], resources: [] })}>{t("Create extension")}</Button>}
    </>}>
    <div className="grid min-w-0 content-start gap-4 overflow-auto p-4">
      {!application && <Notice tone="info" message={t("Application structure is not available with your current access.")} />}
      {unavailable && <Notice tone="warning" message={t("Some application assets are unreadable.")} />}
      {error && <Notice tone="warning" role="alert" message={error} />}
      {pendingPage && <div><Button disabled={joining} onClick={() => void joinPage(pendingPage)}>{t("Retry adding saved page")}</Button></div>}
      <PanelSection title={t(studioAreas.find(item => item.id === activeArea)!.title)}>
        <Input type="search" aria-label={t("Filter resources")} placeholder={t("Filter resources")} value={filter} onChange={event => setFilter(event.target.value)} />
        <DataTable data={visible} columns={columns} getRowId={item => refKey(item.ref)} selectedId={current && refKey(current.ref)} onRowClick={item => setChosen(refKey(item.ref))} searchable={false} height={280} />
        {current && <div className="grid min-w-0 gap-3 py-3">
          <h2 className="text-base font-semibold">{current.title}</h2>
          <PropertyList columns={2} items={[[t("Identity"), <code>{refKey(current.ref)}</code>], [t("Version"), currentDefinition?.version ?? "—"]]} />
          <div className="flex flex-wrap gap-2">
            {current.installed && <Button onClick={() => open(assetRoute(current.ref, owned, host.definitions, id))}>{t("Open declaration")}</Button>}
            {currentDefinition?.page && <Button onClick={() => open({ view: "page-preview", params: { ...current.ref, application: id } })}>{t("Preview")}</Button>}
          </div>
          {currentDefinition?.entity && <Disclosure summary={t("Properties")}><ul className="grid gap-1 pt-2 text-sm sm:grid-cols-2">{currentDefinition.entity.fields.map(field => <li key={field.name}>{field.title || field.name}</li>)}</ul></Disclosure>}
          {dependencies.data && <div className="grid gap-2 text-xs">
            {[["Uses", dependencies.data.requires], ["Used by", dependencies.data.usedBy]].map(([label, refs]) => <div key={String(label)}><strong>{t(String(label))}</strong><div className="flex flex-wrap gap-1">{(refs as Api.AssetRef[]).map(item => <Button size="sm" variant="ghost" key={refKey(item)} onClick={() => open(assetRoute(item, owned, host.definitions, id))}>{item.name}</Button>)}</div></div>)}
          </div>}
          {dependencies.isError && <p role="alert" className="text-xs text-danger">{t("Asset dependencies could not be loaded.")}</p>}
          {currentDefinition?.page && <div className="grid gap-2 border-t border-border pt-3">
            <div className="flex flex-wrap gap-2">
              <ContextHelp title={t("Create page draft")}>{t("Create an editable page in an extension. Its objects and actions keep their original owners.")}</ContextHelp>
              <Select aria-label={t("Target extension")} value={target?.id ?? ""} onChange={event => setTargetID(event.target.value)}><option value="">{t("Choose an extension")}</option>{extensions.map(item => <option key={item.id} value={item.id}>{item.title || item.name}</option>)}</Select>
              <Button disabled={!target || !importedPage || !createPage.available || !!pendingPage} onClick={() => {
                if (!target || !importedPage) return;
                pageApplication.current = target.id;
                createPage.take(undefined, { ...importedPage, name: `${ref.app}${current.ref.name}`.replace(/[^a-z0-9]/g, "") });
              }}>{t("Create page draft")}</Button>
            </div>
            {!importedPage && <p className="text-xs text-muted">{t("This page contains bindings that cannot be converted without changes. Keep it as a code reference.")}</p>}
          </div>}
        </div>}
      </PanelSection>
      {entries.length > 0 && activeArea === "interface" && <PanelSection title={t("Code views")} actions={<ContextHelp title={t("Code views")}>{t("These views are implemented in React. Open the running view to inspect its behavior.")}</ContextHelp>}>
        <div className="flex flex-wrap gap-2">{entries.flatMap(item => item.sections.flatMap(section => viewLinks(section.items).map(link => <Button key={`${item.id}/${JSON.stringify(link.route)}`} onClick={() => open(link.route)}>{link.label}</Button>)))}</div>
      </PanelSection>}
      <PanelSection title={t("Application extensions")}>
        {inventory.isError && <p role="alert" className="text-xs text-danger">{t("The extensions could not be loaded.")}</p>}
        {!extensions.length && <p className="text-sm text-muted">{t("No extensions yet. Create one to add pages and business behavior.")}</p>}
        <ul className="divide-y divide-border">{extensions.map(item => <li key={item.id}><Button variant="row" className="flex w-full justify-between p-2" onClick={() => open({ view: "project", params: { id: item.id, area: activeArea } })}><span>{item.title || item.name}</span><span className="text-xs text-muted">{t(item.state === "published" ? "Published" : "Draft")}</span></Button></li>)}</ul>
      </PanelSection>
      {application && activeArea === "release" && <ApplicationRuntimePanel owner={ref.app} name={ref.name} />}
    </div>
    {createExtension.dialog}{createPage.dialog}
  </Workbench>;
}
