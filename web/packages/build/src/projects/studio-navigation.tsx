import { useHost } from "@platform/app";
import { Button, Disclosure, Input, StructureRow, t, useWorkspace, type Route } from "@platform/ui";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { useStudioApplication } from "./use-studio-application";
import { kindIcon } from "./resource-icons";
import { refKey } from "./resources";
import { useProjectResources, type OwnedResource } from "./resource-inventory";
import { assetRoute, resourceInArea, studioActionGroups, studioApplicationID, studioAreaForRoute, studioAreaKinds, studioAreas, studioContext, studioResourceGroups, studioRoute } from "./studio-model";

/** The resource tree is contributed by Build, inside the existing Shell. */
export function StudioNavigation({ route }: { route?: Route }) {
  const host = useHost(), { open } = useWorkspace();
  const id = studioApplicationID(route);
  const { application: project, owner, query, installed } = useStudioApplication(id);
  const requestedArea = studioAreaForRoute(route);
  const { owned, unavailable } = useProjectResources(project, owner, !installed, { enabledKinds: studioAreaKinds(requestedArea) });
  const [filter, setFilter] = useState("");
  useEffect(() => setFilter(""), [id]);
  if (host.role("build") !== "builder" || !id) return null;
  if (!project) return <p role={query.isError ? "alert" : "status"} className="p-2 text-xs text-muted">{installed ? t("Application structure is not available with your current access.") : query.isError ? t("The project could not be loaded.") : t("Loading…")}</p>;
  const { selected, ref, area } = studioContext(route, owned, host.definitions);
  const showAssets = route?.view !== "project" || !!route.params?.asset;
  const active = ref ? refKey(ref) : selected ? refKey(selected.ref) : undefined;
  const candidates = owned.filter(item => resourceInArea(item, area));
  const search = filter.trim().toLocaleLowerCase();
  const matches = (item: OwnedResource) => [item.title, item.ref.name, t(item.kind.label)].join(" ").toLocaleLowerCase().includes(search);
  const matching = candidates.filter(matches);
  // Keep an action's object visible when only the action matches the search.
  const targets = new Set(matching.filter(item => item.ref.kind === "action").map(item => {
    const definition = host.definitions.find(definition => refKey(definition.ref) === refKey(item.ref));
    return item.ref.app + "/" + definition?.action?.target;
  }));
  const visible = candidates.filter(item => matches(item) || item.ref.kind === "object" && targets.has(item.ref.app + "/" + item.ref.name));
  const groups = studioResourceGroups(project, area, visible);
  const row = (item: OwnedResource) => <StructureRow key={refKey(item.ref)} icon={kindIcon[item.kind.kind]} label={<span className="whitespace-normal break-words" title={item.ref.name}>{item.title}</span>} selected={active === refKey(item.ref)} meta={item.missing ? t("missing") : item.record?.state === "draft" ? t("draft") : undefined} onClick={() => { const target = assetRoute(item.ref, owned, host.definitions, id); open(area === "logic" && item.ref.kind === "object" ? { ...target, params: { ...target.params, tab: "lifecycle" } } : target); }} />;
  return <div role="region" aria-label={t("Application assets")} className="pb-3">
    <h2 className="break-words px-2 pb-3 pt-1 text-sm font-semibold">{project.title || project.name}</h2>
    <div role="navigation" aria-label={t("Application workspaces")} className="mb-3 grid gap-1">
      {studioAreas.map(item => <Button key={item.id} aria-current={area === item.id ? "page" : undefined} variant="ghost" size="sm" className={area === item.id ? "justify-start bg-row-selected font-semibold max-sm:min-h-10" : "justify-start max-sm:min-h-10"} onClick={() => open(studioRoute(id, item.id))}>{t(item.title)}</Button>)}
    </div>
    {showAssets && <>
    <Input type="search" aria-label={t("Filter assets in this workspace")} placeholder={t("Filter assets in this workspace")} value={filter} onChange={event => setFilter(event.target.value)} className="h-7 text-xs" />
    {unavailable && <p role="alert" className="p-2 text-xs text-danger">{t("Some application assets are unreadable.")}</p>}
    <p className="px-2 pb-1 pt-3 text-[11px] text-muted">{t("{count} assets in this workspace", { count: matching.length })}</p>

    {groups.map((group, index) => {
      const actions = studioActionGroups(group.items, host.definitions);
      return <AssetGroup key={id + ":" + area + ":" + group.id} title={group.id.startsWith("concept:") ? group.title : t(group.title)} count={group.items.length} active={active} containsActive={group.items.some(item => refKey(item.ref) === active)} searching={!!search} initiallyOpen={index === 0 || group.items.length <= 8}>
        <div className="grid gap-0.5 py-1">{actions.groups.map(({ object, actions }) => actions.length
          ? <ObjectActions key={refKey(object.ref)} object={object} actions={actions} active={active} searching={!!search} row={row} />
          : row(object))}{actions.other.map(row)}</div>
      </AssetGroup>;
    })}
    {!groups.length && <p role="status" className="px-2 py-3 text-xs text-muted">{search ? t("No assets match your search in this workspace.") : t("No assets in this workspace.")}</p>}
    {!installed && <Button variant="ghost" size="sm" className="mt-3 w-full justify-start whitespace-normal text-left" onClick={() => open({ view: "project", params: { id, section: "concepts", ...(selected ? { asset: refKey(selected.ref) } : {}) } })}>{t("Organize business concepts")}</Button>}
    </>}
  </div>;
}

function AssetGroup({ title, count, active, containsActive, searching, initiallyOpen, children }: { title: string; count: number; active?: string; containsActive: boolean; searching: boolean; initiallyOpen: boolean; children: ReactNode }) {
  const [expanded, setExpanded] = useState(initiallyOpen);
  useEffect(() => { if (containsActive) setExpanded(true); }, [active, containsActive]);
  return <Disclosure open={searching || expanded} onToggle={setExpanded} className="mt-2" summary={<span className="flex min-w-0 flex-1 items-baseline justify-between gap-2 text-xs font-semibold"><span className="break-words">{title}</span><span className="shrink-0 text-[11px] font-normal tabular-nums text-muted">{count}</span></span>}>{children}</Disclosure>;
}

function ObjectActions({ object, actions, active, searching, row }: { object: OwnedResource; actions: OwnedResource[]; active?: string; searching: boolean; row: (item: OwnedResource) => ReactNode }) {
  const [expanded, setExpanded] = useState(false);
  const selected = actions.some(item => refKey(item.ref) === active);
  useEffect(() => { if (selected) setExpanded(true); }, [active, selected]);
  const shown = searching || expanded;
  return <div>
    <div className="flex items-center gap-1"><div className="min-w-0 flex-1">{row(object)}</div><Button size="sm" variant="ghost" aria-label={t("Actions for {name}", { name: object.title })} aria-expanded={shown} className="h-6 shrink-0 gap-1 px-1 text-[11px] text-muted" onClick={() => setExpanded(!shown)}>{actions.length}{shown ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}</Button></div>
    {shown && <div className="ml-3 grid gap-0.5 border-l border-border pl-1">{actions.map(row)}</div>}
  </div>;
}
