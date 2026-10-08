// A list of resources in groups (ADR-0083 D6): every group is a heading with
// its count that folds; a search filters rows across groups and opens the
// groups that match; a "group by" switch changes what the groups mean. The
// alternative to a flat grid of cards or a long table for builders'
// inventories: object types by project, actions by object, functions by kind.
import { ChevronDown, ChevronRight } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { Input, Select } from "../primitives/input";
import { t } from "../i18n";

export type GroupBy<T> = { id: string; label: string; of: (item: T) => { id: string; label: string; hint?: string; order?: number } };

export function GroupedList<T>({ items, groupings, grouping, onGrouping, id, row, text, selected, onSelect, toolbar, empty, className, dense }: {
  items: T[];
  /** The ways the list may be grouped; the first is the default. */
  groupings: GroupBy<T>[];
  grouping?: string; onGrouping?: (id: string) => void;
  id: (item: T) => string;
  row: (item: T) => ReactNode;
  /** What the search box matches, lower-cased by the list. */
  text: (item: T) => string;
  selected?: string; onSelect?: (item: T) => void;
  toolbar?: ReactNode; empty?: ReactNode; className?: string; dense?: boolean;
}) {
  const [ownGrouping, setOwnGrouping] = useState(groupings[0]?.id ?? "");
  const current = groupings.find((g) => g.id === (grouping ?? ownGrouping)) ?? groupings[0];
  const [q, setQ] = useState("");
  const [folded, setFolded] = useState<Record<string, boolean>>({});
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
  const pad = dense ? "py-0.5" : "py-1";
  return <div className={cn("grid min-w-0 content-start gap-2", className)}>
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={t("Search")} placeholder={t("Search")} value={q} onChange={(e) => setQ(e.target.value)} className="h-8 min-w-0 flex-1 text-xs" />
      {groupings.length > 1 && <label className="flex items-center gap-1 text-xs text-muted">{t("Group by")}
        <Select aria-label={t("Group by")} value={current?.id} onChange={(e) => (onGrouping ?? setOwnGrouping)(e.target.value)} className="h-8 text-xs">
          {groupings.map((g) => <option key={g.id} value={g.id}>{g.label}</option>)}
        </Select></label>}
      {toolbar}
    </div>
    {!groups.length && <p className="py-2 text-xs text-muted">{needle ? t("Nothing matches.") : empty ?? t("Nothing here yet.")}</p>}
    <div role="tree" className="grid min-w-0 content-start">
      {groups.map((g) => {
        const shut = !needle && !!folded[g.id];
        return <div key={g.id || "_"} role="treeitem" aria-expanded={!shut} className="min-w-0">
          {(g.id || g.label) && <button type="button" onClick={() => setFolded((f) => ({ ...f, [g.id]: !f[g.id] }))}
            className="flex w-full items-center gap-1.5 rounded-sm px-1 py-1 text-left text-xs font-semibold hover:bg-row-hover">
            {shut ? <ChevronRight className="size-3 shrink-0" /> : <ChevronDown className="size-3 shrink-0" />}
            <span className="min-w-0 truncate">{g.label}</span>
            {g.hint && <span className="min-w-0 truncate font-normal text-muted">{g.hint}</span>}
            <span className="ml-auto shrink-0 font-normal tabular-nums text-muted">{g.items.length}</span>
          </button>}
          {!shut && <div role="group" className={cn("grid min-w-0", (g.id || g.label) && "ml-2 border-l border-border pl-1")}>
            {g.items.map((item) => {
              const key = id(item), active = selected === key;
              return <button type="button" key={key} role="treeitem" aria-selected={active} onClick={() => onSelect?.(item)}
                className={cn("flex w-full min-w-0 items-center gap-2 rounded-sm px-2 text-left text-xs hover:bg-row-hover", pad, active && "bg-row-selected")}>
                {row(item)}
              </button>;
            })}
          </div>}
        </div>;
      })}
    </div>
  </div>;
}
