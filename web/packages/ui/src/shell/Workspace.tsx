import { Command } from "cmdk";
import { DockviewReact, themeLight, type DockviewApi, type IDockviewPanelProps } from "dockview-react";
import { ChevronDown, PanelLeft, Search } from "lucide-react";
import { DropdownMenu, Menubar } from "radix-ui";
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { Toaster, toast } from "sonner";
import { cn } from "../lib/cn";
import { routeFromHash, routeKey, routeToHash, type Route } from "./route";

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

type OpenOptions = { window?: "tab" | "float" | "popout" };
type WorkspaceApi = { open: (route: Route, options?: OpenOptions) => void; close: (route: Route) => void; notify: typeof toast };

export const WorkspaceContext = createContext<WorkspaceApi | null>(null);

/** Opens routes as tabs, floats or separate windows, from any view. */
export function useWorkspace(): WorkspaceApi {
  const api = useContext(WorkspaceContext);
  if (!api) throw new Error("useWorkspace outside <Workspace>");
  return api;
}

export const notify = toast;

/**
 * The platform shell: menu bar, session, navigation, a docking workspace whose
 * tabs are routes (one per entity), a command palette (⌘K) and notifications.
 * The layout survives restarts (per `storageKey`); the active tab is in the URL.
 */
export function Workspace({ product, storageKey, views, nav, home, menus = [], commands = [], session, status }: {
  product: string; storageKey: string; views: View[]; nav: NavSection[]; home: Route;
  menus?: Menu[]; commands?: ShellCommand[]; session?: Session; status?: ReactNode;
}) {
  const dock = useRef<DockviewApi>(null);
  const [active, setActive] = useState<string>();
  const [openTabs, setOpenTabs] = useState<{ key: string; title: string }[]>([]);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [navOpen, setNavOpen] = useState(true);
  const byId = useMemo(() => new Map(views.map((v) => [v.id, v])), [views]);

  const open = useCallback((route: Route, options: OpenOptions = {}) => {
    const api = dock.current;
    const view = byId.get(route.view);
    if (!api || !view) return;
    const key = routeKey(route);
    const panel = api.getPanel(key) ?? api.addPanel({ id: key, component: "view", title: view.title(route.params ?? {}), params: { route } });
    if (options.window === "float") api.addFloatingGroup(panel);
    if (options.window === "popout") void api.addPopoutGroup(panel);
    panel.api.setActive();
  }, [byId]);

  const workspace = useMemo<WorkspaceApi>(() => ({
    open, notify: toast, close: (route) => dock.current?.getPanel(routeKey(route))?.api.close(),
  }), [open]);

  const components = useMemo(() => ({
    view: ({ params }: IDockviewPanelProps<{ route: Route }>) => {
      const view = byId.get(params.route.view);
      return <div className="h-full overflow-auto bg-background p-4">
        {view ? view.render(params.route.params ?? {}) : <p className="text-sm text-muted">This view no longer exists.</p>}
      </div>;
    },
  }), [byId]);

  const onReady = useCallback(({ api }: { api: DockviewApi }) => {
    dock.current = api;
    // Read the link first: restoring the layout rewrites the URL to its active tab.
    const linked = routeFromHash(location.hash);
    try {
      const saved = localStorage.getItem(storageKey);
      if (saved) api.fromJSON(JSON.parse(saved));
      for (const p of [...api.panels]) { // tabs of views a new version removed
        if (!byId.has((p.params as { route?: Route } | undefined)?.route?.view ?? "")) p.api.close();
      }
    } catch {
      api.clear(); // a stale or corrupt layout falls back to the home view
    }
    const sync = () => {
      setOpenTabs(api.panels.map((p) => ({ key: p.id, title: p.title ?? p.id })));
      setActive(api.activePanel?.id);
      const route = (api.activePanel?.params as { route?: Route } | undefined)?.route;
      if (route) history.replaceState(null, "", routeToHash(route));
      try { localStorage.setItem(storageKey, JSON.stringify(api.toJSON())); } catch { /* storage unavailable */ }
    };
    api.onDidLayoutChange(sync);
    api.onDidActivePanelChange(sync);
    if (linked && byId.has(linked.view)) open(linked);
    else if (api.panels.length === 0) open(home);
    sync();
  }, [byId, home, open, storageKey]);

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
    { label: "View", items: [
      { label: "Command palette", shortcut: "⌘K", onSelect: () => setPaletteOpen(true) },
      { label: navOpen ? "Hide navigation" : "Show navigation", onSelect: () => setNavOpen(!navOpen) },
      { label: "Reset layout", onSelect: () => { dock.current?.clear(); open(home); } },
    ] },
    { label: "Window", items: [
      { label: "Float tab", disabled: !active, onSelect: () => { const p = activePanel(); if (p) dock.current?.addFloatingGroup(p); } },
      { label: "Move tab to new window", disabled: !active, onSelect: () => { const p = activePanel(); if (p) void dock.current?.addPopoutGroup(p); } },
      { label: "Close tab", disabled: !active, onSelect: () => activePanel()?.api.close() },
      ...openTabs.map((t) => ({ label: t.title, onSelect: () => dock.current?.getPanel(t.key)?.api.setActive() })),
    ] },
  ];

  return (
    <WorkspaceContext.Provider value={workspace}>
      <div className="grid h-dvh grid-rows-[36px_1fr] bg-background text-foreground">
        <header className="flex items-center gap-2 border-b border-border bg-surface px-2">
          <button type="button" aria-label="Toggle navigation" onClick={() => setNavOpen(!navOpen)}
            className="rounded-sm p-1 text-muted hover:bg-row-hover hover:text-foreground"><PanelLeft className="size-4" /></button>
          <span className="pr-2 text-sm font-semibold tracking-tight">{product}</span>
          <Menubar.Root className="flex items-center">
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
          <button type="button" onClick={() => setPaletteOpen(true)}
            className="mx-auto flex h-6 w-72 items-center gap-2 rounded-md border border-border bg-background px-2 text-sm text-muted hover:text-foreground max-lg:hidden">
            <Search className="size-3.5" />Search and commands<kbd className="ml-auto text-xs">⌘K</kbd>
          </button>
          <div className="ml-auto flex items-center gap-2">
            {status}
            {session && <SessionMenu session={session} />}
          </div>
        </header>
        <div className={cn("grid min-h-0", navOpen ? "grid-cols-[220px_1fr]" : "grid-cols-1")}>
          {navOpen && (
            <nav aria-label="Main" className="overflow-auto border-r border-border bg-surface p-2">
              {nav.map((section) => (
                <div key={section.label} className="mb-3">
                  <div className="px-2 pb-1 text-xs font-medium uppercase tracking-wide text-muted">{section.label}</div>
                  {section.items.map((item) => {
                    const key = routeKey(item.route);
                    return (
                      <button key={key} type="button" onClick={() => open(item.route)} aria-current={key === active ? "page" : undefined}
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
          <div className="platform-dock min-h-0 min-w-0">
            <DockviewReact components={components} onReady={onReady} theme={themeLight} />
          </div>
        </div>
      </div>
      <Command.Dialog open={paletteOpen} onOpenChange={setPaletteOpen} label="Command palette"
        overlayClassName="fixed inset-0 z-40 bg-black/30"
        contentClassName="fixed left-1/2 top-[15%] z-50 w-[min(560px,calc(100vw-32px))] -translate-x-1/2 overflow-hidden rounded-md border border-border bg-surface shadow-2xl">
        <Command.Input placeholder="Go to, open, run…" className="h-10 w-full border-b border-border bg-transparent px-3 text-base outline-none" />
        <Command.List className="max-h-80 overflow-auto p-1">
          <Command.Empty className="p-3 text-sm text-muted">No results</Command.Empty>
          {nav.map((section) => (
            <Command.Group key={section.label} heading={section.label} className={paletteGroup}>
              {section.items.map((item) => (
                <Command.Item key={routeKey(item.route)} value={`${section.label} ${item.label}`} className={paletteItem}
                  onSelect={() => { open(item.route); setPaletteOpen(false); }}>{item.icon}{item.label}</Command.Item>
              ))}
            </Command.Group>
          ))}
          {openTabs.length > 0 && (
            <Command.Group heading="Open tabs" className={paletteGroup}>
              {openTabs.map((tab) => (
                <Command.Item key={tab.key} value={`tab ${tab.title} ${tab.key}`} className={paletteItem}
                  onSelect={() => { dock.current?.getPanel(tab.key)?.api.setActive(); setPaletteOpen(false); }}>{tab.title}</Command.Item>
              ))}
            </Command.Group>
          )}
          {commands.length > 0 && (
            <Command.Group heading="Commands" className={paletteGroup}>
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
      <Toaster position="bottom-right" toastOptions={{ className: "!rounded-md !border-border !bg-surface !text-foreground !text-sm" }} />
    </WorkspaceContext.Provider>
  );
}

function SessionMenu({ session }: { session: Session }) {
  return (
    <DropdownMenu.Root>
      <DropdownMenu.Trigger className="flex h-7 items-center gap-2 rounded-md border border-border px-2 text-sm hover:bg-row-hover">
        <span className="grid size-5 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">
          {session.principal.slice(0, 1).toUpperCase()}
        </span>
        <span className="text-left leading-tight">
          <span className="block text-sm">{session.principal}</span>
          <span className="block text-xs text-muted">{session.tenant}{session.detail ? ` · ${session.detail}` : ""}</span>
        </span>
        <ChevronDown className="size-3.5 text-muted" />
      </DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={4} className={menuPanel}>
          <DropdownMenu.Label className="px-2 py-1 text-xs text-muted">Switch tenant or identity</DropdownMenu.Label>
          <DropdownMenu.RadioGroup value={session.current} onValueChange={session.onSwitch}>
            {session.options.map((o) => (
              <DropdownMenu.RadioItem key={o.id} value={o.id} className={menuItem}>
                <DropdownMenu.ItemIndicator className="absolute left-2">•</DropdownMenu.ItemIndicator>{o.label}
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
