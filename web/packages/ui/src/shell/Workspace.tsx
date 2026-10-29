import {useInputDrafts} from "../fields/draft";
import {useTheme} from "../themes/theme";
import { Command } from "cmdk";
import { DockviewDefaultTab, DockviewReact, themeLight,themeDark, type DockviewApi, type IDockviewPanelHeaderProps, type IDockviewPanelProps, type SerializedDockview } from "dockview-react";
import { Bell, ChevronDown, Clock, Ellipsis, Grid2x2, Home as HomeIcon, LayoutGrid, PanelLeft, Search, Sparkles, Star } from "lucide-react";
import { ContextMenu, DropdownMenu, Menubar } from "radix-ui";
import { Component, createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Toaster, toast } from "sonner";
import { cn } from "../lib/cn";
import { routeFromHash, routeKey, routeToHash, type Route } from "./route";
import { language, languages, setLanguage, t } from "../i18n";
import { Dialog } from "../primitives/dialog";
import { Sheet } from "../primitives/sheet";
import { Button } from "../primitives/button";
import { ViewVisibilityContext } from "./ViewVisibility";
import { ViewTransfers } from "./ViewTransfers";
import { resolveLayout, rewriteLayout } from "./WorkspaceLayout";
import { NavigationItems, navigationLinks, type NavSection } from "./Navigation";
export type { NavSection } from "./Navigation";

export type View = {
  id: string;
  title: (params: Record<string, string>) => string;
  icon?: ReactNode;
  render: (params: Record<string, string>) => ReactNode;
  documentKey?: (params: Record<string, string>) => string;
};
export type MenuItem = { label: string; shortcut?: string; onSelect: () => void; disabled?: boolean };
export type Menu = { label: string; items: MenuItem[] };
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
export type Rail = { home: Route; applications: Route; notifications?: { route: Route; unread?: number }; assist?: Route };
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
  applications?: Applications; rail?: Rail; };
const PanelContext = createContext<string | undefined>(undefined);
const DockScope = createContext<string | undefined>(undefined);
const taskKey = (scope: string | undefined, panel: string) => JSON.stringify([scope ?? null, panel]);
const FrontDock = createContext(true);
const ViewCallContext = createContext<{ input?: unknown; expired?: boolean; returnValue?: (value: unknown) => void } | undefined>(undefined);
export const useViewCall = () => useContext(ViewCallContext);

export const WorkspaceContext = createContext<WorkspaceApi | null>(null);
const ViewRegistryContext = createContext<ReadonlyMap<string, View>>(new Map());

export function useWorkspace(): WorkspaceApi {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  if (!api) throw new Error("useWorkspace outside <Workspace>");
  return { ...api, open: (route, options) => api.open(route, { ...options, from: panel }), close: route => api.close(route, panel), closeAll: () => api.closeAll(panel), confirmLeave: (run: () => void) => api.confirmLeave(run, panel), transfer: api.transfer && panel ? (_from, route, input, result) => api.transfer!(panel, route, input, result) : undefined };
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

class ViewBoundary extends Component<{ children: ReactNode; onClose: () => void }, { error?: Error }> {
  state: { error?: Error } = {};
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error) { console.error(error); }
  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert" className="grid max-w-xl gap-2 rounded-md border border-[var(--tone-danger)] p-3 text-sm">
        <p className="font-medium">{t("This view failed to show.")}</p>
        <p className="font-mono text-xs text-muted">{this.state.error.message}</p>
        <div className="flex gap-2">
          <button type="button" className="rounded-md border border-border px-2 py-1 hover:bg-row-hover" onClick={() => this.setState({ error: undefined })}>{t("Try again")}</button>
          <button type="button" className="rounded-md border border-border px-2 py-1 hover:bg-row-hover" onClick={this.props.onClose}>{t("Close tab")}</button>
        </div>
      </div>
    );
  }
}

export function Workspace({ product, productIcon, storageKey, layoutScope, scopeOf, resolveRoute, views, nav, navigation, home, menus = [], commands = [], session, status, applications: appEntries, rail, onActiveRoute, onEmpty, onLanguage, search }: {
  product: string; productIcon?: ReactNode; storageKey: string; views: View[]; nav: NavSection[]; home: Route;
  navigation?: ReactNode;
  layoutScope?: string;
  scopeOf?: (route: Route) => string | undefined;
  resolveRoute?: (route: Route, layout?: string) => Route | Promise<Route>;
  menus?: Menu[]; commands?: ShellCommand[]; session?: Session; status?: ReactNode;
  applications?: Applications; rail?: Rail; onActiveRoute?: (route: Route) => void;
  onEmpty?: () => void;
  search?: (text: string) => Promise<{ id: string; label: string; detail?: string; open: () => void }[]>;
  onLanguage?: (id: string) => unknown;
}) {
  const appearance=useTheme();
  const dock = useRef<DockviewApi>(null);
  const docks = useRef(new Map<string | undefined, DockviewApi>());
  const syncs = useRef(new Map<string | undefined, () => void>());
  const [created, setCreated] = useState<(string | undefined)[]>(() => [layoutScope]);
  const booted = useRef(false);
  const unloaded = useRef(false);
  const drafts = useRef(new Map<string, Map<symbol, { discard: () => void; busy: boolean }>>());
  const transfers = useRef(new ViewTransfers());
  const [pending, setPending] = useState<{ panels: string[]; run: () => void; owner?: symbol }>();
  const taskOwner = useCallback((address: string) => {
    const [storedScope, id] = JSON.parse(address) as [string | null, string];
    const scope = storedScope ?? undefined, api = docks.current.get(scope);
    return { scope, id, api, panel: api?.getPanel(id) };
  }, []);
  const panelIn = useCallback((address: string) => !!taskOwner(address).panel, [taskOwner]);
  const unsaved = useMemo<Unsaved>(() => ({
    register: (panel, owner, discard, busy = false) => {
      if ((discard || busy) && panelIn(panel)) {
        const entries = drafts.current.get(panel) ?? new Map<symbol, { discard: () => void; busy: boolean }>();
        entries.set(owner, { discard: discard ?? (() => {}), busy }); drafts.current.set(panel, entries);
      } else {
        const entries = drafts.current.get(panel);
        entries?.delete(owner);
        if (!entries?.size) drafts.current.delete(panel);
      }
    },
    ask: (panels, run, selected) => {
      const owners = (id: string) => selected === undefined ? [...(drafts.current.get(id)?.values() ?? [])] : [drafts.current.get(id)?.get(selected)].filter(Boolean);
      if (panels.some((id) => owners(id).some((owner) => owner?.busy))) {
        toast.error(t("An operation is in progress. Wait for its result before leaving.")); return;
      }
      const dirty = panels.filter((id) => panelIn(id) && owners(id).length > 0);
      if (dirty.length) setPending({ panels: dirty, run, owner: selected });
      else run();
    },
  }), []);
  const byId = useMemo(() => new Map(views.map((v) => [v.id, v])), [views]);
  const closePanels = useCallback((ids: string[]) => unsaved.ask(ids, () => { for (const id of ids) taskOwner(id).panel?.api.close(); }), [unsaved, taskOwner]);
  const closePanel = useCallback((id: string) => closePanels([id]), [closePanels]);
  const scope = useRef(layoutScope);
  const routeScope = useRef(scopeOf); routeScope.current = scopeOf;
  const routeReader = useRef(resolveRoute); routeReader.current = resolveRoute;
  const viewRegistry = useRef(byId); viewRegistry.current = byId;
  const keyOf = useCallback((route: Route) => {
    const key = viewRegistry.current.get(route.view)?.documentKey;
    return key ? JSON.stringify([route.view, key(route.params ?? {}), route.params?.call]) : routeKey(route);
  }, []);
  const applicationEntries = useRef(appEntries); applicationEntries.current = appEntries;
  const switching = useRef(false);
  const incomingRevision = useRef(0);
  const queuedOpen = useRef<{ route: Route; options: OpenOptions; scope?: string | undefined } | undefined>(undefined);
  const allTasks = useCallback(() => [...docks.current].flatMap(([scope, api]) => api.panels.map(panel => taskKey(scope, panel.id))), []);
  const leaveViews = useCallback((run: () => void) => unsaved.ask(allTasks(), () => {
    ++incomingRevision.current; queuedOpen.current = undefined; run();
  }), [unsaved, allTasks]);
  const applications = useMemo<Applications | undefined>(() => appEntries && ({ ...appEntries, onSelect: (id) => {
    if (id === appEntries.current || !appEntries.apps.some((app) => app.id === id)) return;
    appEntries.onSelect(id);
  } }), [appEntries]);
  const layoutKey = (id: string | undefined) => `${storageKey}:${id ?? "shell"}`;
  const saveLayout = useCallback((api: DockviewApi, id: string | undefined) => {
    try {
      const layout = routeScope.current ? rewriteLayout(api.toJSON(), (route) => routeScope.current!(route) === id ? route : undefined, keyOf) : api.toJSON();
      localStorage.setItem(layoutKey(id), JSON.stringify(layout));
    } catch {   }
  }, [storageKey, keyOf]);
  const loadLayout = useCallback(async (api: DockviewApi, id: string | undefined) => {
    try {
      const saved = localStorage.getItem(layoutKey(id));
      const restored = saved && await resolveLayout(JSON.parse(saved) as SerializedDockview,
        (route) => routeReader.current?.(route, id) ?? route, (view) => viewRegistry.current.has(view), keyOf);
      const layout = restored && routeScope.current
        ? rewriteLayout(restored, route => routeScope.current!(route) === id ? route : undefined, keyOf)
        : restored;
      if (layout) api.fromJSON(layout); else api.clear();
    } catch {
      api.clear();
    }
    for (const panel of [...api.panels]) {
      const route = (panel.params as { route?: Route } | undefined)?.route;
      const view = viewRegistry.current.get(route?.view ?? "");
      if (!view) panel.api.close(); else panel.setTitle(view.title(route?.params ?? {}));
    }
    return true;
  }, [storageKey]);
  const popping = useRef(new Set<string>());
  const popout = useCallback((id: string, dockScope: string | undefined) => {
    const api = docks.current.get(dockScope), panel = api?.getPanel(id), address = taskKey(dockScope, id);
    if (!api || !panel || panel.api.location.type === "popout" || popping.current.has(address)) return;
    popping.current.add(address);
    void api.addPopoutGroup(panel, { onDidOpen: ({ window: child }) => {
      child.addEventListener("load", () => {
        const root = document.documentElement, target = child.document.documentElement;
        const mirror = () => {
          target.lang = root.lang; target.dir = root.dir; target.className = root.className;
          for (const key of ["theme"]) {
            if (root.dataset[key]) target.dataset[key] = root.dataset[key]; else delete target.dataset[key];
          }
        };
        mirror();
        const observer = new MutationObserver(mirror);
        observer.observe(root, { attributes: true, attributeFilter: ["lang", "dir", "class", "data-theme"] });
        child.addEventListener("unload", () => observer.disconnect(), { once: true });
      }, { once: true });
    } }).then((opened) => { if (!opened) toast.error(t("Could not open a new window. Allow pop-ups and try again.")); })
      .catch(() => toast.error(t("Could not open a new window. Allow pop-ups and try again.")))
      .finally(() => popping.current.delete(address));
  }, []);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (drafts.current.size) { event.preventDefault(); event.returnValue = ""; }
    };
    addEventListener("beforeunload", beforeUnload);
    return () => removeEventListener("beforeunload", beforeUnload);
  }, []);
  const [active, setActive] = useState<string>();
  const [activeRoute, setActiveRoute] = useState<string>();
  const [openTabs, setOpenTabs] = useState<{ key: string; title: string }[]>([]);
  const [paletteOpen, setPaletteOpen] = useState(false);
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
  const navKey = `${storageKey}:nav`;
  const [memory, setMemory] = useState<{ recent: RecentEntry[]; favorites: string[] }>(() => {
    try { const saved = localStorage.getItem(navKey); if (saved) return { recent: [], favorites: [], ...JSON.parse(saved) }; } catch {   }
    return { recent: [], favorites: [] };
  });
  const remember = useCallback((change: (m: { recent: RecentEntry[]; favorites: string[] }) => { recent: RecentEntry[]; favorites: string[] }) => setMemory((m) => {
    const next = change(m);
    try { localStorage.setItem(navKey, JSON.stringify(next)); } catch {   }
    return next;
  }), [navKey]);
  const toggleFavorite = useCallback((app: string) => remember((m) => ({ ...m, favorites: m.favorites.includes(app) ? m.favorites.filter((id) => id !== app) : [...m.favorites, app] })), [remember]);
  const currentApp = useRef(applications?.current); currentApp.current = applications?.current;
  const visited = useCallback((route: Route, title: string) => {
    if (rail && [routeKey(rail.home), routeKey(rail.applications)].includes(routeKey(route))) return;
    const key = keyOf(route);
    remember((m) => ({ ...m, recent: [{ route, title, app: currentApp.current, at: Date.now() }, ...m.recent.filter((r) => keyOf(r.route) !== key)].slice(0, 20) }));
  }, [rail, remember, keyOf]);
  const [navOpen, setNavOpen] = useState(nav.length > 0);
  const navPreferences = useRef(new Map<string, boolean>());
  const navScope = applications?.current ?? layoutScope ?? "";
  const hasNavigation = nav.length > 0 || !!navigation;
  useEffect(() => {
    if (hasNavigation) setNavOpen(navPreferences.current.get(navScope) ?? true);
  }, [navScope, hasNavigation]);
  const [inspection, setInspection] = useState<{ route: Route; focus: HTMLElement | null }>();
  useEffect(() => setInspection(undefined), [active, layoutScope]);
  const [compact, setCompact] = useState(() => typeof matchMedia !== "undefined" && matchMedia("(max-width: 639px)").matches);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  const navigationTrigger = useRef<HTMLButtonElement>(null);
  useEffect(() => setMobileNavOpen(false), [activeRoute, layoutScope]);
  const compactGroup = useRef(false);
  useEffect(() => {
    const media = matchMedia("(max-width: 639px)");
    const changed = () => { setCompact(media.matches); setMobileNavOpen(false); };
    media.addEventListener("change", changed);
    return () => media.removeEventListener("change", changed);
  }, []);
  useEffect(() => {
    const api = dock.current, panel = api?.activePanel;
    if (compact && panel?.group.api.location.type === "grid") {
      if (!panel.api.isMaximized()) {
        panel.api.maximize();
        compactGroup.current = true;
      }
    } else if (!compact && compactGroup.current) {
      api?.exitMaximizedGroup();
      compactGroup.current = false;
    }
  }, [compact, active]);
  const navigationVisible = compact ? mobileNavOpen : navOpen;
  const toggleNavigation = () => compact ? setMobileNavOpen((open) => !open) : setNavOpen((open) => {
    navPreferences.current.set(navScope, !open);
    return !open;
  });

  const followed = useRef(onActiveRoute);
  followed.current = onActiveRoute;
  const emptied = useRef(onEmpty); emptied.current = onEmpty;
  const visit = useRef(visited); visit.current = visited;

  const canBeside = (id: string | undefined, dockScope: string | undefined) => {
    const api = docks.current.get(dockScope), panel = id && api?.getPanel(id);
    return !!api && !!panel && !compact && (panel.group.panels.length > 1 || api.groups.some((group) => group !== panel.group && group.api.location.type === "grid"));
  };
  const placeBeside = useCallback((id: string, reference: string, dockScope: string | undefined) => {
    const api = docks.current.get(dockScope), panel = api?.getPanel(id), source = api?.getPanel(reference)?.group;
    if (!api || !panel || !source || compact) return;
    if (panel.group !== source && panel.group.api.location.type === "grid") return;
    const other = api.groups.find((group) => group !== source && group.api.location.type === "grid");
    if (!other && (source.api.location.type !== "grid" || source.panels.length < 2)) return;
    panel.api.moveTo({ group: other ?? api.addGroup({ referenceGroup: source, direction: "right" }) });
  }, [compact]);
  const show = useCallback((route: Route, options: OpenOptions, dockScope: string | undefined) => {
    const api = dockScope === scope.current ? dock.current : docks.current.get(dockScope), view = byId.get(route.view);
    if (!view) return;
    if (!api) {
      setCreated((prev) => prev.includes(dockScope) ? prev : [...prev, dockScope]);
      queuedOpen.current = { route, options, scope: dockScope };
      return;
    }
    const reference = api.activePanel?.id;
    const key = keyOf(route), title = view.title(route.params ?? {});
    let panel = api.getPanel(key);
    if (panel) {
      if (view.documentKey) panel.api.updateParameters({ route });
      if (options.window === "beside" && reference) placeBeside(panel.id, reference, dockScope);
    } else if (options.window === "beside" && !compact && api.activePanel?.group.api.location.type === "grid") {
      const current = api.activePanel.group;
      const other = api.groups.find((group) => group !== current && group.api.location.type === "grid")
        ?? api.addGroup({ referenceGroup: current, direction: "right" });
      panel = api.addPanel({ id: key, component: "view", title, params: { route }, position: { referenceGroup: other } });
    }
    panel ??= api.addPanel({ id: key, component: "view", title, params: { route } });
    panel.api.setActive();
    syncs.current.get(dockScope)?.();
    if (options.window === "popout") popout(panel.id, dockScope);
    if (options.replace && options.from && options.from !== taskKey(dockScope, panel.id)) closePanel(options.from);
  }, [byId, compact, popout, placeBeside, keyOf, closePanel]);
  const openResolved = useCallback((route: Route, options: OpenOptions = {}, hint?: string | undefined) => {
    if (!viewRegistry.current.has(route.view)) return;
    const source = options.from ? taskOwner(options.from) : undefined;
    if (source && !source.panel) return;
    const target = hint ?? (route.params?.call && source ? source.scope : routeScope.current?.(route) ?? source?.scope ?? scope.current);
    const call = route.params?.call ? transfers.current.read(route.params.call, taskKey(target, keyOf(route))) : undefined;
    const origin = call ? taskOwner(call.from).panel : undefined;
    const originRoute = (origin?.params as { route?: Route } | undefined)?.route;
    const scopeApi = target === scope.current ? dock.current : docks.current.get(target);
    if (options.ifCurrent && (source?.api ?? scopeApi)?.activePanel?.id !== keyOf(options.ifCurrent)) return;
    if (source && source.scope !== scope.current) {
      if (target === source.scope && options.window !== "inspect") show(route, { ...options, window: "tab" }, target);
      return;
    }
    if (options.window === "inspect") {
      setInspection(previous => ({ route, focus: previous?.focus ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null) }));
      return;
    }
    setInspection(undefined);
    if (switching.current) { queuedOpen.current = { route, options, scope: target }; return; }
    if (target !== scope.current) {
      show(route, options, target);
      followed.current?.(originRoute ?? route);
      return;
    }
    show(route, options, target);
  }, [show, keyOf, taskOwner]);
  const open = useCallback((route: Route, options: OpenOptions = {}, hint?: string | undefined) => {
    const resolve = routeReader.current;
    if (!resolve) { openResolved(route, options, hint); return; }
    const source = options.from ? taskOwner(options.from) : undefined;
    if (source && !source.panel) return;
    if (options.ifCurrent && (source?.api ?? dock.current)?.activePanel?.id !== keyOf(options.ifCurrent)) return;
    const foreground = !source || source.scope === scope.current;
    const revision = foreground ? ++incomingRevision.current : incomingRevision.current;
    const take = (resolved: Route) => { if (!foreground || revision === incomingRevision.current) openResolved(resolved, options, hint); };
    try {
      const resolved = resolve(route);
      if (resolved instanceof Promise) void resolved.then(take).catch(() => {
        if (revision === incomingRevision.current) toast.error(t("Could not open this view."));
      });
      else take(resolved);
    } catch {
      if (revision === incomingRevision.current) toast.error(t("Could not open this view."));
    }
  }, [openResolved, keyOf, taskOwner]);
  const readLink = useCallback((route: Route) => {
    const revision = ++incomingRevision.current;
    void Promise.resolve().then(() => routeReader.current?.(route) ?? route).then((resolved) => {
      if (revision !== incomingRevision.current) return;
      if (viewRegistry.current.has(resolved.view)) openResolved(resolved);
      else {
        toast.error(t("This address points to a task your current access does not include."));
        if (!dock.current?.panels.length) open(applicationEntries.current?.apps.find((app) => app.id === scope.current)?.home ?? home);
      }
    }).catch(() => {
      if (revision !== incomingRevision.current) return;
      const previous = (dock.current?.activePanel?.params as { route?: Route } | undefined)?.route;
      if (previous) history.replaceState(null, "", routeToHash(previous));
      toast.error(t("Could not open this view."));
    });
  }, [open, openResolved, home]);
  useEffect(() => {
    const linked = routeFromHash(location.hash);
    if (!resolveRoute || !linked || !dock.current) return;
    const revision = incomingRevision.current;
    void Promise.resolve(resolveRoute(linked)).then(resolved => {
      if (revision === incomingRevision.current && routeToHash(linked) === location.hash && routeKey(resolved) !== routeKey(linked)) openResolved(resolved);
    }).catch(() => {   });
  }, [resolveRoute, openResolved]);
  const beside = useCallback((id: string, dockScope: string | undefined) => {
    placeBeside(id, id, dockScope);
    docks.current.get(dockScope)?.getPanel(id)?.api.setActive();
  }, [placeBeside]);
  const closeAll = useCallback(() => closePanels((dock.current?.panels ?? []).map((p) => taskKey(scope.current, p.id))), [closePanels]);
  const closeOthers = useCallback((id: string, dockScope: string | undefined) => closePanels((docks.current.get(dockScope)?.panels ?? []).filter((p) => p.id !== id).map((p) => taskKey(dockScope, p.id))), [closePanels]);
  const closeRight = useCallback((id: string, dockScope: string | undefined) => {
    const group = docks.current.get(dockScope)?.getPanel(id)?.group; if (!group) return;
    const index = group.panels.findIndex((p) => p.id === id);
    closePanels(group.panels.slice(index + 1).map((p) => taskKey(dockScope, p.id)));
  }, [closePanels]);
  useEffect(() => {
    if (scope.current === layoutScope && dock.current && docks.current.has(layoutScope)) return;
    scope.current = layoutScope;
    setCreated((prev) => prev.includes(layoutScope) ? prev : [...prev, layoutScope]);
    const api = docks.current.get(layoutScope);
    if (api) {
      dock.current = api;
      switching.current = false;
      syncs.current.get(layoutScope)?.();
    }
  }, [layoutScope]);
  useEffect(() => {
    unloaded.current = false;
    booted.current = false;
    return () => { unloaded.current = true; };
  }, []);

  const workspace = useMemo<WorkspaceApi>(() => ({
    open, notify: toast, close: (route, from) => closePanel(taskKey(from ? taskOwner(from).scope : scope.current, keyOf(route))),
    closeAll: from => {
      if (!from) { closeAll(); return; }
      const owner = taskOwner(from);
      closePanels((owner.api?.panels ?? []).map(panel => taskKey(owner.scope, panel.id)));
    }, unsaved,
    confirmLeave: (run, exceptPanel) => unsaved.ask(allTasks().filter(id => id !== exceptPanel), run),
    transfer: (from, route, input, result) => {
      const source = taskOwner(from);
      if (!source.panel) return;
      const id = transfers.current.start(from, input, result), called = { ...route, params: { ...route.params, call: id } };
      transfers.current.bind(id, taskKey(source.scope, keyOf(called))); open(called, { from });
    },
    palette: () => setPaletteOpen(true),
    retitle: (id, name) => {
      const panel = taskOwner(id).panel;
      const route = (panel?.params as { route?: Route } | undefined)?.route;
      const category = route ? byId.get(route.view)?.title(route.params ?? {}) : undefined;
      const title = category && category !== name ? `${category} · ${name}` : name;
      if (panel && panel.api.title !== title) panel.setTitle(title);
    },
    recent: memory.recent, favorites: memory.favorites, toggleFavorite, applications, rail,
  }), [open, closePanel, closePanels, closeAll, unsaved, memory, toggleFavorite, applications, rail, byId, allTasks, taskOwner]);

  const tab = useCallback((props: IDockviewPanelHeaderProps) => {
    const id = props.api.id, dockScope = useContext(DockScope), address = taskKey(dockScope, id);
    const items: { label: string; run: () => void; separator?: boolean; disabled?: boolean }[] = [
      { label: t("Close"), run: () => closePanel(address) },
      { label: t("Close others"), run: () => closeOthers(id, dockScope) },
      { label: t("Close to the right"), run: () => closeRight(id, dockScope) },
      { label: t("Close all"), run: () => closePanels((docks.current.get(dockScope)?.panels ?? []).map(panel => taskKey(dockScope, panel.id))) },
      { label: t("Open beside"), run: () => beside(id, dockScope), separator: true, disabled: !canBeside(id, dockScope) },
      { label: t("Move to new window"), run: () => popout(id, dockScope) },
    ];
    return <ContextMenu.Root>
      <ContextMenu.Trigger asChild><div className="flex h-full"><DockviewDefaultTab {...props} closeActionOverride={() => closePanel(address)} /></div></ContextMenu.Trigger>
      <ContextMenu.Portal><ContextMenu.Content className={menuPanel}>
        {items.map((item) => <div key={item.label}>{item.separator && <ContextMenu.Separator className="my-1 h-px bg-border" />}
          <ContextMenu.Item onSelect={item.run} disabled={item.disabled} className={menuItem}>{item.label}</ContextMenu.Item></div>)}
      </ContextMenu.Content></ContextMenu.Portal>
    </ContextMenu.Root>;
  }, [closePanel, closePanels, closeOthers, closeRight, beside, popout, compact]);

  const components = useMemo(() => ({
    view: ({ params, api }: IDockviewPanelProps<{ route: Route }>) => {
      const view = useContext(ViewRegistryContext).get(params.route.view);
      const dockScope = useContext(DockScope), address = taskKey(dockScope, api.id);
      const ticket = params.route.params?.call;
      const call = ticket ? transfers.current.read(ticket, address) : undefined;
      const returned = (value: unknown) => {
        if (!ticket) return;
        unsaved.ask([address], () => {
          const origin = transfers.current.finish(ticket, address, value);
          if (origin && taskOwner(origin).panel) {
            taskOwner(origin).panel!.api.setActive();
            taskOwner(address).panel?.api.close();
          }
        });
      };
      const [panelVisible, setVisible] = useState(api.isVisible);
      const front = useContext(FrontDock);
      useEffect(() => {
        const changed = api.onDidVisibilityChange((event) => setVisible(event.isVisible));
        return () => changed.dispose();
      }, [api]);
      const visible = panelVisible && front;
      return <div style={{ display: panelVisible ? undefined : "none" }} role="region" aria-label={view?.title(params.route.params ?? {}) ?? t("Workspace view")} className="h-full overflow-auto bg-background p-4">
        <ViewVisibilityContext.Provider value={visible}><PanelContext.Provider value={address}><ViewCallContext.Provider value={ticket ? { input: call?.input, expired: !call, returnValue: call ? returned : undefined } : undefined}><ViewBoundary onClose={() => closePanel(address)}>
          {view ? view.render(params.route.params ?? {}) : <p className="text-sm text-muted">{t("This view no longer exists.")}</p>}
        </ViewBoundary></ViewCallContext.Provider></PanelContext.Provider></ViewVisibilityContext.Provider>
      </div>;
    },
  }), [closePanel, taskOwner, unsaved]);

  const onDockReady = useCallback(({ api }: { api: DockviewApi }, dockScope: string | undefined) => {
    docks.current.set(dockScope, api);
    if (dockScope === scope.current) dock.current = api;
    api.onDidRemovePanel((panel) => {
      const address = taskKey(dockScope, panel.id);
      transfers.current.remove(address);
      drafts.current.delete(address);
      setPending((request) => {
        if (!request) return request;
        const panels = request.panels.filter((id) => id !== address);
        return panels.length ? { ...request, panels } : undefined;
      });
    });
    const sync = () => {
      const front = dockScope === scope.current && dock.current === api;
      if (front) {
        setOpenTabs(api.panels.map((panel) => ({ key: panel.id, title: panel.title ?? panel.id })));
        setActive(api.activePanel?.id);
        const route = (api.activePanel?.params as { route?: Route } | undefined)?.route;
        setActiveRoute(route && routeKey(route));
        if (!switching.current) {
          history.replaceState(null, "", route ? routeToHash(route) : "#/");
          const call = route?.params?.call ? transfers.current.read(route.params.call, taskKey(dockScope, api.activePanel!.id)) : undefined;
          const origin = call ? taskOwner(call.from).panel : undefined;
          const originRoute = (origin?.params as { route?: Route } | undefined)?.route;
          if (route) followed.current?.(originRoute ?? route); else emptied.current?.();
          if (route && !call) visit.current(route, api.activePanel?.title ?? route.view);
        }
      }
      if (!unloaded.current) saveLayout(api, dockScope);
    };
    syncs.current.set(dockScope, sync);
    api.onDidLayoutChange(sync);
    api.onDidActivePanelChange(sync);
    const linked = !booted.current ? routeFromHash(location.hash) : undefined;
    const linkRevision = incomingRevision.current;
    switching.current = true;
    void loadLayout(api, dockScope).then((loaded) => {
      if (!loaded || unloaded.current || docks.current.get(dockScope) !== api) return;
      const queued = queuedOpen.current?.scope === dockScope ? queuedOpen.current : undefined;
      if (queued) queuedOpen.current = undefined;
      const front = dockScope === scope.current;
      if (front) { dock.current = api; switching.current = false; }
      if (!api.panels.length && !linked && !queued) show(applicationEntries.current?.apps.find((app) => app.id === dockScope)?.home ?? home, {}, dockScope);
      if (queued) show(queued.route, queued.options, dockScope);
      if (front) {
        booted.current = true;
        if (!queued && linked && linkRevision === incomingRevision.current) readLink(linked);
      }
      sync();
    });
  }, [home, open, show, readLink, loadLayout, saveLayout, taskOwner]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented && mobileNavOpen) { setMobileNavOpen(false); navigationTrigger.current?.focus(); }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setPaletteOpen((v) => !v); }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "w" && dock.current?.activePanel) { e.preventDefault(); closePanel(taskKey(scope.current, dock.current.activePanel.id)); }
    };
    const onHash = () => { const route = routeFromHash(location.hash); if (route) readLink(route); };
    addEventListener("keydown", onKey);
    addEventListener("hashchange", onHash);
    return () => { removeEventListener("keydown", onKey); removeEventListener("hashchange", onHash); };
  }, [readLink, closePanel, mobileNavOpen]);

  const activePanel = () => dock.current?.activePanel;
  const builtInMenus: Menu[] = [
    { label: t("View"), items: [
      { label: t("Command palette"), shortcut: "⌘K", onSelect: () => setPaletteOpen(true) },
      { label: navigationVisible ? t("Hide navigation") : t("Show navigation"), onSelect: toggleNavigation },
      { label: t("Reset layout"), onSelect: () => leaveViews(() => { dock.current?.clear(); open(applicationEntries.current?.apps.find((app) => app.id === scope.current)?.home ?? home); }) },
    ] },
    { label: t("Appearance"), items: [
      ...([["system", t("Follow system")], ["light", t("Light")], ["dark", t("Dark")]] as const).map(([scheme, label]) => ({ label, shortcut: appearance.scheme === scheme ? "✓" : undefined, onSelect: () => appearance.setScheme(scheme) })),
    ] },
    { label: t("Window"), items: [
      { label: t("Open beside"), disabled: !canBeside(active, scope.current), onSelect: () => { const p = activePanel(); if (p) beside(p.id, scope.current); } },
      { label: t("Move to new window"), disabled: !active, onSelect: () => { const p = activePanel(); if (p) popout(p.id, scope.current); } },
      { label: t("Close"), shortcut: "⌘W", disabled: !active, onSelect: () => { const p = activePanel(); if (p) closePanel(taskKey(scope.current, p.id)); } },
      { label: t("Close others"), disabled: openTabs.length < 2, onSelect: () => { const p = activePanel(); if (p) closeOthers(p.id, scope.current); } },
      { label: t("Close all"), disabled: !openTabs.length, onSelect: closeAll },
      ...openTabs.map((t) => ({ label: t.title, onSelect: () => dock.current?.getPanel(t.key)?.api.setActive() })),
    ] },
  ];

  const watermark = useCallback(() => <div className="grid h-full place-content-center gap-3 p-6 text-center text-sm text-muted">
    <p>{t("No open tabs.")}</p>
    <div className="flex justify-center gap-2">
      <Button onClick={() => open(applicationEntries.current?.apps.find((app) => app.id === scope.current)?.home ?? home)}>{t("Open home")}</Button>
      <Button variant="ghost" onClick={() => setPaletteOpen(true)}>{t("Search and commands")}</Button>
    </div>
  </div>, [open, home]);

  const favoriteApps = (applications?.apps ?? []).filter((a) => memory.favorites.includes(a.id));
  const starred = !!applications?.current && memory.favorites.includes(applications.current);
  type RailItem = { id: string; label: string; icon: ReactNode; badge?: number; active?: boolean; run: () => void };
  const railItems: RailItem[] = rail ? [
    { id: "home", label: t("Home"), icon: <HomeIcon />, active: active === routeKey(rail.home), run: () => open(rail.home) },
    { id: "search", label: t("Search"), icon: <Search />, run: () => setPaletteOpen(true) },
    ...(rail.notifications ? [{ id: "notifications", label: t("Notifications"), icon: <Bell />, badge: rail.notifications.unread, active: active === routeKey(rail.notifications.route), run: () => open(rail.notifications!.route) }] : []),
    { id: "applications", label: t("Applications"), icon: <Grid2x2 />, active: active === routeKey(rail.applications), run: () => open(rail.applications) },
  ] : [];
  const railButton = (item: RailItem) => (
    <button key={item.id} type="button" title={item.label} aria-label={item.label} aria-current={item.active ? "page" : undefined} onClick={item.run}
      className={cn("relative grid size-9 place-items-center rounded-md text-muted hover:bg-row-hover hover:text-foreground [&_svg]:size-4", item.active && "bg-row-selected text-foreground")}>
      {item.icon}{item.badge ? <span className="absolute right-0.5 top-0.5 min-w-4 rounded-full bg-primary px-1 text-center text-2xs font-semibold leading-4 text-primary-foreground">{item.badge > 99 ? "99+" : item.badge}</span> : null}
    </button>
  );
  const sessionMenu = session && <SessionMenu compact={!!rail && !compact} session={{ ...session, onAccount: session.onAccount ? () => leaveViews(() => session.onAccount?.()) : undefined, onSwitch: (id) => leaveViews(() => session.onSwitch(id)) }}
    onLanguageSelect={(id) => leaveViews(() => {
      void Promise.resolve(onLanguage?.(id)).catch(() => undefined).finally(() => setLanguage(id));
    })} />;

  return (
    <WorkspaceContext.Provider value={workspace}>
      <ViewRegistryContext.Provider value={byId}>
      <div className={cn("grid h-dvh w-full min-w-0 max-w-full overflow-hidden bg-background text-foreground",
        rail && !compact ? "grid-cols-[44px_minmax(0,1fr)]" : "grid-cols-1", rail && compact ? "grid-rows-[36px_1fr_44px]" : "grid-rows-[36px_1fr]")}>
        {rail && !compact && (
          <aside aria-label={t("Platform")} data-ui="chrome" className="row-span-2 flex flex-col items-center gap-1 border-r border-border bg-surface py-1.5">
            {railItems.map(railButton)}
            <div className="my-1 h-px w-6 bg-border" />
            <RailList label={t("Recent")} icon={<Clock />} empty={t("Nothing opened yet.")}
              items={memory.recent.slice(0, 12).map((r) => ({ id: routeKey(r.route), label: r.title, detail: r.app ? applications?.apps.find((a) => a.id === r.app)?.title : undefined, run: () => open(r.route) }))} />
            <RailList label={t("Favorites")} icon={<Star />} empty={t("Star an application to keep it here.")}
              items={favoriteApps.map((a) => ({ id: a.id, label: a.title, icon: a.icon, run: () => applications!.onSelect(a.id) }))} />
            <div className="mt-auto flex flex-col items-center gap-1">
              {rail.assist && railButton({ id: "assist", label: t("Assistant"), icon: <Sparkles />, active: active === routeKey(rail.assist), run: () => open(rail.assist!) })}
              {sessionMenu}
            </div>
          </aside>
        )}
        <header data-ui="chrome" className="flex min-w-0 items-center gap-2 overflow-hidden border-b border-border bg-surface px-2">
          <button ref={navigationTrigger} type="button" aria-label={t("Toggle navigation")} aria-expanded={navigationVisible} onClick={toggleNavigation}
            className="rounded-sm p-1 text-muted hover:bg-row-hover hover:text-foreground"><PanelLeft className="size-4" /></button>
          {applications && applications.apps.length > 0
            ? <AppMenu applications={applications} product={product} icon={productIcon} />
            : <span className="flex items-center gap-2 pr-2 text-sm font-semibold tracking-tight [&_svg]:size-4 [&_svg]:text-muted">{productIcon}{product}</span>}
          {applications?.current && (
            <button type="button" aria-label={starred ? t("Remove from favorites") : t("Add to favorites")} aria-pressed={starred} onClick={() => toggleFavorite(applications.current!)}
              className={cn("rounded-sm p-1 hover:bg-row-hover", starred ? "text-[var(--tone-warning)]" : "text-muted hover:text-foreground")}>
              <Star className="size-3.5" fill={starred ? "currentColor" : "none"} />
            </button>
          )}
          <Menubar.Root className="hidden items-center sm:flex">
            {[...menus, ...builtInMenus].map((menu) => (
              <Menubar.Menu key={menu.label}>
                <Menubar.Trigger className="h-6 rounded-sm px-2 text-sm outline-none hover:bg-row-hover data-[state=open]:bg-row-hover">
                  {menu.label}
                </Menubar.Trigger>
                <Menubar.Portal>
                  <Menubar.Content align="start" sideOffset={4} className={menuPanel}>
                    {menu.items.map((item) => (
                      <Menubar.Item key={item.label} disabled={item.disabled} onSelect={item.onSelect} className={menuItem}>
                        {item.label}{item.shortcut && <span className="ml-auto pl-6 text-xs text-muted">{item.shortcut}</span>}
                      </Menubar.Item>
                    ))}
                  </Menubar.Content>
                </Menubar.Portal>
              </Menubar.Menu>
            ))}
          </Menubar.Root>
          {compact && <DropdownMenu.Root>
            <DropdownMenu.Trigger aria-label={t("Workspace menu")} className="grid size-7 shrink-0 place-items-center rounded-sm text-muted hover:bg-row-hover hover:text-foreground data-[state=open]:bg-row-hover">
              <Ellipsis className="size-4" />
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content align="start" sideOffset={4} className={cn(menuPanel, "max-h-[70dvh] max-w-[calc(100vw-16px)] overflow-auto")}>
                {[...menus, ...builtInMenus].map((menu, index) => <DropdownMenu.Group key={menu.label}>
                  {index > 0 && <DropdownMenu.Separator className="my-1 h-px bg-border" />}
                  <DropdownMenu.Label className="px-2 py-1 text-xs font-medium text-muted">{menu.label}</DropdownMenu.Label>
                  {menu.items.map((item) => <DropdownMenu.Item key={item.label} disabled={item.disabled} onSelect={item.onSelect} className={cn(menuItem, "h-auto min-h-8 gap-3 py-1 pl-2")}>
                    <span className="min-w-0 break-words">{item.label}</span>{item.shortcut && <span className="ml-auto shrink-0 text-xs text-muted">{item.shortcut}</span>}
                  </DropdownMenu.Item>)}
                </DropdownMenu.Group>)}
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>}
          <button type="button" aria-label={t("Search and commands")} onClick={() => setPaletteOpen(true)}
            className="mx-auto flex h-6 w-6 shrink-0 items-center justify-center gap-2 rounded-md border border-border bg-background text-sm text-muted hover:text-foreground lg:w-72 lg:justify-start lg:px-2">
            <Search className="size-3.5" /><span className="max-lg:hidden">{t("Search and commands")}</span><kbd className="ml-auto text-xs max-lg:hidden">⌘K</kbd>
          </button>
          <div className="ml-auto flex items-center gap-2">
            <span className="max-sm:hidden">{status}</span>
            {(!rail || compact) && sessionMenu}
          </div>
        </header>
        <div className={cn("relative grid min-h-0 min-w-0", !compact && navOpen && (nav.length > 0 || navigation) ? "grid-cols-[220px_minmax(0,1fr)]" : "grid-cols-1")}>
          {compact && mobileNavOpen && <button type="button" aria-label={t("Close navigation")} className="fixed inset-x-0 bottom-0 top-9 z-20 bg-overlay" onClick={() => setMobileNavOpen(false)} />}
          {(nav.length > 0 || navigation) && (!compact && navOpen || compact && mobileNavOpen) && (
            <nav aria-label={t("Main")} className={cn("overflow-auto border-r border-border bg-surface p-2",
              compact && "fixed inset-y-9 left-0 z-30 w-[min(19rem,85vw)] shadow-lg")}>
              {navigation}
              {nav.map((section) => (
                <div key={section.label} className="mb-3">
                  <div className="px-2 pb-1 text-xs font-medium uppercase tracking-wide text-muted">{section.label}</div>
                  <NavigationItems items={section.items} active={active} onOpen={route => { open(route); setMobileNavOpen(false); }} />
                </div>
              ))}
            </nav>
          )}
          <div className="platform-dock relative min-h-0 min-w-0 overflow-hidden">
            {created.map((dockScope) => (
              <div key={dockScope ?? "shell"} data-density={appEntries?.apps.find(app => app.id === dockScope)?.category === "business" ? "comfortable" : "compact"} className="absolute inset-0" style={{ visibility: dockScope === layoutScope ? "visible" : "hidden", zIndex: dockScope === layoutScope ? 1 : 0 }}>
                <DockScope.Provider value={dockScope}><FrontDock.Provider value={dockScope === layoutScope}>
                  <DockviewReact defaultRenderer="always" defaultTabComponent={tab} watermarkComponent={watermark} components={components} onReady={(event) => onDockReady(event, dockScope)} theme={appearance.theme==="dark"?themeDark:themeLight} />
                </FrontDock.Provider></DockScope.Provider>
              </div>
            ))}
          </div>
        </div>
        {inspection && <Sheet open onOpenChange={open => { if (!open) setInspection(undefined); }} title={byId.get(inspection.route.view)?.title(inspection.route.params ?? {}) ?? t("Record")} width={560} returnFocus={inspection.focus}>
          <ViewBoundary onClose={() => setInspection(undefined)}>
            {byId.get(inspection.route.view)?.render(inspection.route.params ?? {})}
          </ViewBoundary>
        </Sheet>}
        {rail && compact && (
          <nav aria-label={t("Platform")} data-ui="chrome" className="flex items-center justify-around border-t border-border bg-surface px-2">
            {railItems.map(railButton)}
          </nav>
        )}
      </div>
      <Command.Dialog open={paletteOpen} onOpenChange={setPaletteOpen} label={t("Command palette")}
        overlayClassName="fixed inset-0 z-40 bg-overlay"
        contentClassName="fixed left-1/2 top-[15%] z-50 w-[min(560px,calc(100vw-32px))] -translate-x-1/2 overflow-hidden rounded-md border border-border bg-surface text-foreground shadow-2xl">
        <Command.Input value={typed} onValueChange={setTyped} placeholder={t("Search records, go to, open, run…")} className="h-10 w-full border-b border-border bg-transparent px-3 text-base outline-none" />
        {searchPending && <p role="status" className="px-3 py-2 text-sm text-muted">{t("Searching records…")}</p>}
        {answer?.error !== undefined && <div role="alert" className="grid gap-1 border-b border-border px-3 py-2 text-sm">
          <p className="text-danger">{t("Record search failed.")} {answer.error}</p>
          <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => setSearchAttempt(attempt => attempt + 1)}>{t("Retry")}</Button>
        </div>}
        <Command.List className="max-h-80 overflow-auto p-1">
          {!searchPending && answer?.error === undefined && <Command.Empty className="p-3 text-sm text-muted">{t("No results")}</Command.Empty>}
          {found.length > 0 && (
            <Command.Group heading={t("Records")} className={paletteGroup}>
              {found.map((hit) => (
                <Command.Item key={hit.id} value={`record ${answer!.text} ${hit.label} ${hit.id}`} className={paletteItem}
                  onSelect={() => { hit.open(); setPaletteOpen(false); }}>{hit.label}{hit.detail && <span className="ml-auto font-mono text-xs text-muted">{hit.detail}</span>}</Command.Item>
              ))}
            </Command.Group>
          )}
          {applications && applications.apps.length > 0 && (
            <Command.Group heading={t("Applications")} className={paletteGroup}>
              {applications.apps.map((a) => (
                <Command.Item key={a.id} value={`app ${a.title} ${applications.categories.find((c) => c.id === a.category)?.label ?? ""}`} className={paletteItem}
                  onSelect={() => { applications.onSelect(a.id); setPaletteOpen(false); }}>{a.icon}{a.title}
                  <span className="ml-auto text-xs text-muted">{applications.categories.find((c) => c.id === a.category)?.label}</span></Command.Item>
              ))}
            </Command.Group>
          )}

          {nav.map((section) => (
            <Command.Group key={section.label} heading={section.label} className={paletteGroup}>
              {navigationLinks(section.items).map((item) => (
                <Command.Item key={routeKey(item.route)} value={`${section.label} ${item.label}`} className={paletteItem}
                  onSelect={() => { open(item.route); setPaletteOpen(false); }}>{item.icon}{item.label}</Command.Item>
              ))}
            </Command.Group>
          ))}
          {openTabs.length > 0 && (
            <Command.Group heading={t("Open tabs")} className={paletteGroup}>
              {openTabs.map((tab) => (
                <Command.Item key={tab.key} value={`tab ${tab.title} ${tab.key}`} className={paletteItem}
                  onSelect={() => { dock.current?.getPanel(tab.key)?.api.setActive(); setPaletteOpen(false); }}>{tab.title}</Command.Item>
              ))}
            </Command.Group>
          )}
          {commands.length > 0 && (
            <Command.Group heading={t("Commands")} className={paletteGroup}>
              {commands.map((c) => (
                <Command.Item key={c.id} value={`${c.group ?? ""} ${c.label}`} className={paletteItem}
                  onSelect={() => { setPaletteOpen(false); c.run(); }}>
                  {c.label}{c.shortcut && <span className="ml-auto text-xs text-muted">{c.shortcut}</span>}
                </Command.Item>
              ))}
            </Command.Group>
          )}
        </Command.List>
      </Command.Dialog>
      <Dialog open={!!pending} title={t("Unsaved changes")} onOpenChange={(open) => {
        if (!open) { const id = pending?.panels[0]; setPending(undefined); if (id) dock.current?.getPanel(id)?.api.setActive(); }
      }}>
        <div className="grid gap-3 text-sm">
          <p>{t("Save your work in the editor, or discard the changes to continue.")}</p>
          <ul className="list-inside list-disc text-muted">{pending?.panels.map((id) => <li key={id}>{dock.current?.getPanel(id)?.title ?? id}</li>)}</ul>
          <div className="flex justify-end gap-2">
            <Button onClick={() => { const id = pending?.panels[0]; setPending(undefined); if (id) dock.current?.getPanel(id)?.api.setActive(); }}>{t("Keep editing")}</Button>
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
      <Toaster position="bottom-right" toastOptions={{ className: "!rounded-md !border-border !bg-surface !text-foreground !text-sm" }} />
      </ViewRegistryContext.Provider>
    </WorkspaceContext.Provider>
  );
}

function AppMenu({ applications, product, icon }: { applications: Applications; product: string; icon?: ReactNode }) {
  const current = applications.apps.find((a) => a.id === applications.current);
  const groups = applications.categories.map(category => ({ ...category, apps: applications.apps.filter(app => app.category === category.id) })).filter(group => group.apps.length);
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger aria-label={t("Switch application")} className="flex h-7 min-w-0 max-w-64 items-center gap-2 rounded-md px-2 text-sm font-semibold tracking-tight hover:bg-row-hover max-sm:max-w-32 [&>svg]:size-4 [&>svg]:shrink-0 [&>svg]:text-muted">
        {current?.icon ?? icon ?? <LayoutGrid />}<span className="truncate">{product}</span><ChevronDown className="size-3.5 shrink-0 text-muted" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="start" sideOffset={4} collisionPadding={8} style={{ width: `min(calc(100vw - 16px), ${Math.min(4, groups.length) * 15}rem)` }} className={cn(menuPanel, "max-h-[70dvh] overflow-auto")}>
          <DropdownMenu.RadioGroup value={applications.current ?? ""} onValueChange={applications.onSelect} className={cn("grid grid-cols-1 gap-3 p-1", groups.length > 1 && "sm:grid-cols-2", groups.length > 2 && "lg:grid-cols-3", groups.length > 3 && "xl:grid-cols-4")}>
            {groups.map((category) => {
              return <div key={category.id}>
                <DropdownMenu.Label className="px-2 py-1 text-xs text-muted">{category.label}</DropdownMenu.Label>
                {category.apps.map((a) => (
                  <DropdownMenu.RadioItem key={a.id} value={a.id} className={cn(menuItem, "h-auto min-h-8 gap-2 py-1.5 [&_svg]:size-3.5 [&_svg]:shrink-0")}>
                    <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{a.icon}<span className="min-w-0 break-words">{a.title}</span>
                  </DropdownMenu.RadioItem>
                ))}
              </div>;
            })}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

function RailList({ label, icon, items, empty }: { label: string; icon: ReactNode; empty: string; items: { id: string; label: string; detail?: string; icon?: ReactNode; run: () => void }[] }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger title={label} aria-label={label}
        className="grid size-9 place-items-center rounded-md text-muted hover:bg-row-hover hover:text-foreground data-[state=open]:bg-row-hover data-[state=open]:text-foreground [&_svg]:size-4">
        {icon}
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content side="right" align="start" sideOffset={6} className={cn(menuPanel, "max-h-[70vh] min-w-56 max-w-80 overflow-auto")}>
          <DropdownMenu.Label className="px-2 py-1 text-xs text-muted">{label}</DropdownMenu.Label>
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

function SessionMenu({ session, onLanguageSelect, compact = false }: { session: Session; onLanguageSelect: (id: string) => void; compact?: boolean }) {
  const shown = session.name || session.principal;
  const avatar = <span className="grid size-5 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">
    {shown.slice(0, 1).toUpperCase()}
  </span>;
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger aria-label={`${session.principal} · ${session.tenant}`} title={compact ? `${session.principal} · ${session.tenant}` : undefined}
        className={compact ? "grid size-9 place-items-center rounded-md hover:bg-row-hover data-[state=open]:bg-row-hover"
          : "flex h-7 shrink-0 items-center gap-2 rounded-md border border-border px-2 text-sm hover:bg-row-hover"}>
        {avatar}
        {!compact && <>
          <span className="text-left leading-tight max-sm:hidden">
            <span className="block text-sm">{shown}</span>
            <span className="block text-xs text-muted">{session.tenant}{session.detail ? ` · ${session.detail}` : ""}</span>
          </span>
          <ChevronDown className="size-3.5 text-muted" />
        </>}
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content side={compact ? "right" : "bottom"} align="end" sideOffset={4} className={menuPanel}>
          <DropdownMenu.Label className="px-2 py-1 text-xs">
            <span className="block text-sm text-foreground">{shown}</span>
            <span className="block text-muted">{session.name ? `${session.principal} · ` : ""}{session.tenant}{session.detail ? ` · ${session.detail}` : ""}</span>
          </DropdownMenu.Label>
          {session.onAccount && <DropdownMenu.Item className={menuItem} onSelect={() => session.onAccount?.()}>{t("My account")}</DropdownMenu.Item>}
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <DropdownMenu.Label className="px-2 py-1 text-xs text-muted">{t("Switch tenant or identity")}</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={session.current} onValueChange={session.onSwitch}>
            {session.options.map((o) => (
              <DropdownMenu.RadioItem key={o.id} value={o.id} className={menuItem}>
                <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{o.label}
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
          <DropdownMenu.Separator className="my-1 h-px bg-border" />
          <DropdownMenu.Label className="px-2 py-1 text-xs text-muted">{t("Language")}</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={language()} onValueChange={onLanguageSelect}>
            {languages.map((l) => (
              <DropdownMenu.RadioItem key={l.id} value={l.id} className={menuItem}>
                <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{l.name}
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

const menuPanel = "z-50 min-w-48 rounded-md border border-border bg-surface p-1 text-sm shadow-lg";
const menuItem = "relative flex h-7 cursor-default select-none items-center rounded-sm pl-6 pr-2 outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-row-hover";
const paletteGroup = "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-muted";
const paletteItem = "flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-sm data-[selected=true]:bg-row-selected [&_svg]:size-3.5";
