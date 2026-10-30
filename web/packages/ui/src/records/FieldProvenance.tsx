import { History, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Popover } from "radix-ui";
import type { Api } from "@platform/kernel";
import { Notice } from "../components/Notice";
import { Tag } from "../components/StatusTag";
import { t } from "../i18n";
import { humanizeKernelError } from "../lib/errors";
import { formatStamp } from "../lib/datetime-format";
import { Button } from "../primitives/button";
import type { RecordSource } from "./Records";

const loaded = new Map<string, Promise<Api.RecordProvenance>>();
const limit = 64;

function provenanceOf(source: RecordSource, type: string, id: string, revision: number): Promise<Api.RecordProvenance> {
  const key = JSON.stringify([source.scope, type, id, revision]);
  const known = loaded.get(key);
  if (known) return known;
  const request = source.provenance!(type, id);
  loaded.set(key, request);
  request.catch(() => { if (loaded.get(key) === request) loaded.delete(key); });
  while (loaded.size > limit) loaded.delete(loaded.keys().next().value!);
  return request;
}

type State = { loading: true } | { loading: false; error: string } | { loading: false; fields: Api.RecordProvenance["fields"] };

export function FieldProvenance({ source, type, id, revision, field, label }: { source: RecordSource; type: string; id: string; revision: number; field: string; label: string }) {
  const [open, setOpen] = useState(false);
  const [state, setState] = useState<State>();
  useEffect(() => {
    if (!open || !source.provenance) return;
    let active = true;
    setState({ loading: true });
    provenanceOf(source, type, id, revision).then(
      result => { if (active) setState({ loading: false, fields: result.fields }); },
      error => { if (active) setState({ loading: false, error: humanizeKernelError(error) }); },
    );
    return () => { active = false; };
  }, [open, source, type, id, revision]);
  if (!source.provenance) return null;
  const entry = state && "fields" in state ? state.fields[field] : undefined;
  return <Popover.Root open={open} onOpenChange={setOpen}>
    <Popover.Trigger asChild><Button size="icon" variant="ghost" className="size-5 shrink-0 text-muted" aria-label={t("Where did the value of {field} come from?", { field: label })} title={t("Where did this value come from?")}><History aria-hidden="true" /></Button></Popover.Trigger>
    <Popover.Portal><Popover.Content side="bottom" align="start" sideOffset={6} collisionPadding={12} aria-label={t("Value origin of {field}", { field: label })} className="z-50 grid w-72 max-w-[calc(100vw-1.5rem)] gap-2 rounded-md border border-border bg-surface p-3 text-sm shadow-lg">
      <div className="flex items-center justify-between gap-2"><h2 className="min-w-0 truncate text-xs font-semibold">{label}</h2><Popover.Close asChild><Button size="icon" variant="ghost" aria-label={t("Close")}><X aria-hidden="true" /></Button></Popover.Close></div>
      {!state || state.loading ? <p role="status" className="text-xs text-muted">{t("Loading…")}</p>
        : "error" in state ? <Notice tone="danger" message={state.error} />
        : !entry ? <p className="text-xs text-muted">{t("No source is recorded for this value.")}</p>
        : entry.kind === "edit" ? <div className="grid gap-1 text-xs"><Tag label={t("Edited")} tone="info" /><p>{entry.at ? t("Edited at {time}", { time: formatStamp(entry.at) }) : t("Edited")}</p></div>
        : <div className="grid gap-1 text-xs">
          <Tag label={entry.connector?.startsWith("pipeline:") ? t("Pipeline") : t("Source")} tone="teal" />
          {entry.connector && <p className="break-all font-mono text-muted">{entry.connector}</p>}
          {entry.sourceTime && <p>{t("Source time {time}", { time: formatStamp(entry.sourceTime) })}</p>}
          {entry.at && <p className="text-muted">{t("Recorded at {time}", { time: formatStamp(entry.at) })}</p>}
        </div>}
    </Popover.Content></Popover.Portal>
  </Popover.Root>;
}
