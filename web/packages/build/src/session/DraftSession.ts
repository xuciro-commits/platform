import { useCallback, useEffect, useReducer, useState } from "react";

import {reduceDraft,type DraftEdit} from "./draft-history";

/** Per-editor document history; UI selection and runtime records stay outside. */
export function useDraftSession<T, C = never>(initial: T, scope?: string) {
  const [state, dispatch] = useReducer(reduceDraft<T>, { draft: initial, saved: JSON.stringify(initial), past: [], future: [] });
  const [copied, setCopied] = useState<{scope?:string;value:C}>();
  useEffect(() => setCopied(undefined), [scope]);
  const copy = useCallback((value:C) => setCopied({scope,value}), [scope]);
  const edit = useCallback((edit: DraftEdit<T>, key?: string) => dispatch({ type: "edit", edit, key }), []);
  const load = useCallback((draft: T) => dispatch({ type: "load", draft }), []);
  const saved = useCallback((draft: T, normalized?: T) => dispatch({ type: "saved", draft, normalized }), []);
  const undo = useCallback(() => dispatch({ type: "undo" }), []);
  const redo = useCallback(() => dispatch({ type: "redo" }), []);
  return { draft: state.draft, dirty: JSON.stringify(state.draft) !== state.saved, canUndo: state.past.length > 0, canRedo: state.future.length > 0, edit, load, saved, undo, redo, clipboard:copied?.scope===scope?copied?.value:undefined, copy };
}
