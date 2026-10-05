// The application a studio editor was opened from (ADR-0047 §6.2, M2): the
// editor keeps the application in scope, so the way back and the asset's
// membership stay visible without rebuilding the asset or its route.
import { createContext, useContext, type ReactNode } from "react";

const Scope = createContext<string | undefined>(undefined);

/** Everything a view renders inside carries the application it belongs to. */
export function ApplicationScope({ application, children }: { application?: string; children: ReactNode }) {
  if (!application) return <>{children}</>;
  return <Scope.Provider value={application}>{children}</Scope.Provider>;
}

/** The application id in scope, when the editor was opened from one. */
export function useApplicationScope(): string | undefined {
  return useContext(Scope);
}
