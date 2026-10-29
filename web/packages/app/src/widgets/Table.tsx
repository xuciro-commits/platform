import type * as PageUI from "../pageui/types";
import { Panel, RecordList, t, type EntityRecord, type ListState, type RecordSource,type RecordEditPort,type RecordSelectionPort } from "@platform/ui";
import { useState } from "react";
import { ImportControl, SaveViewDialog } from "./list-operations";

export type TablePorts = {onOpenReference?:(type:string,id:string)=>void;onNavigate?:(record:EntityRecord)=>void;cards?:{layout:"grid"|"list";labelField:string};
  importable?: boolean; savable?: boolean;
  presentation?:PageUI.PageTablePresentation;columns?:import("@platform/ui").RecordColumnPresentation[];showSearch?:boolean;
  keepActive?:boolean;
  selectionSet?:RecordSelectionPort;
  inlineEdit?:RecordEditPort;
  source: RecordSource; object: string; fields?: string[]; domain?: unknown[];
  window?: Parameters<typeof RecordList>[0]["window"]; selected?: EntityRecord;
  onSelect: (record?: EntityRecord) => void;
  status?: "missing-window" | "missing-parent" | "invalid-reference"; plural?: string;
};

export function TableRenderer({source,object,fields,domain,window,selected,onSelect,status,plural,inlineEdit,selectionSet,keepActive,columns,showSearch,presentation,cards,onNavigate,importable,savable,onOpenReference}:TablePorts) {
  const [saving,setSaving]=useState<ListState>();
  if(status==="missing-window")return <Panel role="status">{t("Query window is unavailable.")}</Panel>;
  if(status==="invalid-reference")return <p role="alert" className="text-sm text-danger">{t("This section's parent reference is unavailable.")}</p>;
  if(status==="missing-parent")return <div className="flex h-40 items-center justify-center rounded-md border border-dashed border-border p-4 text-center"><p className="text-sm text-muted">{t("Select a record to see related {records}.",{records:plural??object})}</p></div>;
  return <div className="min-w-0 [&_[role=table]]:max-h-80">
    <RecordList onOpenReference={onOpenReference} onNavigate={onNavigate} cards={cards} selectedId={selected?.id} tablePresentation={presentation} columnPresentation={columns} showSearch={showSearch} selectionSet={selectionSet} inlineEdit={inlineEdit} source={source} type={object} fields={fields} height="auto" domain={domain} window={window}
      toolbar={<ImportControl source={source} type={object} importable={!!importable}/>}
      onSave={savable?(state:ListState)=>setSaving(state):undefined}
      onOpen={record=>onSelect(!keepActive&&!selectionSet&&record.id===selected?.id?undefined:record)}/>
    {savable&&<SaveViewDialog type={object} state={saving} onClose={()=>setSaving(undefined)}/>}
  </div>;
}
