export type DraftEdit<T> = Partial<T> | ((draft: T) => T);
type Snapshot<T,B> = { draft: T; inputs?:B };
export type DraftHistory<T,B=never> = { draft: T; saved: string; past: Snapshot<T,B>[]; future: Snapshot<T,B>[]; key?: string };
export type DraftCommand<T,B=never> = { type: "edit"; edit: DraftEdit<T>; key?: string; inputs?:B } | { type: "load"; draft: T } | { type: "saved"; draft: T; normalized?: T } | { type: "undo"; inputs?:B } | { type: "redo"; inputs?:B };
const signature = (draft: unknown) => JSON.stringify(draft);

export function reduceDraft<T,B=never>(state: DraftHistory<T,B>, command: DraftCommand<T,B>): DraftHistory<T,B> {
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
    return previous ? { ...state, draft: previous.draft, past: state.past.slice(0, -1), future: [{draft:state.draft,inputs:command.inputs}, ...state.future], key: undefined } : state;
  }
  if (command.type === "redo") {
    const draft = state.future[0];
    return draft ? { ...state, draft:draft.draft, past: [...state.past, { draft: state.draft, inputs:command.inputs }], future: state.future.slice(1), key: undefined } : state;
  }
  const draft = typeof command.edit === "function" ? command.edit(state.draft) : { ...state.draft, ...command.edit };
  if (signature(draft) === signature(state.draft)) return state;
  // Typing in one property coalesces into one command. Layout operations,
  // selection renames and binding changes remain atomic history entries.
  const past = command.key && command.key === state.key && !state.future.length ? state.past : [...state.past.slice(-79), { draft: state.draft,inputs:command.inputs }];
  return { ...state, draft, past, future: [], key: command.key };
}
