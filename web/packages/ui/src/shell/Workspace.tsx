import {useInputDrafts} from "../fields/draft";
import {useTheme} from "../themes/theme";
import { DockviewReact, themeLight,themeDark, type DockviewApi, type IDockviewPanelHeaderProps, type IDockviewPanelProps, type SerializedDockview } from "dockview-react";
import { Bell, Clock, Ellipsis, Grid2x2, Home as HomeIcon, PanelLeft, Search, Sparkles, Star } from "lucide-react";
import { DropdownMenu, Menubar } from "radix-ui";
import { Component, createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Toaster, toast } from "sonner";
import { cn } from "../lib/cn";
import { routeFromHash, routeKey, routeToHash, type Route } from "./route";
import { setLanguage, t } from "../i18n";
import { Sheet } from "../primitives/sheet";
import { Button } from "../primitives/button";
import { ViewVisibilityContext } from "./ViewVisibility";
import { ViewTransfers } from "./ViewTransfers";
import { resolveLayout, rewriteLayout } from "./WorkspaceSupport";
import { NavigationItems, type NavSection } from "./Navigation";
import { AppMenu, CommandPalette, RailList, SessionMenu, TabHeader, UnsavedChanges, menuItem, menuPanel, type PanelCommand } from "./WorkspaceSupport";
export type { NavSection } from "./Navigation";

export type View = {
  id: string;
  /** Tab title for a route of this view, e.g. the entity's code. */
  title: (params: Record<string, string>) => string;
  icon?: ReactNode;
  render: (params: Record<string, string>) => ReactNode;
  /** Navigation within one document can change without opening another tab. */
  documentKey?: (params: Record<string, string>) => string;
};
export type MenuItem = { label: string; shortcut?: string; onSelect: () => void; disabled?: boolean };
export type Menu = { label: string; items: MenuItem[] };
export type ShellCommand = { id: string; label: string; group?: string; shortcut?: string; run: () => void };
export type Session = {
  tenant: string; principal: string; detail?: string;
  /** The member's display name (ADR-0028), shown over the principal when set. */
  name?: string;
  options: { id: string; label: string }[]; current: string; onSwitch: (id: string) => void;
  /** Opens the member's own account page, when the host has one. */
  onAccount?: () => void;
};

/** A platform application in the Applications portal (ADR-0018 §3.2): a stable id, a category and a home route. */
export type PlatformApplication = { id: string; title: string; icon?: ReactNode; description?: string; category: string; home: Route };
/** The portal: every application this member may open, its categories in display order, and the one in front. */
export type Applications = { apps: PlatformApplication[]; categories: { id: string; label: string }[]; current?: string; onSelect: (id: string) => void };
/** The constant left rail (ADR-0018 §3.1): where Home, the portal, notifications and the assistant live. */
export type Rail = { home: Route; applications: Route; notifications?: { route: Route; unread?: number }; assist?: Route };
/** A route the member opened, remembered by the shell for the Recent list and Home. */
export type RecentEntry = { route: Route; title: string; app?: string; at: number };

/** Inspect keeps the current task; tab, beside and popout open working documents. */
type OpenOptions = { window?: "tab" | "beside" | "popout" | "inspect"; ifCurrent?: Route };
type Unsaved = {
  register: (panel: string, owner: symbol, discard?: () => void, busy?: boolean) => void;
  ask: (panels: string[], run: () => void, owner?: symbol) => void;
};
type WorkspaceApi = { open: (route: Route, options?: OpenOptions) => void; close: (route: Route) => void; closeAll: () => void; notify: typeof toast; unsaved?: Unsaved;
  /** Names a panel's tab after what it shows ("Object type · Visit") once the view knows it; the view's static title stays the category. */
  retitle?: (panel: string, title: string) => void;
  transfer?: (from: string, route: Route, input: unknown, result: (value: unknown) => void) => void;
  /** Opens the search and command palette (⌘K). */
  palette: () => void;
  /** Routes opened recently in this shell, newest first (per `storageKey`). */
  recent: RecentEntry[];
  /** Platform application ids the member starred, and the switch. */
  favorites: string[]; toggleFavorite: (app: string) => void;
  /** The portal and rail this shell was given, for views that list applications (Home). */
  applications?: Applications; rail?: Rail; };
const PanelContext = createContext<string | undefined>(undefined);
const ViewCallContext = createContext<{ input?: unknown; expired?: boolean; returnValue?: (value: unknown) => void } | undefined>(undefined);
export const useViewCall = () => useContext(ViewCallContext);

export const WorkspaceContext = createContext<WorkspaceApi | null>(null);

/** Opens routes as tabs, beside the current group or in a separate window. */
export function useWorkspace(): WorkspaceApi {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  if (!api) throw new Error("useWorkspace outside <Workspace>");
  return { ...api, transfer: api.transfer && panel ? (_from, route, input, result) => api.transfer!(panel, route, input, result) : undefined };
}

/** The tab of the current view shows "<category> · <name>" once the name is known (UX-03);
 * several objects, modules or projects open at once stay distinguishable. */
export function useViewTitle(name?: string) {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  useEffect(() => { if (api?.retitle && panel && name) api.retitle(panel, name); }, [api, panel, name]);
}

/** Editors own their draft; the shell owns close/reload confirmation. */
export function useUnsavedChanges(dirty: boolean, discard: () => void, busy = false) {
  const inputs=useInputDrafts();dirty ||=!!inputs?.dirty;busy ||=!!inputs?.busy;
  const unsaved = useContext(WorkspaceContext)?.unsaved;
  const panel = useContext(PanelContext);
  const owner = useRef(Symbol());
  const reset = useRef(discard);
  reset.current = ()=>{inputs?.clear();discard();};
  useLayoutEffect(() => {
    if (!panel || !unsaved) return; // local reference examples have no draft host
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
    /** Cancel one local editor without discarding sibling documents in its tab. */
    confirmOwnDiscard: (run: () => void) => panel && unsaved ? unsaved.ask([panel], run, owner.current) : run(),
  };
}

export const notify = toast;

/** Keeps a view's failure inside its tab: the rest of the workspace goes on, and the tab can be closed or tried again. */
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

/**
 * The platform shell (ADR-0018): a constant rail (Home, search, notifications,
 * applications, recent, favorites, assistant, account), a header
 * with the current application and its menus, a context pane with that
 * application's navigation, and a docking workspace whose tabs are routes
 * (one per entity) with a command palette (⌘K).
 * Tabs are grouped by application (ADR-0018 D1): each `layoutScope` has its own
 * remembered layout, swapped in when the scope changes; the recent/favorites memory is per `storageKey`; the active tab is in the URL.
 */
export function Workspace({ product, productIcon, storageKey, layoutScope, scopeOf, resolveRoute, views, nav, navigation, home, menus = [], commands = [], session, status, applications: appEntries, rail, onActiveRoute, onEmpty, onLanguage, search }: {
  product: string; productIcon?: ReactNode; storageKey: string; views: View[]; nav: NavSection[]; home: Route;
  navigation?: ReactNode;
  /** The application whose tabs are shown; its layout is remembered apart from the others'. */
  layoutScope?: string;
  /** The scope a route belongs to (undefined for the shell's own views, which open in any scope). */
  scopeOf?: (route: Route) => string | undefined;
  /** Decode incoming hashes and saved tabs before mounting a view. Apps produce canonical routes. */
  resolveRoute?: (route: Route, layout?: string) => Route | Promise<Route>;
  menus?: Menu[]; commands?: ShellCommand[]; session?: Session; status?: ReactNode;
  applications?: Applications; rail?: Rail; onActiveRoute?: (route: Route) => void;
  /** Reports an empty workspace without passing undefined to existing route listeners. */
  onEmpty?: () => void;
  /** Records matching what is typed in the palette (⌘K), opened on choice: the palette searches data, not only commands. */
  search?: (text: string) => Promise<{ id: string; label: string; detail?: string; open: () => void }[]>;
  /** Keeps a chosen language beyond this browser, e.g. as the member's preference; the page reloads in it after. */
  onLanguage?: (id: string) => unknown;
}) {
  const appearance=useTheme();
  const dock = useRef<DockviewApi>(null);
  const drafts = useRef(new Map<string, Map<symbol, { discard: () => void; busy: boolean }>>());
  const transfers = useRef(new ViewTransfers());
  const [pending, setPending] = useState<{ panels: string[]; run: () => void; owner?: symbol }>();
  const unsaved = useMemo<Unsaved>(() => ({
    register: (panel, owner, discard, busy = false) => {
      if ((discard || busy) && dock.current?.getPanel(panel)) {
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
      const dirty = panels.filter((id) => dock.current?.getPanel(id) && owners(id).length > 0);
      if (dirty.length) setPending({ panels: dirty, run, owner: selected });
      else run();
    },
  }), []);
  const byId = useMemo(() => new Map(views.map((v) => [v.id, v])), [views]);
  const closePanels = useCallback((ids: string[]) => unsaved.ask(ids, () => { for (const id of ids) dock.current?.getPanel(id)?.api.close(); }), [unsaved]);
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
  const layoutRevision = useRef(0), incomingRevision = useRef(0);
  const queuedOpen = useRef<{ route: Route; options: OpenOptions } | undefined>(undefined);
  const leaveViews = useCallback((run: () => void) => unsaved.ask((dock.current?.panels ?? []).map((panel) => panel.id), () => {
    ++incomingRevision.current; queuedOpen.current = undefined; run();
  }), [unsaved]);
  const applications = useMemo<Applications | undefined>(() => appEntries && ({ ...appEntries, onSelect: (id) => {
    if (id === appEntries.current || !appEntries.apps.some((app) => app.id === id)) return;
    leaveViews(() => appEntries.onSelect(id));
  } }), [appEntries, leaveViews]);
  const layoutKey = (id: string | undefined) => `${storageKey}:${id ?? "shell"}`;
  const saveLayout = useCallback((id: string | undefined) => {
    try {
      const api = dock.current; if (!api) return;
      const layout = routeScope.current ? rewriteLayout(api.toJSON(), (route) => routeScope.current!(route) === id ? route : undefined, keyOf) : api.toJSON();
      localStorage.setItem(layoutKey(id), JSON.stringify(layout));
    } catch { /* storage unavailable */ }
  }, [storageKey, keyOf]);
  const loadLayout = useCallback(async (id: string | undefined) => {
    const api = dock.current, revision = ++layoutRevision.current;
    if (!api) return false;
    try {
      const saved = localStorage.getItem(layoutKey(id));
      const restored = saved && await resolveLayout(JSON.parse(saved) as SerializedDockview,
        (route) => routeReader.current?.(route, id) ?? route, (view) => viewRegistry.current.has(view), keyOf);
      const layout = restored && routeScope.current
        ? rewriteLayout(restored, route => routeScope.current!(route) === id ? route : undefined, keyOf)
        : restored;
      if (revision !== layoutRevision.current || dock.current !== api) return false;
      if (layout) api.fromJSON(layout); else api.clear();
    } catch {
      if (revision !== layoutRevision.current || dock.current !== api) return false;
      api.clear(); // malformed layouts are not executable state
    }
    for (const panel of [...api.panels]) {
      const route = (panel.params as { route?: Route } | undefined)?.route;
      const view = viewRegistry.current.get(route?.view ?? "");
      if (!view) panel.api.close(); else panel.setTitle(view.title(route?.params ?? {}));
    }
    return true;
  }, [storageKey]);
  const popping = useRef(new Set<string>());
  const popout = useCallback((id: string) => {
    const api = dock.current, panel = api?.getPanel(id);
    if (!api || !panel || panel.api.location.type === "popout" || popping.current.has(id)) return;
    popping.current.add(id);
    // Dockview rehomes the same panel and returns it when the child closes.
    // Moving it is not a destructive close, and must not discard its draft.
    void api.addPopoutGroup(panel, { onDidOpen: ({ window: child }) => {
      child.addEventListener("load", () => {
        const root = document.documentElement, target = child.document.documentElement;
        const mirror = () => {
          target.lang = root.lang; target.dir = root.dir; target.className = root.className;
          for (const key of ["theme", "skin"]) {
            if (root.dataset[key]) target.dataset[key] = root.dataset[key]; else delete target.dataset[key];
          }
        };
        mirror();
        const observer = new MutationObserver(mirror);
        observer.observe(root, { attributes: true, attributeFilter: ["lang", "dir", "class", "data-theme", "data-skin"] });
        child.addEventListener("unload", () => observer.disconnect(), { once: true });
      }, { once: true });
    } }).then((opened) => { if (!opened) toast.error(t("Could not open a new window. Allow pop-ups and try again.")); })
      .catch(() => toast.error(t("Could not open a new window. Allow pop-ups and try again.")))
      .finally(() => popping.current.delete(id));
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
  const [found, setFound] = useState<{ id: string; label: string; detail?: string; open: () => void }[]>([]);
  useEffect(() => { // records for what is typed, a moment after typing stops
    if (!search || typed.trim().length < 2) { setFound([]); return; }
    let live = true;
    const wait = setTimeout(() => { void search(typed).then((hits) => { if (live) setFound(hits); }).catch(() => undefined); }, 200);
    return () => { live = false; clearTimeout(wait); };
  }, [search, typed]);
  // What the member comes back to (ADR-0018 §4.2): recent routes and starred applications, per shell.
  const navKey = `${storageKey}:nav`;
  const [memory, setMemory] = useState<{ recent: RecentEntry[]; favorites: string[] }>(() => {
    try { const saved = localStorage.getItem(navKey); if (saved) return { recent: [], favorites: [], ...JSON.parse(saved) }; } catch { /* storage unavailable */ }
    return { recent: [], favorites: [] };
  });
  const remember = useCallback((change: (m: { recent: RecentEntry[]; favorites: string[] }) => { recent: RecentEntry[]; favorites: string[] }) => setMemory((m) => {
    const next = change(m);
    try { localStorage.setItem(navKey, JSON.stringify(next)); } catch { /* storage unavailable */ }
    return next;
  }), [navKey]);
  const toggleFavorite = useCallback((app: string) => remember((m) => ({ ...m, favorites: m.favorites.includes(app) ? m.favorites.filter((id) => id !== app) : [...m.favorites, app] })), [remember]);
  const currentApp = useRef(applications?.current); currentApp.current = applications?.current;
  const visited = useCallback((route: Route, title: string) => {
    if (rail && [routeKey(rail.home), routeKey(rail.applications)].includes(routeKey(route))) return; // the rail's own places are not "recent"
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
  // Keep desktop splits intact while showing one full-width group on a phone.
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

  const canBeside = (id?: string) => {
    const api = dock.current, panel = id && api?.getPanel(id);
    return !!api && !!panel && !compact && (panel.group.panels.length > 1 || api.groups.some((group) => group !== panel.group && group.api.location.type === "grid"));
  };
  const placeBeside = useCallback((id: string, reference: string) => {
    const api = dock.current, panel = api?.getPanel(id), source = api?.getPanel(reference)?.group;
    if (!api || !panel || !source || compact) return;
    if (panel.group !== source && panel.group.api.location.type === "grid") return; // already beside the reference
    const other = api.groups.find((group) => group !== source && group.api.location.type === "grid");
    // Moving the only tab to a new group just collapses its old group. There
    // must be a remaining view, or a real existing group, to be beside.
    if (!other && (source.api.location.type !== "grid" || source.panels.length < 2)) return;
    panel.api.moveTo({ group: other ?? api.addGroup({ referenceGroup: source, direction: "right" }) });
  }, [compact]);
  const show = useCallback((route: Route, options: OpenOptions = {}) => {
    const api = dock.current, view = byId.get(route.view);
    if (!api || !view) return;
    const reference = api.activePanel?.id;
    const key = keyOf(route), title = view.title(route.params ?? {});
    let panel = api.getPanel(key);
    if (panel) {
      if (view.documentKey) panel.api.updateParameters({ route });
      if (options.window === "beside" && reference) placeBeside(panel.id, reference);
    } else if (options.window === "beside" && !compact && api.activePanel?.group.api.location.type === "grid") {
      const current = api.activePanel.group;
      const other = api.groups.find((group) => group !== current && group.api.location.type === "grid")
        ?? api.addGroup({ referenceGroup: current, direction: "right" });
      panel = api.addPanel({ id: key, component: "view", title, params: { route }, position: { referenceGroup: other } });
    }
    panel ??= api.addPanel({ id: key, component: "view", title, params: { route } });
    panel.api.setActive();
    syncLayout.current();
    if (options.window === "popout") popout(panel.id);
  }, [byId, compact, popout, placeBeside, keyOf]);
  const openResolved = useCallback((route: Route, options: OpenOptions = {}) => {
    const api = dock.current;
    if (!api || !viewRegistry.current.has(route.view)) return;
    // A completed save may replace its own new-record tab, but must not
    // take focus back after the member has already chosen another document.
    if (options.ifCurrent && api.activePanel?.id !== keyOf(options.ifCurrent)) return;
    if (options.window === "inspect") {
      setInspection(previous => ({ route, focus: previous?.focus ?? (document.activeElement instanceof HTMLElement ? document.activeElement : null) }));
      return;
    }
    setInspection(undefined);
    const revision = ++incomingRevision.current;
    if (switching.current) { queuedOpen.current = { route, options }; return; }
    const target = routeScope.current?.(route);
    if (target !== undefined && target !== scope.current) {
      const previous = (api.activePanel?.params as { route?: Route } | undefined)?.route;
      if (previous) history.replaceState(null, "", routeToHash(previous)); // a cancelled hash/back navigation must not leave a false URL
      unsaved.ask(api.panels.map((panel) => panel.id), () => { if (revision === incomingRevision.current) show(route, options); });
    } else show(route, options);
  }, [show, unsaved, keyOf]);
  const open = useCallback((route: Route, options: OpenOptions = {}) => {
    const resolve = routeReader.current;
    if (!resolve) { openResolved(route, options); return; }
    if (options.ifCurrent && dock.current?.activePanel?.id !== keyOf(options.ifCurrent)) return;
    const revision = ++incomingRevision.current;
    const take = (resolved: Route) => { if (revision === incomingRevision.current) openResolved(resolved, options); };
    try {
      const resolved = resolve(route);
      if (resolved instanceof Promise) void resolved.then(take).catch(() => {
        if (revision === incomingRevision.current) toast.error(t("Could not open this view."));
      });
      else take(resolved);
    } catch {
      if (revision === incomingRevision.current) toast.error(t("Could not open this view."));
    }
  }, [openResolved, keyOf]);
  const readLink = useCallback((route: Route) => {
    const revision = ++incomingRevision.current;
    void Promise.resolve().then(() => routeReader.current?.(route) ?? route).then((resolved) => {
      if (revision !== incomingRevision.current) return;
      if (viewRegistry.current.has(resolved.view)) openResolved(resolved);
      else if (!dock.current?.panels.length) open(applicationEntries.current?.apps.find((app) => app.id === scope.current)?.home ?? home);
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
    // A release can arrive after the original link opened. Follow its new
    // route without closing the original tab or taking focus from a later link.
    void Promise.resolve(resolveRoute(linked)).then(resolved => {
      if (revision === incomingRevision.current && routeToHash(linked) === location.hash && routeKey(resolved) !== routeKey(linked)) openResolved(resolved);
    }).catch(() => { /* An explicit navigation reports resolution failures. */ });
  }, [resolveRoute, openResolved]);
  const beside = useCallback((id: string) => {
    placeBeside(id, id);
    dock.current?.getPanel(id)?.api.setActive();
  }, [placeBeside]);
  const closeAll = useCallback(() => closePanels((dock.current?.panels ?? []).map((p) => p.id)), [closePanels]);
  const closeOthers = useCallback((id: string) => closePanels((dock.current?.panels ?? []).filter((p) => p.id !== id).map((p) => p.id)), [closePanels]);
  const closeRight = useCallback((id: string) => {
    const group = dock.current?.getPanel(id)?.group; if (!group) return;
    const index = group.panels.findIndex((p) => p.id === id);
    closePanels(group.panels.slice(index + 1).map((p) => p.id));
  }, [closePanels]);
  const syncLayout = useRef<() => void>(() => {});
  // Swaps are restoration, not navigation: no intermediate tab may pick an
  // application. Old/new routes and shell tabs retain their separate ownership.
  useEffect(() => {
    const api = dock.current;
    if (!api || layoutScope === scope.current) return;
    switching.current = true;
    ++incomingRevision.current;
    const sourceRoute = (api.activePanel?.params as { route?: Route } | undefined)?.route;
    const carried = api.panels.map((panel) => (panel.params as { route?: Route } | undefined)?.route)
      .filter((route): route is Route => !!route && (routeScope.current?.(route) === layoutScope || routeScope.current?.(route) === undefined));
    saveLayout(scope.current);
    if (scope.current !== undefined) saveLayout(undefined);
    scope.current = layoutScope;
    api.clear();
    void loadLayout(layoutScope).then((loaded) => {
      if (!loaded) return;
      const restored = api.activePanel?.id;
      const targetHome = applicationEntries.current?.apps.find((app) => app.id === layoutScope)?.home ?? home;
      const incoming = sourceRoute && routeScope.current?.(sourceRoute) === layoutScope ? sourceRoute : undefined;
      // A shared Home/portal tab is not the target application's home. Restored
      // app tabs keep their active entity; a new app still gets its own entry.
      if (!api.panels.length && !incoming) show(targetHome);
      const fallback = api.activePanel?.id;
      for (const route of carried) show(route);
      api.getPanel(incoming ? keyOf(incoming) : restored ?? fallback ?? "")?.api.setActive();
      const queued = queuedOpen.current; queuedOpen.current = undefined;
      switching.current = false;
      syncLayout.current();
      if (queued) open(queued.route, queued.options);
    });
  }, [layoutScope, saveLayout, loadLayout, show, open, home]);
  useEffect(() => () => { ++layoutRevision.current; ++incomingRevision.current; }, []);

  const workspace = useMemo<WorkspaceApi>(() => ({
    open, notify: toast, close: (route) => closePanel(keyOf(route)), closeAll, unsaved,
    transfer: (from, route, input, result) => {
      if (!dock.current?.getPanel(from)) return;
      const id = transfers.current.start(from, input, result), called = { ...route, params: { ...route.params, call: id } };
      transfers.current.bind(id, keyOf(called)); open(called);
    },
    palette: () => setPaletteOpen(true),
    retitle: (id, name) => {
      const panel = dock.current?.getPanel(id);
      const route = (panel?.params as { route?: Route } | undefined)?.route;
      const category = route ? byId.get(route.view)?.title(route.params ?? {}) : undefined;
      const title = category && category !== name ? `${category} · ${name}` : name;
      if (panel && panel.api.title !== title) panel.setTitle(title);
    },
    recent: memory.recent, favorites: memory.favorites, toggleFavorite, applications, rail,
  }), [open, closePanel, closeAll, unsaved, memory, toggleFavorite, applications, rail, byId]);

  /** One list of tab commands: the tab's own menu and the Window menu read the same source (ADR-0018 D1). */
  const panelCommands = useCallback((id?: string): PanelCommand[] => [
    { label: t("Close"), shortcut: "⌘W", run: () => closePanel(id!), disabled: !id },
    { label: t("Close others"), run: () => closeOthers(id!), disabled: !id || openTabs.length < 2 },
    { label: t("Close to the right"), run: () => closeRight(id!), disabled: !id },
    { label: t("Close all"), run: closeAll, disabled: !openTabs.length },
    { label: t("Open beside"), run: () => beside(id!), disabled: !id || !canBeside(id) },
    { label: t("Move to new window"), run: () => popout(id!), disabled: !id },
  ], [closePanel, closeOthers, closeRight, closeAll, beside, popout, openTabs.length, compact]);
  const tab = useCallback((props: IDockviewPanelHeaderProps) => <TabHeader tab={props} commands={panelCommands(props.api.id)} onClose={() => closePanel(props.api.id)} />, [panelCommands, closePanel]);
  // A closed tab keeps the draft registry; keeping editing returns to the tab that is still there.
  const keepEditing = useCallback(() => { const id = pending?.panels[0]; setPending(undefined); if (id) dock.current?.getPanel(id)?.api.setActive(); }, [pending]);
  const discardPending = useCallback(() => {
    if (!pending) return;
    const own = (id: string) => [...(drafts.current.get(id)?.entries() ?? [])].filter(([key]) => pending.owner === undefined || key === pending.owner);
    if (pending.panels.some((id) => own(id).some(([, entry]) => entry.busy))) { toast.error(t("An operation is in progress. Wait for its result before leaving.")); return; }
    for (const id of pending.panels) for (const [, entry] of own(id)) entry.discard();
    const run = pending.run; setPending(undefined); run();
  }, [pending]);

  const components = useMemo(() => ({
    view: ({ params, api }: IDockviewPanelProps<{ route: Route }>) => {
      const view = byId.get(params.route.view);
      const ticket = params.route.params?.call;
      const call = ticket ? transfers.current.read(ticket, api.id) : undefined;
      const returned = (value: unknown) => {
        if (!ticket) return;
        const origin = transfers.current.finish(ticket, api.id, value);
        if (origin && dock.current?.getPanel(origin)) { dock.current.getPanel(origin)!.api.setActive(); closePanel(api.id); }
      };
      const [visible, setVisible] = useState(api.isVisible);
      useEffect(() => {
        const changed = api.onDidVisibilityChange((event) => setVisible(event.isVisible));
        return () => changed.dispose();
      }, [api]);
      return <div style={{ display: visible ? undefined : "none" }} role="region" aria-label={view?.title(params.route.params ?? {}) ?? t("Workspace view")} className="h-full overflow-auto bg-background p-4">
        <ViewVisibilityContext.Provider value={visible}><PanelContext.Provider value={api.id}><ViewCallContext.Provider value={ticket ? { input: call?.input, expired: !call, returnValue: call ? returned : undefined } : undefined}><ViewBoundary onClose={() => closePanel(api.id)}>
          {view ? view.render(params.route.params ?? {}) : <p className="text-sm text-muted">{t("This view no longer exists.")}</p>}
        </ViewBoundary></ViewCallContext.Provider></PanelContext.Provider></ViewVisibilityContext.Provider>
      </div>;
    },
  }), [byId, closePanel]);

  const onReady = useCallback(({ api }: { api: DockviewApi }) => {
    dock.current = api;
    api.onDidRemovePanel((panel) => {
      transfers.current.remove(panel.id);
      drafts.current.delete(panel.id);
      setPending((request) => {
        if (!request) return request;
        const panels = request.panels.filter((id) => id !== panel.id);
        return panels.length ? { ...request, panels } : undefined;
      });
    });
    // Capture the incoming link before any restored tab may rewrite it.
    const linked = routeFromHash(location.hash), linkRevision = incomingRevision.current;
    const sync = () => {
      setOpenTabs(api.panels.map((panel) => ({ key: panel.id, title: panel.title ?? panel.id })));
      setActive(api.activePanel?.id);
      const route = (api.activePanel?.params as { route?: Route } | undefined)?.route;
      setActiveRoute(route && routeKey(route));
      if (!switching.current) {
        history.replaceState(null, "", route ? routeToHash(route) : "#/");
        if (route) followed.current?.(route); else emptied.current?.();
        if (route) visit.current(route, api.activePanel?.title ?? route.view);

        saveLayout(scope.current);
        if (scope.current !== undefined) saveLayout(undefined);
      }
    };
    syncLayout.current = sync;
    api.onDidLayoutChange(sync);
    api.onDidActivePanelChange(sync);
    switching.current = true;
    void loadLayout(scope.current).then((loaded) => {
      if (!loaded) return;
      const queued = queuedOpen.current; queuedOpen.current = undefined;
      if (!api.panels.length && !linked && !queued) show(applicationEntries.current?.apps.find((app) => app.id === scope.current)?.home ?? home);
      switching.current = false;
      if (queued) open(queued.route, queued.options);
      else if (linked && linkRevision === incomingRevision.current) readLink(linked);
      sync();
    });
  }, [home, open, show, readLink, loadLayout, saveLayout]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape" && !e.defaultPrevented && mobileNavOpen) { setMobileNavOpen(false); navigationTrigger.current?.focus(); }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setPaletteOpen((v) => !v); }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "w" && dock.current?.activePanel) { e.preventDefault(); closePanel(dock.current.activePanel.id); }
    };
    const onHash = () => { const route = routeFromHash(location.hash); if (route) readLink(route); };
    addEventListener("keydown", onKey);
    addEventListener("hashchange", onHash);
    return () => { removeEventListener("keydown", onKey); removeEventListener("hashchange", onHash); };
  }, [readLink, closePanel, mobileNavOpen]);

  const builtInMenus: Menu[] = [
    { label: t("View"), items: [
      { label: t("Command palette"), shortcut: "⌘K", onSelect: () => setPaletteOpen(true) },
      { label: navigationVisible ? t("Hide navigation") : t("Show navigation"), onSelect: toggleNavigation },
      { label: t("Reset layout"), onSelect: () => leaveViews(() => { dock.current?.clear(); open(applicationEntries.current?.apps.find((app) => app.id === scope.current)?.home ?? home); }) },
    ] },
    { label: t("Appearance"), items: [
      // A checkmark is the "shortcut" slot: the menu stays one shape.
      ...([["system", t("Follow system")], ["light", t("Light")], ["dark", t("Dark")]] as const).map(([scheme, label]) => ({ label, shortcut: appearance.scheme === scheme ? "✓" : undefined, onSelect: () => appearance.setScheme(scheme) })),
      ...([["default", t("Standard look")], ["industrial", t("Industrial look")]] as const).map(([skin, label]) => ({ label, shortcut: appearance.skin === skin ? "✓" : undefined, onSelect: () => appearance.setSkin(skin) })),
    ] },
    { label: t("Window"), items: [
      ...panelCommands(active).map(({ label, disabled, shortcut, run }) => ({ label, disabled, shortcut, onSelect: run })),
      ...openTabs.map((entry) => ({ label: entry.title, onSelect: () => dock.current?.getPanel(entry.key)?.api.setActive() })),
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
      {item.icon}{item.badge ? <span className="absolute right-0.5 top-0.5 min-w-4 rounded-full bg-primary px-1 text-center text-[10px] font-semibold leading-4 text-primary-foreground">{item.badge > 99 ? "99+" : item.badge}</span> : null}
    </button>
  );
  const sessionMenu = session && <SessionMenu compact={!!rail && !compact} session={{ ...session, onAccount: session.onAccount ? () => leaveViews(() => session.onAccount?.()) : undefined, onSwitch: (id) => leaveViews(() => session.onSwitch(id)) }}
    onLanguageSelect={(id) => leaveViews(() => {
      void Promise.resolve(onLanguage?.(id)).catch(() => undefined).finally(() => setLanguage(id));
    })} />;

  return (
    <WorkspaceContext.Provider value={workspace}>
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
          {compact && mobileNavOpen && <button type="button" aria-label={t("Close navigation")} className="fixed inset-x-0 bottom-0 top-9 z-20 bg-black/25" onClick={() => setMobileNavOpen(false)} />}
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
          <div className="platform-dock min-h-0 min-w-0 overflow-hidden">
            <DockviewReact defaultRenderer="always" defaultTabComponent={tab} watermarkComponent={watermark} components={components} onReady={onReady} theme={appearance.theme==="dark"?themeDark:themeLight} />
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
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} typed={typed} onTypedChange={setTyped} found={found}
        applications={applications} nav={nav} tabs={openTabs} commands={commands} onOpen={open}
        onFocusTab={(key) => dock.current?.getPanel(key)?.api.setActive()} />
      <UnsavedChanges panels={pending?.panels} title={(id) => dock.current?.getPanel(id)?.title ?? id} onKeep={keepEditing} onDiscard={discardPending} />
      <Toaster position="bottom-right" toastOptions={{ className: "!rounded-md !border-border !bg-surface !text-foreground !text-sm" }} />
    </WorkspaceContext.Provider>
  );
}
