import { useHost, useReadQuery } from "@platform/app";
import type { Project } from "./project";
import { installedStudioRef } from "./studio-applications";
import { refKey } from "./resources";

/** Looking at a code application never reads or creates a Build draft. */
export function useStudioApplication(id?: string) {
  const { definitions } = useHost();
  const ref = installedStudioRef(id);
  const query = useReadQuery<{ record?: Project }>(`/v1/records/build.app/${encodeURIComponent(id ?? "")}`, undefined, !!id && !ref);
  const project = ref ? undefined : query.data?.record;
  const definition = definitions.find(item => item.ref.kind === "app" && (ref ? refKey(item.ref) === refKey(ref) : item.ref.app === "build" && item.ref.name === project?.name));
  return { project, definition, application: project ?? definition?.application, owner: ref?.app ?? "build", query, installed: !!ref };
}
