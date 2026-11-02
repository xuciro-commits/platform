import { useInputDrafts } from "../fields/draft";
import { createContext, useContext, useEffect, useLayoutEffect, useRef, type ReactNode } from "react";
import { toast } from "sonner";
import type { Route } from "./route";

export type View = {
  id: string;
  title: (params: Record<string, string>) => string;
  icon?: ReactNode;
  render: (params: Record<string, string>) => ReactNode;
  documentKey?: (params: Record<string, string>) => string;
};
export type ShellCommand = { id: string; label: string; group?: string; shortcut?: string; run: () => void };
export type SearchHit = { id: string; label: string; detail?: string; group?: "resource" | "object"; open: () => void };
export type RecordSearch = (text: string) => Promise<SearchHit[]>;
export type Session = {
  tenant: string; principal: string; detail?: string;
  name?: string;
  options: { id: string; label: string }[]; current: string; onSwitch: (id: string) => void;
  onAccount?: () => void;
};

export type PlatformApplication = { id: string; title: string; icon?: ReactNode; description?: string; category: string; home: Route };
export type Applications = { apps: PlatformApplication[]; categories: { id: string; label: string }[]; current?: string; onSelect: (id: string) => void };
export type Rail = { home: Route; applications: Route; files?: Route; notifications?: { route: Route; unread?: number }; assist?: Route };
export type RecentEntry = { route: Route; title: string; app?: string; at: number };

export type OpenOptions = { window?: "tab" | "beside" | "popout" | "inspect"; ifCurrent?: Route; from?: string; replace?: boolean };
export type Unsaved = {
  register: (panel: string, owner: symbol, discard?: () => void, busy?: boolean) => void;
  flush: (panel: string, owner: symbol, run?: () => Promise<unknown>) => void;
  ask: (panels: string[], run: () => void, owner?: symbol) => void;
};
export type WorkspaceApi = { open: (route: Route, options?: OpenOptions) => void; close: (route: Route, from?: string) => void; closeAll: (from?: string) => void; notify: typeof toast; unsaved?: Unsaved;
  confirmLeave: (run: () => void, exceptPanel?: string) => void;
  retitle?: (panel: string, title: string) => void;
  transfer?: (from: string, route: Route, input: unknown, result: (value: unknown) => void) => void;
  palette: () => void;
  recent: RecentEntry[];
  favorites: string[]; toggleFavorite: (app: string) => void;
  applications?: Applications; rail?: Rail;
  sidebarSlot?: HTMLElement | null; claimSidebar?: (panel: string) => () => void; activePanel?: string; };
export type Entry = { id: string; route: Route };
export type Memory = { recent: RecentEntry[]; favorites: string[]; last: Record<string, Route> };
export type Pending = { panels: string[]; run: () => void; owner?: symbol; cancel?: () => void };

export const PanelContext = createContext<string | undefined>(undefined);
export const ViewCallContext = createContext<{ input?: unknown; expired?: boolean; returnValue?: (value: unknown) => void } | undefined>(undefined);
export const useViewCall = () => useContext(ViewCallContext);

export const WorkspaceContext = createContext<WorkspaceApi | null>(null);

let serial = 0;
export const entryId = (kind: "view" | "side") => `${kind}:${++serial}`;

export function useWorkspace(): WorkspaceApi {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  if (!api) throw new Error("useWorkspace outside <Workspace>");
  return { ...api, open: (route, options) => api.open(route, { ...options, from: panel }), close: route => api.close(route, panel), closeAll: () => api.closeAll(panel), confirmLeave: (run: () => void) => api.confirmLeave(run, panel), transfer: api.transfer && panel ? (_from, route, input, result) => api.transfer!(panel, route, input, result) : undefined };
}

export function useViewTitle(name?: string) {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  useEffect(() => { if (api?.retitle && panel && name) api.retitle(panel, name); }, [api, panel, name]);
}

export function useUnsavedChanges(dirty: boolean, discard: () => void, busy = false, stored = false) {
  const inputs=useInputDrafts();dirty = !stored && (dirty || !!inputs?.dirty);busy ||=!!inputs?.busy;
  const unsaved = useContext(WorkspaceContext)?.unsaved;
  const panel = useContext(PanelContext);
  const owner = useRef(Symbol());
  const reset = useRef(discard);
  reset.current = ()=>{inputs?.clear();discard();};
  useLayoutEffect(() => {
    if (!panel || !unsaved) return;
    unsaved.register(panel, owner.current, dirty ? () => {
      unsaved.register(panel, owner.current);
      reset.current();
    } : undefined, busy);
    return () => unsaved.register(panel, owner.current);
  }, [dirty, busy, panel, unsaved]);
  return {
    markSaved: () => { if(inputs?.dirty)return;if (panel) unsaved?.register(panel, owner.current); },
    discardChanges: () => { if (panel) unsaved?.register(panel, owner.current); reset.current(); },
    confirmDiscard: (run: () => void) => panel && unsaved ? unsaved.ask([panel], run) : run(),
    confirmOwnDiscard: (run: () => void) => panel && unsaved ? unsaved.ask([panel], run, owner.current) : run(),
  };
}

export function useLeaveFlush(run: () => Promise<unknown>) {
  const unsaved = useContext(WorkspaceContext)?.unsaved;
  const panel = useContext(PanelContext);
  const owner = useRef(Symbol()), latest = useRef(run);
  latest.current = run;
  useLayoutEffect(() => {
    if (!panel || !unsaved) return;
    unsaved.flush(panel, owner.current, () => latest.current());
    return () => unsaved.flush(panel, owner.current);
  }, [panel, unsaved]);
}

export const notify = toast;
