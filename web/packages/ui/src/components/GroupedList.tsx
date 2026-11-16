import { useMemo, useState, type ReactNode } from "react";
import { TreeList, type TreeListItemData } from "@astryxdesign/core/TreeList";
import { Text } from "@astryxdesign/core/Text";
import { cn } from "../lib/cn";
import { Input, Select } from "../primitives/input";
import { t } from "../i18n";

export type GroupBy<T> = { id: string; label: string; of: (item: T) => { id: string; label: string; hint?: string; order?: number } };

export function GroupedList<T>({ items, groupings, grouping, onGrouping, id, row, text, selected, onSelect, toolbar, empty, className, dense }: {
  items: T[];
  groupings: GroupBy<T>[];
  grouping?: string; onGrouping?: (id: string) => void;
  id: (item: T) => string;
  row: (item: T) => ReactNode;
  text: (item: T) => string;
  selected?: string; onSelect?: (item: T) => void;
  toolbar?: ReactNode; empty?: ReactNode; className?: string; dense?: boolean;
}) {
  const [ownGrouping, setOwnGrouping] = useState(groupings[0]?.id ?? "");
  const current = groupings.find((g) => g.id === (grouping ?? ownGrouping)) ?? groupings[0];
  const [q, setQ] = useState("");
  const needle = q.trim().toLocaleLowerCase();
  const groups = useMemo(() => {
    const out = new Map<string, { id: string; label: string; hint?: string; order: number; items: T[] }>();
    for (const item of items) {
      if (needle && !text(item).toLocaleLowerCase().includes(needle)) continue;
      const g = current ? current.of(item) : { id: "", label: "" };
      const slot = out.get(g.id) ?? { id: g.id, label: g.label, hint: g.hint, order: g.order ?? 0, items: [] };
      slot.items.push(item);
      out.set(g.id, slot);
    }
    return Array.from(out.values()).sort((a, b) => a.order - b.order || a.label.localeCompare(b.label));
  }, [items, needle, current, text]);
  const tree: TreeListItemData[] = groups.map((group) => ({
    id: group.id || group.label || "_",
    label: group.label || group.hint || "",
    isExpanded: true,
    endContent: <Text type="supporting" hasTabularNumbers>{group.items.length}</Text>,
    children: group.items.map((item) => ({
      id: id(item),
      label: row(item),
      isSelected: selected === id(item),
      onClick: onSelect ? () => onSelect(item) : undefined,
    })),
  }));
  return <div className={cn("grid min-w-0 content-start gap-2", className)}>
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={t("Search")} placeholder={t("Search")} value={q} onChange={(e) => setQ(e.target.value)} className="h-8 min-w-0 flex-1 text-xs" />
      {groupings.length > 1 && <label className="flex items-center gap-1 text-xs text-muted">{t("Group by")}
        <Select aria-label={t("Group by")} value={current?.id} onChange={(e) => (onGrouping ?? setOwnGrouping)(e.target.value)} className="h-8 text-xs">
          {groupings.map((g) => <option key={g.id} value={g.id}>{g.label}</option>)}
        </Select></label>}
      {toolbar}
    </div>
    {!groups.length ? <Text type="supporting">{needle ? t("Nothing matches.") : empty ?? t("Nothing here yet.")}</Text> : <TreeList aria-label={t("Grouped records")} items={tree} density={dense ? "compact" : "balanced"} variant="lineGuides" />}
  </div>;
}
