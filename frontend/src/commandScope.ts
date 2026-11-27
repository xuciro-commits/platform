import { createContext, useContext, useLayoutEffect, useRef } from "react";

export type Command = { id: string; label: string; run: () => void };
export type Destination = { href: string; label: string };
type RecordDestination = Destination & { type: string; id: string };
export type Recent = Destination | RecordDestination;

export const SearchScope = createContext<{
  openSearch: () => void;
  remember: (destination: Recent) => void;
  register: (getCommands: () => Command[]) => () => void;
} | null>(null);

export function useCommandSearch() {
  return useContext(SearchScope);
}

export function usePageCommands(commands: Command[]) {
  const scope = useCommandSearch();
  const current = useRef(commands);
  current.current = commands;
  useLayoutEffect(() => scope?.register(() => current.current), [scope]);
}
