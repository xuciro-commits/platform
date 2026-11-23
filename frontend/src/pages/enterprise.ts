import { type Api, request } from "../host";

export const elementType = "enterprise.element";
export const personStereotype = "ActualPerson";

export const elementHref = (id: string) => `/admin/people/${encodeURIComponent(id)}`;

export function stereotypesOf(field?: { stereotype?: string; stereotypes?: string[] }) {
  if (field?.stereotypes?.length) return field.stereotypes;
  return field?.stereotype ? [field.stereotype] : undefined;
}

export async function findElements(stereotypes: string[] | undefined, query: string) {
  const params = new URLSearchParams({ limit: "9" });
  if (stereotypes?.length) params.set("stereotype", stereotypes.join(","));
  if (query.trim()) params.set("q", query.trim());
  const answer = await request<Api.EnterpriseQueryResult>(`/v1/enterprise-query?${params}`);
  return answer.elements.map((element) => ({ id: element.id, label: element.name }));
}
