import { useCallback, useEffect, useReducer, useRef, useState } from "react";

import {useInputBuffers,useInputDrafts} from "@platform/ui";
export {DraftInputs,useDraftInput} from "@platform/ui";
import {reduceDraft,type DraftEdit} from "./draft-history";

/** Per-editor document history; UI selection and runtime records stay outside. */
export function useDraftSession<T, C = never>(initial: T, scope?: string) {
  const inherited=useInputDrafts(),own=useInputBuffers(),buffers=inherited??own;
  const [state,dispatch]=useReducer(reduceDraft<T,typeof buffers.values>,{draft:initial,saved:JSON.stringify(initial),past:[],future:[]});
  const raw=useRef(buffers.values);raw.current=buffers.values;
  const [rawRedo,setRawRedo]=useState<typeof buffers.values>();
  const [copied, setCopied] = useState<{scope?:string;value:C}>();
  useEffect(() => setCopied(undefined), [scope]);
  const copy = useCallback((value:C) => setCopied({scope,value}), [scope]);
  const edit = useCallback((edit: DraftEdit<T>, key?: string) => (setRawRedo(undefined),dispatch({ type: "edit", edit, key,inputs:raw.current })), []);
  const load = useCallback((draft: T) => { buffers.clear();setRawRedo(undefined); dispatch({ type: "load", draft }); }, [buffers.clear]);
  const saved = useCallback((draft: T, normalized?: T) => dispatch({ type: "saved", draft, normalized }), []);
  const undo = () => {if(buffers.dirty){setRawRedo(buffers.values);buffers.clear();}else {const prior=state.past.at(-1)?.inputs;dispatch({type:"undo",inputs:buffers.values});buffers.clear();if(prior)for(const [key,value] of Object.entries(prior))buffers.set(key,value);setRawRedo(undefined);}};
  const redo = () => {if(rawRedo){for(const [key,value] of Object.entries(rawRedo))buffers.set(key,value);setRawRedo(undefined);}else {const next=state.future[0]?.inputs;dispatch({type:"redo",inputs:buffers.values});buffers.clear();if(next)for(const [key,value] of Object.entries(next))buffers.set(key,value);}};
  return { draft: state.draft, dirty: buffers.dirty || JSON.stringify(state.draft) !== state.saved, inputs: buffers, inputProblems: Object.fromEntries(Object.entries(buffers.values).map(([key, value]) => [key, value.problem])), canUndo: buffers.dirty || state.past.length > 0, canRedo: !!rawRedo || state.future.length > 0, edit, load, saved, undo, redo, clipboard:copied?.scope===scope?copied?.value:undefined, copy };
}
