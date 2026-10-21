import type {Api} from "@platform/kernel";
import type {AggregateData,AggregateQuery} from "../charts/spec";

// What the host describes is generated from its Go types (ADR-0005 D7); the kit
// only refines what it holds in general: any entity's record.
export type FieldInfo = Api.FieldInfo;
export type State = Api.State;
export type Lifecycle = Api.LifecycleInfo;
export type EntityInfo = Api.EntityInfo;
type Stamp = Api.Stamp;
export type EntityRecord = { id: string; revision: number; created: Stamp; changed: Stamp; archived?: boolean } & Record<string, unknown>;
export type RecordQuery = Omit<Api.Query,"domain"> & {domain?:unknown[]};
export type RecordPageData = Omit<Api.RecordPage, "records"> & { records: EntityRecord[] };
export type InterfaceRecordIdentity = Pick<Api.InterfaceRecord, "type" | "id">;
export type InterfaceRecordData = Omit<Api.InterfaceRecord, "record"> & { record: EntityRecord };
export type InterfaceRecordPageData = Omit<Api.InterfaceRecordPage, "records"> & { records: InterfaceRecordData[] };
export type RecordChange = Api.RecordChange;
export type AttachedFile = EntityRecord & { name: string; size: number; contentType: string; by: string; regions?:Api.ImageRegion[] };

export type RecordComment = EntityRecord & { text: string; by: string; mentions?: string[] };

export type RecordView = Omit<Api.RecordView, "record" | "related" | "linked" | "activity" | "processes" | "approvals" | "tasks" | "files" | "comments"> & {
  /** Open tasks about the record the member may take (a flow's question, a correction to make). */
  tasks: Api.InboxTask[];
  /** The approval requests to move the record, newest first (F-38). */
  approvals: Api.ApprovalRequest[];
  files: AttachedFile[];
  comments: RecordComment[];
  record: EntityRecord; related: RelatedRecords[];
  /** Records of any app linked to it (the relations app), by type. */
  linked: RelatedRecords[];
  /** What apps told about it through protocols, oldest first. */
  activity: Api.Note[];
  /** The flow instances about the record (ADR-0006 D4). */
  processes: (EntityRecord & { title: string; state: string; tokens?: { step: string }[] })[];
};
type RelatedRecords = Omit<Api.Related, "records"> & { records: EntityRecord[] };
export type Money = { amount: number; currency: string };

/** Where records come from: the host's reads, wired by the app; with aggregates, lists can group, pivot and chart (ADR-0024). */
export type RecordSource = {
	enterpriseQuery?: (stereotypes: string[], search: string) => Promise<{ elements: Api.EnterpriseQueryElement[] }>;
	enterpriseResolve?: (ids: string[]) => Promise<{ elements: Api.EnterpriseQueryElement[] }>;
  watchEnterprise?: (changed: () => void) => () => void;
    scope?: string;
  ruleIdentity?: {me:string;timezone:string};
  entity: (type: string) => EntityInfo | undefined;
  list: (type: string, query: RecordQuery) => Promise<RecordPageData>;
  exportCSV?: (type: string, query: RecordQuery) => Promise<Blob>;
  get: (type: string, id: string) => Promise<RecordView>;
  aggregate?: (type: string, query: AggregateQuery) => Promise<AggregateData>;
  interfaceList?: (name: string, query: RecordQuery) => Promise<InterfaceRecordPageData>;
  watchInterfaceList?: (name: string, query: RecordQuery, changed: () => void) => () => void;
  namedInterfaceList?: (binding: Api.AssetBinding, query: RecordQuery) => Promise<InterfaceRecordPageData>;
  watchNamedInterfaceList?: (binding: Api.AssetBinding, query: RecordQuery, changed: () => void) => () => void;
  /** Moves each time the host's data changed (F-32): lists, pages, charts and pivots read again. */
  revision?: number;
  watchList?: (type: string, query: RecordQuery, changed: () => void) => () => void;
  watchRecord?: (type: string, id: string, changed: () => void) => () => void;
  watchAggregate?: (type: string, query: AggregateQuery, changed: () => void) => () => void;
};

export type OriginalRecordTitle={object:string;record:EntityRecord;info:EntityInfo;labelField:string};
/** Reversible compound identity that is also safe inside the canvas's quoted CSS selectors. */
export const recordIdentity=(object:string,id:string)=>encodeURIComponent(JSON.stringify([object,id]));
/** Title metadata comes from the original readable object; data is never coerced into a name. */
export function originalRecordTitle({object,record,info,labelField}:OriginalRecordTitle):string|undefined {
 if(!record||typeof record.id!=="string"||!record.id||record.archived===true||!Number.isSafeInteger(record.revision)||record.revision<0||!info||info.type!==object||!Array.isArray(info.fields))return;
 const field=info.fields.find(field=>field.name===labelField);
 if(labelField!=="id"&&(!field||!["text","longtext","choice","reference"].includes(field.type)))return;
 const raw=labelField==="id"?record.id:record[labelField];
 return raw==null?record.id:typeof raw==="string"?raw:undefined;
}
