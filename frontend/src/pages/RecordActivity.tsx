import { Avatar } from "@astryxdesign/core/Avatar";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Switch } from "@astryxdesign/core/Switch";
import { Text } from "@astryxdesign/core/Text";
import { TextArea } from "@astryxdesign/core/TextArea";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { useState } from "react";
import { type Api, type Read, useRead } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { reasonOf, send } from "./act";
import type { Described } from "./describe";
import { plain, present } from "./values";

type Comment = { id: string; text: string; by: string; created: { at: string } };
type Note = { at: string; title: string; text: string; by: string };
type Event = { key: string; at: string; by: string; title: string; lines: string[] };
type Section = Read<Api.RecordSectionView>;

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
  const say = (field: Api.FieldInfo, value: unknown) =>
    field.name === described.lifecycle?.field ? t(described.state({ [field.name]: plain(value) })?.title ?? present(field, plain(value))) : present(field, plain(value));
  const events: Event[] = [];
  if (history.status === "ready") {
    for (const change of history.data.history ?? []) {
      events.push({
        key: change.change, at: change.at, by: change.by, title: t(described.action(change.schema)?.title ?? "Changed"),
        lines: change.fields.flatMap((entry) => {
          const field = described.field(entry.field);
          if (!field) return [];
          const after = say(field, entry.after);
          return [`${t(field.title)}: ${entry.before === undefined ? after : `${say(field, entry.before)} → ${after}`}`];
        }),
      });
    }
  }
  if (activity.status === "ready") {
    for (const [index, note] of ((activity.data.activity ?? []) as Note[]).entries()) {
      events.push({ key: `activity-${index}`, at: note.at, by: note.by, title: t(note.title), lines: [note.text] });
    }
  }
  if (comments.status === "ready") {
    for (const message of (comments.data.comments ?? []) as Comment[]) {
      events.push({ key: message.id, at: message.created.at, by: message.by, title: t("Commented"), lines: [message.text] });
    }
  }
  events.sort((a, b) => Date.parse(b.at) - Date.parse(a.at));
  const sources = [activity, history, comments];
  const unavailable = sources.find((entry) => entry.status === "error" || entry.status === "refused");
  const loading = sources.some((entry) => entry.status === "loading");
  return (
    <VStack gap={5}>
      {comment && comments.status === "ready" && (
        <VStack gap={2}>
          <TextArea label={t("Write a comment")} value={text} onChange={setText} isDisabled={busy}
            status={failure ? { type: "error", message: failure } : undefined} />
          <HStack hAlign="end"><Button label={t("Post comment")} variant="primary" isDisabled={!text.trim() || busy} onClick={post} /></HStack>
        </VStack>
      )}
      {unavailable && <Banner status="warning" title={t("Some activity is unavailable")} description={unavailable.reason} />}
      {loading && <Text type="supporting">{t("Loading…")}</Text>}
      {!loading && !unavailable && !events.length && <Text type="supporting">{t("No activity yet")}</Text>}
      {events.map((event) => (
        <HStack key={event.key} gap={3} vAlign="start">
          <Avatar name={event.by} size="sm" />
          <StackItem size="fill">
            <VStack gap={1}>
              <HStack gap={1} vAlign="center" wrap="wrap">
                <Text type="body" weight="semibold">{event.by}</Text>
                <Text type="body">{event.title}</Text>
                <Timestamp value={event.at} format="auto" />
              </HStack>
              {event.lines.map((line, index) => <Text key={index} type="body">{line}</Text>)}
            </VStack>
          </StackItem>
        </HStack>
      ))}
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
