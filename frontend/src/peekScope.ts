import { createContext, useContext } from "react";

export type PeekTarget = { type: string; id: string; name?: string };
export type PeekContextValue = { open: (target: PeekTarget) => void; close: () => void };

export const PeekContext = createContext<PeekContextValue | null>(null);

export function usePeek() {
  return useContext(PeekContext);
}
