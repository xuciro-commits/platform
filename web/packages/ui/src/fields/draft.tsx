import {createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode} from "react";
import {t} from "../i18n";

export type InputDraft = {text: string; source: string; problem: string; reveal?: (path?:string) => void};
export type InputBuffers = {
  scope: string; values: Record<string, InputDraft>; set: (key: string, value?: InputDraft) => boolean;
  clear: (prefix?: string) => void; dirty: boolean; invalid: boolean; limit?: string;
  busy: boolean; failure?: string; reject: (message?: string) => void; pending: (change: number) => void;
  reveal?: (path?:string) => void; problemsShown?:boolean;
};
export const DraftInputs = createContext<InputBuffers | undefined>(undefined);
export const useInputDrafts = () => useContext(DraftInputs);
export function useInputBuffers(): InputBuffers {
  const [failure, reject] = useState<string>();
  const [running, setRunning] = useState(0);
  const pending = useCallback((change: number) => setRunning(old => Math.max(0, old + change)), []);
  const [values, setValues] = useState<Record<string, InputDraft>>({});
  const current = useRef(values);
  const [limit, setLimit] = useState<string>();
  const set = useCallback((key: string, value?: InputDraft) => {
    const next = {...current.current};
    if (value) next[key] = value; else delete next[key];
    if (Object.keys(next).length > 256 || Object.values(next).reduce((sum, item) => sum + item.text.length, 0) > 4 * 1024 * 1024) {
      setLimit(t("The input draft limit was reached. Correct or discard an existing draft first.")); return false;
    }
    current.current = next; setValues(next); setLimit(undefined); reject(undefined); return true;
  }, []);
  const clear = useCallback((prefix?: string) => {
    const next = prefix ? Object.fromEntries(Object.entries(current.current).filter(([key]) => !key.startsWith(prefix))) : {};
    current.current = next; setValues(next); setLimit(undefined); reject(undefined);
  }, []);
  return {scope: "", values, set, clear, failure, reject, busy: running > 0, pending,
    dirty: Object.keys(values).length > 0 || !!limit, invalid: !!limit || Object.values(values).some(value => !!value.problem), limit};
}
export function InputDraftProvider({children, scope = "", isolated = false, reveal}: {children: ReactNode; scope?: string; isolated?: boolean; reveal?: (path?:string) => void}) {
  const parent = useInputDrafts(), own = useInputBuffers(), owner = !isolated && parent ? parent : own;
  const pending=useCallback((change:number)=>{own.pending(change);parent?.pending(change);},[own.pending,parent?.pending]);
  return <DraftInputs.Provider value={{...owner,pending:isolated?pending:owner.pending, failure: owner.failure ?? parent?.failure,
    scope: `${!isolated && parent ? parent.scope : ""}/${scope}`, reveal: reveal ? path=>{if(!isolated)parent?.reveal?.(path);reveal(path);} : owner.reveal}}>{children}</DraftInputs.Provider>;
}
export function InputProblems() {
  const owner = useInputDrafts();
  if (owner?.problemsShown || !owner?.invalid && !owner?.failure) return null;
  return <div role="alert" className="grid gap-1 rounded border border-danger/30 p-2 text-xs text-danger">
    {owner.failure && <p>{owner.failure}</p>}{owner.limit && <p>{owner.limit}</p>}
    {Object.entries(owner.values).filter(([, value]) => value.problem).map(([key, value]) => <div key={key} className="flex gap-2">
      <button type="button" className="text-left underline" onClick={() => {
        value.reveal?.(key); requestAnimationFrame(() => [...document.querySelectorAll<HTMLElement>("[data-draft-key]")].find(node=>node.dataset.draftKey===key)?.focus());
      }}>{key.split("/").at(-1)}: {value.problem}</button>
      <button type="button" className="ml-auto underline" disabled={owner.busy} onClick={() => owner.set(key)}>{t("Discard")}</button>
    </div>)}
  </div>;
}

/** Unparsed text belongs to its document, even while its inspector is unmounted. */
export function useDraftInput(key: string, source: string, retainOnRefresh = false) {
  const owner = useContext(DraftInputs), path = `${owner?.scope ?? ""}/${key}`;
  const [local, setLocal] = useState<InputDraft>();
  const stored = owner ? owner.values[path] : local;
  const current = retainOnRefresh || stored?.problem || stored?.source === source ? stored : undefined;
  useEffect(() => {
    if (!retainOnRefresh && stored && !stored.problem && stored.source !== source) { if (owner) owner.set(path); else setLocal(undefined); }
  }, [source, path, stored?.source, owner?.set,retainOnRefresh]);
  const write = (text: string, problem = "") => {
    if (text.length > 65536) {
      const next = {text: current?.text ?? source, source, problem: t("This input exceeds 65536 characters. The previous draft is retained."), reveal: owner?.reveal};
      if (owner) owner.set(path, next); else setLocal(next); return false;
    }
    const next = text === source && !problem ? undefined : {text, source, problem, reveal: owner?.reveal};
    if (owner) return owner.set(path, next);
    setLocal(next); return true;
  };
  const clear = () => { if (owner) owner.set(path); else setLocal(undefined); };
  return {text: current?.text ?? source, problem: current?.problem ?? "", write, clear, retained: !!owner, path};
}
