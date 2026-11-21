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
import { ChatBubbleLeftIcon, DocumentTextIcon, PencilSquareIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { type KeyboardEvent, useState } from "react";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { reasonOf, send } from "./act";
import type { Described } from "./describe";
import { plain, present } from "./values";

type Comment = { id: string; text: string; by: string; created: { at: string } };
type Note = { at: string; title: string; text: string; by: string };
type ChangeLine = { field: string; before?: string; after: string };
type Event =
  | { kind: "change"; key: string; at: string; by: string; title: string; changes: ChangeLine[] }
  | { kind: "comment"; key: string; at: string; by: string; text: string }
  | { kind: "activity"; key: string; at: string; by: string; title: string; text: string };
type Section = Read<Api.RecordSectionView>;

const styles = stylex.create({
  rail: { width: 28, flexShrink: 0, alignItems: "center" },
  line: { width: 1, flexGrow: 1, minHeight: spacingVars["--spacing-4"], backgroundColor: colorVars["--color-border"] },
  bubble: { backgroundColor: colorVars["--color-background-muted"], borderRadius: radiusVars["--radius-container"], padding: spacingVars["--spacing-3"] },
  body: { minWidth: 0, paddingBlockEnd: spacingVars["--spacing-4"] },
  stamp: { flexShrink: 0, marginInlineStart: "auto" },
});

export function RecordActivity({ described, id, comments, catalog }: { described: Described; id: string; comments: Section; catalog: Api.Action[] }) {
  const target = `${described.type}/${id}`;
  const root = `/v1/records/${described.type}/${encodeURIComponent(id)}/sections`;
  const activity = useRead<Api.RecordSectionView>(`${root}/activity`);
  const history = useRead<Api.RecordSectionView>(`${root}/history`);
  const comment = catalog.find((action) => action.schema === "platform.comment.add");
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
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
  const sources = [activity, history, comments];
  const unavailable = sources.find((entry) => entry.status === "error" || entry.status === "refused");
  const loading = sources.some((entry) => entry.status === "loading");
  return (
    <VStack gap={5}>
      {comment && comments.status === "ready" && (
        <VStack gap={2} onKeyDown={submitKey}>
          <TextArea label={t("Write a comment")} value={text} onChange={setText} isDisabled={busy}
            status={failure ? { type: "error", message: failure } : undefined} />
          <HStack hAlign="end"><Button label={t("Post comment")} variant="primary" isDisabled={!text.trim() || busy} onClick={post} /></HStack>
        </VStack>
      )}
      {unavailable && <Banner status="warning" title={t("Some activity is unavailable")} description={unavailable.reason} />}
      {loading && <Text type="supporting">{t("Loading…")}</Text>}
      {!loading && !unavailable && !events.length && <Text type="supporting">{t("No activity yet")}</Text>}
      <VStack gap={0}>
        {events.map((event, position) => {
          const last = position === events.length - 1;
          const actor = person(event.by);
          const glyph = event.kind === "comment" ? ChatBubbleLeftIcon : event.kind === "activity" ? DocumentTextIcon : PencilSquareIcon;
          return (
            <HStack key={event.key} gap={3} vAlign="stretch">
              <VStack gap={1} xstyle={styles.rail}>
                {event.kind === "comment" ? <Avatar name={actor} size="sm" tooltip={false} /> : <Icon icon={glyph} size="sm" color="secondary" />}
                {!last && <VStack xstyle={styles.line} />}
              </VStack>
              <StackItem size="fill" xstyle={styles.body}>
                <VStack gap={2}>
                  <HStack gap={2} vAlign="center" wrap="wrap">
                    <HStack gap={1} vAlign="center" wrap="wrap">
                      <Text type="body" weight="medium">{actor}</Text>
                      <Text type="body" color="secondary">{event.kind === "comment" ? t("Commented") : event.title}</Text>
                    </HStack>
                    <HStack xstyle={styles.stamp}>
                      <Timestamp value={event.at} format="relative" type="supporting" color="secondary" />
                    </HStack>
                  </HStack>
                  {event.kind === "change" && event.changes.length > 0 && (
                    <VStack gap={1}>
                      {event.changes.map((line, index) => (
                        <HStack key={index} gap={1} vAlign="center" wrap="wrap">
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
                  )}
                  {event.kind === "comment" && (
                    <VStack xstyle={styles.bubble}>
                      <Text type="body">{event.text}</Text>
                    </VStack>
                  )}
                  {event.kind === "activity" && event.text && (
                    <Card padding={3}>
                      <Text type="body">{event.text}</Text>
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
