import type { BusinessTime, LinkTraversal, ObjectSet, ObjectSetRequest, Query, QuerySet, RecordSetPredicate } from "./gen/host";

export type ObjectSetOperation = "load" | "aggregate" | "count";
export type RecordQueryInput = Omit<Query, "domain"> & { domain?: unknown[] };
export type AggregateQueryInput = Pick<ObjectSetRequest, "groups" | "measures" | "window" | "histogram" | "maxRows" | "collection"> & {
  domain?: unknown[];
  search?: string;
  archived?: boolean;
  set?: QuerySet;
  traversal?: LinkTraversal;
  businessTime?: BusinessTime;
};

type Selection = { domain?: unknown[]; search?: string; set?: QuerySet; traversal?: LinkTraversal; archived?: boolean; businessTime?: BusinessTime };

function predicateSet(object: string, p: RecordSetPredicate): ObjectSet {
  const set: ObjectSet = {};
  if (Array.isArray(p.domain) && p.domain.length) set.domain = p.domain;
  if (p.search) set.search = p.search;
  if (!p.set) return { object, ...set };
  const operands = p.set.inputs.map(input => predicateSet(object, input));
  if (p.set.op === "union") set.union = operands;
  else if (p.set.op === "intersect") set.intersect = operands;
  else set.subtract = operands;
  return set;
}

export function objectSetOf(object: string, q: Selection): ObjectSet {
  let set = predicateSet(object, { domain: q.domain, search: q.search, set: q.set });
  if (q.traversal) {
    const around = { from: { ids: [q.traversal.id], archived: true }, link: q.traversal.binding, direction: q.traversal.direction };
    set = q.set ? { intersect: [{ around }, set] } : { ...set, object: undefined, around };
  }
  if (q.archived) set.archived = true;
  if (q.businessTime) set.asOf = q.businessTime;
  return set;
}

export function loadRequest(object: string, q: RecordQueryInput = {}): ObjectSetRequest {
  const request: ObjectSetRequest = { set: objectSetOf(object, q) };
  if (q.sort?.length) request.sort = q.sort;
  if (q.offset) request.offset = q.offset;
  if (q.limit) request.limit = q.limit;
  return request;
}

export function interfaceRequest(name: string, q: RecordQueryInput = {}): ObjectSetRequest {
  const set: ObjectSet = { interface: name };
  if (Array.isArray(q.domain) && q.domain.length) set.domain = q.domain;
  if (q.search) set.search = q.search;
  const request: ObjectSetRequest = { set, limit: q.limit || 25 };
  if (q.sort?.length) request.sort = q.sort;
  if (q.offset) request.offset = q.offset;
  return request;
}

export function aggregateRequest(object: string, q: AggregateQueryInput = {}): ObjectSetRequest {
  const request: ObjectSetRequest = { set: objectSetOf(object, q) };
  if (q.groups?.length) request.groups = q.groups;
  if (q.measures?.length) request.measures = q.measures;
  if (q.window) request.window = q.window;
  if (q.histogram) request.histogram = q.histogram;
  if (q.maxRows) request.maxRows = q.maxRows;
  if (q.collection) request.collection = q.collection;
  return request;
}

export function countRequest(object: string, q: Selection = {}): ObjectSetRequest {
  return { set: objectSetOf(object, q) };
}

export const objectSetPath = (op: ObjectSetOperation, request: ObjectSetRequest) => `/v1/object-sets/${op}\n${JSON.stringify(request)}`;
