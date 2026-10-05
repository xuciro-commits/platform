import {useTheme} from "../theme";
import { Command } from "cmdk";
import { DockviewDefaultTab, DockviewReact, themeLight,themeDark, type DockviewApi, type IDockviewPanelHeaderProps, type IDockviewPanelProps } from "dockview-react";
import { ChevronDown, LayoutGrid, PanelLeft, Search } from "lucide-react";
import { DropdownMenu, Menubar } from "radix-ui";
import { Component, createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Toaster, toast } from "sonner";
import { cn } from "../lib/cn";
import { routeFromHash, routeKey, routeToHash, type Route } from "./route";
import { language, languages, setLanguage, t } from "../i18n";
import { Dialog } from "../primitives/dialog";
import { Button } from "../primitives/button";
import { ViewVisibilityContext } from "./ViewVisibility";
import { ViewTransfers } from "./ViewTransfers";

export type View = {
  id: string;
  /** Tab title for a route of this view, e.g. the entity's code. */
  title: (params: Record<string, string>) => string;
  icon?: ReactNode;
  render: (params: Record<string, string>) => ReactNode;
};
export type NavSection = { label: string; items: { label: string; icon?: ReactNode; route: Route; badge?: ReactNode }[] };
export type MenuItem = { label: string; shortcut?: string; onSelect: () => void; disabled?: boolean };
export type Menu = { label: string; items: MenuItem[] };
export type ShellCommand = { id: string; label: string; group?: string; shortcut?: string; run: () => void };
export type Session = {
  tenant: string; principal: string; detail?: string;
  options: { id: string; label: string }[]; current: string; onSwitch: (id: string) => void;
};

/** The apps a member may open (ADR-0018): the launcher in the menu bar and the palette switch between them. */
export type Launcher = { apps: { id: string; title: string; icon?: ReactNode }[]; current?: string; label?: string; onSelect: (id: string) => void };

type OpenOptions = { window?: "tab" | "float" | "popout" };
type Unsaved = {
  register: (panel: string, owner: symbol, discard?: () => void) => void;
  ask: (panels: string[], run: () => void) => void;
};
type WorkspaceApi = { open: (route: Route, options?: OpenOptions) => void; close: (route: Route) => void; notify: typeof toast; unsaved?: Unsaved;
  transfer?: (from: string, route: Route, input: unknown, result: (value: unknown) => void) => void };
const PanelContext = createContext<string | undefined>(undefined);
const ViewCallContext = createContext<{ input?: unknown; expired?: boolean; returnValue?: (value: unknown) => void } | undefined>(undefined);
export const useViewCall = () => useContext(ViewCallContext);

export const WorkspaceContext = createContext<WorkspaceApi | null>(null);

/** Opens routes as tabs, floats or separate windows, from any view. */
export function useWorkspace(): WorkspaceApi {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  if (!api) throw new Error("useWorkspace outside <Workspace>");
  return { ...api, transfer: api.transfer && panel ? (_from, route, input, result) => api.transfer!(panel, route, input, result) : undefined };
}

/** Editors own their draft; the shell owns close/reload confirmation. */
export function useUnsavedChanges(dirty: boolean, discard: () => void) {
  const { unsaved } = useWorkspace();
  const panel = useContext(PanelContext);
  const owner = useRef(Symbol());
  const reset = useRef(discard);
  reset.current = discard;
  useLayoutEffect(() => {
    if (!panel || !unsaved) return; // local reference examples have no draft host
    unsaved.register(panel, owner.current, dirty ? () => {
      unsaved.register(panel, owner.current);
      reset.current();
    } : undefined);
    return () => unsaved.register(panel, owner.current);
  }, [dirty, panel, unsaved]);
  return {
    markSaved: () => { if (panel) unsaved?.register(panel, owner.current); },
    discardChanges: () => { if (panel) unsaved?.register(panel, owner.current); reset.current(); },
    confirmDiscard: (run: () => void) => panel && unsaved ? unsaved.ask([panel], run) : run(),
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

/** Narrow workspaces use full-width tabs, while retaining each record's context. */
function dockFloating(api: DockviewApi) {
  const root = api.groups.find((group) => group.api.location.type === "grid");
  if (!root) return;
  const active = api.activePanel;
  for (const group of [...api.groups].filter((group) => group.api.location.type === "floating")) {
    for (const panel of [...group.panels]) {
      panel.api.setRenderer("always");
      panel.api.moveTo({ group: root });
    }
  }
  active?.api.setActive();
}

/**
 * The platform shell: menu bar, session, navigation, a docking workspace whose
 * tabs are routes (one per entity), a command palette (⌘K) and notifications.
 * The layout survives restarts (per `storageKey`); the active tab is in the URL.
 */
export function Workspace({ product, storageKey, views, nav, home, menus = [], commands = [], session, status, launcher, entryPoints, onActiveRoute, onLanguage, search }: {
  product: string; storageKey: string; views: View[]; nav: NavSection[]; home: Route;
  menus?: Menu[]; commands?: ShellCommand[]; session?: Session; status?: ReactNode;
  launcher?: Launcher; entryPoints?: Launcher; onActiveRoute?: (route: Route) => void;
  /** Records matching what is typed in the palette (⌘K), opened on choice: the palette searches data, not only commands. */
  search?: (text: string) => Promise<{ id: string; label: string; detail?: string; open: () => void }[]>;
  /** Keeps a chosen language beyond this browser, e.g. as the member's preference; the page reloads in it after. */
  onLanguage?: (id: string) => unknown;
}) {
  const appearance=useTheme();
  const dock = useRef<DockviewApi>(null);
  const drafts = useRef(new Map<string, Map<symbol, () => void>>());
  const transfers = useRef(new ViewTransfers());
  const [pending, setPending] = useState<{ panels: string[]; run: () => void }>();
  const unsaved = useMemo<Unsaved>(() => ({
    register: (panel, owner, discard) => {
      if (discard && dock.current?.getPanel(panel)) {
        const entries = drafts.current.get(panel) ?? new Map<symbol, () => void>();
        entries.set(owner, discard); drafts.current.set(panel, entries);
      } else {
        const entries = drafts.current.get(panel);
        entries?.delete(owner);
        if (!entries?.size) drafts.current.delete(panel);
      }
    },
    ask: (panels, run) => {
      const dirty = panels.filter((id) => dock.current?.getPanel(id) && drafts.current.has(id));
      if (dirty.length) setPending({ panels: dirty, run });
      else run();
    },
  }), []);
  const closePanel = useCallback((id: string) => unsaved.ask([id], () => dock.current?.getPanel(id)?.api.close()), [unsaved]);
  const popout = useCallback((id: string) => unsaved.ask([id], () => {
    const panel = dock.current?.getPanel(id);
    if (panel) void dock.current?.addPopoutGroup(panel, { onDidOpen: ({ window: child }) => {
      child.addEventListener("beforeunload", (event) => {
        if (panel.group.panels.some((p) => drafts.current.has(p.id))) { event.preventDefault(); event.returnValue = ""; }
      });
    } });
  }), [unsaved]);
  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (drafts.current.size) { event.preventDefault(); event.returnValue = ""; }
    };
    addEventListener("beforeunload", beforeUnload);
    return () => removeEventListener("beforeunload", beforeUnload);
  }, []);
  const [active, setActive] = useState<string>();
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
  const [navOpen, setNavOpen] = useState(nav.length > 0);
  const [compact, setCompact] = useState(() => typeof matchMedia !== "undefined" && matchMedia("(max-width: 639px)").matches);
  const [mobileNavOpen, setMobileNavOpen] = useState(false);
  useEffect(() => {
    const media = matchMedia("(max-width: 639px)");
    const changed = () => { setCompact(media.matches); setMobileNavOpen(false); };
    media.addEventListener("change", changed);
    return () => media.removeEventListener("change", changed);
  }, []);
  const navigationVisible = compact ? mobileNavOpen : navOpen;
  const toggleNavigation = () => compact ? setMobileNavOpen((open) => !open) : setNavOpen((open) => !open);
  const byId = useMemo(() => new Map(views.map((v) => [v.id, v])), [views]);
  useEffect(() => { if (compact && dock.current) dockFloating(dock.current); }, [compact]);

  const followed = useRef(onActiveRoute);
  followed.current = onActiveRoute;

  const open = useCallback((route: Route, options: OpenOptions = {}) => {
    const api = dock.current;
    const view = byId.get(route.view);
    if (!api || !view) return;
    const key = routeKey(route);
    const title = view.title(route.params ?? {});
    const floating = options.window === "float" && !compact ? api.groups.find((g) => g.api.location.type === "floating") : undefined;
    let panel = api.getPanel(key);
    if (panel && floating && panel.group !== floating) {
      panel.api.setRenderer("always");
      panel.api.moveTo({ group: floating });
    }
    if (!panel && options.window === "float" && !compact) {
      // One floating window above the page: what opens next joins it as a tab, so people click back and forth.
      const width = Math.min(820, api.width - 48), height = Math.max(280, api.height - 64);
      panel = floating
        ? api.addPanel({ id: key, component: "view", renderer: "always", title, params: { route }, position: { referenceGroup: floating } })
        : api.addPanel({ id: key, component: "view", renderer: "onlyWhenVisible", title, params: { route }, floating: { width, height, x: api.width - width - 24, y: 32 } });
    }
    panel ??= api.addPanel({ id: key, component: "view", title, params: { route } });
    if (options.window === "popout") popout(panel.id);
    panel.api.setActive();
  }, [byId, compact, popout]);

  const workspace = useMemo<WorkspaceApi>(() => ({
    open, notify: toast, close: (route) => closePanel(routeKey(route)), unsaved,
    transfer: (from, route, input, result) => {
      if (!dock.current?.getPanel(from)) return;
      const id = transfers.current.start(from, input, result), called = { ...route, params: { ...route.params, call: id } };
      transfers.current.bind(id, routeKey(called)); open(called);
    },
  }), [open, closePanel, unsaved]);

  const tab = useCallback((props: IDockviewPanelHeaderProps) =>
    <DockviewDefaultTab {...props} closeActionOverride={() => closePanel(props.api.id)} />, [closePanel]);

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
    // Read the link first: restoring the layout rewrites the URL to its active tab.
    const linked = routeFromHash(location.hash);
    try {
      const saved = localStorage.getItem(storageKey);
      if (saved) api.fromJSON(JSON.parse(saved));
      for (const p of [...api.panels]) { // discard removed views; refresh titles in the reader's language
        const route = (p.params as { route?: Route } | undefined)?.route;
        const view = byId.get(route?.view ?? "");
        if (!view) p.api.close();
        else p.setTitle(view.title(route?.params ?? {}));
      }
    } catch {
      api.clear(); // a stale or corrupt layout falls back to the home view
    }
    if (compact) dockFloating(api);
    const sync = () => {
      setOpenTabs(api.panels.map((p) => ({ key: p.id, title: p.title ?? p.id })));
      setActive(api.activePanel?.id);
      const route = (api.activePanel?.params as { route?: Route } | undefined)?.route;
      if (route) { history.replaceState(null, "", routeToHash(route)); followed.current?.(route); }
      try { localStorage.setItem(storageKey, JSON.stringify(api.toJSON())); } catch { /* storage unavailable */ }
    };
    api.onDidLayoutChange(sync);
    api.onDidActivePanelChange(sync);
    if (linked && byId.has(linked.view)) open(linked);
    else if (api.panels.length === 0) open(home);
    sync();
  }, [byId, home, open, storageKey, compact]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setPaletteOpen((v) => !v); }
    };
    const onHash = () => { const route = routeFromHash(location.hash); if (route) open(route); };
    addEventListener("keydown", onKey);
    addEventListener("hashchange", onHash);
    return () => { removeEventListener("keydown", onKey); removeEventListener("hashchange", onHash); };
  }, [open]);

  const activePanel = () => dock.current?.activePanel;
  const builtInMenus: Menu[] = [
    { label: t("View"), items: [
      { label: t("Command palette"), shortcut: "⌘K", onSelect: () => setPaletteOpen(true) },
      { label: navigationVisible ? t("Hide navigation") : t("Show navigation"), onSelect: toggleNavigation },
      { label: t("Reset layout"), onSelect: () => unsaved.ask([...drafts.current.keys()], () => { dock.current?.clear(); open(home); }) },
    ] },
    { label: t("Window"), items: [
      { label: t("Float tab"), disabled: !active, onSelect: () => { const p = activePanel(); if (p) dock.current?.addFloatingGroup(p); } },
      { label: t("Move tab to new window"), disabled: !active, onSelect: () => { const p = activePanel(); if (p) popout(p.id); } },
      { label: t("Close tab"), disabled: !active, onSelect: () => { const p = activePanel(); if (p) closePanel(p.id); } },
      ...openTabs.map((t) => ({ label: t.title, onSelect: () => dock.current?.getPanel(t.key)?.api.setActive() })),
    ] },
  ];

  return (
    <WorkspaceContext.Provider value={workspace}>
      <div className="grid h-dvh w-full min-w-0 max-w-full grid-rows-[36px_1fr] overflow-hidden bg-background text-foreground">
        <header className="flex min-w-0 items-center gap-2 overflow-hidden border-b border-border bg-surface px-2">
          <button type="button" aria-label={t("Toggle navigation")} aria-expanded={navigationVisible} onClick={toggleNavigation}
            className="rounded-sm p-1 text-muted hover:bg-row-hover hover:text-foreground"><PanelLeft className="size-4" /></button>
          {entryPoints && <AppMenu launcher={entryPoints} product={entryPoints.apps.find((entry) => entry.id === entryPoints.current)?.title ?? product} label={t("Workspaces")} />}
          {launcher ? <AppMenu launcher={launcher} product={product} label={launcher.label} /> : !entryPoints && <span className="pr-2 text-sm font-semibold tracking-tight">{product}</span>}
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
          <button type="button" aria-label={t("Search and commands")} onClick={() => setPaletteOpen(true)}
            className="mx-auto flex h-6 w-6 shrink-0 items-center justify-center gap-2 rounded-md border border-border bg-background text-sm text-muted hover:text-foreground lg:w-72 lg:justify-start lg:px-2">
            <Search className="size-3.5" /><span className="max-lg:hidden">{t("Search and commands")}</span><kbd className="ml-auto text-xs max-lg:hidden">⌘K</kbd>
          </button>
          <div className="ml-auto flex items-center gap-2">
            <span className="max-sm:hidden">{status}</span>
            {session && <SessionMenu session={{ ...session, onSwitch: (id) => unsaved.ask([...drafts.current.keys()], () => session.onSwitch(id)) }}
              onLanguageSelect={(id) => unsaved.ask([...drafts.current.keys()], () => {
                void Promise.resolve(onLanguage?.(id)).catch(() => undefined).finally(() => setLanguage(id));
              })} />}
          </div>
        </header>
        <div className={cn("relative grid min-h-0 min-w-0", !compact && navOpen ? "grid-cols-[220px_minmax(0,1fr)]" : "grid-cols-1")}>
          {compact && mobileNavOpen && <button type="button" aria-label={t("Close navigation")} className="fixed inset-0 z-20 bg-black/25" onClick={() => setMobileNavOpen(false)} />}
          {(!compact && navOpen || compact && mobileNavOpen) && (
            <nav aria-label={t("Main")} className={cn("overflow-auto border-r border-border bg-surface p-2",
              compact && "fixed inset-y-9 left-0 z-30 w-[min(19rem,85vw)] shadow-lg")}>
              {nav.map((section) => (
                <div key={section.label} className="mb-3">
                  <div className="px-2 pb-1 text-xs font-medium uppercase tracking-wide text-muted">{section.label}</div>
                  {section.items.map((item) => {
                    const key = routeKey(item.route);
                    return (
                      <button key={key} type="button" onClick={() => { open(item.route); setMobileNavOpen(false); }} aria-current={key === active ? "page" : undefined}
                        className={cn("flex h-7 w-full items-center gap-2 rounded-md px-2 text-sm hover:bg-row-hover [&_svg]:size-3.5",
                          key === active && "bg-row-selected font-medium")}>
                        {item.icon}{item.label}<span className="ml-auto">{item.badge}</span>
                      </button>
                    );
                  })}
                </div>
              ))}
            </nav>
          )}
          <div className="platform-dock min-h-0 min-w-0 overflow-hidden">
            <DockviewReact defaultRenderer="always" defaultTabComponent={tab} components={components} onReady={onReady} theme={appearance.theme==="dark"?themeDark:themeLight} />
          </div>
        </div>
      </div>
      <Command.Dialog open={paletteOpen} onOpenChange={setPaletteOpen} label={t("Command palette")}
        overlayClassName="fixed inset-0 z-40 bg-black/30"
        contentClassName="fixed left-1/2 top-[15%] z-50 w-[min(560px,calc(100vw-32px))] -translate-x-1/2 overflow-hidden rounded-md border border-border bg-surface shadow-2xl">
        <Command.Input value={typed} onValueChange={setTyped} placeholder={t("Search records, go to, open, run…")} className="h-10 w-full border-b border-border bg-transparent px-3 text-base outline-none" />
        <Command.List className="max-h-80 overflow-auto p-1">
          <Command.Empty className="p-3 text-sm text-muted">{t("No results")}</Command.Empty>
          {found.length > 0 && (
            <Command.Group heading={t("Records")} className={paletteGroup}>
              {found.map((hit) => (
                <Command.Item key={hit.id} value={`record ${typed} ${hit.label} ${hit.id}`} className={paletteItem}
                  onSelect={() => { hit.open(); setPaletteOpen(false); }}>{hit.label}{hit.detail && <span className="ml-auto font-mono text-xs text-muted">{hit.detail}</span>}</Command.Item>
              ))}
            </Command.Group>
          )}
          {entryPoints && (
            <Command.Group heading={t("Workspaces")} className={paletteGroup}>
              {entryPoints.apps.map((entry) => <Command.Item key={entry.id} value={`workspace ${entry.title}`} className={paletteItem}
                onSelect={() => { entryPoints.onSelect(entry.id); setPaletteOpen(false); }}>{entry.icon}{entry.title}</Command.Item>)}
            </Command.Group>
          )}
          {launcher && (
            <Command.Group heading={t("Apps")} className={paletteGroup}>
              {launcher.apps.map((a) => (
                <Command.Item key={a.id} value={`app ${a.title}`} className={paletteItem}
                  onSelect={() => { launcher.onSelect(a.id); setPaletteOpen(false); }}>{a.icon}{a.title}</Command.Item>
              ))}
            </Command.Group>
          )}
          {nav.map((section) => (
            <Command.Group key={section.label} heading={section.label} className={paletteGroup}>
              {section.items.map((item) => (
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
              for (const id of pending.panels) for (const discard of [...(drafts.current.get(id)?.values() ?? [])]) discard();
              const run = pending.run; setPending(undefined); run();
            }}>{t("Discard changes")}</Button>
          </div>
        </div>
      </Dialog>
      <Toaster position="bottom-right" toastOptions={{ className: "!rounded-md !border-border !bg-surface !text-foreground !text-sm" }} />
    </WorkspaceContext.Provider>
  );
}

function AppMenu({ launcher, product, label = t("Apps") }: { launcher: Launcher; product: string; label?: string }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger aria-label={label} className="flex h-7 min-w-0 max-w-56 items-center gap-2 rounded-md px-2 text-sm font-semibold tracking-tight hover:bg-row-hover max-sm:max-w-28">
        <LayoutGrid className="size-4 shrink-0 text-muted" /><span className="truncate">{product}</span><ChevronDown className="size-3.5 shrink-0 text-muted" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="start" sideOffset={4} className={menuPanel}>
          <DropdownMenu.Label className="px-2 py-1 text-xs text-muted">{label}</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={launcher.current ?? ""} onValueChange={launcher.onSelect}>
            {launcher.apps.map((a) => (
              <DropdownMenu.RadioItem key={a.id} value={a.id} className={cn(menuItem, "gap-2 [&_svg]:size-3.5")}>
                <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{a.icon}{a.title}
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

function SessionMenu({ session, onLanguageSelect }: { session: Session; onLanguageSelect: (id: string) => void }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger aria-label={`${session.principal} · ${session.tenant}`} className="flex h-7 shrink-0 items-center gap-2 rounded-md border border-border px-2 text-sm hover:bg-row-hover">
        <span className="grid size-5 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">
          {session.principal.slice(0, 1).toUpperCase()}
        </span>
        <span className="text-left leading-tight max-sm:hidden">
          <span className="block text-sm">{session.principal}</span>
          <span className="block text-xs text-muted">{session.tenant}{session.detail ? ` · ${session.detail}` : ""}</span>
        </span>
        <ChevronDown className="size-3.5 text-muted" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={4} className={menuPanel}>
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
