import { createContext, useCallback, useContext, useEffect, useReducer, useState } from "react";

import {reduceDraft,type DraftEdit} from "./draft-history";

type InputDraft = { text: string; source: string; problem: string };
export const DraftInputs = createContext<{ scope: string; values: Record<string, InputDraft>; set: (key: string, value?: InputDraft) => void } | undefined>(undefined);

/** Unparsed text belongs to its document, even while its inspector is unmounted. */
export function useDraftInput(key: string, source: string) {
  const owner = useContext(DraftInputs), path = `${owner?.scope ?? ""}/${key}`;
  const [local, setLocal] = useState<InputDraft>();
  const stored = owner ? owner.values[path] : local;
  const current = stored?.source === source ? stored : undefined;
  useEffect(() => {
    if (stored && stored.source !== source) { if (owner) owner.set(path); else setLocal(undefined); }
  }, [source, path, stored?.source, owner?.set]);
  const write = (text: string, problem = "") => {
    const next = text === source && !problem ? undefined : { text, source, problem };
    if (owner) owner.set(path, next); else setLocal(next);
  };
  const clear = () => { if (owner) owner.set(path); else setLocal(undefined); };
  return { text: current?.text ?? source, problem: current?.problem ?? "", write, clear, retained: !!owner };
}

/** Per-editor document history; UI selection and runtime records stay outside. */
export function useDraftSession<T, C = never>(initial: T, scope?: string) {
  const [state, dispatch] = useReducer(reduceDraft<T>, { draft: initial, saved: JSON.stringify(initial), past: [], future: [] });
  const [inputs, setInputs] = useState<Record<string, InputDraft>>({});
  const setInput = useCallback((key: string, value?: InputDraft) => setInputs(old => {
    if (!value && !old[key]) return old;
    const next = { ...old }; if (value) next[key] = value; else delete next[key]; return next;
  }), []);
  const [copied, setCopied] = useState<{scope?:string;value:C}>();
  useEffect(() => setCopied(undefined), [scope]);
  const copy = useCallback((value:C) => setCopied({scope,value}), [scope]);
  const edit = useCallback((edit: DraftEdit<T>, key?: string) => dispatch({ type: "edit", edit, key }), []);
  const load = useCallback((draft: T) => { setInputs({}); dispatch({ type: "load", draft }); }, []);
  const saved = useCallback((draft: T, normalized?: T) => dispatch({ type: "saved", draft, normalized }), []);
  const undo = useCallback(() => dispatch({ type: "undo" }), []);
  const redo = useCallback(() => dispatch({ type: "redo" }), []);
  return { draft: state.draft, dirty: Object.keys(inputs).length > 0 || JSON.stringify(state.draft) !== state.saved, inputs: { scope: "", values: inputs, set: setInput }, inputProblems: Object.fromEntries(Object.entries(inputs).map(([key, value]) => [key, value.problem])), canUndo: state.past.length > 0, canRedo: state.future.length > 0, edit, load, saved, undo, redo, clipboard:copied?.scope===scope?copied?.value:undefined, copy };
}
