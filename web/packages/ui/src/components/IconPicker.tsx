import { useMemo, useState, type ReactNode } from "react";
import {
  Activity, AlertTriangle, Award, BadgeCheck, BarChart3, Bell, BookOpen, Bookmark, Boxes, Braces, Briefcase, Building, Building2, CalendarDays, ClipboardList, Clock, Cog,
  Crown, Database, DoorOpen, FileSpreadsheet, FileText, Filter, Flag, Folder, FolderTree, Factory, Gauge, GitBranch, Globe2, Handshake, Home, Hotel, IdCard, Landmark, Layers, LayoutGrid,
  ListOrdered, Mail, MapPin, MessageSquare, Network, Package, QrCode, Rocket, Scale, Search, Server, Settings2, ShieldAlert, Smartphone, Sparkles, Store, Table2, Tag,
  Plug, Target, TrendingUp, Truck, UserCheck, UserRound, Users, Warehouse, Workflow, Wrench, Zap,
} from "lucide-react";
import { cn } from "../lib/cn";
import { Button } from "../primitives/button";
import { t } from "../i18n";
import { Popover } from "@astryxdesign/core/Popover";
import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";

export type IconName = string;

export const iconGlyphs: Record<IconName, ReactNode> = {
  "building-2": <Building2 />, building: <Building />, factory: <Factory />, warehouse: <Warehouse />, store: <Store />, hotel: <Hotel />, home: <Home />,
  "map-pin": <MapPin />, truck: <Truck />, globe: <Globe2 />, grid: <LayoutGrid />, layers: <Layers />,
  users: <Users />, "user-round": <UserRound />, "user-check": <UserCheck />, "id-card": <IdCard />, badge: <BadgeCheck />, briefcase: <Briefcase />,
  handshake: <Handshake />, "calendar-days": <CalendarDays />, clock: <Clock />, target: <Target />, "list-ordered": <ListOrdered />, "clipboard-list": <ClipboardList />,
  "trending-up": <TrendingUp />, award: <Award />, flag: <Flag />,
  boxes: <Boxes />, package: <Package />, wrench: <Wrench />, cog: <Cog />, "qr-code": <QrCode />, smartphone: <Smartphone />,
  workflow: <Workflow />, "git-branch": <GitBranch />, rocket: <Rocket />, sparkles: <Sparkles />, "settings-2": <Settings2 />,
  chart: <BarChart3 />, database: <Database />, table: <Table2 />, spreadsheet: <FileSpreadsheet />, gauge: <Gauge />, activity: <Activity />,
  network: <Network />, server: <Server />, plug: <Plug />, search: <Search />, filter: <Filter />, tag: <Tag />,
  "file-text": <FileText />, folder: <Folder />, "folder-tree": <FolderTree />, bookmark: <Bookmark />, "book-open": <BookOpen />, mail: <Mail />,
  "message-square": <MessageSquare />, bell: <Bell />, milestone: <Award />,
  braces: <Braces />, zap: <Zap />,
  landmark: <Landmark />, crown: <Crown />, door: <DoorOpen />, scale: <Scale />, book: <BookOpen />, user: <UserRound />,
  clipboard: <ClipboardList />, people: <Users />, calendar: <CalendarDays />, map: <MapPin />,
  "shield-alert": <ShieldAlert />, alert: <AlertTriangle />,
};

export const iconGroups: { id: string; title: string; names: IconName[] }[] = [
  { id: "structure", title: "Structure and places", names: ["building-2", "building", "factory", "warehouse", "store", "hotel", "home", "map-pin", "truck", "globe", "grid", "layers", "landmark", "door", "scale"] },
  { id: "people", title: "People and posts", names: ["users", "user-round", "user-check", "id-card", "badge", "briefcase", "handshake", "award", "user", "crown"] },
  { id: "work", title: "Work and time", names: ["workflow", "list-ordered", "clipboard-list", "calendar-days", "clock", "target", "trending-up", "flag", "braces", "zap"] },
  { id: "things", title: "Things", names: ["boxes", "package", "wrench", "cog", "qr-code", "smartphone"] },
  { id: "data", title: "Data and evidence", names: ["chart", "database", "table", "spreadsheet", "gauge", "activity", "network", "server", "plug"] },
  { id: "find", title: "Find and sign", names: ["search", "filter", "tag", "bookmark"] },
  { id: "documents", title: "Documents and messages", names: ["file-text", "folder", "folder-tree", "book-open", "book", "mail", "message-square", "bell"] },
  { id: "change", title: "Change", names: ["git-branch", "rocket", "sparkles", "settings-2"] },
  { id: "govern", title: "Risk and control", names: ["shield-alert", "alert"] },
];

export function IconGlyph({ name, className }: { name?: string; className?: string }) {
  return <span className={cn("[&_svg]:size-4 [&_svg]:shrink-0", className)} aria-hidden="true">{iconGlyphs[name ?? ""] ?? <Boxes />}</span>;
}

export function IconPicker({ value, onChange, disabled, label, id }: {
  value?: string; onChange: (name: string) => void; disabled?: boolean; label?: string; id?: string;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const needle = query.trim().toLowerCase();
  const groups = useMemo(() => iconGroups.map((g) => ({ ...g, names: g.names.filter((n) => !needle || n.includes(needle) || t(n).toLowerCase().includes(needle)) })).filter((g) => g.names.length), [needle]);
  return <Popover label={t("Choose an icon")} role="dialog" placement="below" alignment="start" width={320} padding={2} isOpen={open} onOpenChange={setOpen}
    content={<VStack gap={2}>
      <TextInput label={t("Find an icon…")} isLabelHidden size="sm" startIcon={Search} value={query} onChange={(value) => setQuery(value)} placeholder={t("Find an icon…")} hasAutoFocus />
      <div className="max-h-64 overflow-auto">
        {groups.map((g) => <div key={g.id} className="mb-2">
          <Text type="label" weight="semibold">{t(g.title)}</Text>
          <div className="grid grid-cols-6 gap-1">{g.names.map((n) => <Button key={n} type="button" size="icon" variant={n === value ? "default" : "ghost"} aria-label={t(n)} aria-pressed={n === value} title={t(n)}
            onClick={() => { onChange(n); setOpen(false); setQuery(""); }}><IconGlyph name={n} /></Button>)}</div>
        </div>)}
        {!groups.length && <Text type="supporting">{t("Nothing matches.")}</Text>}
      </div>
      <HStack gap={1} justify="between"><Button type="button" size="sm" variant="ghost" onClick={() => { onChange(""); setOpen(false); }}>{t("No icon")}</Button><Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>{t("Close")}</Button></HStack>
    </VStack>}>
    <Button type="button" id={id} disabled={disabled} aria-label={label ?? t("Choose an icon")} className="h-8 w-full justify-start gap-2 px-2 text-sm">
      {value ? <><IconGlyph name={value} /><span className="truncate">{t(value)}</span><span className="ml-auto font-mono text-2xs text-muted">{value}</span></> : <span className="text-muted">{t("No icon")}</span>}
    </Button>
  </Popover>;
}
