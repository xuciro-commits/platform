import type { Api, Entry } from "@platform/kernel";

/** The generated AuthzRequest behind "why was this refused": the submission's
 * own action schema — never the authority that carries it — its principal,
 * and the record it targeted. The engine answers per `action:<schema>` owner. */
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
