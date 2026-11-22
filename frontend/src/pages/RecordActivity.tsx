import { Avatar } from "@astryxdesign/core/Avatar";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Switch } from "@astryxdesign/core/Switch";
import { Text } from "@astryxdesign/core/Text";
import { TextArea } from "@astryxdesign/core/TextArea";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { colorVars, radiusVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import { ChatBubbleLeftIcon, ChevronDownIcon, ChevronRightIcon, DocumentTextIcon, PencilSquareIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { type KeyboardEvent, useState } from "react";
import { type Api, type Read, useRead } from "../host";
import { t, useLocale } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { reasonOf, send } from "./act";
import { type Described, statusField } from "./describe";
import { plain, present } from "./values";

type Comment = { id: string; text: string; by: string; created: { at: string } };
type Note = { at: string; title: string; text: string; by: string };
type ChangeLine = { field: string; before?: string; after: string };
type ChangeEvent = { kind: "change"; key: string; at: string; by: string; title: string; changes: ChangeLine[] };
type Event =
  | ChangeEvent
  | { kind: "comment"; key: string; at: string; by: string; text: string }
  | { kind: "activity"; key: string; at: string; by: string; title: string; text: string };
type TimelineItem =
  | Event
  | { kind: "group"; key: string; at: string; by: string; items: ChangeEvent[] };
type Section = Read<Api.RecordSectionView>;

const styles = stylex.create({
  rail: { width: 28, flexShrink: 0, alignItems: "center" },
  line: { width: 1, flexGrow: 1, minHeight: spacingVars["--spacing-4"], backgroundColor: colorVars["--color-border"] },
  bubble: { backgroundColor: colorVars["--color-background-muted"], borderRadius: radiusVars["--radius-container"], padding: spacingVars["--spacing-3"] },
  body: { minWidth: 0, paddingBlockEnd: spacingVars["--spacing-4"] },
  stamp: { flexShrink: 0, marginInlineStart: "auto" },
});

type Stay = { value: string; label: string; from: string; until?: string; by: string };

function duration(locale: string, from: string, until?: string) {
  const minutes = Math.max(0, (Date.parse(until ?? new Date().toISOString()) - Date.parse(from)) / 60000);
  const [amount, unit] = minutes < 60 ? [minutes, "minute"] : minutes < 48 * 60 ? [minutes / 60, "hour"] : [minutes / 1440, "day"];
  return new Intl.NumberFormat(locale, { style: "unit", unit, unitDisplay: "short", maximumFractionDigits: 0 }).format(Math.floor(amount));
}

function collapse(events: Event[]): TimelineItem[] {
  const result: TimelineItem[] = [];
  for (const event of events) {
    const prev = result.at(-1);
    if (event.kind === "change" && prev && prev.kind === "change" && prev.by === event.by) {
      result[result.length - 1] = { kind: "group", key: prev.key, at: prev.at, by: prev.by, items: [prev, event] };
      continue;
    }
    if (event.kind === "change" && prev && prev.kind === "group" && prev.by === event.by) {
      prev.items.push(event);
      continue;
    }
    result.push(event);
  }
  return result;
}

export function RecordActivity({ described, id, comments, catalog }: { described: Described; id: string; comments: Section; catalog: Api.Action[] }) {
  const target = `${described.type}/${id}`;
  const root = `/v1/records/${described.type}/${encodeURIComponent(id)}/sections`;
  const activity = useRead<Api.RecordSectionView>(`${root}/activity`);
  const history = useRead<Api.RecordSectionView>(`${root}/history`);
  const comment = catalog.find((action) => action.schema === "platform.comment.add");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const toggleGroup = (key: string) => setExpanded((previous) => {
    const next = new Set(previous);
    if (!next.delete(key)) next.add(key);
    return next;
  });
  const post = async () => {
    if (!comment || !text.trim() || busy) return;
    setBusy(true);
    setFailure("");
    try {
      await send(comment, crypto.randomUUID(), { target, text }, 0);
      setText("");
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
  const submitKey = (event: KeyboardEvent<HTMLElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      void post();
    }
  };
  const refs = history.status === "ready" ? history.data.refs : undefined;
  const people = Object.assign({}, ...[activity, history, comments].map((section) => section.status === "ready" ? section.data.people : undefined));
  const say = (field: Api.FieldInfo, value: unknown) => {
    const given = plain(value);
    const name = typeof given === "string" ? refs?.[field.name]?.[given] : undefined;
    if (name) return name;
    return field.name === described.lifecycle?.field ? t(described.state({ [field.name]: given })?.title ?? present(field, given)) : present(field, given);
  };
  const person = (member: string) => people[member] ?? t("Unnamed member");
  const locale = useLocale();
  const status = statusField(described);
  const stays: Stay[] = [];
  if (history.status === "ready" && status) {
    const ordered = [...(history.data.history ?? [])].sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
    for (const change of ordered) {
      const entry = change.fields.find((candidate) => candidate.field === status.name);
      if (!entry || entry.after == null) continue;
      const previous = stays.at(-1);
      if (previous) previous.until = change.at;
      stays.push({ value: String(plain(entry.after)), label: say(status, entry.after), from: change.at, by: change.by });
    }
  }
  const events: Event[] = [];
  if (history.status === "ready") {
    for (const change of history.data.history ?? []) {
      events.push({
        kind: "change", key: change.change, at: change.at, by: change.by, title: t(described.action(change.schema)?.title ?? "Changed"),
        changes: change.fields.flatMap((entry) => {
          const field = described.field(entry.field);
          if (!field) return [];
          const after = say(field, entry.after);
          return [{ field: t(field.title), before: entry.before === undefined ? undefined : say(field, entry.before), after }];
        }),
      });
    }
  }
  if (activity.status === "ready") {
    for (const [index, note] of ((activity.data.activity ?? []) as Note[]).entries()) {
      events.push({ kind: "activity", key: `activity-${index}`, at: note.at, by: note.by, title: t(note.title), text: note.text });
    }
  }
  if (comments.status === "ready") {
    for (const message of (comments.data.comments ?? []) as Comment[]) {
      events.push({ kind: "comment", key: message.id, at: message.created.at, by: message.by, text: message.text });
    }
  }
  events.sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
  const items = collapse(events);
  const sources = [activity, history, comments];
  const unavailable = sources.find((entry) => entry.status === "error" || entry.status === "refused");
  const loading = sources.some((entry) => entry.status === "loading");
  const renderChanges = (lines: ChangeLine[]) => (
    <VStack gap={1} role="list">
      {lines.map((line, index) => (
        <HStack key={index} gap={1} vAlign="center" wrap="wrap" role="listitem"
          aria-label={`${line.field}: ${line.before === undefined ? line.after : `${line.before} → ${line.after}`}`}>
          <Text type="supporting" color="secondary">{line.field}</Text>
          {line.before !== undefined && (
            <>
              <Text type="supporting" weight="medium" maxLines={1}>{line.before}</Text>
              <Text type="supporting" color="secondary">{"→"}</Text>
            </>
          )}
          <Text type="supporting" weight="medium" maxLines={1}>{line.after}</Text>
        </HStack>
      ))}
    </VStack>
  );
  return (
    <VStack gap={5}>
      {comment && comments.status === "ready" && (
        <VStack gap={2} onKeyDown={submitKey}>
          <TextArea label={t("Write a comment")} value={text} onChange={setText} isDisabled={busy}
            status={failure ? { type: "error", message: failure } : undefined} />
          <HStack hAlign="end"><Button label={t("Post comment")} variant="primary" isDisabled={!text.trim() || busy} onClick={post} /></HStack>
        </VStack>
      )}
      {stays.length > 0 && (
        <VStack gap={2}>
          <Text type="label" color="secondary">{t("Time in each state")}</Text>
          <HStack gap={4} wrap="wrap" role="list" aria-label={t("Time in each state")}>
            {stays.map((stay) => (
              <VStack key={stay.from} gap={0} role="listitem" aria-label={`${stay.label}: ${duration(locale, stay.from, stay.until)}`}>
                <Text type="body" weight="medium">{stay.label}</Text>
                <Text type="supporting" color="secondary">{`${duration(locale, stay.from, stay.until)} · ${person(stay.by)}`}</Text>
              </VStack>
            ))}
          </HStack>
        </VStack>
      )}
      {unavailable && <Banner status="warning" title={t("Some activity is unavailable")} description={unavailable.reason} />}
      {loading && <Text type="supporting">{t("Loading…")}</Text>}
      {!loading && !unavailable && !items.length && <Text type="supporting">{t("No activity yet")}</Text>}
      <VStack gap={0}>
        {items.map((item, position) => {
          const last = position === items.length - 1;
          const actor = person(item.by);
          const glyph = item.kind === "comment" ? ChatBubbleLeftIcon : item.kind === "activity" ? DocumentTextIcon : PencilSquareIcon;
          const open = item.kind === "group" && expanded.has(item.key);
          return (
            <HStack key={item.key} id={item.key} gap={3} vAlign="stretch">
              <VStack gap={1} xstyle={styles.rail}>
                {item.kind === "comment" ? <Avatar name={actor} size="sm" tooltip={false} /> : <Icon icon={glyph} size="sm" color="secondary" />}
                {!last && <VStack xstyle={styles.line} />}
              </VStack>
              <StackItem size="fill" xstyle={styles.body}>
                <VStack gap={2}>
                  <HStack gap={2} vAlign="center" wrap="wrap">
                    <HStack gap={1} vAlign="center" wrap="wrap">
                      <Text type="body" weight="medium">{actor}</Text>
                      <Text type="body" color="secondary">{item.kind === "comment" ? t("Commented") : item.kind === "group" ? item.items[0].title : item.title}</Text>
                    </HStack>
                    <HStack xstyle={styles.stamp}>
                      <Timestamp value={item.at} format="relative" type="supporting" color="secondary" />
                    </HStack>
                  </HStack>
                  {item.kind === "change" && item.changes.length > 0 && renderChanges(item.changes)}
                  {item.kind === "group" && item.items[0].changes.length > 0 && renderChanges(item.items[0].changes)}
                  {item.kind === "group" && (
                    <HStack>
                      <Button variant="ghost" size="sm" label={`${item.items.length - 1} ${t(item.items.length === 2 ? "earlier change" : "earlier changes")}`}
                        icon={<Icon icon={open ? ChevronDownIcon : ChevronRightIcon} size="sm" />} onClick={() => toggleGroup(item.key)} />
                    </HStack>
                  )}
                  {item.kind === "group" && open && (
                    <VStack gap={3}>
                      {item.items.slice(1).map((entry) => (
                        <VStack key={entry.key} id={entry.key} gap={1}>
                          <HStack gap={2} vAlign="center">
                            <Text type="supporting" color="secondary">{entry.title}</Text>
                            <Timestamp value={entry.at} format="relative" type="supporting" color="secondary" />
                          </HStack>
                          {entry.changes.length > 0 && renderChanges(entry.changes)}
                        </VStack>
                      ))}
                    </VStack>
                  )}
                  {item.kind === "comment" && (
                    <VStack xstyle={styles.bubble}>
                      <Text type="body">{item.text}</Text>
                    </VStack>
                  )}
                  {item.kind === "activity" && item.text && (
                    <Card padding={3}>
                      <Text type="body">{item.text}</Text>
                    </Card>
                  )}
                </VStack>
              </StackItem>
            </HStack>
          );
        })}
      </VStack>
    </VStack>
  );
}

export function Collaboration({ target, comments, catalog, entities }: { target: string; comments: Section; catalog: Api.Action[]; entities: Api.EntityInfo[] }) {
  const following = comments.status === "ready" && Boolean(comments.data.following);
  const follow = catalog.find((action) => action.schema === "platform.follow.add");
  const unfollow = catalog.find((action) => action.schema === "platform.follow.remove");
  const assign = catalog.find((action) => action.schema === "work.task.create");
  const task = entities.find((type) => type.type === assign?.target);
  const me = useRead<Api.MeView>(follow && unfollow ? "/v1/me" : null);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [assigning, setAssigning] = useState(false);
  const toggle = async (on: boolean) => {
    const action = on ? follow : unfollow;
    if (!action || me.status !== "ready" || busy) return;
    setBusy(true);
    setFailure("");
    try {
      const id = `${me.data.principalId}@${target.replaceAll("/", "~")}`;
      await send(action, id, on ? { target } : {});
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
  return (
    <VStack gap={2}>
      {failure && <Banner status="error" title={t("Not done")} description={failure} />}
      {comments.status === "ready" && follow && unfollow && me.status === "ready" &&
        <Switch label={t("Follow")} value={following} onChange={toggle} isDisabled={busy} />}
      {assign && task && <Button label={t("Assign work")} variant="secondary" onClick={() => setAssigning(true)} />}
      {assigning && assign && task && (
        <ActionDialog action={assign} described={task} heading={t("Assign work")} confirm={t("Create")} revision={0}
          fixed={{ ref: target }} onClose={() => setAssigning(false)} onDone={() => setAssigning(false)} />
      )}
    </VStack>
  );
}
