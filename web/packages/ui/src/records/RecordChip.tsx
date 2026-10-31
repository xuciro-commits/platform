import { User } from "lucide-react";
import { createContext, useContext } from "react";
import { useRecordView, type EntityRecord, type RecordSource } from "./Records";
import { Tooltip } from "../primitives/tooltip";

export const RecordReadContext=createContext<{scope:string|RecordSource;type:string;record:EntityRecord}|undefined>(undefined);

export function RecordChip({ source, type, id, person = false, ariaLabel, onOpen }: {
  source: RecordSource; type: string; id: string;
  person?: boolean; ariaLabel?: string; onOpen?: (type: string, id: string) => void;
}) {
  const display = source.entity(type)?.display;
  const current=useContext(RecordReadContext),known=current?.scope===(source.scope??source)&&current.type===type&&current.record.id===id?current.record:undefined;
  const { view, loading, error } = useRecordView(source, type, !known&&display&&display!=="id"?id:undefined);
  const value = display && display !== "id" ? known?known[display]:!loading&&!error?view?.record[display]:undefined : undefined;
  const title = value === undefined || value === null || value === "" ? undefined : String(value);
  const shown = title ?? (id.length > 14 ? `${id.slice(0, 11)}…` : id);
  return <Tooltip content={title && title !== id ? `${type}/${id}` : undefined}><button type="button" data-entity={id} disabled={!onOpen}
    aria-label={ariaLabel ?? `${type}/${id}`}
    onClick={(e) => { e.stopPropagation(); onOpen?.(type, id); }}
    className="inline-flex h-[18px] max-w-44 items-center gap-1 rounded-sm border border-border bg-row-selected px-1.5 text-xs font-medium leading-none whitespace-nowrap text-primary hover:underline disabled:no-underline">
    {person && <User className="size-3 shrink-0" aria-hidden />}
    <span className="truncate">{shown}</span>
  </button></Tooltip>;
}
