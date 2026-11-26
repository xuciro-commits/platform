import { PersonLabel } from "./MemberField";
import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { useContainerReveal, useMediaQuery } from "@astryxdesign/core/hooks";
import type { UseContainerRevealReturn } from "@astryxdesign/core/hooks";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, LayoutPanel, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { ResizeHandle, useResizable } from "@astryxdesign/core/Resizable";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Tab, TabList } from "@astryxdesign/core/TabList";
import { Table, pixel, proportional, useTableRowStatus } from "@astryxdesign/core/Table";
import type { TableColumn, TablePlugin } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { Toolbar } from "@astryxdesign/core/Toolbar";
import { VisuallyHidden } from "@astryxdesign/core/VisuallyHidden";
import { ArrowLeftIcon, ArrowPathIcon, CheckIcon, EnvelopeIcon, XMarkIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { PageTitle, useGutter } from "./frame";
import { usePath } from "../router";
import { useIsDark } from "../shell/appearance";
import { reasonOf, send } from "./act";
import { type InboxItem, type InboxTab, inTab, inboxTabs, useInbox } from "./inbox";
import { motionEases, motionStyles, motionTimings, prefersReducedMotion } from "./motion";
import { ReferenceLink } from "./ReferenceField";
import { useRecordHref } from "../apps/entries";
import { AccessRequestDecisionCard } from "./rebac/AccessRequestDecisionCard";
import { todayISO, useMemberDirectoryMap, useUnitChoices } from "./rebac/shared";

const base = "/home/inbox";
const HOVER_INTENT_MS = 90;
const SINGLE_SURFACE_BELOW = 900;
const STACK_ROWS_BELOW = 560;
const PANE_DEFAULT_SHARE = 0.55;
const PANE_MIN_WIDTH = 400;
const LIST_MIN_WIDTH = 320;
const MESSAGE_MEASURE = 680;
const OPEN_CELL_STYLE = { backgroundColor: "var(--color-accent-muted)" };
const styles = stylex.create({
  claimCell: { flexBasis: "100%", flexShrink: 0 },
  clip: { overflow: "hidden" },
  emptyArtwork: (dark: boolean) => ({
    width: 240,
    aspectRatio: "16 / 10",
    backgroundImage: "url(/media/anatomy/inbox-empty." + (dark ? "dark" : "light") + ".svg)",
    backgroundPosition: "center",
    backgroundSize: "contain",
    backgroundRepeat: "no-repeat",
  }),
});
const INTERACTIVE_WITHIN_ROW = 'button, input, a, label, dialog, [role="checkbox"], [role="button"], [role="menuitem"]';

function isRowItself(event: { target: EventTarget | null; currentTarget: EventTarget | null }) {
  const control = (event.target as HTMLElement | null)?.closest(INTERACTIVE_WITHIN_ROW);
  const row = event.currentTarget as HTMLElement;
  return !control || control === row || !row.contains(control);
}

function useSurfaceWidth() {
  const ref = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  useLayoutEffect(() => {
    const node = ref.current;
    if (!node) return;
    const { paddingInlineStart, paddingInlineEnd } = getComputedStyle(node);
    setWidth(node.offsetWidth - parseFloat(paddingInlineStart) - parseFloat(paddingInlineEnd));
    const observer = new ResizeObserver((entries) => entries[0] && setWidth(entries[0].contentRect.width));
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  return [ref, width] as const;
}

type Act = (item: InboxItem, schema: string, payload?: Record<string, unknown>, silent?: boolean) => void;

function useActions() {
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const toast = useToast();
  const [failure, setFailure] = useState("");
  const hasUnreadAction = catalog.status === "ready" && catalog.data.some((candidate) => candidate.schema === "platform.notification.unread");
  const act: Act = useCallback(
    async (item, schema, payload = {}, silent = false) => {
      const action = catalog.status === "ready" ? catalog.data.find((candidate) => candidate.schema === schema) : undefined;
      if (!action) return;
      setFailure("");
      try {
        const isNote = schema === "platform.notification.read" || schema === "platform.notification.unread";
        const target = isNote ? item.note : item.task;
        if (!target) return;
        const kept = await send(action, target.id, payload, "revision" in target ? target.revision : undefined);
        reread();
        if (!silent) {
          const reverseSchema = action.reverse || (schema === "platform.notification.read" ? "platform.notification.unread" : schema === "work.task.complete" ? "work.task.reopen" : undefined);
          const back = reverseSchema && catalog.status === "ready" ? catalog.data.find((candidate) => candidate.schema === reverseSchema) : undefined;
          const label = schema === "platform.notification.read" ? t("Marked as read") : schema === "platform.notification.unread" ? t("Marked as unread") : t("Done");
          if (back) {
            const dismiss = toast({
              body: `${label}: “${item.title}”`,
              endContent: (
                <Button
                  label={t("Undo")}
                  variant="ghost"
                  onClick={async () => {
                    dismiss();
                    try {
                      await send(back, target.id, {}, kept.revision);
                      reread();
                    } catch (problem) {
                      toast({ body: reasonOf(problem), type: "error" });
                    }
                  }}
                />
              ),
            });
          } else {
            toast({ body: `${label}: “${item.title}”` });
          }
        }
      } catch (problem) {
        setFailure(reasonOf(problem));
      }
    },
    [catalog, toast],
  );
  return { act, failure, hasUnreadAction };
}

function ReceivedCell({ item, reveal, act, isOverlaid }: { item: InboxItem; reveal: UseContainerRevealReturn; act: Act; isOverlaid: boolean }) {
  const label = item.kind === "task" ? t("Done") : t("Mark as read");
  const can = item.kind === "task" ? item.open && !item.task?.answers?.length : item.kind === "notification" && item.unread;
  return (
    <HStack gap={1} vAlign="center" hAlign="end" height={28}>
      {can && (
        <HStack {...reveal.getContentRevealProps()} xstyle={isOverlaid ? styles.claimCell : undefined} gap={0.5} vAlign="center" hAlign="end">
          <IconButton
            variant="ghost"
            size="sm"
            label={`${label} — ${item.title}`}
            tooltip={label}
            icon={<Icon icon={CheckIcon} size="sm" />}
            onClick={() => act(item, item.kind === "task" ? "work.task.complete" : "platform.notification.read")}
          />
        </HStack>
      )}
      <StackItem size="fill">
        <HStack {...(isOverlaid || !can ? {} : reveal.getContentRevealProps({ isRevealInverted: true }))} xstyle={styles.clip} hAlign="end">
          <Text color="secondary" hasTabularNumbers justify="end" maxLines={1} hasTruncateTooltip={false} weight={item.unread ? "semibold" : "normal"}>
            {item.at && <Timestamp value={item.at} format="relative" />}
          </Text>
        </HStack>
      </StackItem>
    </HStack>
  );
}

const kindOf = (item: InboxItem) => t(item.record?.kind ?? (item.kind === "task" ? "Task" : "Notification"));

function RelatedRecord({ reference }: { reference: string }) {
  const [type, id] = reference.split("/");
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const record = useRead<Api.RecordView>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`);
  const entity = types.status === "ready" ? types.data.find((candidate) => candidate.type === type) : undefined;
  const values = record.status === "ready" ? record.data.values : undefined;
  const name = entity?.display && values?.[entity.display] != null ? String(values[entity.display]) : id;
  if (record.status !== "ready") return <Text type="body">{id}</Text>;
  return <ReferenceLink type={type} id={id} name={name} />;
}

function InboxAccessRequestSection({ requestId }: { requestId: string }) {
  const requests = useRead<Api.AccessRequest[]>("/v1/access-requests");
  const apps = useRead<Api.AppInfo[]>("/v1/apps");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const { nameOf: memberName } = useMemberDirectoryMap();
  const { unitName } = useUnitChoices();
  if (requests.status !== "ready") return null;
  const found = requests.data.find((candidate) => candidate.id === requestId);
  if (!found) return null;
  const appTitle = (id?: string) => (apps.status === "ready" ? apps.data.find((entry) => entry.id === id)?.title ?? id ?? "" : id ?? "");
  return (
    <AccessRequestDecisionCard
      request={found}
      appTitle={appTitle}
      unitLabel={unitName}
      memberName={memberName}
      today={todayISO()}
      actions={catalog.status === "ready" ? catalog.data : []}
    />
  );
}

function ItemPane({ item, act, onClose, isFullWidth, canMarkUnread }: { item: InboxItem; act: Act; onClose: () => void; isFullWidth: boolean; canMarkUnread: boolean }) {
  const marked = useRef(new Set<string>());
  useEffect(() => {
    if (!item.unread || marked.current.has(item.id)) return;
    marked.current.add(item.id);
    act(item, "platform.notification.read", {}, true);
  }, [item, act]);
  const task = item.task;
  const gutter = useGutter();
  const unreadButton = canMarkUnread && item.kind === "notification" && !item.unread ? (
    <IconButton
      variant="ghost"
      label={t("Mark as unread")}
      tooltip={t("Mark as unread")}
      icon={<Icon icon={EnvelopeIcon} size="sm" />}
      onClick={() => act(item, "platform.notification.unread")}
    />
  ) : null;
  const close = (
    <HStack gap={1} vAlign="center">
      {unreadButton}
      <IconButton variant="ghost" label={t("Close")} tooltip={t("Close")} icon={<Icon icon={XMarkIcon} size="sm" />} onClick={onClose} />
    </HStack>
  );
  const accessRequestId = task?.ref === "read:access-requests" && task.key?.startsWith("access:") ? task.key.slice(7) : undefined;
  const record = item.ref && !item.ref.startsWith("work.task/") && !item.ref.startsWith("read:") ? item.ref : undefined;
  const recordHref = useRecordHref();
  return (
    <VStack key={item.id} height="100%" xstyle={motionStyles.tabEnter}>
      <VStack paddingBlockStart={gutter} paddingInline={isFullWidth ? 0 : gutter}>
        <Toolbar
          label={t("Item")}
          startContent={
            isFullWidth ? (
              <Button label={t("Inbox")} variant="ghost" icon={<Icon icon={ArrowLeftIcon} size="sm" />} onClick={onClose} />
            ) : (
              <Heading level={2} maxLines={2} hasTruncateTooltip>
                {item.title}
              </Heading>
            )
          }
          endContent={isFullWidth ? unreadButton : close}
        />
      </VStack>
      <StackItem size="fill">
        <VStack paddingInline={isFullWidth ? 0 : gutter} paddingBlockEnd={gutter} gap={4} isScrollable height="100%">
          {isFullWidth && (
            <Heading level={2} maxLines={2}>
              {item.title}
            </Heading>
          )}
          <HStack gap={2} vAlign="center">
            <Token size="sm" label={kindOf(item)} />
            {task && <Token size="sm" color={task.state === "open" ? "blue" : "default"} label={t(task.state === "open" ? "Open" : task.state === "done" ? "Done" : "Canceled")} />}
          </HStack>
          {item.body && (
            <VStack maxWidth={MESSAGE_MEASURE}>
              <Text>{item.body}</Text>
            </VStack>
          )}
          <MetadataList>
            {record && (
              <MetadataListItem label={t("Record")}>
                <RelatedRecord reference={record} />
              </MetadataListItem>
            )}
            <MetadataListItem label={t("From")}>
              <PersonLabel name={item.from} />
            </MetadataListItem>
            {task?.assignee && (
              <MetadataListItem label={t("Assignee")}>
                <PersonLabel name={task.assignee} />
              </MetadataListItem>
            )}
            {item.record?.due && (
              <MetadataListItem label={t("Due")}>
                <Timestamp value={item.record.due.length === 10 ? `${item.record.due}T00:00:00` : item.record.due} format="date" type="body" color="primary" />
              </MetadataListItem>
            )}
            {task?.due && (
              <MetadataListItem label={t("Due")}>
                <Timestamp value={task.due} format="date" type="body" color="primary" />
              </MetadataListItem>
            )}
            {task?.answer && (
              <MetadataListItem label={t("Answer")}>
                <Text type="body">{task.answerTitles?.[task.answers?.indexOf(task.answer) ?? -1] ?? task.answer}</Text>
              </MetadataListItem>
            )}
          </MetadataList>
          {item.record && (
            <HStack gap={2}>
              <Button label={t("Open full page")} variant="primary" href={recordHref(item.record.type, item.record.id)} />
            </HStack>
          )}
          {accessRequestId && <InboxAccessRequestSection requestId={accessRequestId} />}
          {task?.state === "open" && !accessRequestId && (
            <HStack gap={2} wrap="wrap">
              {task.answers?.length ? (
                task.answers.map((answer, index) => (
                  <Button key={answer} label={t(task.answerTitles?.[index] ?? answer)} variant={index === 0 ? "primary" : "secondary"} onClick={() => act(item, "work.task.complete", { answer })} />
                ))
              ) : (
                <Button label={t("Done")} variant="primary" icon={<Icon icon={CheckIcon} size="sm" />} onClick={() => act(item, "work.task.complete")} />
              )}
              {!task.assignee && <Button label={t("Take")} variant="secondary" onClick={() => act(item, "work.task.claim")} />}
            </HStack>
          )}
        </VStack>
      </StackItem>
    </VStack>
  );
}

export function InboxPage() {
  const asked = usePath().slice(base.length + 1) as InboxTab;
  const tab = inboxTabs.some((candidate) => candidate.value === asked) ? asked : "all";
  const inbox = useInbox();
  const gutter = useGutter();
  const dark = useIsDark();
  const { act, failure, hasUnreadAction } = useActions();
  const [chosenId, setChosenId] = useState<string | null | undefined>(undefined);
  const visible = useMemo(() => inbox.items.filter((item) => inTab(item, tab)), [inbox.items, tab]);
  const unread = useMemo(() => Object.fromEntries(inboxTabs.map(({ value }) => [value, inbox.items.filter((item) => item.unread && inTab(item, value)).length])), [inbox.items]);
  const reveal = useContainerReveal();
  const [surfaceRef, surfaceWidth] = useSurfaceWidth();
  const [listRef, listWidth] = useSurfaceWidth();
  const rowTops = useRef<Map<string, number>>(new Map());
  const visibleOrder = visible.map((item) => item.id).join(",");
  const isSingleSurface = 0 < surfaceWidth && surfaceWidth < SINGLE_SURFACE_BELOW;
  const isStacked = 0 < listWidth && listWidth < STACK_ROWS_BELOW;
  const isTouch = useMediaQuery("(hover: none)");
  const openId = chosenId === null ? null : chosenId !== undefined && visible.some((item) => item.id === chosenId) ? chosenId : isSingleSurface ? null : (visible[0]?.id ?? null);
  const opened = visible.find((item) => item.id === openId) ?? null;
  const isPaneFullWidth = isSingleSurface && opened != null;

  const moved = useRef(false);
  const placing = useRef(false);
  const pane = useResizable({
    defaultSize: "55%",
    minSize: PANE_MIN_WIDTH,
    maxSize: surfaceWidth > 0 ? Math.max(PANE_MIN_WIDTH, surfaceWidth - LIST_MIN_WIDTH) : undefined,
    onSizeChange: useCallback(() => {
      if (!placing.current) moved.current = true;
    }, []),
  });
  const { resize } = pane;
  useLayoutEffect(() => {
    if (moved.current || surfaceWidth <= 0) return;
    placing.current = true;
    resize(Math.round(surfaceWidth * PANE_DEFAULT_SHARE));
    placing.current = false;
  }, [surfaceWidth, resize]);

  useLayoutEffect(() => {
    const root = listRef.current;
    if (!root) return;
    const nodes = [...root.querySelectorAll<HTMLElement>("[data-inbox-row]")];
    const nextTops = new Map<string, number>();
    for (const node of nodes) {
      const id = node.dataset.inboxRow;
      if (!id) continue;
      const top = node.getBoundingClientRect().top;
      nextTops.set(id, top);
      const previous = rowTops.current.get(id);
      if (previous !== undefined && !prefersReducedMotion()) {
        const delta = previous - top;
        if (Math.abs(delta) > 1 && Math.abs(delta) < 320) {
          node.animate(
            [{ transform: `translate3d(0, ${delta}px, 0)` }, { transform: "none" }],
            { duration: motionTimings.slot, easing: motionEases.slot },
          );
        }
      }
    }
    rowTops.current = nextTops;
  }, [visibleOrder, listRef]);

  const rowStatus = useTableRowStatus<InboxItem>({
    getStatus: useCallback((item: InboxItem) => (item.unread ? { color: "accent" as const, label: t("Unread") } : null), []),
  });
  const rows: TablePlugin<InboxItem> = useMemo(
    () => ({
      transformBodyRow: (props, item) => {
        const container = reveal.getContainerProps({ hoverDelay: HOVER_INTENT_MS });
        return {
          ...props,
          htmlProps: {
            ...props.htmlProps,
            "data-inbox-row": item.id,
            className: [props.htmlProps.className, container.className].filter(Boolean).join(" "),
            style: { ...props.htmlProps.style, ...container.style },
            tabIndex: 0,
            "aria-current": item.id === openId ? true : undefined,
            onClick: (event) => isRowItself(event) && setChosenId(item.id),
            onKeyDown: (event) => {
              if ((event.key !== "Enter" && event.key !== " ") || !isRowItself(event)) return;
              event.preventDefault();
              setChosenId(item.id);
            },
          },
        };
      },
      transformBodyCell: (props, _column, item) => (item.id === openId ? { ...props, htmlProps: { ...props.htmlProps, style: { ...props.htmlProps.style, ...OPEN_CELL_STYLE } } } : props),
    }),
    [reveal, openId],
  );

  const received: TableColumn<InboxItem> = {
    key: "at",
    header: t("Received"),
    width: pixel(isTouch ? 140 : 120),
    align: "end",
    renderCell: (item) => <ReceivedCell item={item} reveal={reveal} act={act} isOverlaid={!isTouch} />,
  };
  const columns: TableColumn<InboxItem>[] = isStacked
    ? [
        {
          key: "title",
          header: t("Item"),
          width: proportional(1, { minWidth: 160 }),
          renderCell: (item) => (
            <VStack gap={0.5}>
              <Text maxLines={1} weight={item.unread ? "semibold" : "normal"}>{item.from}</Text>
              <Text maxLines={1} color="secondary">{item.title}</Text>
            </VStack>
          ),
        },
        received,
      ]
    : [
        {
          key: "from",
          header: t("From"),
          width: pixel(170),
          renderCell: (item) => <PersonLabel name={item.from} weight={item.unread ? "semibold" : "normal"} />,
        },
        {
          key: "title",
          header: t("Subject"),
          width: proportional(3),
          renderCell: (item) => (
            <HStack gap={2} vAlign="center">
              <StackItem size="static">
                <Text maxLines={1} weight={item.unread ? "semibold" : "normal"}>{item.title}</Text>
              </StackItem>
              {item.body && (
                <StackItem size="fill">
                  <Text maxLines={1} color="secondary">— {item.body}</Text>
                </StackItem>
              )}
            </HStack>
          ),
        },
        {
          key: "kind",
          header: t("Kind"),
          width: pixel(130),
          renderCell: (item) => <Token size="sm" label={kindOf(item)} />,
        },
        received,
      ];
  const unreadCount = inbox.items.filter((item) => item.unread).length;

  return (
    <Layout
      ref={surfaceRef}
      height="fill"
      header={
        <LayoutHeader hasDivider={isPaneFullWidth} padding={0}>
          <VStack gap={0}>
            <VStack padding={gutter} paddingBlockStart={3} paddingBlockEnd={3}>
              <PageTitle title={t("Inbox")} actions={<Button label={t("Refresh")} variant="ghost" icon={<Icon icon={ArrowPathIcon} size="sm" />} onClick={reread} />}>
                <Text type="supporting">
                  {unreadCount} {t("unread")}
                </Text>
              </PageTitle>
            </VStack>
            {!isPaneFullWidth && (
              <VStack paddingInline={gutter}>
                <TabList hasDivider value={tab} onChange={() => undefined}>
                  {inboxTabs.map((option) => (
                    <Tab
                      key={option.value}
                      value={option.value}
                      label={t(option.label)}
                      href={option.value === "all" ? base : `${base}/${option.value}`}
                      endContent={
                        unread[option.value] ? (
                          <Badge variant="neutral" label={<>{unread[option.value]}<VisuallyHidden> {t("unread")}</VisuallyHidden></>} />
                        ) : undefined
                      }
                    />
                  ))}
                </TabList>
              </VStack>
            )}
          </VStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent role="main" ref={listRef} padding={gutter} isScrollable>
          {failure && <VStack xstyle={motionStyles.bannerEnter}><Banner status="error" title={t("Not done")} description={failure} /></VStack>}
          {inbox.status === "loading" && (
            <VStack gap={2}>
              {[0, 1, 2, 3].map((index) => <Skeleton key={index} height={36} index={index} />)}
            </VStack>
          )}
          {(inbox.status === "error" || inbox.status === "refused") && <Banner status="error" title={t("The inbox could not be loaded")} description={inbox.reason || t("Reload the page to try again.")} />}
          {inbox.status === "ready" &&
            (isPaneFullWidth && opened ? (
              <ItemPane item={opened} act={act} onClose={() => setChosenId(null)} isFullWidth canMarkUnread={hasUnreadAction} />
            ) : visible.length === 0 ? (
              <VStack padding={6} hAlign="center" gap={3} xstyle={motionStyles.fadeEnter}>
                <Card variant="transparent" padding={0} xstyle={styles.emptyArtwork(dark)} />
                <EmptyState title={t("Nothing here")} description={t("Work assigned to you and notifications about your records arrive here.")} />
              </VStack>
            ) : (
              <Table data={visible} columns={columns} idKey="id" plugins={{ rowStatus, rows }} density="balanced" dividers="none" hasHover textOverflow="truncate" />
            ))}
        </LayoutContent>
      }
      end={
        isSingleSurface || !opened ? undefined : (
          <>
            <ResizeHandle direction="horizontal" isReversed hasDivider pillPlacement="end" resizable={pane.props} label={t("Resize the reading pane")} />
            <LayoutPanel resizable={pane.props} padding={0} role="complementary" label={t("Item")}>
              <ItemPane item={opened} act={act} onClose={() => setChosenId(null)} isFullWidth={false} canMarkUnread={hasUnreadAction} />
            </LayoutPanel>
          </>
        )
      }
    />
  );
}
