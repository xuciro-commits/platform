// One icon vocabulary for the whole platform (ADR-0084 D1). The kit owns the
// names, the glyphs and their words; the host only checks that a name looks
// like a name, so the set can grow here without a host change. Anything that
// lets a person choose an icon — a module, an application, an enterprise
// stereotype — draws this picker, and every view that shows a stored name draws
// IconGlyph, so an unknown name is a default glyph rather than a blank.
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  Activity, AlertTriangle, Award, BadgeCheck, BarChart3, Bell, BookOpen, Bookmark, Boxes, Braces, Briefcase, Building, Building2, CalendarDays, ClipboardList, Clock, Cog,
  Crown, Database, DoorOpen, FileSpreadsheet, FileText, Filter, Flag, Folder, FolderTree, Factory, Gauge, GitBranch, Globe2, Handshake, Home, Hotel, IdCard, Landmark, Layers, LayoutGrid,
  ListOrdered, Mail, MapPin, MessageSquare, Network, Package, QrCode, Rocket, Scale, Search, Server, Settings2, ShieldAlert, Smartphone, Sparkles, Store, Table2, Tag,
  Target, TrendingUp, Truck, UserCheck, UserRound, Users, Warehouse, Workflow, Wrench, Zap,
} from "lucide-react";
import { cn } from "../lib/cn";
import { t } from "../i18n";

export type IconName = string;

/** name → glyph. The single place a stored icon name becomes a picture. */
export const iconGlyphs: Record<IconName, ReactNode> = {
  "building-2": <Building2 />, building: <Building />, factory: <Factory />, warehouse: <Warehouse />, store: <Store />, hotel: <Hotel />, home: <Home />,
  "map-pin": <MapPin />, truck: <Truck />, globe: <Globe2 />, grid: <LayoutGrid />, layers: <Layers />,
  users: <Users />, "user-round": <UserRound />, "user-check": <UserCheck />, "id-card": <IdCard />, badge: <BadgeCheck />, briefcase: <Briefcase />,
  handshake: <Handshake />, "calendar-days": <CalendarDays />, clock: <Clock />, target: <Target />, "list-ordered": <ListOrdered />, "clipboard-list": <ClipboardList />,
  "trending-up": <TrendingUp />, award: <Award />, flag: <Flag />,
  boxes: <Boxes />, package: <Package />, wrench: <Wrench />, cog: <Cog />, "qr-code": <QrCode />, smartphone: <Smartphone />,
  workflow: <Workflow />, "git-branch": <GitBranch />, rocket: <Rocket />, sparkles: <Sparkles />, "settings-2": <Settings2 />,
  chart: <BarChart3 />, database: <Database />, table: <Table2 />, spreadsheet: <FileSpreadsheet />, gauge: <Gauge />, activity: <Activity />,
  network: <Network />, server: <Server />, search: <Search />, filter: <Filter />, tag: <Tag />,
  "file-text": <FileText />, folder: <Folder />, "folder-tree": <FolderTree />, bookmark: <Bookmark />, "book-open": <BookOpen />, mail: <Mail />,
  "message-square": <MessageSquare />, bell: <Bell />, milestone: <Award />,
  braces: <Braces />, zap: <Zap />,
  // UAF's organization picture needs these (ADR-0090 D2): the profile's book
  // and user, plus the kinds' landmark, crown, door and scale.
  landmark: <Landmark />, crown: <Crown />, door: <DoorOpen />, scale: <Scale />, book: <BookOpen />, user: <UserRound />,
  // Names the kit used before the vocabulary grew (ADR-0036 D3): stored
  // applications keep their glyph, the picker offers the current set.
  clipboard: <ClipboardList />, people: <Users />, calendar: <CalendarDays />, map: <MapPin />,
  "shield-alert": <ShieldAlert />, alert: <AlertTriangle />,
};

/** The groups the picker offers, in the order a person looks for things. */
export const iconGroups: { id: string; title: string; names: IconName[] }[] = [
  { id: "structure", title: "Structure and places", names: ["building-2", "building", "factory", "warehouse", "store", "hotel", "home", "map-pin", "truck", "globe", "grid", "layers", "landmark", "door", "scale"] },
  { id: "people", title: "People and posts", names: ["users", "user-round", "user-check", "id-card", "badge", "briefcase", "handshake", "award", "user", "crown"] },
  { id: "work", title: "Work and time", names: ["workflow", "list-ordered", "clipboard-list", "calendar-days", "clock", "target", "trending-up", "flag", "braces", "zap"] },
  { id: "things", title: "Things", names: ["boxes", "package", "wrench", "cog", "qr-code", "smartphone"] },
  { id: "data", title: "Data and evidence", names: ["chart", "database", "table", "spreadsheet", "gauge", "activity", "network", "server"] },
  { id: "find", title: "Find and sign", names: ["search", "filter", "tag", "bookmark"] },
  { id: "documents", title: "Documents and messages", names: ["file-text", "folder", "folder-tree", "book-open", "book", "mail", "message-square", "bell"] },
  { id: "change", title: "Change", names: ["git-branch", "rocket", "sparkles", "settings-2"] },
  { id: "govern", title: "Risk and control", names: ["shield-alert", "alert"] },
];

/** The glyph for a stored name; an unknown name is the kit's default, never blank. */
export function IconGlyph({ name, className }: { name?: string; className?: string }) {
  return <span className={cn("[&_svg]:size-4 [&_svg]:shrink-0", className)} aria-hidden="true">{iconGlyphs[name ?? ""] ?? <Boxes />}</span>;
}

/** A field that chooses an icon: one button, a searchable grid, a way to clear it. */
export function IconPicker({ value, onChange, disabled, label, id }: {
  value?: string; onChange: (name: string) => void; disabled?: boolean; label?: string; id?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const close = (event: MouseEvent) => { if (!box.current?.contains(event.target as Node)) setOpen(false); };
    const key = (event: KeyboardEvent) => { if (event.key === "Escape") setOpen(false); };
    window.addEventListener("mousedown", close); window.addEventListener("keydown", key);
    return () => { window.removeEventListener("mousedown", close); window.removeEventListener("keydown", key); };
  }, [open]);
  const needle = query.trim().toLowerCase();
  const groups = useMemo(() => iconGroups.map((g) => ({ ...g, names: g.names.filter((n) => !needle || n.includes(needle) || t(n).toLowerCase().includes(needle)) })).filter((g) => g.names.length), [needle]);
  return <div ref={box} className="relative">
    <button type="button" id={id} disabled={disabled} aria-haspopup="dialog" aria-expanded={open} aria-label={label}
      onClick={() => setOpen(!open)} className={cn("flex h-8 w-full items-center gap-2 rounded-md border border-border bg-surface px-2 text-left text-sm", disabled && "opacity-60")}>
      {value ? <><IconGlyph name={value} /><span className="truncate">{t(value)}</span><span className="ml-auto font-mono text-[10px] text-muted">{value}</span></>
        : <span className="text-muted">{t("No icon")}</span>}
    </button>
    {open && <div role="dialog" aria-label={t("Choose an icon")} className="absolute left-0 top-full z-30 mt-1 w-80 rounded-md border border-border bg-surface p-2 shadow-lg">
      <input autoFocus value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("Find an icon…")} aria-label={t("Find an icon…")}
        className="mb-2 h-8 w-full rounded-md border border-border bg-background px-2 text-sm" />
      <div className="max-h-64 overflow-auto">
        {groups.map((g) => <div key={g.id} className="mb-2">
          <p className="px-1 pb-1 text-[10px] font-semibold uppercase tracking-wide text-muted">{t(g.title)}</p>
          <div className="grid grid-cols-6 gap-1">{g.names.map((n) => <button key={n} type="button" title={t(n)} aria-label={t(n)} aria-pressed={n === value}
            onClick={() => { onChange(n); setOpen(false); setQuery(""); }}
            className={cn("flex h-8 items-center justify-center rounded-md border border-transparent text-muted hover:border-border hover:bg-row-hover hover:text-foreground", n === value && "border-border bg-row-selected text-foreground")}>
            <IconGlyph name={n} /></button>)}</div>
        </div>)}
        {!groups.length && <p className="p-2 text-xs text-muted">{t("Nothing matches.")}</p>}
      </div>
      <div className="mt-1 flex justify-between border-t border-border pt-1">
        <button type="button" className="rounded px-2 py-1 text-xs text-muted hover:bg-row-hover" onClick={() => { onChange(""); setOpen(false); }}>{t("No icon")}</button>
        <button type="button" className="rounded px-2 py-1 text-xs text-muted hover:bg-row-hover" onClick={() => setOpen(false)}>{t("Close")}</button>
      </div>
    </div>}
  </div>;
}
