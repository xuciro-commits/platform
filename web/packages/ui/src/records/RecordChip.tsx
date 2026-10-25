import { User } from "lucide-react";
import { useRecordView, type RecordSource } from "./Records";

/** A referenced record as a chip: the resolved title (the id until it arrives),
 * opening the record in the inspect pane beside the list. The cell's copied or
 * exported value stays the raw id — presentation never replaces data. The
 * title resolves through the caller's own authorized source each time its
 * identity or revision moves: no cross-identity reuse, no permanent failure
 * fallback, and a rename arrives with the next revision. */
export function RecordChip({ source, type, id, person = false, ariaLabel, onOpen }: {
  source: RecordSource; type: string; id: string;
  /** The target is a person: a leading avatar glyph. */
  person?: boolean; ariaLabel?: string; onOpen?: (type: string, id: string) => void;
}) {
  const display = source.entity(type)?.display;
  const { view, loading, error } = useRecordView(source, type, display&&display!=="id"?id:undefined);
  const value = !loading && !error && display && display !== "id" ? view?.record[display] : undefined;
  const title = value === undefined || value === null || value === "" ? undefined : String(value);
  const shown = title ?? (id.length > 14 ? `${id.slice(0, 11)}…` : id);
  return <button type="button" data-entity={id} disabled={!onOpen}
    aria-label={ariaLabel ?? `${type}/${id}`}
    title={title && title !== id ? `${type}/${id}` : undefined}
    onClick={(e) => { e.stopPropagation(); onOpen?.(type, id); }}
    className="inline-flex h-[18px] max-w-44 items-center gap-1 rounded-sm border border-border bg-row-selected px-1.5 text-xs font-medium leading-none whitespace-nowrap text-primary hover:underline disabled:no-underline">
    {person && <User className="size-3 shrink-0" aria-hidden />}
    <span className="truncate">{shown}</span>
  </button>;
}
