import { useEffect, useState } from "react";
import { type Api, type Read, useRead } from "../host";
import { domainOf, hasFilters, type ViewState } from "./view";

type Row = Record<string, unknown>;
export const pageSize = 100;

type LoadedPage = {
  signature: string;
  records: Record<string, unknown>[];
  refs: NonNullable<Api.RecordPage["refs"]>;
  formats: NonNullable<Api.RecordPage["formats"]>;
  actions: NonNullable<Api.RecordPage["actions"]>;
  total: number;
  partial: boolean;
};

const noPage = (signature = ""): LoadedPage => ({ signature, records: [], refs: {}, formats: {}, actions: {}, total: 0, partial: false });

function mergePage(loaded: LoadedPage, page: Api.RecordPage): LoadedPage {
  const records = new Map(loaded.records.map((record) => [String(record.id), record]));
  for (const value of page.records) {
    const record = value as Row;
    records.set(String(record.id), record);
  }
  const refs = { ...loaded.refs };
  for (const [field, values] of Object.entries(page.refs ?? {})) refs[field] = { ...refs[field], ...values };
  const actions = { ...loaded.actions };
  for (const [id, available] of Object.entries(page.actions ?? {})) actions[id] = available;
  return {
    signature: loaded.signature,
    records: [...records.values()],
    refs,
    formats: { ...loaded.formats, ...page.formats },
    actions,
    total: page.total,
    partial: loaded.partial || Boolean(page.partial),
  };
}

export function useRecordPages(type: string, view: ViewState, enabled: boolean, choiceValue: (field: string, value: string) => string, externalDomain: string, range: unknown[][]) {
  const baseFilters = view.filters.filter((filter) => !filter.field.includes(":"));
  const conditions = [...domainOf(baseFilters, choiceValue), ...range];
  try {
    const external = externalDomain ? (JSON.parse(externalDomain) as unknown) : [];
    if (Array.isArray(external)) conditions.push(...external.filter(Array.isArray));
  } catch {
    conditions.push(["id", "in", []]);
  }
  const domain = conditions.length ? JSON.stringify(conditions) : "";
  const sort = view.sort.filter((item) => !item.sortKey.includes(":")).map((item) => (item.direction === "descending" ? "-" : "") + item.sortKey).join(",");
  const signature = JSON.stringify([type, enabled, domain, sort, view.search]);
  const [offset, setOffset] = useState(0);
  const [loaded, setLoaded] = useState<LoadedPage>(() => noPage());
  const currentOffset = loaded.signature === signature ? offset : 0;
  const query = new URLSearchParams({ offset: String(currentOffset), limit: String(pageSize), actions: "1" });
  if (domain) query.set("domain", domain);
  if (sort) query.set("sort", sort);
  if (view.search.trim()) query.set("search", view.search.trim());
  const path = enabled ? `/v1/records/${type}?${query}` : null;
  const exported = new URLSearchParams(query);
  ["offset", "limit", "actions"].forEach((name) => exported.delete(name));
  const page = useRead<Api.RecordPage>(path);
  const filtered = hasFilters(view, externalDomain);
  const count = useRead<Pick<Api.RecordPage, "total">>(enabled && filtered ? `/v1/records/${type}?limit=1` : null);
  useEffect(() => {
    if (loaded.signature !== signature) setOffset(0);
  }, [loaded.signature, signature]);
  useEffect(() => {
    if (page.status !== "ready" || page.path !== path) return;
    setLoaded((previous) => mergePage(previous.signature === signature && currentOffset > 0 ? previous : noPage(signature), page.data));
  }, [page, path, signature, currentOffset]);
  const answer: Api.RecordPage = {
    records: loaded.records,
    refs: loaded.refs,
    formats: loaded.formats,
    actions: loaded.actions,
    total: loaded.total,
    partial: loaded.partial,
  };
  const read: Read<Api.RecordPage> =
    loaded.signature === signature
      ? { status: "ready", data: answer }
      : page.status === "refused" || page.status === "error" || page.status === "loading"
        ? page
        : { status: "loading" };
  return {
    read,
    answer,
    exportPath: `/v1/export/${type}?${exported}`,
    total: loaded.total,
    unfiltered: filtered ? count.status === "ready" ? count.data.total : undefined : loaded.total,
    loaded: loaded.records.length,
    hasMore: loaded.signature === signature && loaded.records.length < loaded.total,
    loading: loaded.signature === signature && page.status === "loading",
    loadMore: () => {
      if (page.status === "loading") return;
      setOffset(loaded.records.length);
    },
  };
}
