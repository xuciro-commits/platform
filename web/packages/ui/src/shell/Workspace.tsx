import { useInputDrafts } from "../fields/draft";
import { useTheme } from "../themes/theme";
import { Command } from "cmdk";
import { ArrowLeft, Bell, ChevronsLeft, Clock, FolderTree, Grid2x2, Home as HomeIcon, Maximize2, Menu as MenuIcon, Search, Sparkles, Star, X } from "lucide-react";
import { DropdownMenu } from "radix-ui";
import { createPortal } from "react-dom";
import { Component, createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Toaster, toast } from "sonner";
import { cn } from "../lib/cn";
import { routeFromHash, routeKey, routeToHash, type Route } from "./route";
import { language, languages, setLanguage, t } from "../i18n";
import { Dialog } from "../primitives/dialog";
import { Button } from "../primitives/button";
import { ViewVisibilityContext } from "./ViewVisibility";
import { ViewTransfers } from "./ViewTransfers";
import { NavigationItems, navigationLinks, routeCovers, type NavSection } from "./Navigation";
import { Tooltip } from "../primitives/tooltip";
export type { NavSection } from "./Navigation";

export type View = {
  id: string;
  title: (params: Record<string, string>) => string;
  icon?: ReactNode;
  render: (params: Record<string, string>) => ReactNode;
  documentKey?: (params: Record<string, string>) => string;
};
export type ShellCommand = { id: string; label: string; group?: string; shortcut?: string; run: () => void };
type SearchHit = { id: string; label: string; detail?: string; open: () => void };
type RecordSearch = (text: string) => Promise<SearchHit[]>;
export type Session = {
  tenant: string; principal: string; detail?: string;
  name?: string;
  options: { id: string; label: string }[]; current: string; onSwitch: (id: string) => void;
  onAccount?: () => void;
};

export type PlatformApplication = { id: string; title: string; icon?: ReactNode; description?: string; category: string; home: Route };
export type Applications = { apps: PlatformApplication[]; categories: { id: string; label: string }[]; current?: string; onSelect: (id: string) => void };
export type Rail = { home: Route; applications: Route; files?: Route; notifications?: { route: Route; unread?: number }; assist?: Route };
export type RecentEntry = { route: Route; title: string; app?: string; at: number };

type OpenOptions = { window?: "tab" | "beside" | "popout" | "inspect"; ifCurrent?: Route; from?: string; replace?: boolean };
type Unsaved = {
  register: (panel: string, owner: symbol, discard?: () => void, busy?: boolean) => void;
  ask: (panels: string[], run: () => void, owner?: symbol) => void;
};
type WorkspaceApi = { open: (route: Route, options?: OpenOptions) => void; close: (route: Route, from?: string) => void; closeAll: (from?: string) => void; notify: typeof toast; unsaved?: Unsaved;
  confirmLeave: (run: () => void, exceptPanel?: string) => void;
  retitle?: (panel: string, title: string) => void;
  transfer?: (from: string, route: Route, input: unknown, result: (value: unknown) => void) => void;
  palette: () => void;
  recent: RecentEntry[];
  favorites: string[]; toggleFavorite: (app: string) => void;
  applications?: Applications; rail?: Rail;
  sidebarSlot?: HTMLElement | null; claimSidebar?: (panel: string) => () => void; activePanel?: string; };
type Entry = { id: string; route: Route };
type Memory = { recent: RecentEntry[]; favorites: string[]; last: Record<string, Route> };
type Pending = { panels: string[]; run: () => void; owner?: symbol; cancel?: () => void };

const PanelContext = createContext<string | undefined>(undefined);
const ViewCallContext = createContext<{ input?: unknown; expired?: boolean; returnValue?: (value: unknown) => void } | undefined>(undefined);
export const useViewCall = () => useContext(ViewCallContext);

export const WorkspaceContext = createContext<WorkspaceApi | null>(null);

let serial = 0;
const entryId = (kind: "view" | "side") => `${kind}:${++serial}`;

export function useWorkspace(): WorkspaceApi {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  if (!api) throw new Error("useWorkspace outside <Workspace>");
  return { ...api, open: (route, options) => api.open(route, { ...options, from: panel }), close: route => api.close(route, panel), closeAll: () => api.closeAll(panel), confirmLeave: (run: () => void) => api.confirmLeave(run, panel), transfer: api.transfer && panel ? (_from, route, input, result) => api.transfer!(panel, route, input, result) : undefined };
}

export type ResourceSection = { id: string; label: string; icon?: ReactNode; badge?: ReactNode; group?: string };

export function ResourceNavigation({ back, title, icon, meta, sections, value, onChange }: {
  back?: { label: string; onClick: () => void }; title: string; icon?: ReactNode; meta?: ReactNode;
  sections: ResourceSection[]; value: string; onChange: (id: string) => void;
}) {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  const main = !!panel?.startsWith("view:");
  const claim = api?.claimSidebar;
  useLayoutEffect(() => main && panel && claim ? claim(panel) : undefined, [main, panel, claim]);
  const target = api?.sidebarSlot;
  if (!main || api?.activePanel !== panel || !target) return null;
  const groups: { label?: string; items: ResourceSection[] }[] = [];
  for (const section of sections) {
    const last = groups.at(-1);
    if (last && last.label === section.group) last.items.push(section); else groups.push({ label: section.group, items: [section] });
  }
  return createPortal(<div className="grid gap-1">
    {back && <button type="button" onClick={back.onClick} className="flex min-w-0 items-center gap-1.5 rounded-md px-2 py-1 text-left text-xs text-muted hover:bg-row-hover hover:text-foreground [&_svg]:size-3.5 [&_svg]:shrink-0"><ArrowLeft /><span className="truncate">{back.label}</span></button>}
    <div className="flex min-w-0 items-center gap-2 px-2 py-1.5">
      {icon && <span className="grid size-6 shrink-0 place-items-center rounded-md bg-row-selected text-primary [&_svg]:size-3.5">{icon}</span>}
      <span className="grid min-w-0"><span className="truncate text-sm font-semibold">{title}</span>{meta && <span className="truncate font-mono text-2xs text-muted">{meta}</span>}</span>
    </div>
    <nav aria-label={title} className="grid">
      {groups.map((group, index) => <div key={`${group.label ?? ""}:${index}`} className="mb-1">
        {group.label && <div className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wider text-muted">{group.label}</div>}
        {group.items.map(section => <button key={section.id} type="button" onClick={() => onChange(section.id)} aria-current={section.id === value ? "page" : undefined}
          className={cn("flex min-h-7 w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm text-foreground hover:bg-row-hover [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted", section.id === value && "bg-row-selected font-medium text-primary [&_svg]:text-primary")}>
          {section.icon}<span className="min-w-0 flex-1 truncate">{section.label}</span>{section.badge != null && <span className="shrink-0 text-xs text-muted">{section.badge}</span>}
        </button>)}
      </div>)}
    </nav>
  </div>, target);
}

export function useViewTitle(name?: string) {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  useEffect(() => { if (api?.retitle && panel && name) api.retitle(panel, name); }, [api, panel, name]);
}

export function useUnsavedChanges(dirty: boolean, discard: () => void, busy = false) {
  const inputs=useInputDrafts();dirty ||=!!inputs?.dirty;busy ||=!!inputs?.busy;
  const unsaved = useContext(WorkspaceContext)?.unsaved;
  const panel = useContext(PanelContext);
  const owner = useRef(Symbol());
  const reset = useRef(discard);
  reset.current = ()=>{inputs?.clear();discard();};
  useLayoutEffect(() => {
    if (!panel || !unsaved) return;
    unsaved.register(panel, owner.current, dirty ? () => {
      unsaved.register(panel, owner.current);
      reset.current();
    } : undefined, busy);
    return () => unsaved.register(panel, owner.current);
  }, [dirty, busy, panel, unsaved]);
  return {
    markSaved: () => { if(inputs?.dirty)return;if (panel) unsaved?.register(panel, owner.current); },
    discardChanges: () => { if (panel) unsaved?.register(panel, owner.current); reset.current(); },
    confirmDiscard: (run: () => void) => panel && unsaved ? unsaved.ask([panel], run) : run(),
    confirmOwnDiscard: (run: () => void) => panel && unsaved ? unsaved.ask([panel], run, owner.current) : run(),
  };
}

export const notify = toast;

class ViewBoundary extends Component<{ children: ReactNode; onClose: () => void; closeLabel: string }, { error?: Error }> {
  state: { error?: Error } = {};
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error) { console.error(error); }
  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert" className="grid max-w-xl gap-2 rounded-md border border-[var(--tone-danger)] bg-surface p-3 text-sm">
        <p className="font-medium">{t("This view failed to show.")}</p>
        <p className="font-mono text-xs text-muted">{this.state.error.message}</p>
        <div className="flex gap-2">
          <Button size="sm" onClick={() => this.setState({ error: undefined })}>{t("Try again")}</Button>
          <Button size="sm" variant="ghost" onClick={this.props.onClose}>{this.props.closeLabel}</Button>
        </div>
      </div>
    );
  }
}

export function Workspace({ product, productIcon, storageKey, layoutScope, scopeOf, resolveRoute, views, nav, navigation, home, commands = [], session, status, applications: appEntries, rail, onActiveRoute, onLanguage, search }: {
  product: string; productIcon?: ReactNode; storageKey: string; views: View[]; nav: NavSection[]; home: Route;
  navigation?: ReactNode;
  layoutScope?: string;
  scopeOf?: (route: Route) => string | undefined;
  resolveRoute?: (route: Route, layout?: string) => Route | Promise<Route>;
  commands?: ShellCommand[]; session?: Session; status?: ReactNode;
  applications?: Applications; rail?: Rail; onActiveRoute?: (route: Route) => void;
  search?: (text: string) => Promise<{ id: string; label: string; detail?: string; open: () => void }[]>;
  onLanguage?: (id: string) => unknown;
}) {
  const appearance = useTheme();
  const byId = useMemo(() => new Map(views.map((v) => [v.id, v])), [views]);
  const registry = useRef(byId); registry.current = byId;
  const keyOf = useCallback((route: Route) => {
    const key = registry.current.get(route.view)?.documentKey;
    return key ? JSON.stringify([route.view, key(route.params ?? {}), route.params?.call]) : routeKey(route);
  }, []);
  const routeScope = useRef(scopeOf); routeScope.current = scopeOf;
  const routeReader = useRef(resolveRoute); routeReader.current = resolveRoute;
  const currentScope = useRef(layoutScope); currentScope.current = layoutScope;

  const [stack, setStackState] = useState<Entry[]>([]);
  const stackRef = useRef<Entry[]>([]);
  const [side, setSideState] = useState<Entry & { focus: HTMLElement | null }>();
  const sideRef = useRef<Entry & { focus: HTMLElement | null }>(undefined);
  const setSide = useCallback((entry?: Entry & { focus: HTMLElement | null }) => { sideRef.current = entry; setSideState(entry); }, []);
  const [titles, setTitles] = useState<Record<string, string>>({});
  const drafts = useRef(new Map<string, Map<symbol, { discard: () => void; busy: boolean }>>());
  const transfers = useRef(new ViewTransfers());
  const [pending, setPending] = useState<Pending>();
  const lastHash = useRef<string | undefined>(undefined);
  const revision = useRef(0);

  const routeOf = useCallback((id: string) => stackRef.current.find(entry => entry.id === id)?.route ?? (sideRef.current?.id === id ? sideRef.current.route : undefined), []);
  const forget = useCallback((ids: string[]) => {
    for (const id of ids) { transfers.current.remove(id); drafts.current.delete(id); }
    setTitles(previous => Object.fromEntries(Object.entries(previous).filter(([id]) => !ids.includes(id))));
  }, []);
  const deferred = useRef<{ panels: string[]; run: () => void; cancel?: () => void; selected?: symbol; timer: ReturnType<typeof setTimeout> }>(undefined);
  const owners = useCallback((id: string, selected?: symbol) => selected === undefined ? [...(drafts.current.get(id)?.values() ?? [])] : [drafts.current.get(id)?.get(selected)].filter(Boolean), []);
  const busyIn = useCallback((panels: string[], selected?: symbol) => panels.some((id) => owners(id, selected).some((owner) => owner?.busy)), [owners]);
  const guard = useCallback((panels: string[], run: () => void, cancel?: () => void, selected?: symbol) => {
    if (busyIn(panels, selected)) {
      if (deferred.current) { clearTimeout(deferred.current.timer); deferred.current.cancel?.(); }
      deferred.current = { panels, run, cancel, selected, timer: setTimeout(() => {
        const waiting = deferred.current; deferred.current = undefined;
        toast.error(t("An operation is in progress. Wait for its result before leaving.")); waiting?.cancel?.();
      }, 1500) };
      return;
    }
    const dirty = panels.filter((id) => !!routeOf(id) && owners(id, selected).length > 0);
    if (dirty.length) setPending({ panels: dirty, run, owner: selected, cancel });
    else run();
  }, [busyIn, owners, routeOf]);
  const unsaved = useMemo<Unsaved>(() => ({
    register: (panel, owner, discard, busy = false) => {
      if ((discard || busy) && routeOf(panel)) {
        const entries = drafts.current.get(panel) ?? new Map<symbol, { discard: () => void; busy: boolean }>();
        entries.set(owner, { discard: discard ?? (() => {}), busy }); drafts.current.set(panel, entries);
      } else {
        const entries = drafts.current.get(panel);
        entries?.delete(owner);
        if (!entries?.size) drafts.current.delete(panel);
      }
      const waiting = deferred.current;
      if (waiting && !busyIn(waiting.panels, waiting.selected)) {
        clearTimeout(waiting.timer); deferred.current = undefined;
        queueMicrotask(() => guard(waiting.panels, waiting.run, waiting.cancel, waiting.selected));
      }
    },
    ask: (panels, run, selected) => guard(panels, run, undefined, selected),
  }), [busyIn, guard, routeOf]);
  const allPanels = useCallback(() => [...stackRef.current.map(entry => entry.id), ...(sideRef.current ? [sideRef.current.id] : [])], []);

  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (drafts.current.size) { event.preventDefault(); event.returnValue = ""; }
    };
    addEventListener("beforeunload", beforeUnload);
    return () => removeEventListener("beforeunload", beforeUnload);
  }, []);

  const writeHash = useCallback((route: Route, replace = false) => {
    const hash = routeToHash(route);
    lastHash.current = hash;
    if (location.hash === hash) return;
    if (replace) history.replaceState(null, "", hash); else history.pushState(null, "", hash);
  }, []);
  const setStack = useCallback((entries: Entry[], replace = false) => {
    const removed = stackRef.current.filter(entry => !entries.some(kept => kept.id === entry.id)).map(entry => entry.id);
    stackRef.current = entries; setStackState(entries);
    if (removed.length) forget(removed);
    const top = entries.at(-1);
    if (top) writeHash(top.route, replace);
  }, [forget, writeHash]);
  const restoreHash = useCallback(() => { if (lastHash.current && location.hash !== lastHash.current) history.replaceState(null, "", lastHash.current); }, []);

  const showMain = useCallback((route: Route, options: OpenOptions & { link?: boolean } = {}) => {
    const entries = stackRef.current, top = entries.at(-1);
    const replace = options.replace || options.link;
    if (top && keyOf(top.route) === keyOf(route)) {
      if (routeKey(top.route) !== routeKey(route)) setStack([...entries.slice(0, -1), { ...top, route }], replace);
      else if (options.link) writeHash(route, true);
      return;
    }
    const below = entries.findIndex(entry => keyOf(entry.route) === keyOf(route));
    if (below >= 0) {
      const above = entries.slice(below + 1).map(entry => entry.id);
      guard(above, () => setStack([...entries.slice(0, below), { ...entries[below]!, route }], replace), options.link ? restoreHash : undefined);
      return;
    }
    if (route.params?.call) { setStack([...entries, { id: entryId("view"), route }], replace); return; }
    guard(entries.map(entry => entry.id), () => setStack([{ id: entryId("view"), route }], replace), options.link ? restoreHash : undefined);
  }, [keyOf, guard, setStack, writeHash, restoreHash]);
  const showSide = useCallback((route: Route) => {
    const current = sideRef.current;
    if (current && keyOf(current.route) === keyOf(route)) { setSide({ ...current, route }); return; }
    const focus = current?.focus ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null);
    const run = () => { if (current) forget([current.id]); setSide({ id: entryId("side"), route, focus }); };
    if (current) guard([current.id], run); else run();
  }, [keyOf, guard, forget, setSide]);
  const closeSide = useCallback(() => {
    const current = sideRef.current;
    if (!current) return;
    guard([current.id], () => { forget([current.id]); setSide(undefined); if (current.focus?.isConnected) current.focus.focus(); });
  }, [guard, forget, setSide]);

  const openResolved = useCallback((route: Route, options: OpenOptions & { link?: boolean } = {}) => {
    if (!registry.current.has(route.view)) {
      toast.error(t("This address points to a task your current access does not include."));
      if (options.link) { if (stackRef.current.length) restoreHash(); else showMain(home, { replace: true }); }
      return;
    }
    const source = options.from ? routeOf(options.from) : undefined;
    if (options.from && !source) return;
    if (options.ifCurrent && (!source || keyOf(source) !== keyOf(options.ifCurrent))) return;
    if (options.window === "popout") { window.open(`${location.pathname}${location.search}${routeToHash(route)}`, "_blank", "noopener"); return; }
    if (options.window === "inspect" || options.window === "beside") { showSide(route); return; }
    if (options.from && options.from === sideRef.current?.id && !route.params?.call) { forget([options.from]); setSide(undefined); }
    showMain(route, options);
  }, [keyOf, routeOf, showMain, showSide, restoreHash, home, forget, setSide]);
  const open = useCallback((route: Route, options: OpenOptions & { link?: boolean } = {}) => {
    const resolve = routeReader.current;
    const ticket = ++revision.current;
    const take = (resolved: Route) => { if (ticket === revision.current || options.window === "inspect" || options.window === "beside") openResolved(resolved, options); };
    const failed = () => { if (ticket === revision.current) { toast.error(t("Could not open this view.")); if (options.link) restoreHash(); } };
    try {
      const resolved = resolve ? resolve(route, currentScope.current) : route;
      if (resolved instanceof Promise) void resolved.then(take).catch(failed);
      else take(resolved);
    } catch { failed(); }
  }, [openResolved, restoreHash]);

  const navKey = `${storageKey}:nav`;
  const [memory, setMemory] = useState<Memory>(() => {
    try { const saved = localStorage.getItem(navKey); if (saved) return { recent: [], favorites: [], last: {}, ...JSON.parse(saved) }; } catch {   }
    return { recent: [], favorites: [], last: {} };
  });
  const remember = useCallback((change: (m: Memory) => Memory) => setMemory((m) => {
    const next = change(m);
    try { localStorage.setItem(navKey, JSON.stringify(next)); } catch {   }
    return next;
  }), [navKey]);
  const toggleFavorite = useCallback((app: string) => remember((m) => ({ ...m, favorites: m.favorites.includes(app) ? m.favorites.filter((id) => id !== app) : [...m.favorites, app] })), [remember]);
  const memoryRef = useRef(memory); memoryRef.current = memory;

  const applicationEntries = useRef(appEntries); applicationEntries.current = appEntries;
  const applications = useMemo<Applications | undefined>(() => appEntries && ({ ...appEntries, onSelect: (id) => {
    const app = appEntries.apps.find(entry => entry.id === id);
    if (!app) return;
    const target = id === appEntries.current ? app.home : memoryRef.current.last[id] ?? app.home;
    guard(allPanels(), () => { appEntries.onSelect(id); open(target); });
  } }), [appEntries, guard, allPanels, open]);

  const top = stack.at(-1);
  const topKey = top ? `${top.id}:${routeKey(top.route)}` : undefined;
  const followed = useRef(onActiveRoute); followed.current = onActiveRoute;
  useEffect(() => {
    if (!top) return;
    const origin = top.route.params?.call ? stack[0]!.route : top.route;
    followed.current?.(origin);
    if (top.route.params?.call) return;
    const scope = routeScope.current ? routeScope.current(top.route) : currentScope.current;
    const title = titles[top.id] ?? registry.current.get(top.route.view)?.title(top.route.params ?? {}) ?? top.route.view;
    const homes = rail ? [rail.home, rail.applications, ...(rail.files ? [rail.files] : [])].map(routeKey) : [];
    remember((m) => ({
      ...m,
      last: scope ? { ...m.last, [scope]: top.route } : m.last,
      recent: homes.includes(routeKey(top.route)) ? m.recent : [{ route: top.route, title, app: scope, at: Date.now() }, ...m.recent.filter((r) => keyOf(r.route) !== keyOf(top.route))].slice(0, 20),
    }));
  }, [topKey]);
  useEffect(() => {
    const title = top ? titles[top.id] ?? registry.current.get(top.route.view)?.title(top.route.params ?? {}) : undefined;
    document.title = title ? `${title} · ${product}` : product;
  }, [topKey, titles, product]);

  const opener = useRef(open); opener.current = open;
  const homeRoute = useRef(home); homeRoute.current = home;
  useEffect(() => {
    const linked = routeFromHash(location.hash);
    if (linked) opener.current(linked, { link: true }); else opener.current(homeRoute.current, { replace: true });
    const onHash = () => {
      if (location.hash === lastHash.current) return;
      const route = routeFromHash(location.hash);
      if (route) opener.current(route, { link: true }); else opener.current(homeRoute.current, { replace: true });
    };
    addEventListener("hashchange", onHash);
    return () => removeEventListener("hashchange", onHash);
  }, []);

  const goHome = useCallback(() => {
    const scope = currentScope.current;
    open(applicationEntries.current?.apps.find((app) => app.id === scope)?.home ?? home);
  }, [open, home]);
  const close = useCallback((route: Route, from?: string) => {
    if (sideRef.current && (from === sideRef.current.id || keyOf(sideRef.current.route) === keyOf(route))) { closeSide(); return; }
    const entries = stackRef.current, index = entries.findIndex(entry => keyOf(entry.route) === keyOf(route));
    if (index < 0) return;
    if (index > 0) guard(entries.slice(index).map(entry => entry.id), () => setStack(entries.slice(0, index), true));
    else guard(entries.map(entry => entry.id), goHome);
  }, [keyOf, closeSide, guard, setStack, goHome]);

  const [paletteOpen, setPaletteOpen] = useState(false);
  const [sidebarSlot, setSidebarSlot] = useState<HTMLElement | null>(null);
  const [claims, setClaims] = useState<string[]>([]);
  const claimSidebar = useCallback((panel: string) => {
    setClaims(current => current.includes(panel) ? current : [...current, panel]);
    return () => setClaims(current => current.filter(id => id !== panel));
  }, []);
  const workspace = useMemo<WorkspaceApi>(() => ({
    open, notify: toast, close,
    closeAll: () => guard(allPanels(), () => { if (sideRef.current) { forget([sideRef.current.id]); setSide(undefined); } goHome(); }),
    unsaved,
    confirmLeave: (run, exceptPanel) => guard(allPanels().filter(id => id !== exceptPanel), run),
    transfer: (from, route, input, result) => {
      if (!routeOf(from)) return;
      const resolve = routeReader.current;
      const start = (resolved: Route) => {
        if (!registry.current.has(resolved.view) || !routeOf(from)) return;
        const id = entryId("view"), ticket = transfers.current.start(from, input, result);
        transfers.current.bind(ticket, id);
        setStack([...stackRef.current, { id, route: { ...resolved, params: { ...resolved.params, call: ticket } } }]);
      };
      try {
        const resolved = resolve ? resolve(route, currentScope.current) : route;
        if (resolved instanceof Promise) void resolved.then(start).catch(() => toast.error(t("Could not open this view.")));
        else start(resolved);
      } catch { toast.error(t("Could not open this view.")); }
    },
    palette: () => setPaletteOpen(true),
    retitle: (id, name) => {
      setTitles(previous => previous[id] === name ? previous : { ...previous, [id]: name });
      const route = routeOf(id);
      if (route && stackRef.current.at(-1)?.id === id) remember(m => ({ ...m, recent: m.recent.map(entry => keyOf(entry.route) === keyOf(route) ? { ...entry, title: name } : entry) }));
    },
    recent: memory.recent, favorites: memory.favorites, toggleFavorite, applications, rail, sidebarSlot, claimSidebar, activePanel: stack.at(-1)?.id,
  }), [open, close, guard, allPanels, forget, setSide, goHome, unsaved, routeOf, setStack, remember, keyOf, memory, toggleFavorite, applications, rail, sidebarSlot, claimSidebar, stack]);

  const returnFrom = useCallback((entry: Entry, ticket: string) => (value: unknown) => guard([entry.id], () => {
    transfers.current.finish(ticket, entry.id, value);
    const entries = stackRef.current, index = entries.findIndex(item => item.id === entry.id);
    if (index > 0) setStack(entries.slice(0, index), true);
  }), [guard, setStack]);

  const [compact, setCompact] = useState(() => typeof matchMedia !== "undefined" && matchMedia("(max-width: 767px)").matches);
  useEffect(() => {
    const media = matchMedia("(max-width: 767px)");
    const changed = () => { setCompact(media.matches); setMobileNavOpen(false); };
    media.addEventListener("change", changed);
    return () => media.removeEventListener("change", changed);
  }, []);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  useEffect(() => setMobileNavOpen(false), [topKey]);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => {
    try { return JSON.parse(localStorage.getItem(`${storageKey}:sidebar`) ?? "{}") as Record<string, boolean>; } catch { return {}; }
  });
  const pageScope = top ? (scopeOf ? scopeOf(top.route) : layoutScope ?? "") : undefined;
  const sidebarScope = pageScope ?? "";
  const sidebarCollapsed = !!collapsed[sidebarScope];
  const toggleSidebar = useCallback(() => {
    if (compact) { setMobileNavOpen(open => !open); return; }
    setCollapsed(previous => {
      const next = { ...previous, [sidebarScope]: !previous[sidebarScope] };
      try { localStorage.setItem(`${storageKey}:sidebar`, JSON.stringify(next)); } catch {   }
      return next;
    });
  }, [compact, sidebarScope, storageKey]);
  const hasNavigation = pageScope !== undefined && (nav.length > 0 || !!navigation);

  const [typed, setTyped] = useState("");
  const [searchAttempt, setSearchAttempt] = useState(0);
  const [recordSearch, setRecordSearch] = useState<{ source: RecordSearch; text: string; attempt: number; hits?: SearchHit[]; error?: string }>();
  const searching = paletteOpen && !!search && typed.trim().length >= 2;
  const answer = searching && recordSearch?.source === search && recordSearch.text === typed && recordSearch.attempt === searchAttempt ? recordSearch : undefined;
  const found = answer?.hits ?? [];
  const searchPending = searching && answer?.hits === undefined && answer?.error === undefined;
  useEffect(() => {
    if (!searching || !search) { setRecordSearch(undefined); return; }
    let live = true;
    setRecordSearch(undefined);
    const wait = setTimeout(() => {
      void search(typed).then((hits) => { if (live) setRecordSearch({ source: search, text: typed, attempt: searchAttempt, hits }); })
        .catch((error) => { if (live) setRecordSearch({ source: search, text: typed, attempt: searchAttempt, error: error instanceof Error ? error.message : String(error) }); });
    }, 200);
    return () => { live = false; clearTimeout(wait); };
  }, [search, typed, searching, searchAttempt]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setPaletteOpen((v) => !v); }
      if ((e.metaKey || e.ctrlKey) && e.key === "\\") { e.preventDefault(); toggleSidebar(); }
      if (e.key === "Escape" && !e.defaultPrevented && compact && mobileNavOpen) setMobileNavOpen(false);
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, [toggleSidebar, compact, mobileNavOpen]);

  const leave = useCallback((run: () => void) => guard(allPanels(), run), [guard, allPanels]);
  const activeKey = top ? routeKey(top.route) : undefined;
  const currentApp = applications?.apps.find(app => app.id === pageScope);
  const pinnedApps = (applications?.apps ?? []).filter((a) => memory.favorites.includes(a.id));
  const onRailRoute = (route?: Route) => !!route && !!top && routeKey(route) === routeKey(top.route);
  type RailItem = { id: string; label: string; icon: ReactNode; badge?: number; active?: boolean; run: () => void };
  const railItems: RailItem[] = rail ? [
    { id: "home", label: t("Home"), icon: <HomeIcon />, active: onRailRoute(rail.home), run: () => open(rail.home) },
    { id: "search", label: t("Search"), icon: <Search />, run: () => setPaletteOpen(true) },
    ...(rail.files ? [{ id: "files", label: t("Files"), icon: <FolderTree />, active: onRailRoute(rail.files), run: () => open(rail.files!) }] : []),
    { id: "applications", label: t("Applications"), icon: <Grid2x2 />, active: onRailRoute(rail.applications), run: () => open(rail.applications) },
  ] : [];
  const notificationsItem = rail?.notifications && { id: "notifications", label: t("Notifications"), icon: <Bell />, badge: rail.notifications.unread, active: onRailRoute(rail.notifications.route), run: () => open(rail.notifications!.route) };
  const sessionMenu = session && <SessionMenu rail={!compact} scheme={appearance.scheme} onScheme={appearance.setScheme} session={{ ...session, onAccount: session.onAccount ? () => leave(() => session.onAccount?.()) : undefined, onSwitch: (id) => leave(() => session.onSwitch(id)) }}
    onLanguageSelect={(id) => leave(() => {
      void Promise.resolve(onLanguage?.(id)).catch(() => undefined).finally(() => setLanguage(id));
    })} />;
  const density = currentApp?.category === "business" ? "comfortable" : "compact";
  const sideView = side ? byId.get(side.route.view) : undefined;
  const sideTitle = side ? titles[side.id] ?? sideView?.title(side.route.params ?? {}) ?? t("Details") : "";
  const fullRoute = (route: Route): Route => { const { inspect: _inspect, ...params } = route.params ?? {}; return Object.keys(params).length ? { ...route, params } : { view: route.view }; };
  const builtInCommands: ShellCommand[] = [
    ...(hasNavigation ? [{ id: "shell:navigation", label: sidebarCollapsed ? t("Show navigation") : t("Hide navigation"), group: t("View"), shortcut: "⌘\\", run: toggleSidebar }] : []),
    ...([["light", t("Light")], ["dark", t("Dark")], ["system", t("Follow system")]] as const).map(([scheme, label]) => ({ id: `shell:appearance:${scheme}`, label: `${t("Appearance")}: ${label}`, group: t("View"), run: () => appearance.setScheme(scheme) })),
  ];

  const sidebar = hasNavigation && (
    <Sidebar app={currentApp} product={product} productIcon={productIcon} nav={nav} navigation={navigation} active={activeKey}
      resource={!!top && claims.includes(top.id)} slot={setSidebarSlot}
      collapsed={!compact && sidebarCollapsed} onToggle={toggleSidebar} status={status}
      pinned={!!currentApp && memory.favorites.includes(currentApp.id)} onPin={currentApp ? () => toggleFavorite(currentApp.id) : undefined}
      onHome={currentApp ? () => open(currentApp.home) : undefined}
      onOpen={route => { open(route); setMobileNavOpen(false); }} />
  );

  return (
    <WorkspaceContext.Provider value={workspace}>
      <div className={cn("grid h-dvh w-full min-w-0 max-w-full overflow-hidden bg-background text-foreground",
        compact ? rail ? "grid-rows-[40px_minmax(0,1fr)_48px]" : "grid-rows-[40px_minmax(0,1fr)]" : rail ? "grid-cols-[var(--rail-width)_auto_minmax(0,1fr)]" : "grid-cols-[auto_minmax(0,1fr)]")}>
        {!compact && rail && (
          <aside aria-label={t("Platform")} data-ui="rail" className="flex min-h-0 flex-col items-center gap-1 overflow-y-auto border-r border-rail-border bg-rail py-2">
            {railItems.map(item => <RailButton key={item.id} {...item} />)}
            <RailList label={t("Recent")} icon={<Clock />} empty={t("Nothing opened yet.")}
              items={memory.recent.slice(0, 12).map((r) => ({ id: routeKey(r.route), label: r.title, detail: r.app ? applications?.apps.find((a) => a.id === r.app)?.title : undefined, run: () => open(r.route) }))} />
            <RailList label={t("Favorites")} icon={<Star />} empty={t("Pin an application from its sidebar or from Applications.")}
              items={pinnedApps.map(app => ({ id: app.id, label: app.title, icon: app.icon, run: () => applications!.onSelect(app.id) }))} />
            <div className="mt-auto flex flex-col items-center gap-1 pt-2">
              {notificationsItem && <RailButton {...notificationsItem} />}
              {rail.assist && <RailButton id="assist" label={t("AIP Assist")} icon={<Sparkles />} active={onRailRoute(rail.assist)} run={() => open(rail.assist!)} />}
              {sessionMenu}
            </div>
          </aside>
        )}
        {compact && (
          <header data-ui="chrome" className="flex min-w-0 items-center gap-2 border-b border-border bg-surface px-2">
            {hasNavigation && <Button size="icon" variant="ghost" aria-label={t("Toggle navigation")} aria-expanded={mobileNavOpen} onClick={toggleSidebar}><MenuIcon /></Button>}
            <span className="flex min-w-0 flex-1 items-center gap-2 truncate text-sm font-semibold [&_svg]:size-4 [&_svg]:shrink-0">{currentApp?.icon ?? productIcon}{currentApp?.title ?? product}</span>
            <Button size="icon" variant="ghost" aria-label={t("Search and commands")} onClick={() => setPaletteOpen(true)}><Search /></Button>
            {sessionMenu}
          </header>
        )}
        {!compact && (sidebar || <div />)}
        <div className={cn("relative grid min-h-0 min-w-0", side && !compact ? "grid-cols-[minmax(0,1fr)_var(--side-pane-width)]" : "grid-cols-1")}>
          {compact && mobileNavOpen && sidebar && <>
            <button type="button" aria-label={t("Close navigation")} className="fixed inset-x-0 bottom-12 top-10 z-20 bg-overlay" onClick={() => setMobileNavOpen(false)} />
            <div className="fixed bottom-12 left-0 top-10 z-30 flex w-[min(19rem,85vw)] shadow-lg">{sidebar}</div>
          </>}
          <main data-density={density} className="relative min-h-0 min-w-0">
            {stack.map((entry, index) => {
              const view = byId.get(entry.route.view);
              const visible = index === stack.length - 1;
              const ticket = entry.route.params?.call;
              const call = ticket ? transfers.current.read(ticket, entry.id) : undefined;
              return <div key={entry.id} hidden={!visible} role="region" aria-label={titles[entry.id] ?? view?.title(entry.route.params ?? {}) ?? t("Workspace view")} className="absolute inset-0 overflow-auto px-5 py-4 max-md:px-3 max-md:py-3">
                <ViewVisibilityContext.Provider value={visible}><PanelContext.Provider value={entry.id}><ViewCallContext.Provider value={ticket ? { input: call?.input, expired: !call, returnValue: call ? returnFrom(entry, ticket) : undefined } : undefined}>
                  <ViewBoundary closeLabel={t("Go to home")} onClose={goHome}>
                    <div key={keyOf(entry.route)} className="contents">{view ? view.render(entry.route.params ?? {}) : <p className="text-sm text-muted">{t("This view no longer exists.")}</p>}</div>
                  </ViewBoundary>
                </ViewCallContext.Provider></PanelContext.Provider></ViewVisibilityContext.Provider>
              </div>;
            })}
          </main>
          {side && <aside aria-label={sideTitle} data-density={density} className={cn("flex min-h-0 min-w-0 flex-col border-l border-border bg-surface", compact && "fixed inset-x-0 bottom-12 top-10 z-30")}
            onKeyDown={event => { if (event.key === "Escape" && !event.defaultPrevented) { event.stopPropagation(); closeSide(); } }}>
            <header className="flex h-10 shrink-0 items-center gap-1 border-b border-border pl-3 pr-1">
              <h2 className="min-w-0 flex-1 truncate text-sm font-semibold">{sideTitle}</h2>
              <Button size="icon" variant="ghost" aria-label={t("Open as page")} title={t("Open as page")} onClick={() => { const route = fullRoute(side.route); guard([side.id], () => { forget([side.id]); setSide(undefined); open(route); }); }}><Maximize2 /></Button>
              <Button size="icon" variant="ghost" aria-label={t("Close")} title={t("Close")} onClick={closeSide}><X /></Button>
            </header>
            <div className="min-h-0 flex-1 overflow-auto p-3">
              <PanelContext.Provider value={side.id}>
                <ViewBoundary key={side.id} closeLabel={t("Close")} onClose={() => { forget([side.id]); setSide(undefined); }}>
                  <div key={keyOf(side.route)} className="contents">{sideView?.render(side.route.params ?? {})}</div>
                </ViewBoundary>
              </PanelContext.Provider>
            </div>
          </aside>}
        </div>
        {compact && rail && (
          <nav aria-label={t("Platform")} data-ui="rail" className="flex items-center justify-around border-t border-rail-border bg-rail px-2">
            {railItems.map(item => <RailButton key={item.id} {...item} />)}
            {notificationsItem && <RailButton {...notificationsItem} />}
          </nav>
        )}
      </div>
      <Command.Dialog open={paletteOpen} onOpenChange={setPaletteOpen} label={t("Command palette")}
        overlayClassName="fixed inset-0 z-40 bg-overlay"
        contentClassName="fixed left-1/2 top-[14%] z-50 w-[min(600px,calc(100vw-32px))] -translate-x-1/2 overflow-hidden rounded-lg border border-border bg-surface text-foreground shadow-lg">
        <div className="flex items-center gap-2 border-b border-border px-3"><Search className="size-4 shrink-0 text-muted" />
          <Command.Input value={typed} onValueChange={setTyped} placeholder={t("Search records, go to, open, run…")} className="h-11 w-full bg-transparent text-base outline-none" /></div>
        {searchPending && <p role="status" className="px-3 py-2 text-sm text-muted">{t("Searching records…")}</p>}
        {answer?.error !== undefined && <div role="alert" className="grid gap-1 border-b border-border px-3 py-2 text-sm">
          <p className="text-danger">{t("Record search failed.")} {answer.error}</p>
          <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => setSearchAttempt(attempt => attempt + 1)}>{t("Retry")}</Button>
        </div>}
        <Command.List className="max-h-[min(420px,60dvh)] overflow-auto p-1">
          {!searchPending && answer?.error === undefined && <Command.Empty className="p-3 text-sm text-muted">{t("No results")}</Command.Empty>}
          {found.length > 0 && (
            <Command.Group heading={t("Records")} className={paletteGroup}>
              {found.map((hit) => (
                <Command.Item key={hit.id} value={`record ${answer!.text} ${hit.label} ${hit.id}`} className={paletteItem}
                  onSelect={() => { hit.open(); setPaletteOpen(false); }}>{hit.label}{hit.detail && <span className="ml-auto font-mono text-xs text-muted">{hit.detail}</span>}</Command.Item>
              ))}
            </Command.Group>
          )}
          {nav.length > 0 && pageScope && nav.map((section) => (
            <Command.Group key={section.label} heading={currentApp ? `${currentApp.title} · ${section.label}` : section.label} className={paletteGroup}>
              {navigationLinks(section.items).map((item) => (
                <Command.Item key={routeKey(item.route)} value={`${section.label} ${item.label}`} className={paletteItem}
                  onSelect={() => { open(item.route); setPaletteOpen(false); }}>{item.icon}{item.label}</Command.Item>
              ))}
            </Command.Group>
          ))}
          {applications && applications.apps.length > 0 && (
            <Command.Group heading={t("Applications")} className={paletteGroup}>
              {applications.apps.map((a) => (
                <Command.Item key={a.id} value={`app ${a.title} ${applications.categories.find((c) => c.id === a.category)?.label ?? ""}`} className={paletteItem}
                  onSelect={() => { applications.onSelect(a.id); setPaletteOpen(false); }}>{a.icon}{a.title}
                  <span className="ml-auto text-xs text-muted">{applications.categories.find((c) => c.id === a.category)?.label}</span></Command.Item>
              ))}
            </Command.Group>
          )}
          {memory.recent.length > 0 && (
            <Command.Group heading={t("Recent")} className={paletteGroup}>
              {memory.recent.slice(0, 8).map((entry) => (
                <Command.Item key={routeKey(entry.route)} value={`recent ${entry.title} ${routeKey(entry.route)}`} className={paletteItem}
                  onSelect={() => { open(entry.route); setPaletteOpen(false); }}><Clock />{entry.title}
                  {entry.app && <span className="ml-auto text-xs text-muted">{applications?.apps.find(app => app.id === entry.app)?.title}</span>}</Command.Item>
              ))}
            </Command.Group>
          )}
          {[...commands, ...builtInCommands].length > 0 && (
            <Command.Group heading={t("Commands")} className={paletteGroup}>
              {[...commands, ...builtInCommands].map((c) => (
                <Command.Item key={c.id} value={`${c.group ?? ""} ${c.label}`} className={paletteItem}
                  onSelect={() => { setPaletteOpen(false); c.run(); }}>
                  {c.label}{(c.shortcut || c.group) && <span className="ml-auto text-xs text-muted">{c.shortcut ?? c.group}</span>}
                </Command.Item>
              ))}
            </Command.Group>
          )}
        </Command.List>
      </Command.Dialog>
      <Dialog open={!!pending} title={t("Unsaved changes")} onOpenChange={(open) => { if (!open) { pending?.cancel?.(); setPending(undefined); } }}>
        <div className="grid gap-3 text-sm">
          <p>{t("Save your work in the editor, or discard the changes to continue.")}</p>
          <ul className="list-inside list-disc text-muted">{pending?.panels.map((id) => { const route = routeOf(id); return <li key={id}>{titles[id] ?? (route ? byId.get(route.view)?.title(route.params ?? {}) : id)}</li>; })}</ul>
          <div className="flex justify-end gap-2">
            <Button onClick={() => { pending?.cancel?.(); setPending(undefined); }}>{t("Keep editing")}</Button>
            <Button variant="danger" onClick={() => {
              if (!pending) return;
              if (pending.panels.some((id) => [...(drafts.current.get(id)?.entries() ?? [])].some(([key, owner]) => (pending.owner === undefined || key === pending.owner) && owner.busy))) {
                toast.error(t("An operation is in progress. Wait for its result before leaving.")); return;
              }
              for (const id of pending.panels) for (const [key, owner] of [...(drafts.current.get(id)?.entries() ?? [])]) if (pending.owner === undefined || key === pending.owner) owner.discard();
              const run = pending.run; setPending(undefined); run();
            }}>{t("Discard changes")}</Button>
          </div>
        </div>
      </Dialog>
      <Toaster position="bottom-right" toastOptions={{ className: "!rounded-md !border-border !bg-surface !text-foreground !text-sm !shadow-md" }} />
    </WorkspaceContext.Provider>
  );
}

function Sidebar({ app, product, productIcon, nav, navigation, active, resource, slot, collapsed, onToggle, onOpen, status, pinned, onPin, onHome }: {
  app?: PlatformApplication; product: string; productIcon?: ReactNode; nav: NavSection[]; navigation?: ReactNode; active?: string; resource: boolean; slot: (element: HTMLElement | null) => void;
  collapsed: boolean; onToggle: () => void; onOpen: (route: Route) => void; status?: ReactNode; pinned: boolean; onPin?: () => void; onHome?: () => void;
}) {
  const title = app?.title ?? product, icon = app?.icon ?? productIcon;
  if (collapsed) return <nav aria-label={title} className="flex w-11 min-h-0 flex-col items-center gap-0.5 overflow-y-auto border-r border-border bg-sidebar py-2">
    <Tooltip content={t("Show navigation")} side="right"><button type="button" aria-label={t("Show navigation")} onClick={onToggle} className="mb-2 grid size-8 place-items-center rounded-md text-primary hover:bg-row-hover [&_svg]:size-4">{icon}</button></Tooltip>
    {nav.flatMap(section => navigationLinks(section.items)).map(link => <Tooltip key={routeKey(link.route)} content={link.label} side="right">
      <button type="button" aria-label={link.label} aria-current={routeCovers(link.route, active) ? "page" : undefined} onClick={() => onOpen(link.route)}
        className={cn("grid size-8 shrink-0 place-items-center rounded-md text-muted hover:bg-row-hover hover:text-foreground [&_svg]:size-4", routeCovers(link.route, active) && "bg-row-selected text-primary")}>
        {link.icon ?? <span className="text-xs font-semibold">{link.label.slice(0, 1)}</span>}
      </button></Tooltip>)}
  </nav>;
  return <nav aria-label={title} className="flex h-full w-[var(--sidebar-width)] min-h-0 flex-col border-r border-border bg-sidebar">
    <div className="group/app flex h-11 shrink-0 items-center gap-1 border-b border-border pl-2 pr-1">
      <button type="button" onClick={onHome} disabled={!onHome} className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-1 py-1 text-left hover:bg-row-hover disabled:hover:bg-transparent">
        <span className="grid size-6 shrink-0 place-items-center rounded-md bg-row-selected text-primary [&_svg]:size-3.5">{icon}</span>
        <span className="min-w-0 truncate text-sm font-semibold">{title}</span>
      </button>
      {onPin && <Button size="icon" variant="ghost" aria-label={pinned ? t("Remove from favorites") : t("Add to favorites")} aria-pressed={pinned} title={pinned ? t("Remove from favorites") : t("Add to favorites")} onClick={onPin}
        className={cn(pinned ? "text-[var(--tone-warning)]" : "text-muted opacity-0 group-hover/app:opacity-100 focus-visible:opacity-100")}><Star fill={pinned ? "currentColor" : "none"} /></Button>}
      <Button size="icon" variant="ghost" aria-label={t("Hide navigation")} title={`${t("Hide navigation")} ⌘\\`} onClick={onToggle} className="text-muted max-md:hidden"><ChevronsLeft /></Button>
    </div>
    <div className="min-h-0 flex-1 overflow-y-auto px-2 pb-3 pt-2">
      {navigation}
      <div ref={slot} />
      {!resource && nav.map((section) => (
        <div key={section.label} className="mb-2">
          <div className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wider text-muted">{section.label}</div>
          <NavigationItems items={section.items} active={active} onOpen={onOpen} />
        </div>
      ))}
    </div>
    {status && <div className="shrink-0 border-t border-border px-2 py-1.5">{status}</div>}
  </nav>;
}

function RailButton({ label, icon, badge, active, run }: { id: string; label: string; icon: ReactNode; badge?: number; active?: boolean; run: () => void }) {
  return <Tooltip content={label} side="right"><button type="button" aria-label={label} aria-current={active ? "page" : undefined} onClick={run}
    className={cn("relative grid size-9 shrink-0 place-items-center rounded-md text-rail-foreground transition-colors hover:bg-rail-hover hover:text-rail-active [&_svg]:size-[18px]", active && "bg-rail-selected text-rail-active before:absolute before:-left-1.5 before:top-2 before:h-5 before:w-0.5 before:rounded-full before:bg-primary")}>
    {icon}{badge ? <span className="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-primary px-1 text-center text-2xs font-semibold leading-4 text-primary-foreground ring-2 ring-rail">{badge > 99 ? "99+" : badge}</span> : null}
  </button></Tooltip>;
}

function RailList({ label, icon, items, empty }: { label: string; icon: ReactNode; empty: string; items: { id: string; label: string; detail?: string; icon?: ReactNode; run: () => void }[] }) {
  return (
    <DropdownMenu.Root>
      <Tooltip content={label} side="right"><DropdownMenu.Trigger aria-label={label}
        className="grid size-9 shrink-0 place-items-center rounded-md text-rail-foreground hover:bg-rail-hover hover:text-rail-active data-[state=open]:bg-rail-selected data-[state=open]:text-rail-active [&_svg]:size-[18px]">
        {icon}
      </DropdownMenu.Trigger></Tooltip>
      <DropdownMenu.Portal>
        <DropdownMenu.Content side="right" align="start" sideOffset={8} className={cn(menuPanel, "max-h-[70vh] min-w-64 max-w-80 overflow-auto")}>
          <DropdownMenu.Label className={menuLabel}>{label}</DropdownMenu.Label>
          {items.length === 0 && <p className="px-2 py-1.5 text-xs text-muted">{empty}</p>}
          {items.map((item) => (
            <DropdownMenu.Item key={item.id} onSelect={item.run} className={cn(menuItem, "gap-2 pl-2 [&_svg]:size-3.5")}>
              {item.icon}<span className="truncate">{item.label}</span>{item.detail && <span className="ml-auto truncate pl-3 text-xs text-muted">{item.detail}</span>}
            </DropdownMenu.Item>
          ))}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

function SessionMenu({ session, onLanguageSelect, rail, scheme, onScheme }: { session: Session; onLanguageSelect: (id: string) => void; rail: boolean; scheme: string; onScheme: (scheme: "light" | "dark" | "system") => void }) {
  const shown = session.name || session.principal;
  return (
    <DropdownMenu.Root>
      <Tooltip content={`${shown} · ${session.tenant}`} side={rail ? "right" : "bottom"}><DropdownMenu.Trigger aria-label={`${session.principal} · ${session.tenant}`}
        className={cn("grid size-9 shrink-0 place-items-center rounded-md", rail ? "hover:bg-rail-hover data-[state=open]:bg-rail-selected" : "hover:bg-row-hover")}>
        <span className="grid size-6 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">{shown.slice(0, 1).toUpperCase()}</span>
      </DropdownMenu.Trigger></Tooltip>
      <DropdownMenu.Portal>
        <DropdownMenu.Content side={rail ? "right" : "bottom"} align="end" sideOffset={8} className={cn(menuPanel, "min-w-64")}>
          <div className="px-2 pb-2 pt-1">
            <span className="block text-sm font-medium">{shown}</span>
            <span className="block text-xs text-muted">{session.name ? `${session.principal} · ` : ""}{session.tenant}{session.detail ? ` · ${session.detail}` : ""}</span>
          </div>
          {session.onAccount && <DropdownMenu.Item className={cn(menuItem, "pl-2")} onSelect={() => session.onAccount?.()}>{t("My account")}</DropdownMenu.Item>}
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <DropdownMenu.Label className={menuLabel}>{t("Appearance")}</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={scheme} onValueChange={value => onScheme(value as "light" | "dark" | "system")}>
            {([["light", t("Light")], ["dark", t("Dark")], ["system", t("Follow system")]] as const).map(([id, label]) => <DropdownMenu.RadioItem key={id} value={id} className={menuItem}>
              <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{label}
            </DropdownMenu.RadioItem>)}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <DropdownMenu.Label className={menuLabel}>{t("Language")}</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={language()} onValueChange={onLanguageSelect}>
            {languages.map((l) => (
              <DropdownMenu.RadioItem key={l.id} value={l.id} className={menuItem}>
                <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{l.name}
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
          {session.options.length > 0 && <>
            <DropdownMenu.Separator className="my-1 h-px bg-border" />
            <DropdownMenu.Label className={menuLabel}>{t("Switch tenant or identity")}</DropdownMenu.Label>
            <DropdownMenu.RadioGroup value={session.current} onValueChange={session.onSwitch}>
              {session.options.map((o) => (
                <DropdownMenu.RadioItem key={o.id} value={o.id} className={menuItem}>
                  <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{o.label}
                </DropdownMenu.RadioItem>
              ))}
            </DropdownMenu.RadioGroup>
          </>}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

const menuPanel = "z-50 min-w-48 rounded-md border border-border bg-surface p-1 text-sm text-foreground shadow-md";
const menuLabel = "px-2 pb-1 pt-1.5 text-2xs font-semibold uppercase tracking-wider text-muted";
const menuItem = "relative flex h-7 cursor-default select-none items-center rounded-sm pl-6 pr-2 outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-row-hover";
const paletteGroup = "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:text-2xs [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wider [&_[cmdk-group-heading]]:text-muted";
const paletteItem = "flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-sm data-[selected=true]:bg-row-selected [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted";
