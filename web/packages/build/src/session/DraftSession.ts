import { useCallback, useReducer } from "react";

type Edit<T> = Partial<T> | ((draft: T) => T);
type Snapshot<T> = { draft: T };
type State<T> = { draft: T; saved: string; past: Snapshot<T>[]; future: T[]; key?: string };
type Command<T> = { type: "edit"; edit: Edit<T>; key?: string } | { type: "load"; draft: T } | { type: "saved"; draft: T; normalized?: T } | { type: "undo" } | { type: "redo" };
const signature = (draft: unknown) => JSON.stringify(draft);

function reduce<T>(state: State<T>, command: Command<T>): State<T> {
  if (command.type === "load") {
    const saved = signature(command.draft);
    return saved === signature(state.draft) ? { ...state, saved } : { draft: command.draft, saved, past: [], future: [] };
  }
  if (command.type === "saved") {
    const normalized = command.normalized ?? command.draft;
    // Acknowledgement can normalize omitted values/key order. Preserve the
    // command history and never replace edits made after this submission.
    return { ...state, draft: signature(state.draft) === signature(command.draft) ? normalized : state.draft, saved: signature(normalized), key: undefined };
  }
  if (command.type === "undo") {
    const previous = state.past.at(-1);
    return previous ? { ...state, draft: previous.draft, past: state.past.slice(0, -1), future: [state.draft, ...state.future], key: undefined } : state;
  }
  if (command.type === "redo") {
    const draft = state.future[0];
    return draft ? { ...state, draft, past: [...state.past, { draft: state.draft }], future: state.future.slice(1), key: undefined } : state;
  }
  const draft = typeof command.edit === "function" ? command.edit(state.draft) : { ...state.draft, ...command.edit };
  if (signature(draft) === signature(state.draft)) return state;
  // Typing in one property coalesces into one command. Layout operations,
  // selection renames and binding changes remain atomic history entries.
  const past = command.key && command.key === state.key && !state.future.length ? state.past : [...state.past.slice(-79), { draft: state.draft }];
  return { ...state, draft, past, future: [], key: command.key };
}

/** Per-editor document history; UI selection and runtime records stay outside. */
export function useDraftSession<T>(initial: T) {
  const [state, dispatch] = useReducer(reduce<T>, { draft: initial, saved: signature(initial), past: [], future: [] });
  const edit = useCallback((edit: Edit<T>, key?: string) => dispatch({ type: "edit", edit, key }), []);
  const load = useCallback((draft: T) => dispatch({ type: "load", draft }), []);
  const saved = useCallback((draft: T, normalized?: T) => dispatch({ type: "saved", draft, normalized }), []);
  const undo = useCallback(() => dispatch({ type: "undo" }), []);
  const redo = useCallback(() => dispatch({ type: "redo" }), []);
  return { draft: state.draft, dirty: signature(state.draft) !== state.saved, canUndo: state.past.length > 0, canRedo: state.future.length > 0, edit, load, saved, undo, redo };
}
