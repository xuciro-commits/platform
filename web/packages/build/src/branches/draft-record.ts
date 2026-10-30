import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { t, type EntityRecord } from "@platform/ui";
import { useMemo } from "react";
import { resourcePath, useBranchActions, useBranchRead, useBranches } from "./api";
import { useCurrentBranch } from "./current";

export function withBuild<T extends object>(spec: T): T {
  const { build, ...rest } = spec as T & { build?: { module?: string; sourceHash?: string; buildHash?: string; toolchain?: string } };
  return build ? { ...rest, module: build.module, sourceHash: build.sourceHash, buildHash: build.buildHash, toolchain: build.toolchain, state: "compiled" } as unknown as T : spec;
}

type Data<T> = { record?: T & EntityRecord; valueErrors?: Record<string, string> };
export type DraftQuery<T> = {
  data?: Data<T>; isError: boolean; isFetching: boolean; isLoading: boolean; isPlaceholderData: boolean;
  refetch: () => Promise<{ isSuccess: boolean; data?: Data<T> }>;
};

export function useDraftRecord<T extends { id: string; revision: number }>(type: string, id: string, enabled = true) {
  const host = useHost();
  const builder = host.holds("build", "builder");
  const { id: currentId } = useCurrentBranch();
  const list = useBranches(builder && !!currentId);
  const branch = list.data?.find(item => item.id === currentId && (item.state === "open" || item.state === "proposed"));
  const main = useReadQuery<Data<T>>(`/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}`, undefined, enabled && id !== "new");
  const view = useBranchRead<Api.BranchResourceView>(resourcePath(branch?.id ?? "", type, id), !!branch && enabled && id !== "new");
  const actions = useBranchActions();
  const base = main.data?.record;
  const spec = view.data?.branch as Record<string, unknown> | undefined;
  const lastSeq = branch?.resources.find(item => item.type === type && item.id === id)?.lastSeq ?? 0;
  const deleted = view.data?.op === "delete";
  const record = useMemo(() => !branch || !(spec || base) ? base : ({ ...(base ?? { id, state: "draft" }), ...(spec ? withBuild(spec) : {}), id, archived: deleted || base?.archived, revision: (base?.revision ?? 0) + lastSeq }) as T & EntityRecord,
    [branch?.id, base, spec, deleted, lastSeq, id]);
  const data = useMemo(() => branch ? ({ ...main.data, record } as Data<T>) : main.data, [branch?.id, main.data, record]);
  const query: DraftQuery<T> = {
    data, isError: main.isError && !spec || !!branch && view.isError && id !== "new", isFetching: main.isFetching || view.isFetching,
    isLoading: main.isLoading || !!branch && view.isLoading, isPlaceholderData: main.isPlaceholderData,
    refetch: async () => {
      if (!branch) { const answer = await main.refetch(); return { isSuccess: answer.isSuccess, data: answer.data }; }
      const [first, second] = await Promise.all([main.refetch(), view.refetch(), list.refetch()]);
      const nextSpec = (second.data?.branch ?? undefined) as Record<string, unknown> | undefined, nextBase = first.data?.record;
      return { isSuccess: (first.isSuccess || !!nextSpec) && second.isSuccess, data: { ...first.data, record: { ...(nextBase ?? { id, state: "draft" }), ...(nextSpec ? withBuild(nextSpec) : {}), id, revision: (nextBase?.revision ?? 0) + (list.data?.find(item => item.id === branch.id)?.resources.find(item => item.type === type && item.id === id)?.lastSeq ?? lastSeq) } as T & EntityRecord } };
    },
  };
  const save = async (target: string, payload: Record<string, unknown>) => {
    const next: Record<string, unknown> = { ...(target === id ? spec ?? {} : {}) };
    for (const [name, value] of Object.entries(payload)) { if (value === null || value === undefined) delete next[name]; else next[name] = value; }
    const exists = target === id ? !!(base || spec) : false;
    await actions.save(branch!.id, type, target, { op: exists ? "update" : "create", spec: next });
  };
  return { query, scope: branch?.id ?? "", branch, save, mainRevision: base?.revision ?? 0, publishRefusal: branch ? t("A branch is published by merging it. Use Save in the branch bar to review, propose and merge.") : undefined };
}
