import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { BottomSheet } from "@astryxdesign/core/BottomSheet";
import { Button } from "@astryxdesign/core/Button";
import { CheckboxInput } from "@astryxdesign/core/CheckboxInput";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";
import { Section } from "@astryxdesign/core/Section";
import { Selector } from "@astryxdesign/core/Selector";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { ToggleButton } from "@astryxdesign/core/ToggleButton";
import { Token } from "@astryxdesign/core/Token";
import { BookmarkIcon, BookmarkSquareIcon, PencilIcon, XMarkIcon } from "@heroicons/react/24/outline";
import { BookmarkIcon as BookmarkSolidIcon } from "@heroicons/react/24/solid";
import * as stylex from "@stylexjs/stylex";
import { type ReactNode, useState } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { reasonOf, send } from "./act";
import { chipChrome } from "./ListFilters";
import { readView, sameView, type ViewState } from "./view";

type Preference = { revision: number; pinned?: string[]; default?: string };
type Scope = "self" | "project" | "tenant";
type Draft = { title: string; scope: Scope; project: string; pinned: boolean; preferred: boolean };
type Work = { (): Promise<void> };

const styles = stylex.create({
  dialogHeaderBleed: {
    marginInlineStart: "calc(-1 * var(--container-padding-inline-start, 0px))",
    marginInlineEnd: "calc(-1 * var(--container-padding-inline-end, 0px))",
    marginBlockStart: "calc(-1 * var(--container-padding-block-start, 0px))",
  },
  label: { flexShrink: 0 },
  slot: { flexShrink: 1, minWidth: 0 },
  scroller: { overflowX: "auto", overflowY: "hidden", overscrollBehaviorInline: "contain", scrollbarWidth: "none" },
  chip: { flexShrink: 0 },
  tokens: { flexWrap: "wrap", rowGap: 4 },
  token: { minWidth: 0, maxWidth: "100%" },
});

type SavedViewsProps = {
  entity: string;
  view: ViewState;
  fallback: ViewState;
  columnCount: number;
  filterLabel: (filter: PowerSearchFilter) => string;
  compact: boolean;
};

export function useSavedViews({ entity, view, fallback, columnCount, filterLabel, compact }: SavedViewsProps) {
  const path = usePath();
  const search = useSearch();
  const selected = new URLSearchParams(search).get("saved") ?? "";
  const [creating, setCreating] = useState<string | null>(null);
  const [editing, setEditing] = useState<Draft | null>(null);
  const [removing, setRemoving] = useState(false);
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const views = useRead<Api.SavedView[]>("/v1/views");
  const me = useRead<Api.MeView>("/v1/me");
  const projects = useRead<Api.RecordPage>(editing?.scope === "project" ? "/v1/records/build.app?limit=200" : null);
  const principal = me.status === "ready" ? me.data.principalId : "";
  const preference = useRead<Api.RecordView>(principal ? `/v1/records/work.viewpreference/${encodeURIComponent(`${principal}@${entity}`)}` : null);
  const pref = preference.status === "ready" ? (preference.data.record as Preference) : undefined;
  const visible = views.status === "ready" ? views.data.filter((candidate) => candidate.entity === entity) : [];
  const ordered = [...visible].sort((left, right) => Number(pref?.pinned?.includes(right.id)) - Number(pref?.pinned?.includes(left.id)));
  const current = ordered.find((candidate) => candidate.id === selected);
  const owned = current?.owner === principal;
  const actions = catalog.status === "ready" ? catalog.data : [];
  const action = (schema: string) => actions.find((candidate) => candidate.schema === schema);
  const saveAction = action("work.view.save");
  const removeAction = action("work.view.remove");
  const shareAction = action("work.view.share");
  const preferenceAction = action("work.viewpreference.set");
  const changed = current ? !sameView(readView(current.state, fallback), view) : false;

  const go = (next: URLSearchParams) => navigate(path + (next.size ? "?" + next : ""));
  const apply = (candidate: Api.SavedView | null) => {
    const next = new URLSearchParams(search);
    if (candidate) {
      next.set("view", JSON.stringify(readView(candidate.state, fallback)));
      next.set("saved", candidate.id);
    } else {
      next.delete("view");
      next.delete("saved");
    }
    go(next);
  };
  const attempt = async (work: Work) => {
    setBusy(true);
    setFailure("");
    try {
      await work();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
  const create = (title: string) =>
    attempt(async () => {
      if (!saveAction) return;
      const id = `VIEW-${crypto.randomUUID()}`;
      await send(saveAction, id, { title: title.trim(), entity, state: JSON.stringify(view) }, 0);
      setCreating(null);
      reread();
      const next = new URLSearchParams(search);
      next.set("saved", id);
      go(next);
    });
  const edit = () => {
    if (!current) return;
    setFailure("");
    setEditing({
      title: current.title,
      scope: (current.share as Scope | undefined) ?? "self",
      project: current.project ?? "",
      pinned: Boolean(pref?.pinned?.includes(current.id)),
      preferred: pref?.default === current.id,
    });
  };
  const saveEdit = (draft: Draft, state?: string) =>
    attempt(async () => {
      if (!current) return;
      if (owned && saveAction && (draft.title.trim() !== current.title || state)) {
        await send(saveAction, current.id, { title: draft.title.trim(), entity, state: state ?? current.state }, current.revision);
      }
      if (owned && shareAction && (draft.scope !== ((current.share as Scope | undefined) ?? "self") || draft.project !== (current.project ?? ""))) {
        await send(shareAction, current.id, { scope: draft.scope, project: draft.scope === "project" ? draft.project : "" }, current.revision + (draft.title.trim() !== current.title || state ? 1 : 0));
      }
      if (preferenceAction && principal) {
        const pinned = Boolean(pref?.pinned?.includes(current.id));
        const preferred = pref?.default === current.id;
        if (draft.pinned !== pinned || (draft.preferred && !preferred)) {
          await send(preferenceAction, `${principal}@${entity}`, { view: current.id, ...(draft.pinned !== pinned ? { pinned: draft.pinned } : {}), ...(draft.preferred && !preferred ? { default: true } : {}) }, pref?.revision ?? 0);
        }
      }
      setEditing(null);
      reread();
    });
  const remove = () =>
    attempt(async () => {
      if (!removeAction || !current) return;
      await send(removeAction, current.id, {}, current.revision);
      setRemoving(false);
      setEditing(null);
      reread();
      apply(null);
    });

  const summary = (state: ViewState) => (
    <MetadataList columns="single" label={{ position: "start", width: 104 }}>
      <MetadataListItem label={t("Filters")}>
        {state.filters.length === 0 && !state.search ? (
          <Text type="body">{t("None")}</Text>
        ) : (
          <HStack gap={1} xstyle={styles.tokens}>
            {state.search && <Token label={`“${state.search}”`} size="sm" xstyle={styles.token} />}
            {state.filters.map((filter, index) => (
              <Token key={index} label={filterLabel(filter)} size="sm" xstyle={styles.token} />
            ))}
          </HStack>
        )}
      </MetadataListItem>
      <MetadataListItem label={t("Columns")}>
        <Text type="body">{`${state.columns.length} / ${columnCount}`}</Text>
      </MetadataListItem>
      <MetadataListItem label={t("Density")}>
        <Text type="body">{t(state.density === "compact" ? "Compact" : state.density === "spacious" ? "Spacious" : "Balanced")}</Text>
      </MetadataListItem>
      <MetadataListItem label={t("Layout")}>
        <Text type="body">{t(state.layout === "board" ? "Board" : "Table")}</Text>
      </MetadataListItem>
    </MetadataList>
  );

  const bar = (
    <HStack gap={2} vAlign="center" wrap="nowrap" minHeight={32}>
      <Text type="label" xstyle={styles.label}>
        {t("Saved views")}:
      </Text>
      <StackItem xstyle={styles.slot}>
        <HStack gap={2} vAlign="center" wrap="nowrap" xstyle={styles.scroller}>
          <ToggleButton
            label={t("All")}
            size="sm"
            isPressed={!current}
            xstyle={[styles.chip, chipChrome.chrome, current ? chipChrome.empty : undefined, current && chipChrome.surface]}
            onPressedChange={() => apply(null)}
          />
          {ordered.map((candidate) => {
            const pressed = candidate.id === selected;
            return (
              <ToggleButton
                key={candidate.id}
                label={candidate.title}
                size="sm"
                isPressed={pressed}
                icon={<Icon icon={BookmarkIcon} size="sm" />}
                xstyle={[styles.chip, chipChrome.chrome, !pressed && chipChrome.empty, !pressed && chipChrome.surface]}
                onPressedChange={(next) => apply(next ? candidate : null)}
              />
            );
          })}
          {current && <IconButton label={t("Edit saved view")} tooltip={t("Edit saved view")} variant="ghost" size="sm" icon={<Icon icon={PencilIcon} size="sm" />} xstyle={styles.chip} onClick={edit} />}
        </HStack>
      </StackItem>
    </HStack>
  );

  const createButton = (
    <IconButton label={t("Create saved view")} tooltip={t("Create saved view")} variant="ghost" size="sm" isDisabled={!saveAction} icon={<Icon icon={BookmarkSquareIcon} size="sm" />} onClick={() => { setFailure(""); setCreating(""); }} />
  );

  const dialogs = (
    <>
      {creating != null && <Adaptive isOpen onOpenChange={(open) => !open && setCreating(null)} title={t("Create saved view")} subtitle={t("Captures the filters and the table as they are now.")} compact={compact}>
        {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
        <TextInput label={t("View name")} value={creating ?? ""} onChange={setCreating} hasAutoFocus />
        <VStack gap={1}>
          <Text type="label">{t("This view saves")}</Text>
          {summary(view)}
        </VStack>
        <HStack hAlign="end">
          <Button label={t("Create")} variant="primary" isDisabled={busy || !(creating ?? "").trim()} onClick={() => create(creating ?? "")} />
        </HStack>
      </Adaptive>}
      {editing && <Adaptive isOpen onOpenChange={(open) => !open && setEditing(null)} title={t("Edit saved view")} subtitle={current?.title ?? ""} compact={compact}>
        {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
        {editing && current && (
          <>
            <TextInput label={t("View name")} value={editing.title} isDisabled={!owned} onChange={(title) => setEditing({ ...editing, title })} />
            {owned && (
              <Selector
                label={t("Who can read this view")}
                value={editing.scope}
                onChange={(scope) => setEditing({ ...editing, scope: scope as Scope })}
                options={[
                  { value: "self", label: t("Only me") },
                  { value: "project", label: t("Project") },
                  { value: "tenant", label: t("Everyone in this workspace") },
                ]}
              />
            )}
            {owned && editing.scope === "project" && (
              <Selector
                label={t("Project")}
                value={editing.project}
                onChange={(project) => setEditing({ ...editing, project })}
                options={projects.status === "ready" ? (projects.data.records as { id: string; title: string }[]).map((entry) => ({ value: entry.id, label: entry.title })) : []}
              />
            )}
            <CheckboxInput label={t("Pin to the top")} value={editing.pinned} onChange={(pinned) => setEditing({ ...editing, pinned })} />
            <CheckboxInput label={t("Open this view by default")} value={editing.preferred} onChange={(preferred) => setEditing({ ...editing, preferred })} />
            <VStack gap={1}>
              <Text type="label">{t("This view saves")}</Text>
              {summary(readView(current.state, fallback))}
            </VStack>
            <HStack gap={2} vAlign="center" wrap="wrap">
              {owned && <Button label={t("Remove view")} variant="destructive" isDisabled={busy || !removeAction} onClick={() => setRemoving(true)} />}
              <StackItem size="fill">
                <HStack gap={2} hAlign="end">
                  {owned && changed && <Button label={t("Save the current table into it")} variant="secondary" isDisabled={busy || !editing.title.trim()} onClick={() => saveEdit(editing, JSON.stringify(view))} />}
                  <Button label={t("Save")} variant="primary" isDisabled={busy || !editing.title.trim() || (editing.scope === "project" && !editing.project)} onClick={() => saveEdit(editing)} />
                </HStack>
              </StackItem>
            </HStack>
          </>
        )}
      </Adaptive>}
      {removing && current && (
        <AlertDialog
          isOpen
          title={`${t("Remove view")} “${current.title}”?`}
          description={t("The saved definition is archived. Its records and permissions do not change.")}
          actionLabel={t("Remove view")}
          actionVariant="destructive"
          isActionLoading={busy}
          onOpenChange={(open) => !open && setRemoving(false)}
          onAction={remove}
        />
      )}
    </>
  );

  return { bar, createButton, dialogs, toggleIcons: { off: BookmarkIcon, on: BookmarkSolidIcon } };
}

function Adaptive({ isOpen, onOpenChange, title, subtitle, compact, children }: { isOpen: boolean; onOpenChange: (open: boolean) => void; title: string; subtitle: string; compact: boolean; children: ReactNode }) {
  if (compact) {
    return (
      <BottomSheet isOpen={isOpen} onOpenChange={onOpenChange} label={title} purpose="form" height="tall">
        <Section variant="transparent" padding={4} paddingBlockStart={0}>
          <VStack gap={4}>
            <HStack gap={2}>
              <StackItem size="fill">
                <VStack gap={1}>
                  <Heading level={3}>{title}</Heading>
                  <Text type="supporting" color="secondary">
                    {subtitle}
                  </Text>
                </VStack>
              </StackItem>
              <IconButton label={t("Close")} variant="ghost" size="sm" icon={<Icon icon={XMarkIcon} size="sm" />} onClick={() => onOpenChange(false)} />
            </HStack>
            {children}
          </VStack>
        </Section>
      </BottomSheet>
    );
  }
  return (
    <Dialog isOpen={isOpen} onOpenChange={onOpenChange} purpose="form" width={440}>
      <DialogHeader title={title} subtitle={subtitle} onOpenChange={onOpenChange} xstyle={styles.dialogHeaderBleed} />
      <Section variant="transparent" padding={4}>
        <VStack gap={4}>{children}</VStack>
      </Section>
    </Dialog>
  );
}
