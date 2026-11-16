import { CommandPalette as AstryxCommandPalette, CommandPaletteFooter, CommandPaletteInput } from "@astryxdesign/core/CommandPalette";
import type { SearchableItem, SearchSource } from "@astryxdesign/core/Typeahead";
import { useCallback, useMemo, useRef, useState, type ReactNode } from "react";
import { Button } from "../primitives/button";
import { t } from "../i18n";
import type { SearchApi, SearchResult, SearchWindow } from "./search";

type PaletteData = { group: string; detail?: string; icon?: ReactNode; result: SearchResult };
type PaletteItem = SearchableItem<PaletteData>;

function toItems(answer: Awaited<ReturnType<SearchApi["search"]>>): PaletteItem[] {
  return answer.groups.flatMap((group) => group.items.map((result) => ({
    id: result.id,
    label: result.label,
    auxiliaryData: { group: group.label, detail: result.detail, icon: result.icon, result },
  })));
}

export function CommandPalette({ open, onOpenChange, search }: { open: boolean; onOpenChange: (open: boolean) => void; search: SearchApi }) {
  const [errors, setErrors] = useState<string[]>([]);
  const items = useRef<PaletteItem[]>([]);
  const window = useRef<SearchWindow | undefined>(undefined);

  const source = useMemo<SearchSource<PaletteItem>>(() => ({
    search: async (query) => {
      const answer = await search.search(query);
      setErrors(answer.errors);
      items.current = toItems(answer);
      return items.current;
    },
    bootstrap: async () => {
      const answer = await search.search("");
      setErrors(answer.errors);
      items.current = toItems(answer);
      return items.current;
    },
  }), [search]);

  const close = useCallback(() => {
    window.current = undefined;
    onOpenChange(false);
  }, [onOpenChange]);

  return <AstryxCommandPalette<PaletteItem>
    isOpen={open}
    onOpenChange={(next) => { if (!next) close(); else onOpenChange(true); }}
    searchSource={source}
    label={t("Command palette")}
    width={600}
    maxHeight={480}
    emptySearchText={t("No results")}
    emptyBootstrapText={t("Type to search")}
    input={<CommandPaletteInput placeholder={t("Search, or type > commands, @ members, # projects, : object types")} label={t("Command palette")} onKeyDown={(event) => { if (event.key === "Enter" && !event.defaultPrevented) window.current = event.metaKey || event.ctrlKey ? "beside" : undefined; }} />}
    footer={errors.length ? <CommandPaletteFooter><span role="alert" className="flex items-center gap-2 text-danger">{t("Search failed.")} {errors.join(" ")}<Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>{t("Close")}</Button></span></CommandPaletteFooter> : undefined}
    renderItem={(item) => {
      const data = item.auxiliaryData;
      return <span className="flex min-w-0 flex-1 items-center gap-2">{data?.icon}<span className="min-w-0 truncate">{item.label}</span>{data?.detail && <span className="ml-auto min-w-0 truncate pl-3 text-xs text-muted">{data.detail}</span>}</span>;
    }}
    onValueChange={(id) => {
      const item = items.current.find((entry) => entry.id === id);
      if (!item) return;
      const target = window.current;
      window.current = undefined;
      close();
      search.open(item.auxiliaryData!.result, target);
    }}
  />;
}
