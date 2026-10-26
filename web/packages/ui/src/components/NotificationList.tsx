import { cn } from "../lib/cn";
import { Button } from "../primitives/button";
import { t } from "../i18n";
import {pageUIManifest} from "@platform/kernel";

export type NotificationItem = { id: string; title: string; body?: string; at: string; read: boolean; app?: string;ref?:string };
export type NotificationListProps<T extends NotificationItem>={items:readonly T[];onRead?:(notice:T)=>void|Promise<void>;onOpen?:(notice:T)=>void;empty?:string;label?:string;enabled?:boolean;busyIDs?:readonly string[];errors?:Readonly<Record<string,string>>;total?:number;loadedCount?:number;limit?:number};

// A member's notifications, newest first: unread ones stand out and can be marked read.
export function NotificationList<T extends NotificationItem>({items,onRead,onOpen,empty=t("Nothing new"),label,enabled=true,busyIDs=[],errors={},total,loadedCount=items.length,limit}:NotificationListProps<T>) {
  if(!Array.isArray(items)||new Set(items.map(notice=>notice?.id)).size!==items.length||items.some(notice=>!notice||typeof notice.id!=="string"||!notice.id||typeof notice.title!=="string"||typeof notice.at!=="string"||!notice.at||typeof notice.read!=="boolean"||[notice.body,notice.app,notice.ref].some(value=>value!==undefined&&typeof value!=="string"))||limit!==undefined&&(!Number.isSafeInteger(limit)||limit<1||limit>pageUIManifest.runtime.workViews.maxNotificationsWindow)||!Number.isSafeInteger(loadedCount)||loadedCount<items.length||total!==undefined&&(!Number.isSafeInteger(total)||total<loadedCount))return <p role="alert">{t("The notification window is unavailable or incompatible.")}</p>;
  const visible=limit===undefined?items:items.slice(0,limit),range=limit!==undefined||total!==undefined||loadedCount!==items.length;
  return (
   <div className="grid min-w-0 gap-2">{range&&<p role="status" className="text-xs text-muted">{total===undefined?t("Showing {shown} of {loaded} loaded notifications; the complete total is unavailable.",{shown:visible.length,loaded:loadedCount}):t("Showing {shown} of {total} notifications.",{shown:visible.length,total})}</p>}{visible.length===0?<p role="status" className="text-sm text-muted">{empty}</p>:<ul aria-label={label??t("Notifications")} className="grid min-w-0 max-w-3xl gap-1.5">
      {visible.map((n) => (
        <li key={n.id} className={cn("flex min-w-0 flex-wrap items-start gap-3 rounded-md border border-border p-3 text-sm", n.read ? "bg-surface" : "bg-row-selected")}>
          <span aria-hidden className={cn("mt-1.5 size-2 shrink-0 rounded-full", n.read ? "bg-transparent" : "bg-[var(--tone-info)]")} />
          <button type="button" className="grid min-w-0 flex-1 gap-0.5 break-words text-left" onClick={() => {if(enabled&&!busyIDs.includes(n.id))onOpen?.(n);}} disabled={!onOpen||!enabled||busyIDs.includes(n.id)}>
            <span className={cn("break-words",!n.read&&"font-semibold")}>{n.title}</span>
            {n.body && <span className="whitespace-pre-wrap break-words text-muted">{n.body}</span>}
            <span className="break-words text-xs text-muted"><time dateTime={n.at}>{n.at}</time>{n.app ? ` · ${n.app}` : ""}</span><span className="break-all font-mono text-xs text-muted">{n.id}{n.ref?` · ${n.ref}`:""}</span>
          </button>
          {!n.read&&onRead&&<Button size="sm" disabled={!enabled||busyIDs.includes(n.id)} onClick={()=>{if(enabled&&!busyIDs.includes(n.id))void onRead(n);}}>{busyIDs.includes(n.id)?t("Confirming read…"):t("Mark read")}</Button>}
          {Object.hasOwn(errors,n.id)&&errors[n.id]&&<p role="alert" className="w-full break-words text-xs text-[var(--tone-danger)]">{errors[n.id]}</p>}
        </li>
      ))}
    </ul>}</div>
  );
}
