import { createContext, type ReactNode, useCallback, useContext } from "react";
import { type Api, useRead, useSession } from "./host";
import { usePath } from "./router";

const Homes = createContext<ReadonlyMap<string, string> | null>(null);

export function RecordRoutesProvider({ homes, children }: { homes: ReadonlyMap<string, string>; children: ReactNode }) {
  return <Homes.Provider value={homes}>{children}</Homes.Provider>;
}

function useHomes() {
  const homes = useContext(Homes);
  if (!homes) throw new Error("Record routes must be provided by the application root");
  return homes;
}

function homeOf(homes: ReadonlyMap<string, string>, type: string) {
  return homes.get(type) ?? `/ontology/objects/${encodeURIComponent(type)}`;
}

function recordPath(list: string, id?: string) {
  return id === undefined ? list : `${list}/${encodeURIComponent(id)}`;
}

export type RecordHome = (type: string, id: string) => string;

export function useRecordHome(): RecordHome {
  const homes = useHomes();
  return useCallback((type, id) => recordPath(homeOf(homes, type), id), [homes]);
}

const remembered = new Map<string, Api.ApplicationView[]>();

type RecordHref = (type: string, id?: string) => string;

export function useRecordHref(): RecordHref;
export function useRecordHref(waitForApplications: true): RecordHref | null;
export function useRecordHref(waitForApplications = false): RecordHref | null {
  const homes = useHomes();
  const app = usePath().split("/")[1];
  const session = useSession();
  const key = `${session?.tenant} ${session?.token}`;
  const views = useRead<Api.ApplicationView[]>("/v1/applications");
  if (views.status === "ready") remembered.set(key, views.data);
  const loaded = views.status === "ready" ? views.data : remembered.get(key);
  const resolve = useCallback((type: string, id?: string) => {
    const pageIn = (view?: Api.ApplicationView) => view?.menu?.flatMap((group) => group.entries).find((entry) => entry.object === type);
    const page = pageIn(loaded?.find((view) => (view.name || view.app) === app));
    const owner = page || homes.has(type) ? undefined : loaded?.find((view) => pageIn(view));
    const list = page ? `/${app}/${page.name}` : owner ? `/${owner.name || owner.app}/${pageIn(owner)?.name}` : homeOf(homes, type);
    return recordPath(list, id);
  }, [app, loaded, homes]);
  return waitForApplications && views.status === "loading" && !loaded ? null : resolve;
}
