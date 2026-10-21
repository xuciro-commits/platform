import { Command } from "cmdk";
import { DockviewDefaultTab, type IDockviewPanelHeaderProps, type SerializedDockview } from "dockview-react";
import { ChevronDown, LayoutGrid } from "lucide-react";
import { ContextMenu, DropdownMenu } from "radix-ui";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";
import { language, languages, t } from "../i18n";
import { Button } from "../primitives/button";
import { Dialog } from "../primitives/dialog";
import { navigationLinks, type NavSection } from "./Navigation";
import { routeKey, type Route } from "./route";
import type { Applications, Session, ShellCommand } from "./Workspace";
export function AppMenu({ applications, product, icon }: { applications: Applications; product: string; icon?: ReactNode }) {
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
export function RailList({ label, icon, items, empty }: { label: string; icon: ReactNode; empty: string; items: { id: string; label: string; detail?: string; icon?: ReactNode; run: () => void }[] }) {
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
export function SessionMenu({ session, onLanguageSelect, compact = false }: { session: Session; onLanguageSelect: (id: string) => void; compact?: boolean }) {
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
export const menuPanel = "z-50 min-w-48 rounded-md border border-border bg-surface p-1 text-sm shadow-lg";
export const menuItem = "relative flex h-7 cursor-default select-none items-center rounded-sm pl-6 pr-2 outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-row-hover";
export const paletteGroup = "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-muted";
export const paletteItem = "flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-sm data-[selected=true]:bg-row-selected [&_svg]:size-3.5";
export function CommandPalette({ open, onOpenChange, typed, onTypedChange, found, applications, nav, tabs, commands, onOpen, onFocusTab }: {
  open: boolean; onOpenChange: (open: boolean) => void; typed: string; onTypedChange: (text: string) => void; found: { id: string; label: string; detail?: string; open: () => void }[];
  applications?: Applications; nav: NavSection[]; tabs: { key: string; title: string }[]; commands: ShellCommand[]; onOpen: (route: { view: string; params?: Record<string, string> }) => void; onFocusTab: (key: string) => void;
}) {
  const choose = (run: () => void) => { run(); onOpenChange(false); };
  return (
    <Command.Dialog open={open} onOpenChange={onOpenChange} label={t("Command palette")}
      overlayClassName="fixed inset-0 z-40 bg-black/30"
      contentClassName="fixed left-1/2 top-[15%] z-50 w-[min(560px,calc(100vw-32px))] -translate-x-1/2 overflow-hidden rounded-md border border-border bg-surface shadow-2xl">
      <Command.Input value={typed} onValueChange={onTypedChange} placeholder={t("Search records, go to, open, run…")} className="h-10 w-full border-b border-border bg-transparent px-3 text-base outline-none" />
      <Command.List className="max-h-80 overflow-auto p-1">
        <Command.Empty className="p-3 text-sm text-muted">{t("No results")}</Command.Empty>
        {found.length > 0 && (
          <Command.Group heading={t("Records")} className={paletteGroup}>
            {found.map((hit) => (
              <Command.Item key={hit.id} value={`record ${typed} ${hit.label} ${hit.id}`} className={paletteItem}
                onSelect={() => choose(hit.open)}>{hit.label}{hit.detail && <span className="ml-auto font-mono text-xs text-muted">{hit.detail}</span>}</Command.Item>
            ))}
          </Command.Group>
        )}
        {applications && applications.apps.length > 0 && (
          <Command.Group heading={t("Applications")} className={paletteGroup}>
            {applications.apps.map((a) => (
              <Command.Item key={a.id} value={`app ${a.title} ${applications.categories.find((c) => c.id === a.category)?.label ?? ""}`} className={paletteItem}
                onSelect={() => choose(() => applications.onSelect(a.id))}>{a.icon}{a.title}
                <span className="ml-auto text-xs text-muted">{applications.categories.find((c) => c.id === a.category)?.label}</span></Command.Item>
            ))}
          </Command.Group>
        )}
        {nav.map((section) => (
          <Command.Group key={section.label} heading={section.label} className={paletteGroup}>
            {navigationLinks(section.items).map((item) => (
              <Command.Item key={routeKey(item.route)} value={`${section.label} ${item.label}`} className={paletteItem}
                onSelect={() => choose(() => onOpen(item.route))}>{item.icon}{item.label}</Command.Item>
            ))}
          </Command.Group>
        ))}
        {tabs.length > 0 && (
          <Command.Group heading={t("Open tabs")} className={paletteGroup}>
            {tabs.map((tab) => (
              <Command.Item key={tab.key} value={`tab ${tab.title} ${tab.key}`} className={paletteItem}
                onSelect={() => choose(() => onFocusTab(tab.key))}>{tab.title}</Command.Item>
            ))}
          </Command.Group>
        )}
        {commands.length > 0 && (
          <Command.Group heading={t("Commands")} className={paletteGroup}>
            {commands.map((c) => (
              <Command.Item key={c.id} value={`${c.group ?? ""} ${c.label}`} className={paletteItem} onSelect={() => choose(c.run)}>
                {c.label}{c.shortcut && <span className="ml-auto text-xs text-muted">{c.shortcut}</span>}
              </Command.Item>
            ))}
          </Command.Group>
        )}
      </Command.List>
    </Command.Dialog>
  );
}
export type PanelCommand = { label: string; run: () => void; disabled?: boolean; shortcut?: string };
export function TabHeader({ tab, commands, onClose }: { tab: IDockviewPanelHeaderProps; commands: PanelCommand[]; onClose: () => void }) {
  return <ContextMenu.Root><ContextMenu.Trigger asChild><div className="flex h-full"><DockviewDefaultTab {...tab} closeActionOverride={onClose} /></div></ContextMenu.Trigger>
    <ContextMenu.Portal><ContextMenu.Content className={menuPanel}>{commands.map((command, index) => <div key={command.label}>
      {index === 4 && <ContextMenu.Separator className="my-1 h-px bg-border" />}
      <ContextMenu.Item onSelect={command.run} disabled={command.disabled} className={menuItem}>{command.label}</ContextMenu.Item>
    </div>)}</ContextMenu.Content></ContextMenu.Portal></ContextMenu.Root>;
}
export function UnsavedChanges({ panels, title, onKeep, onDiscard }: { panels?: string[]; title: (id: string) => string; onKeep: () => void; onDiscard: () => void }) {
  return <Dialog open={!!panels} title={t("Unsaved changes")} onOpenChange={(open) => { if (!open) onKeep(); }}><div className="grid gap-3 text-sm">
    <p>{t("Save your work in the editor, or discard the changes to continue.")}</p>
    <ul className="list-inside list-disc text-muted">{panels?.map((id) => <li key={id}>{title(id)}</li>)}</ul>
    <div className="flex justify-end gap-2"><Button onClick={onKeep}>{t("Keep editing")}</Button><Button variant="danger" onClick={onDiscard}>{t("Discard changes")}</Button></div>
  </div></Dialog>;
}
type Node = SerializedDockview["grid"]["root"]; type Group = Exclude<Node["data"], Node[]>;
/** Rewrite route identities and their group references together. Used for
 * incoming aliases and for app-only persistence; never mutate Dockview's live
 * layout or keep a second editor/draft store. */
export function rewriteLayout(layout: SerializedDockview, map: (route: Route, panel: string) => Route | undefined, keyOf: (route: Route) => string = routeKey): SerializedDockview {
  const panels: SerializedDockview["panels"] = Object.create(null);
  const keys = new Map<string, string>();
  for (const [id, panel] of Object.entries(layout.panels)) {
    const original = panel.params?.route as Route | undefined;
    const route = original && map(original, id);
    if (!route) continue;
    const key = keyOf(route);
    keys.set(id, key);
    panels[key] ??= { ...panel, id: key, params: { ...panel.params, route } };
  }
  const placed = new Set<string>(), groups = new Set<string>();
  const group = (data: Group): Group | undefined => {
    const views = data.views.flatMap((id) => {
      const key = keys.get(id);
      if (!key || placed.has(key)) return [];
      placed.add(key); return [key];
    });
    if (!views.length) return undefined;
    groups.add(data.id);
    const active = data.activeView && keys.get(data.activeView);
    return { ...data, views, activeView: active && views.includes(active) ? active : views[0] };
  };
  const node = (data: Node): Node | undefined => {
    if (data.type === "leaf") {
      const mapped = group(data.data as Group);
      return mapped ? { ...data, data: mapped } : undefined;
    }
    const children = (data.data as Node[]).flatMap((child) => { const mapped = node(child); return mapped ? [mapped] : []; });
    return children.length ? { ...data, data: children } : undefined;
  };
  let root = node(layout.grid.root) ?? { type: "branch" as const, data: [] };
  // Floating is retired (ADR-0018 D1). Keep its old tabs readable, but
  for (const entry of layout.floatingGroups ?? []) {
    const data = entry.data && group(entry.data);
    const restored = entry.grid ? node(entry.grid.root) : data ? { type: "leaf" as const, data } : undefined;
    if (restored) root = root.type === "branch"
      ? { ...root, data: [...root.data as Node[], restored] }
      : { type: "branch", data: [root, restored] };
  }
  const popoutGroups = layout.popoutGroups?.flatMap((entry) => {
    const data = entry.data && group(entry.data), grid = entry.grid && node(entry.grid.root);
    return data || grid ? [{ ...entry, data, grid: grid && entry.grid ? { ...entry.grid, root: grid } : undefined }] : [];
  });
  for (const key of Object.keys(panels)) if (!placed.has(key)) delete panels[key];
  return { ...layout, panels, grid: { ...layout.grid, root }, floatingGroups: undefined, popoutGroups,
    activeGroup: layout.activeGroup && groups.has(layout.activeGroup) ? layout.activeGroup : undefined };
}
/** Resolve only saved/incoming routes, not the canonical routes apps produce.
 * A failed compatibility read leaves the old tab and its refusal intact. */
export async function resolveLayout(layout: SerializedDockview, resolve: (route: Route) => Route | Promise<Route>, exists: (view: string) => boolean, keyOf: (route: Route) => string = routeKey): Promise<SerializedDockview> {
  const routes = new Map<string, Route>();
  await Promise.all(Object.entries(layout.panels).map(async ([id, panel]) => {
    const original = panel.params?.route as Route | undefined;
    if (!original) return;
    let route = original;
    try { route = await resolve(original); } catch { /* preserve an unavailable original route */ }
    if (exists(route.view)) routes.set(id, route);
  }));
  return rewriteLayout(layout, (_route, id) => routes.get(id), keyOf);
}
