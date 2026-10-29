import type { EnterpriseQueryResult } from "./host";

export type EnterpriseStereotype =
	| "ActualOrganization"
	| "ActualPost"
	| "ActualPerson"
;

export type EnterpriseViewpoint =
	| "organization"
;

export const enterpriseWrites = [
	"enterprise.element.add",
	"enterprise.element.edit",
	"enterprise.element.close",
	"enterprise.element.delete",
	"enterprise.element.reopen",
	"enterprise.element.reinstate",
	"enterprise.kind.edit",
	"enterprise.tenant-link.save",
	"enterprise.tenant-link.remove",
	"enterprise.kind.add",
	"enterprise.relationship.add",
	"enterprise.relationship.end",
	"enterprise.relationship.change",
] as const;

export type EnterpriseWrite = (typeof enterpriseWrites)[number];

export type EnterpriseElementAddPayload = {
	stereotype: EnterpriseStereotype;
	name: string;
	kind?: string;
	shortName?: string;
	legal?: boolean;
	external?: boolean;
	from?: string;
	until?: string;
};

export type EnterpriseElementEditPayload = {
	name?: string;
	kind?: string;
	shortName?: string;
	calendar?: string;
	published?: boolean;
};

export type EnterpriseElementClosePayload = {
	until: string;
	reason: string;
};

export type EnterpriseElementDeletePayload = {
	reason: string;
};

export type EnterpriseElementReopenPayload = {
	from?: string;
	reason: string;
};

export type EnterpriseElementReinstatePayload = {
	reason: string;
};

export type EnterpriseKindEditPayload = {
	name: string;
};

export type EnterpriseTenantLinkSavePayload = {
	organization: string;
	tenant: string;
	root: string;
	connection: string;
};

export type EnterpriseTenantLinkRemovePayload = {
};

export type EnterpriseKindAddPayload = {
	name: string;
	kind: string;
	matrix?: boolean;
};

export type EnterpriseRelationshipAddPayload = {
	stereotype: string;
	source: string;
	target: string;
	kind?: string;
	role?: string;
	relation?: string;
	share?: number;
	primary?: boolean;
	from?: string;
	until?: string;
};

export type EnterpriseRelationshipEndPayload = {
	until?: string;
};

export type EnterpriseRelationshipChangePayload = {
	stereotype: string;
	source: string;
	target: string;
	kind?: string;
	role?: string;
	relation?: string;
	share?: number;
	primary?: boolean;
	from?: string;
	until?: string;
	replacement: string;
};

export type EnterpriseWritePayloads = {
	"enterprise.element.add": EnterpriseElementAddPayload;
	"enterprise.element.edit": EnterpriseElementEditPayload;
	"enterprise.element.close": EnterpriseElementClosePayload;
	"enterprise.element.delete": EnterpriseElementDeletePayload;
	"enterprise.element.reopen": EnterpriseElementReopenPayload;
	"enterprise.element.reinstate": EnterpriseElementReinstatePayload;
	"enterprise.kind.edit": EnterpriseKindEditPayload;
	"enterprise.tenant-link.save": EnterpriseTenantLinkSavePayload;
	"enterprise.tenant-link.remove": EnterpriseTenantLinkRemovePayload;
	"enterprise.kind.add": EnterpriseKindAddPayload;
	"enterprise.relationship.add": EnterpriseRelationshipAddPayload;
	"enterprise.relationship.end": EnterpriseRelationshipEndPayload;
	"enterprise.relationship.change": EnterpriseRelationshipChangePayload;
};

export type EnterpriseTarget = { type: string; id: string };

export type EnterpriseDecide = (schema: string, target: EnterpriseTarget, payload: Record<string, unknown>) => Promise<unknown>;

export function enterpriseWrite<K extends EnterpriseWrite>(
	decide: EnterpriseDecide,
	schema: K,
	target: EnterpriseTarget,
	payload: EnterpriseWritePayloads[K],
): Promise<unknown> {
	return decide(schema, target, payload);
}

export type EnterpriseGet = <T>(path: string) => Promise<T>;

export type EnterpriseQueryFilters = {
	stereotype?: EnterpriseStereotype | EnterpriseStereotype[];
	kind?: string;
	q?: string;
	alive?: string;
	limit?: number;
};

export async function queryElements(get: EnterpriseGet, filters: EnterpriseQueryFilters = {}): Promise<EnterpriseQueryResult> {
	const params = new URLSearchParams();
	const stereotypes = filters.stereotype === undefined ? [] : Array.isArray(filters.stereotype) ? filters.stereotype : [filters.stereotype];
	if (stereotypes.length > 0) params.set("stereotype", stereotypes.join(","));
	if (filters.kind) params.set("kind", filters.kind);
	if (filters.q) params.set("q", filters.q);
	if (filters.alive) params.set("alive", filters.alive);
	if (filters.limit) params.set("limit", String(filters.limit));
	const query = params.toString();
	return get<EnterpriseQueryResult>(query === "" ? "/v1/enterprise-query" : "/v1/enterprise-query?" + query);
}

export async function resolveElements(get: EnterpriseGet, ids: readonly string[]): Promise<EnterpriseQueryResult> {
	if (ids.length === 0) return { elements: [] };
	return get<EnterpriseQueryResult>("/v1/enterprise-resolve?ids=" + encodeURIComponent(ids.join(",")));
}
