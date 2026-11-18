import { type Api, useRead } from "../host";

export type InboxItem = {
  id: string;
  kind: "task" | "notification";
  title: string;
  body?: string;
  ref?: string;
  from: string;
  at: string;
  unread: boolean;
  open: boolean;
  task?: Api.InboxTask;
  note?: Api.Notification;
};

export type InboxTab = "all" | "todo" | "notifications" | "closed";

export const inboxTabs: { value: InboxTab; label: string }[] = [
  { value: "all", label: "All" },
  { value: "todo", label: "To do" },
  { value: "notifications", label: "Notifications" },
  { value: "closed", label: "Closed" },
];

export function inTab(item: InboxItem, tab: InboxTab) {
  if (tab === "all") return item.open;
  if (tab === "todo") return item.kind === "task" && item.open;
  if (tab === "notifications") return item.kind === "notification";
  return item.kind === "task" && !item.open;
}

const taskKey = (id: string) => `task:${id}`;

export function useInbox() {
  const offered = useRead<Api.InboxTask[]>("/v1/inbox");
  const notes = useRead<Api.Notification[]>("/v1/notifications");
  const tasks = useRead<Api.RecordPage>("/v1/records/work.task?limit=100");
  const status = [offered, notes, tasks].find((read) => read.status !== "ready")?.status ?? "ready";
  const reason = [offered, notes, tasks].flatMap((read) => (read.status === "error" || read.status === "refused" ? [read.reason] : []))[0] ?? "";
  if (offered.status !== "ready" || notes.status !== "ready" || tasks.status !== "ready") return { status, reason, items: [] as InboxItem[] };
  const byKey = new Map(notes.data.map((note) => [note.key ?? "", note]));
  const open = new Map(offered.data.map((task) => [task.id, task]));
  const closed = (tasks.data.records as Api.InboxTask[]).filter((task) => task.state !== "open" && !open.has(task.id));
  const taskItems = [...open.values(), ...closed].map((task): InboxItem => {
    const note = byKey.get(taskKey(task.id));
    return {
      id: task.id,
      kind: "task",
      title: task.title,
      body: task.body,
      ref: task.ref,
      from: task.created.by ?? task.app,
      at: task.changed.at ?? note?.at ?? "",
      unread: Boolean(note && !note.read),
      open: task.state === "open",
      task,
      note,
    };
  });
  const known = new Set(taskItems.map((item) => taskKey(item.id)));
  const noteItems = notes.data
    .filter((note) => !known.has(note.key ?? ""))
    .map((note): InboxItem => ({ id: note.id, kind: "notification", title: note.title, body: note.body, ref: note.ref, from: note.app, at: note.at, unread: !note.read, open: true, note }));
  const items = [...taskItems, ...noteItems].sort((a, b) => b.at.localeCompare(a.at));
  return { status, reason, items };
}
