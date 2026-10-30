import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useQueryClient } from "@tanstack/react-query";
import { t } from "@platform/ui";

export class BranchFailure extends Error {
  constructor(message: string, readonly status: number, readonly conflicts?: Api.BranchResourceDiff[]) { super(message); }
}

const enc = encodeURIComponent;
export const branchPath = (id: string, tail = "") => `/v1/branches/${enc(id)}${tail}`;
export const resourcePath = (id: string, type: string, draft: string) => branchPath(id, `/resources/${enc(type)}/${enc(draft)}`);

export function useBranchRead<T>(path: string, enabled = true) {
  return useReadQuery<T>(path, undefined, enabled);
}

export function useBranches(enabled = true) {
  return useBranchRead<Api.BranchDetail[]>("/v1/branches", enabled);
}

export function useBranchDetail(id?: string) {
  return useBranchRead<Api.BranchDetail>(branchPath(id ?? ""), !!id);
}

export function useBranchActions() {
  const { client } = useHost();
  const queries = useQueryClient();
  const refresh = async () => {
    client.refreshLiveReads();
    await queries.invalidateQueries({ predicate: query => typeof query.queryKey[2] === "string" && query.queryKey[2].startsWith("/v1/branches") });
  };
  const send = async <T>(method: "POST" | "PUT", path: string, body?: unknown): Promise<T> => {
    const answer = await client.call<T & { conflicts?: Api.BranchResourceDiff[] }>(method, path, body ?? {});
    if (!answer.ok) {
      await refresh();
      throw new BranchFailure(apiErrorMessage(answer.body) ?? t("The branch request was refused."), answer.status, answer.status === 409 ? answer.body.conflicts : undefined);
    }
    await refresh();
    return answer.body;
  };
  return {
    refresh,
    create: (request: Api.BranchCreateRequest) => send<Api.BranchDetail>("POST", "/v1/branches", request),
    close: (id: string) => send<Api.BranchDetail>("POST", branchPath(id, "/close")),
    save: (id: string, type: string, draft: string, request: Api.BranchSaveRequest) => send<Api.BranchDetail>("PUT", resourcePath(id, type, draft), request),
    compile: (id: string, type: string, draft: string) => send<Api.BranchDetail>("POST", `${resourcePath(id, type, draft)}/compile`),
    checks: (id: string, request: Api.BranchCheckRequest = {}) => send<Api.BranchChecks>("POST", branchPath(id, "/checks"), request),
    recheck: (id: string, request: Api.BranchCheckRequest = {}) => send<Api.BranchDetail>("POST", branchPath(id, "/recheck"), request),
    rebase: (id: string, request: Api.BranchResolveRequest = {}) => send<Api.BranchDetail>("POST", branchPath(id, "/rebase"), request),
    propose: (id: string, request: Api.BranchProposeRequest) => send<Api.BranchDetail>("POST", branchPath(id, "/proposal"), request),
    review: (id: string, request: Api.BranchReviewRequest) => send<Api.BranchDetail>("POST", branchPath(id, "/proposal/review"), request),
    withdraw: (id: string) => send<Api.BranchDetail>("POST", branchPath(id, "/proposal/withdraw")),
    merge: (id: string, request: Api.BranchMergeRequest = {}) => send<Api.BranchMergeResult>("POST", branchPath(id, "/merge"), request),
  };
}

export function useBranchChanges(id?: string) {
  return useBranchRead<Api.BranchChangeEntry[]>(branchPath(id ?? "", "/changes?specs=true"), !!id);
}

export function branchTitles(entries: readonly Api.BranchChangeEntry[] = []): Record<string, string> {
  const titles: Record<string, string> = {};
  for (const entry of entries) {
    const title = [entry.spec, entry.base].map(spec => spec && typeof spec === "object" ? (spec as { title?: unknown; name?: unknown }) : undefined)
      .map(spec => typeof spec?.title === "string" && spec.title ? spec.title : typeof spec?.name === "string" ? spec.name : "").find(Boolean);
    if (title) titles[`${entry.type}/${entry.id}`] = title;
  }
  return titles;
}
