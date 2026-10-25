import { useContext, useEffect, useState } from "react";
import { User } from "lucide-react";
import { WorkspaceContext } from "../shell/Workspace";
import type { RecordSource } from "./Records";

/** One resolved title per referenced record per session; chips render the id until it lands. */
const titles = new Map<string, Promise<string>>();

function resolveTitle(source: RecordSource, type: string, id: string): Promise<string> {
  const key = `${type}/${id}`;
  let pending = titles.get(key);
  if (!pending) {
    pending = source.get(type, id).then((view) => {
      const info = source.entity(type);
      const display = info?.display && info.display !== "id" ? info.display : undefined;
      const value = display ? view.record[display] : undefined;
      return value === undefined || value === null || value === "" ? id : String(value);
    }).catch(() => id);
    titles.set(key, pending);
  }
  return pending;
}

/** A referenced record as a chip: the resolved title (the id until it arrives),
 * opening the record in the inspect pane beside the list. The cell's copied or
 * exported value stays the raw id — presentation never replaces data. */
export function RecordChip({ source, type, id, person = false, ariaLabel }: {
  source: RecordSource; type: string; id: string;
  /** The target is a person: a leading avatar glyph. */
  person?: boolean; ariaLabel?: string;
}) {
  const workspace = useContext(WorkspaceContext);
  const [title, setTitle] = useState<string>();
  useEffect(() => {
    let live = true;
    void resolveTitle(source, type, id).then((resolved) => { if (live) setTitle(resolved); });
    return () => { live = false; };
  }, [source, type, id]);
  const shown = title ?? (id.length > 14 ? `${id.slice(0, 11)}…` : id);
  return <button type="button" data-entity={id} disabled={!workspace}
    aria-label={ariaLabel ?? `${type}/${id}`}
    title={title && title !== id ? `${type}/${id}` : undefined}
    onClick={(e) => { e.stopPropagation(); workspace?.open({ view: "record", params: { type, id, inspect: "true" } }, { window: "inspect" }); }}
    className="inline-flex h-[18px] max-w-44 items-center gap-1 rounded-sm border border-border bg-row-selected px-1.5 text-xs font-medium leading-none whitespace-nowrap text-primary hover:underline disabled:no-underline">
    {person && <User className="size-3 shrink-0" aria-hidden />}
    <span className="truncate">{shown}</span>
  </button>;
}
