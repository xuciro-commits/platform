import { type Api, type Read, useRead, useReads } from "../host";

export type InboxItem = {
  id: string;
  kind: "task" | "notification" | "record";
  title: string;
  body?: string;
  ref?: string;
  from: string;
  at: string;
  unread: boolean;
  open: boolean;
  task?: Api.InboxTask;
  note?: Api.Notification;
  record?: { type: string; id: string; kind: string; due: string };
};

type Follow = { member: string; due: string; open?: Record<string, string[]> };

export type InboxTab = "all" | "todo" | "mentions" | "notifications" | "closed";

export const inboxTabs: { value: InboxTab; label: string }[] = [
  { value: "all", label: "All" },
  { value: "todo", label: "To do" },
  { value: "mentions", label: "Mentions" },
  { value: "notifications", label: "Notifications" },
  { value: "closed", label: "Closed" },
];

export function inTab(item: InboxItem, tab: InboxTab) {
  if (tab === "all") return item.open;
  if (tab === "todo") return item.kind !== "notification" && item.open;
  if (tab === "mentions") return item.kind === "notification" && item.note?.category === "mention";
  if (tab === "notifications") return item.kind === "notification";
  return item.kind === "task" && !item.open;
}

const taskKey = (id: string) => `task:${id}`;

const today = (zone?: string) => {
  const now = new Date();
  try {
    return new Intl.DateTimeFormat("en-CA", { timeZone: zone || undefined, year: "numeric", month: "2-digit", day: "2-digit" }).format(now);
  } catch {
    return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
  }
};

function useFollowUps(): { status: Read<unknown>["status"]; reason?: string; data: InboxItem[] } {
  const me = useRead<Api.MeView>("/v1/me");
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const declared = me.status === "ready" && types.status === "ready"
    ? types.data.flatMap((entity) => {
      const follow = (entity.view as { inbox?: Follow } | undefined)?.inbox;
      return follow?.member && follow.due ? [{ entity, follow, who: me.data.principalId, zone: me.data.account.effective.timezone }] : [];
    })
    : [];
  const pages = useReads<Api.RecordPage>(declared.map(({ entity, follow, who, zone }) => {
    const domain = [[follow.member, "=", who], [follow.due, "<=", today(zone)], ...Object.entries(follow.open ?? {}).map(([field, values]) => [field, "in", values])];
    return `/v1/records/${encodeURIComponent(entity.type)}?limit=100&domain=${encodeURIComponent(JSON.stringify(domain))}`;
  }));
  const failed = [me, types, pages].find((read) => read.status === "error" || read.status === "refused");
  if (failed && (failed.status === "error" || failed.status === "refused")) return { status: failed.status, reason: failed.reason, data: [] };
  if (pages.status !== "ready" || pages.data.length !== declared.length) return { status: "loading", data: [] };
  const data = declared.flatMap(({ entity, follow }, index) => (pages.data[index].records as (Record<string, unknown> & { id: string; created?: { by?: string }; changed?: { at?: string } })[]).map((row): InboxItem => ({
    id: `${entity.type}/${row.id}`,
    kind: "record",
    title: String(row[entity.display] ?? row.id),
    ref: `${entity.type}/${row.id}`,
    from: row.created?.by ?? entity.title,
    at: row.changed?.at ?? "",
    unread: false,
    open: true,
    record: { type: entity.type, id: row.id, kind: entity.title, due: String(row[follow.due] ?? "") },
  })));
  return { status: "ready", data };
}

export function useInbox() {
  const offered = useRead<Api.InboxTask[]>("/v1/inbox");
  const notes = useRead<Api.Notification[]>("/v1/notifications");
  const tasks = useRead<Api.RecordPage>("/v1/records/work.task?limit=100");
  const followed = useFollowUps();
  const status = [offered, notes, tasks, followed].find((read) => read.status !== "ready")?.status ?? "ready";
  const reason = [offered, notes, tasks, followed].flatMap((read) => (read.status === "error" || read.status === "refused" ? [read.reason ?? ""] : []))[0] ?? "";
  if (offered.status !== "ready" || notes.status !== "ready" || tasks.status !== "ready" || followed.status !== "ready") return { status, reason, items: [] as InboxItem[] };
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
  const items = [...followed.data, ...taskItems, ...noteItems].sort((a, b) => b.at.localeCompare(a.at));
  return { status, reason, items };
}
