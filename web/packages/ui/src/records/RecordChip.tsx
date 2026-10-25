import { useContext, useEffect, useState } from "react";
import { User } from "lucide-react";
import { WorkspaceContext } from "../shell/Workspace";
import type { RecordSource } from "./Records";

/** A referenced record as a chip: the resolved title (the id until it arrives),
 * opening the record in the inspect pane beside the list. The cell's copied or
 * exported value stays the raw id — presentation never replaces data. The
 * title resolves through the caller's own authorized source each time its
 * identity or revision moves: no cross-identity reuse, no permanent failure
 * fallback, and a rename arrives with the next revision. */
export function RecordChip({ source, type, id, person = false, ariaLabel }: {
  source: RecordSource; type: string; id: string;
  /** The target is a person: a leading avatar glyph. */
  person?: boolean; ariaLabel?: string;
}) {
  const workspace = useContext(WorkspaceContext);
  const [title, setTitle] = useState<string>();
  const identity = source.scope ?? source;
  useEffect(() => {
    let live = true;
    source.get(type, id).then((view) => {
      if (!live) return;
      const info = source.entity(type);
      const display = info?.display && info.display !== "id" ? info.display : undefined;
      const value = display ? view.record[display] : undefined;
      setTitle(value === undefined || value === null || value === "" ? id : String(value));
    }, () => { if (live) setTitle(undefined); });
    return () => { live = false; };
  }, [identity, source.revision, type, id]);
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
