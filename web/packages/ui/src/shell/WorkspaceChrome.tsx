import { ChevronsLeft, Star } from "lucide-react";
import { DropdownMenu } from "radix-ui";
import { Component, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { routeKey, type Route } from "./route";
import { language, languages, t } from "../i18n";
import { Button } from "../primitives/button";
import { NavigationItems, navigationLinks, routeCovers, type NavSection } from "./Navigation";
import { Tooltip } from "../primitives/tooltip";
import type {PlatformApplication, Session} from "./WorkspaceContext";

export const paletteScopes = [">", "@", "#", ":"];

export const shortcutList: [string, string][] = [
  ["⌘K", "Command palette and search"], ["⌘\\", "Collapse or expand the app sidebar"], ["⌘S", "Save now to your working state"], ["⌘⇧S", "Open Review and save"],
  ["⌘Z / ⇧⌘Z", "Undo / redo"], ["⌘C / ⌘V / ⌘D", "Copy / paste / duplicate"], ["Delete", "Delete the selection"], ["Esc", "Close the top overlay, or clear the selection"],
  ["⌥⌘\\ / ⌘J", "Right inspector / bottom dock"], ["↑↓ / Enter", "Move / open"], ["?", "This list"],
];

export class ViewBoundary extends Component<{ children: ReactNode; onClose: () => void; closeLabel: string }, { error?: Error }> {
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

export function Sidebar({ app, product, productIcon, nav, navigation, active, resource, slot, collapsed, onToggle, onOpen, status, pinned, onPin, onHome }: {
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

export function RailButton({ label, icon, badge, active, run }: { id: string; label: string; icon: ReactNode; badge?: number; active?: boolean; run: () => void }) {
  return <Tooltip content={label} side="right"><button type="button" aria-label={label} aria-current={active ? "page" : undefined} onClick={run}
    className={cn("relative grid size-9 shrink-0 place-items-center rounded-md text-rail-foreground transition-colors hover:bg-rail-hover hover:text-rail-active [&_svg]:size-[18px]", active && "bg-rail-selected text-rail-active before:absolute before:-left-1.5 before:top-2 before:h-5 before:w-0.5 before:rounded-full before:bg-primary")}>
    {icon}{badge ? <span className="absolute -right-0.5 -top-0.5 min-w-4 rounded-full bg-primary px-1 text-center text-2xs font-semibold leading-4 text-primary-foreground ring-2 ring-rail">{badge > 99 ? "99+" : badge}</span> : null}
  </button></Tooltip>;
}

export function RailList({ label, icon, items, empty }: { label: string; icon: ReactNode; empty: string; items: { id: string; label: string; detail?: string; icon?: ReactNode; run: () => void }[] }) {
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

export function SessionMenu({ session, onLanguageSelect, rail, scheme, onScheme }: { session: Session; onLanguageSelect: (id: string) => void; rail: boolean; scheme: string; onScheme: (scheme: "light" | "dark" | "system") => void }) {
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
export const paletteGroup = "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:text-2xs [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wider [&_[cmdk-group-heading]]:text-muted";
export const paletteItem = "flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-sm data-[selected=true]:bg-row-selected [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted";
