import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Dialog, Notice, flushSync, t, useInputDrafts } from "@platform/ui";
import { Fragment, createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useCurrentBranch } from "../branches/current";

type Inputs = NonNullable<ReturnType<typeof useInputDrafts>>["values"];
type Snapshot = { value: unknown; inputs?: Inputs; dirty: boolean; base?: unknown; edit?: Api.CompositeEdit; problems?: string[] };
type Working = {
  spec?: unknown; inputs?: Inputs; baseRecord?:unknown; baseline:(record:unknown)=>void; isNew: boolean; saved: boolean; error: string; signature: string;
  capture: (snapshot: Snapshot) => void; persist: () => Promise<void>; clear: () => Promise<void>;
  review: (save: () => unknown) => void;
  edit?: Api.CompositeEdit;
  prepare: (edit: Api.CompositeEdit | undefined, problems?: string[]) => void;
};
const Context = createContext<Working | undefined>(undefined);
export const useWorkingState = () => useContext(Context);
const sessions = new Set<Working>();
export const flushWorkingStates = () => Promise.all([...sessions].map(session => session.persist()));

export function resourceSaveEdit(edit: Api.CompositeEdit, project?: string, specSchema?: Api.SchemaVersion): Api.CompositeEdit {
  const type = edit.target.type;
  if (edit.schema !== `${type}.create` && edit.schema !== `${type}.edit`) return edit;
  const payload = edit.payload && typeof edit.payload === "object" ? edit.payload as Record<string, unknown> : {};
  const embedded = typeof payload.project === "string" ? payload.project : undefined;
  const { project: _project, ...spec } = payload;
  return { ...edit, schema: `${type}.save`, payload: { spec, project: project ?? embedded, specSchema } };
}

export function WorkingState(props: { type: string; id: string; project?: string; onShared?: (target: string) => void; children: ReactNode }) {
 const {id}=useCurrentBranch();
 return <WorkingStateSession key={id||"main"} {...props} branch={id||"main"}/>;
}

function WorkingStateSession({ type, id, children, branch, project, onShared }: { type: string; id: string; children: ReactNode;branch:string;project?:string;onShared?:(target:string)=>void }) {
  const host = useHost();
  const path = `/v1/working-state/${encodeURIComponent(branch)}/${encodeURIComponent(type)}/${encodeURIComponent(id)}`;
  const query = useReadQuery<Api.WorkingStateView>(path, undefined, host.holds("build", "builder"));
  const baseline=useRef<unknown>(undefined);
  const latest = useRef<Snapshot>({ value: {}, dirty: false });
  const ignore = useRef(false), pending = useRef<() => unknown>(undefined);
  const [loaded,setLoaded]=useState(!host.holds("build","builder"));
  useEffect(()=>{if(query.isSuccess && !query.isFetching)setLoaded(true);},[query.isSuccess,query.isFetching]);
  const [saved, setSaved] = useState(""), [error, setError] = useState("");
  const [review, setReview] = useState<{ before: Record<string, unknown>; after: Record<string, unknown> }>();
  const [busy, setBusy] = useState(false);
  const [generation, setGeneration] = useState(0);
  const prior = useRef<Api.WorkingState | undefined>(undefined);
  useEffect(() => {
    if (!query.isSuccess || query.isFetching) return;
    const previous = prior.current;
    prior.current = query.data?.record;
    if (previous && !query.data?.record && !ignore.current) {
      baseline.current = undefined; latest.current = { value: {}, dirty: false }; setSaved("");
      setGeneration(value => value + 1);
      if (id === "new" && previous.edit?.target.id) {
        const target = previous.edit.target.id;
        const resource = branch === "main" ? `/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(target)}` : `/v1/branches/${encodeURIComponent(branch)}/resources/${encodeURIComponent(type)}/${encodeURIComponent(target)}`;
        void host.client.call<{record?:unknown;op?:string}>("GET", resource).then(answer => { if (answer.ok && (answer.body.record || answer.body.op)) onShared?.(target); });
      }
    }
  }, [query.data, query.isSuccess, query.isFetching]);
  const context = useMemo<Working>(() => ({
    get edit() { return query.data?.record?.edit; },
    get baseRecord(){return ignore.current?undefined:query.data?.record?.base;},
    baseline:record=>{if(baseline.current===undefined)baseline.current=record;},
    get spec() { return ignore.current ? undefined : query.data?.record?.spec; },
    get inputs() { return ignore.current ? undefined : query.data?.record?.inputs as Inputs | undefined; },
    isNew: id === "new",
    get saved() { return saved === JSON.stringify(latest.current); },
    error,
    get signature() { return JSON.stringify(latest.current); },
    capture: snapshot => { latest.current = { ...snapshot, edit: latest.current.edit, problems: latest.current.problems }; },
    prepare: (edit, problems = []) => {
      latest.current.edit = edit && resourceSaveEdit(edit, project, host.action(edit.schema)?.schemaVersion);
      latest.current.problems = problems;
    },
    persist: async () => {
      const snapshot = latest.current, target = query.data?.id;
      if (!snapshot.dirty || !target || saved === JSON.stringify(snapshot)) return;
      setError("");
      const ok = await host.decide("build.workingstate.save", { type: "build.workingstate", id: target }, { branch, type, draft: id, spec: snapshot.value, inputs: snapshot.inputs,base:query.data?.record?.base??baseline.current,edit:snapshot.edit,problems:snapshot.problems }, { quiet: true, onRefused: setError });
      if (ok) { host.client.refreshLiveReads();await query.refetch();ignore.current = false; flushSync(()=>setSaved(JSON.stringify(snapshot))); }
    },
    clear: async () => {
      ignore.current = true;
      baseline.current=undefined;
      setSaved("");
      if (query.data?.id) await host.decide("build.workingstate.clear", { type: "build.workingstate", id: query.data.id }, {}, { quiet: true, onRefused: setError });
    },
    review: action => {
      pending.current = action;
      setReview({ before: (latest.current.base ?? {}) as Record<string, unknown>, after: latest.current.value as Record<string, unknown> });
    },
  }), [host, branch, type, id, query.data, saved, error, project]);
  useEffect(() => { sessions.add(context); return () => { sessions.delete(context); }; }, [context]);
  if (!loaded && !query.isError) return <p role="status">{t("Loading working state…")}</p>;
  if (query.isError) return <><Notice tone="danger" message={t("Working state could not be loaded.")} /><Button onClick={() => void query.refetch()}>{t("Retry")}</Button></>;
  const fields = host.source.entity(type)?.fields ?? [];
  const changes = review ? [...new Set([...Object.keys(review.before), ...Object.keys(review.after)])].filter(key => JSON.stringify(review.before[key]) !== JSON.stringify(review.after[key])) : [];
  return <Context.Provider value={context}>
    {error && <Notice tone="danger" role="alert" message={error}/>}
    <Fragment key={generation}>{children}</Fragment>
    <Dialog open={!!review} onOpenChange={open => { if (!open && !busy) setReview(undefined); }} title={t("Review changes")} wide>
      <p className="mb-3 text-sm text-muted">{t(branch === "main" ? "Save these changes to main so other builders can use them." : "Save these changes to the branch so its members can review them.")}</p>
      <div className="max-h-[55vh] overflow-auto divide-y divide-border">{changes.map(key => <div key={key} className="grid grid-cols-[8rem_minmax(0,1fr)_minmax(0,1fr)] gap-3 py-2 text-xs"><strong>{fields.find(field => field.name === key)?.title || key}</strong><pre className="whitespace-pre-wrap break-all text-muted">{JSON.stringify(review?.before[key], null, 2) ?? "—"}</pre><pre className="whitespace-pre-wrap break-all">{JSON.stringify(review?.after[key], null, 2) ?? "—"}</pre></div>)}</div>
      {error && <Notice tone="danger" message={error} />}
      <div className="mt-3 flex justify-end gap-2"><Button disabled={busy} onClick={() => setReview(undefined)}>{t("Cancel")}</Button><Button variant="primary" disabled={busy} onClick={async () => { setBusy(true); await pending.current?.(); setBusy(false); setReview(undefined); }}>{t("Save")}</Button></div>
    </Dialog>
  </Context.Provider>;
}

export function usePrivateDraft<T>(value: T, dirty: boolean, restore: (value: T) => void, ready: boolean, base?: unknown) {
  const working = useWorkingState(), inputs = useInputDrafts();
  const restored = useRef(false);
  useEffect(() => {
    if (ready && working?.spec !== undefined && !restored.current) {
      restored.current = true;
      restore(working.spec as T);
      for (const [key, input] of Object.entries(working.inputs ?? {})) inputs?.set(key, input);
    }
  }, [ready, working?.spec]);
  working?.capture({ value, dirty, inputs: inputs?.values, base });
  return working;
}
