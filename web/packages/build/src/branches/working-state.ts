import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { useMemo } from "react";
import { kindOfType } from "../projects/resources";
import type { JointChoice, ReleaseKind } from "../releases/selection";

export type WorkingDraft = JointChoice & { title: string; label: string };

export function useWorkingDrafts(enabled: boolean) {
  const { holds } = useHost();
  const read = enabled && holds("build", "builder");
  const working = useReadQuery<Api.ReleaseWorkingState>("/v1/releases/drafts/working", undefined, read);
  const drafts = useMemo(() => !read ? [] : (working.data?.drafts ?? []).map((draft): WorkingDraft => ({
    kind: draft.kind as ReleaseKind, id: draft.id, title: draft.title || draft.id,
    label: draft.type === "build.app" ? "Application" : kindOfType(draft.type)?.label ?? draft.kind,
  })), [read, working.data]);
  return { drafts, loading: read && working.isLoading };
}
