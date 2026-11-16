import { CommandPaletteFooter, CommandPaletteGroup, CommandPaletteInput, CommandPaletteItem, CommandPaletteList } from "@astryxdesign/core/CommandPalette";
import { Dialog } from "@astryxdesign/core/Dialog";
import { HStack, Stack, StackItem } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { t } from "../i18n";
import { Button } from "../primitives/button";
import type { SearchApi, SearchResult } from "./search";

type Group = { kind: string; label: string; items: SearchResult[] };

export function CommandPalette({ open, onOpenChange, search }: { open: boolean; onOpenChange: (open: boolean) => void; search: SearchApi }) {
  const [query, setQuery] = useState("");
  const [groups, setGroups] = useState<Group[]>([]);
  const [errors, setErrors] = useState<string[]>([]);
  const [highlighted, setHighlighted] = useState(0);
  const [busy, setBusy] = useState(false);
  const ticket = useRef(0);
  const beside = useRef(false);
  const prefix = useId();
  const listId = `${prefix}-list`;
  const itemId = (index: number) => `${prefix}-item-${index}`;
  const flat = useMemo(() => groups.flatMap((group) => group.items), [groups]);

  const run = useCallback((text: string) => {
    const current = ++ticket.current;
    setBusy(true);
    setErrors([]);
    void search.search(text).then((answer) => {
      if (current !== ticket.current) return;
      setGroups(answer.groups);
      setErrors(answer.errors);
    }).catch((error: unknown) => {
      if (current !== ticket.current) return;
      setGroups([]);
      setErrors([error instanceof Error ? error.message : String(error)]);
    }).finally(() => {
      if (current === ticket.current) setBusy(false);
    });
  }, [search]);

  useEffect(() => {
    if (!open) {
      ticket.current++;
      setQuery("");
      setGroups([]);
      setErrors([]);
      setHighlighted(0);
      setBusy(false);
      return;
    }
    const handle = setTimeout(() => run(query), 120);
    return () => clearTimeout(handle);
  }, [open, query, run]);

  useEffect(() => { setHighlighted(0); }, [groups]);

  const openItem = (item: SearchResult) => {
    onOpenChange(false);
    search.open(item, beside.current ? "beside" : undefined);
    beside.current = false;
  };

  const onKeyDown = (event: KeyboardEvent) => {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const step = event.key === "ArrowDown" ? 1 : -1;
      setHighlighted((current) => Math.max(0, Math.min(flat.length - 1, current + step)));
      return;
    }
    if (event.key === "Enter") {
      event.preventDefault();
      const item = flat[highlighted];
      beside.current = event.metaKey || event.ctrlKey;
      if (item) openItem(item);
    }
  };

  return <Dialog isOpen={open} onOpenChange={(next) => onOpenChange(next)} aria-label={t("Command palette")} width={600} maxHeight={480} position={{ top: 96 }}>
    <Stack gap={2} padding={2}>
      <CommandPaletteInput label={t("Command palette")} role="combobox" value={query} onValueChange={setQuery} onKeyDown={onKeyDown}
        aria-expanded={open} aria-controls={groups.length ? listId : undefined} aria-activedescendant={flat[highlighted] ? itemId(highlighted) : undefined}
        placeholder={t("Search, or type > commands, @ members, # projects, : object types")} />
      {groups.length > 0
        ? <CommandPaletteList id={listId} label={t("Commands")}>
          {groups.map((group) => <CommandPaletteGroup key={group.kind} heading={group.label}>
            {group.items.map((item) => <CommandPaletteItem key={item.id} value={item.id} isDisabled={!!item.disabled} isHighlighted={flat.indexOf(item) === highlighted} onSelect={() => openItem(item)}>
              <HStack gap={2} width="100%">{item.icon}<StackItem size="fill"><Text maxLines={1}>{item.label}</Text></StackItem>{item.detail && <Text type="supporting" maxLines={1}>{item.detail}</Text>}</HStack>
            </CommandPaletteItem>)}
          </CommandPaletteGroup>)}
        </CommandPaletteList>
        : <Text as="p" role="status" type="supporting">{busy ? t("Searching…") : query.trim() ? t("No results") : t("Type to search")}</Text>}
      {errors.length > 0 && <CommandPaletteFooter>
        <HStack gap={2} role="alert"><Text color="primary">{t("Search failed.")} {errors.join(" ")}</Text><Button size="sm" variant="ghost" onClick={() => onOpenChange(false)}>{t("Close")}</Button></HStack>
      </CommandPaletteFooter>}
    </Stack>
  </Dialog>;
}