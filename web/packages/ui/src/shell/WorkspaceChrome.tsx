import { Check, ChevronRight, ChevronsLeft, Languages, LogOut, Repeat2, Star, SunMoon, UserPlus, UserRound, Users } from "lucide-react";
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
  ["⌘K", "Command palette and search"], ["⌘\\", "Collapse or expand the app sidebar"], ["⌘,", "Open my account"], ["⌘S", "Save now to your working state"], ["⌘⇧S", "Open Review and save"],
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
            <DropdownMenu.Item key={item.id} onSelect={item.run} className={cn(menuRow, "[&_svg]:size-3.5")}>
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
  const initial = shown.slice(0, 1).toUpperCase();
  const secondary = [session.name ? session.principal : undefined, session.tenant, session.detail].filter(Boolean).join(" · ");
  const appearances = [["light", t("Light")], ["dark", t("Dark")], ["system", t("Follow system")]] as const;
  const appearance = appearances.find(([id]) => id === scheme)?.[1];
  const current = session.options.find((option) => option.id === session.current)?.label;
  const choice = (value: string, label: string) => (
    <DropdownMenu.RadioItem key={value} value={value} className={menuRadio}>
      <DropdownMenu.ItemIndicator className={menuIndicator}><Check /></DropdownMenu.ItemIndicator>
      <span className="min-w-0 flex-1 truncate">{label}</span>
    </DropdownMenu.RadioItem>
  );
  const section = (icon: ReactNode, label: string, value: string | undefined, body: ReactNode) => (
    <DropdownMenu.Sub>
      <DropdownMenu.SubTrigger className={cn(menuRow, "data-[state=open]:bg-row-hover")}>
        {icon}<span className="min-w-0 flex-1 truncate">{label}</span>{value && <span className={menuValue}>{value}</span>}<ChevronRight className={menuChevron} />
      </DropdownMenu.SubTrigger>
      <DropdownMenu.Portal><DropdownMenu.SubContent sideOffset={6} alignOffset={-4} className={cn(menuPanel, "min-w-52")}>{body}</DropdownMenu.SubContent></DropdownMenu.Portal>
    </DropdownMenu.Sub>
  );
  return (
    <DropdownMenu.Root>
      <Tooltip content={`${shown} · ${session.tenant}`} side={rail ? "right" : "bottom"}><DropdownMenu.Trigger aria-label={`${session.principal} · ${session.tenant}`}
        className={cn("grid size-9 shrink-0 place-items-center rounded-md", rail ? "hover:bg-rail-hover data-[state=open]:bg-rail-selected" : "hover:bg-row-hover")}>
        <span className="grid size-6 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">{initial}</span>
      </DropdownMenu.Trigger></Tooltip>
      <DropdownMenu.Portal>
        <DropdownMenu.Content side={rail ? "right" : "bottom"} align="end" sideOffset={8} className={cn(menuPanel, "w-72")}>
          <div className="flex min-w-0 items-center gap-2.5 px-2 pb-2 pt-1.5">
            <span className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-xs font-semibold text-primary-foreground">{initial}</span>
            <span className="min-w-0 flex-1">
              <span className="block truncate font-medium">{shown}</span>
              <span className="block truncate text-xs text-muted">{secondary}</span>
            </span>
          </div>
          <DropdownMenu.Separator className={menuSeparator} />
          {session.onAccount && <DropdownMenu.Item className={menuRow} onSelect={() => session.onAccount?.()}>
            <UserRound className={menuIcon} /><span className="min-w-0 flex-1 truncate">{t("My account")}</span><span className={menuValue}>⌘,</span>
          </DropdownMenu.Item>}
          {section(<SunMoon className={menuIcon} />, t("Appearance"), appearance,
            <DropdownMenu.RadioGroup value={scheme} onValueChange={value => onScheme(value as "light" | "dark" | "system")}>
              {appearances.map(([id, label]) => choice(id, label))}
            </DropdownMenu.RadioGroup>)}
          {section(<Languages className={menuIcon} />, t("Language"), languages.find((entry) => entry.id === language())?.name,
            <DropdownMenu.RadioGroup value={language()} onValueChange={onLanguageSelect}>
              {languages.map((entry) => choice(entry.id, entry.name))}
            </DropdownMenu.RadioGroup>)}
          {session.accounts && section(<Users className={menuIcon} />, t("Switch account"), session.accounts.find((entry) => entry.id === session.currentAccount)?.label,
            <>
              <DropdownMenu.RadioGroup value={session.currentAccount} onValueChange={(id) => session.onAccountSwitch?.(id)}>
                {session.accounts.map((entry) => choice(entry.id, entry.label))}
              </DropdownMenu.RadioGroup>
              <DropdownMenu.Separator className={menuSeparator} />
              <DropdownMenu.Item className={menuRow} onSelect={() => session.onAddAccount?.()}>
                <UserPlus className={menuIcon} /><span className="min-w-0 flex-1 truncate">{t("Add account")}</span>
              </DropdownMenu.Item>
            </>)}
          {session.options.length > 0 && section(<Repeat2 className={menuIcon} />, session.switchTitle ?? t("Switch tenant or identity"), current,
            <DropdownMenu.RadioGroup value={session.current} onValueChange={session.onSwitch}>
              {session.options.map((option) => choice(option.id, option.label))}
            </DropdownMenu.RadioGroup>)}
          {session.onSignOut && <>
            <DropdownMenu.Separator className={menuSeparator} />
            <DropdownMenu.Item className={menuRow} onSelect={() => session.onSignOut?.()}>
              <LogOut className={menuIcon} /><span className="min-w-0 flex-1 truncate">{t("Sign out")}</span>
            </DropdownMenu.Item>
          </>}
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
  );
}

const menuPanel = "z-50 min-w-48 rounded-xl border border-border bg-surface p-1 text-sm text-foreground shadow-lg";
const menuLabel = "px-2 pb-1 pt-1.5 text-2xs font-semibold uppercase tracking-wider text-muted";
const menuItem = "relative flex h-8 cursor-default select-none items-center gap-2 rounded-lg outline-none data-[disabled]:opacity-40 data-[highlighted]:bg-row-hover";
const menuRow = cn(menuItem, "px-2");
const menuRadio = cn(menuItem, "pl-8 pr-2");
const menuSeparator = "my-1 h-px bg-border";
const menuIcon = "size-4 shrink-0 text-muted";
const menuValue = "ml-auto shrink-0 pl-4 text-xs text-muted";
const menuChevron = "size-3.5 shrink-0 text-muted";
const menuIndicator = "absolute left-2 inline-flex [&_svg]:size-4 [&_svg]:text-primary";
export const paletteGroup = "[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:pb-1 [&_[cmdk-group-heading]]:pt-2 [&_[cmdk-group-heading]]:text-2xs [&_[cmdk-group-heading]]:font-semibold [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wider [&_[cmdk-group-heading]]:text-muted";
export const paletteItem = "flex h-8 cursor-default items-center gap-2 rounded-sm px-2 text-sm data-[selected=true]:bg-row-selected [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted";
