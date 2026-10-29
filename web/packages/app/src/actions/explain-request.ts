import type { Api, Entry } from "@platform/kernel";

export function authzExplainRequest(submission: Entry["submission"], fallbackPrincipal: string, tenant: string): Api.AuthzRequest {
  const target = submission.target;
  return {
    principal: submission.principalId || fallbackPrincipal,
    tenant,
    kind: target?.id ? "write" : "create",
    type: target?.type || "",
    ...(target?.id ? { record: target.id } : {}),
    action: submission.schema?.name || "",
  };
}
