import { CommandPalette, CommandPaletteFooter, CommandPaletteInput } from "@astryxdesign/core/CommandPalette";
import { useHotkeys } from "@astryxdesign/core/hooks";
import { Kbd } from "@astryxdesign/core/Kbd";
import { HStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import type { SearchSource } from "@astryxdesign/core/Typeahead";
import { createContext, type ReactNode, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { type App, type NavItem, pathOf } from "../apps";
import { entries, recordHref } from "../apps/entries";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath } from "../router";

export type Command = { id: string; label: string; run: () => void };
type Destination = { href: string; label: string };
type RecordDestination = Destination & { type: string; id: string };
type Recent = Destination | RecordDestination;
type Result = Command & { auxiliaryData: { group: string } };
type SearchResults = { query: string; items: Result[] };

const SearchScope = createContext<{
  openSearch: () => void;
  remember: (destination: Recent) => void;
  register: (getCommands: () => Command[]) => () => void;
} | null>(null);

export function useCommandSearch() {
  return useContext(SearchScope);
}

export function usePageCommands(commands: Command[]) {
  const scope = useCommandSearch();
  const current = useRef(commands);
  current.current = commands;
  useLayoutEffect(() => scope?.register(() => current.current), [scope]);
}

function destination(item: Destination, group: string): Result {
  return { id: item.href, label: item.label, auxiliaryData: { group }, run: () => void navigate(item.href) };
}

export function CommandSearch({ app, item, apps, tenant, children }: { app: App; item: NavItem; apps: App[]; tenant: string; children: ReactNode }) {
  const path = usePath();
  const [open, setOpen] = useState(false);
  const [failure, setFailure] = useState("");
  const me = useRead<Api.MeView>("/v1/me");
  const storageKey = me.status === "ready" ? `platform:recent:${tenant}:${me.data.principalId}` : "";
  const registry = useRef(new Set<() => Command[]>());
  const shown = useRef<SearchResults>({ query: "", items: [] });
  const remember = useCallback((target: Recent) => {
    if (!storageKey) return;
    const recent = JSON.parse(sessionStorage.getItem(storageKey) ?? "[]") as Recent[];
    sessionStorage.setItem(storageKey, JSON.stringify([target, ...recent.filter((item) => item.href !== target.href)].slice(0, 8)));
  }, [storageKey]);
  const scope = useMemo(() => ({
    openSearch: () => setOpen(true),
    remember,
    register: (commands: () => Command[]) => {
      registry.current.add(commands);
      return () => { registry.current.delete(commands); };
    },
  }), [remember]);
  useHotkeys(["meta+k", "ctrl+k"].map((keys) => ({ keys, onPress: scope.openSearch, allowInInputs: true, isDisabled: open })));

  const targets = useMemo(() => apps.flatMap((application) => application.sections.flatMap((section) => section.items
    .filter((entry) => application.group === "installed" || Object.hasOwn(entries, `${application.id}/${entry.id}`))
    .map((entry) => ({ application, href: pathOf(application, entry), label: t(entry.label) })))), [apps]);
  const entryHref = pathOf(app, item);
  useEffect(() => {
    if ((path === "/" || path === entryHref) && targets.some((target) => target.href === entryHref)) {
      remember({ href: entryHref, label: t(item.label) });
    }
  }, [path, entryHref, item.label, remember, targets]);

  const source = useMemo<SearchSource<Result>>(() => {
    let controller: AbortController | undefined;
    const cancel = () => controller?.abort();
    return {
      cancel,
      async bootstrap() {
        cancel();
        controller = new AbortController();
        const { signal } = controller;
        setFailure("");
        const recent = storageKey ? JSON.parse(sessionStorage.getItem(storageKey) ?? "[]") as Recent[] : [];
        const checked = await Promise.allSettled(recent.map(async (target) => {
          if ("type" in target) {
            await request<Api.RecordView>(`/v1/records/${encodeURIComponent(target.type)}/${encodeURIComponent(target.id)}`, { signal });
          } else if (!targets.some((entry) => entry.href === target.href)) return null;
          return destination(target, t("Recent"));
        }));
        const results = checked.flatMap((result) => result.status === "fulfilled" && result.value ? [result.value] : []);
        if (!signal.aborted) shown.current = { query: "", items: results };
        return results;
      },
      async search(query) {
        cancel();
        controller = new AbortController();
        const { signal } = controller;
        setFailure("");
        const term = query.trim();
        const prefix = term[0];
        const scoped = [">", "@", "#", ":"].includes(prefix);
        const needle = (scoped ? term.slice(1).trim() : term).toLocaleLowerCase();
        const matches = (text: string) => text.toLocaleLowerCase().includes(needle);
        const commands = !scoped || prefix === ">" ? [...registry.current].flatMap((get) => get()).filter((command) => matches(command.label))
          .map((command) => ({ ...command, auxiliaryData: { group: t("Commands") } })) : [];
        const matched = scoped ? [] : targets.filter((target) => matches(target.label) || matches(t(target.application.label)));
        const local = [
          ...commands,
          ...matched.filter((target) => target.application.id === app.id).map((target) => destination(target, t("Current app"))),
          ...matched.filter((target) => target.application.id !== app.id).map((target) => destination(target, t("Apps"))),
        ];
        const [objects, members] = await Promise.allSettled([
          term && prefix !== ">" && prefix !== "@" ? request<Api.Hit[]>(`/v1/search?q=${encodeURIComponent(term)}`, { signal }) : [],
          !scoped || prefix === "@" ? request<Api.MemberOption[]>("/v1/member-directory", { signal }) : [],
        ]);
        const people = (members.status === "fulfilled" ? members.value : [])
          .filter((member) => matches(member.name) || matches(member.id)).slice(0, 8)
          .map((member) => destination({ href: `/admin/members?member=${encodeURIComponent(member.id)}`, label: member.name }, t("Members")));
        const found = (objects.status === "fulfilled" ? objects.value : [])
          .filter((hit) => hit.id && hit.group !== "member" && hit.resource?.canOpen !== false)
          .map((hit) => ({ resource: hit.group === "resource", item: {
            href: recordHref(hit.type, hit.id),
            label: hit.title || t(hit.typeTitle ?? hit.type),
          } }));
        const unique = [...new Map(found.map((hit) => [hit.item.href, hit])).values()];
        const resources = unique.filter((hit) => hit.resource)
          .sort((a, b) => Number(b.item.label.toLocaleLowerCase() === needle) - Number(a.item.label.toLocaleLowerCase() === needle))
          .slice(0, 8).map((hit) => destination(hit.item, t("Resources")));
        const records = unique.filter((hit) => !hit.resource).slice(0, 12).map((hit) => destination(hit.item, t("Objects")));
        const results = [...local, ...resources, ...records, ...people];
        if (!signal.aborted) {
          shown.current = { query, items: results };
          setFailure([
            objects.status === "rejected" && ((objects.reason instanceof Error && objects.reason.message) || t("Objects could not be searched")),
            members.status === "rejected" && ((members.reason instanceof Error && members.reason.message) || t("Members could not be searched")),
          ].filter(Boolean).join(" · "));
        }
        return results;
      },
    };
  }, [app.id, storageKey, targets]);

  const choose = (id: string) => {
    setOpen(false);
    shown.current.items.find((result) => result.id === id)?.run();
  };
  return (
    <SearchScope.Provider value={scope}>
      {children}
      {open && <CommandPalette
        isOpen
        onOpenChange={setOpen}
        label={t("Command palette")}
        searchSource={source}
        input={<CommandPaletteInput label={t("Search")} placeholder={t("Search…")} onKeyDown={(event) => {
          if (event.key !== "Enter" || event.nativeEvent.isComposing || event.keyCode === 229
            || event.currentTarget.getAttribute("aria-activedescendant") || shown.current.query !== event.currentTarget.value) return;
          const first = shown.current.items[0];
          if (first) {
            event.preventDefault();
            choose(first.id);
          }
        }} />}
        emptyBootstrapText={t("Type to search")}
        emptySearchText={t("No results")}
        footer={<CommandPaletteFooter>
          {failure ? <Text type="supporting">{failure}</Text> : <>
            <HStack gap={1} vAlign="center"><Kbd keys="up" /><Kbd keys="down" />{t("Navigate")}</HStack>
            <HStack gap={1} vAlign="center"><Kbd keys="enter" />{t("Select")}</HStack>
            <HStack gap={1} vAlign="center"><Kbd keys="escape" />{t("Close")}</HStack>
          </>}
        </CommandPaletteFooter>}
        onValueChange={choose}
      />}
    </SearchScope.Provider>
  );
}
