import { Panel, RecordList, t, type EntityRecord, type RecordSource,type RecordEditPort,type RecordSelectionPort } from "@platform/ui";

export type TablePorts = {
  columns?:import("@platform/ui").RecordColumnPresentation[];showSearch?:boolean;
  keepActive?:boolean;
  selectionSet?:RecordSelectionPort;
  inlineEdit?:RecordEditPort;
  source: RecordSource; object: string; fields?: string[]; domain?: unknown[];
  window?: Parameters<typeof RecordList>[0]["window"]; selected?: EntityRecord;
  onSelect: (record?: EntityRecord) => void;
  status?: "missing-window" | "missing-parent" | "invalid-reference"; plural?: string;
};

/** Presentation consumes authorized ports, never the host or page session. */
export function TableRenderer({source,object,fields,domain,window,selected,onSelect,status,plural,inlineEdit,selectionSet,keepActive,columns,showSearch}:TablePorts) {
  if(status==="missing-window")return <Panel role="status">{t("Query window is unavailable.")}</Panel>;
  if(status==="invalid-reference")return <p role="alert" className="text-sm text-danger">{t("This section's parent reference is unavailable.")}</p>;
  if(status==="missing-parent")return <div className="flex h-40 items-center justify-center rounded-md border border-dashed border-border p-4 text-center"><p className="text-sm text-muted">{t("Select a record to see related {records}.",{records:plural??object})}</p></div>;
  return <RecordList columnPresentation={columns} showSearch={showSearch} selectionSet={selectionSet} inlineEdit={inlineEdit} source={source} type={object} fields={fields} height={320} domain={domain} window={window} onOpen={record=>onSelect(!keepActive&&!selectionSet&&record.id===selected?.id?undefined:record)}/>;
}
