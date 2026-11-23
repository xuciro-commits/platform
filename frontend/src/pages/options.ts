import { useEffect, useMemo, useState } from "react";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import type { Described } from "./describe";
import { elementType, stereotypesOf } from "./enterprise";
import { type Dot, dots } from "./Transitions";
import { plain, shown } from "./values";
import { newView, readView } from "./view";

type Row = Record<string, unknown>;
export type Options = Record<string, { value: string; label: string; dot?: Dot }[]>;
export const palette: Record<string, Dot> = { gray: "neutral", amber: "warning", blue: "blue", purple: "purple", cyan: "cyan", pink: "pink", green: "green", orange: "orange", red: "red" };

export function useOptions(described: Described | undefined, only?: string[]) {
  const wanted = only?.join(",");
  const picked = (field: Api.FieldInfo) => wanted === undefined || wanted.split(",").includes(field.name);
  const members = useRead<Api.MemberOption[]>(described?.entity.fields.some((field) => field.type === "member" && picked(field)) ? "/v1/member-directory" : null);
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const [references, setReferences] = useState<Options>({});
  const targets = useMemo(() => (described ? described.entity.fields.filter((field) => field.type === "reference" && field.ref &&
    (wanted === undefined || wanted.split(",").includes(field.name))).map((field) => [field.name, String(field.ref), stereotypesOf(field)] as const) : []), [described, wanted]);
  useEffect(() => {
    if (types.status !== "ready" || targets.length === 0) return;
    let live = true;
    void Promise.all(
      targets.map(async ([name, type, stereotypes]) => {
        if (type === elementType) {
          const params = new URLSearchParams({ limit: "200" });
          if (stereotypes?.length) params.set("stereotype", stereotypes.join(","));
          const answer = await request<Api.EnterpriseQueryResult>(`/v1/enterprise-query?${params}`).catch(() => undefined);
          return [name, (answer?.elements ?? []).map((element) => ({ value: element.id, label: element.name || element.id }))] as const;
        }
        const target = types.data.find((entity) => entity.type === type);
        const display = target?.display;
        const color = target?.fields.some((field) => field.name === "color" && field.type === "choice");
        const sort = readView(target?.view == null ? null : JSON.stringify(target.view), newView([])).sort.map((item) => (item.direction === "descending" ? "-" : "") + item.sortKey).join(",");
        const answer = await request<Api.RecordPage>(`/v1/records/${encodeURIComponent(type)}?${new URLSearchParams(sort ? { limit: "200", sort } : { limit: "200" })}`).catch(() => undefined);
        const rows = (answer?.records ?? []) as Row[];
        return [name, rows.map((row) => ({
          value: String(row.id),
          label: shown(plain(display ? row[display] : row.id)) || String(row.id),
          dot: color ? palette[String(plain(row.color))] ?? dots.neutral : undefined,
        }))] as const;
      }),
    ).then((entries) => live && setReferences(Object.fromEntries(entries)));
    return () => {
      live = false;
    };
  }, [targets, types]);
  return useMemo<Options>(() => {
    const people = members.status === "ready" ? members.data.map((member) => ({ value: member.id, label: member.name })) : undefined;
    const options: Options = { ...references };
    for (const field of described?.entity.fields ?? []) if (field.type === "member" && people) options[field.name] = people;
    return options;
  }, [members, references, described]);
}

type Counted = { rows: Row[] };

export function useRelatedCounts(described: Described | undefined, entities: Api.EntityInfo[] | undefined, ids: string[], enabled: boolean) {
  const relations = useMemo(() => described && entities ? entities.flatMap((entity) => entity.fields
    .filter((field) => field.type === "reference" && field.ref === described.type && field.inverse)
    .map((field) => ({ type: entity.type, field: field.name, label: entity.plural }))) : [], [described, entities]);
  const wanted = enabled ? ids.join(",") : "";
  const [counts, setCounts] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!wanted || relations.length === 0) return;
    let live = true;
    const chosen = wanted.split(",");
    void Promise.all(relations.map((relation) => request<Counted>("/v1/object-sets/aggregate", {
      body: { set: { object: relation.type, domain: [[relation.field, "in", chosen]] }, groups: [relation.field], measures: ["count"] },
    }).then((answer) => ({ relation, rows: answer.rows ?? [] })).catch(() => ({ relation, rows: [] as Row[] })))).then((answers) => {
      if (!live) return;
      const parts: Record<string, string[]> = {};
      for (const { relation, rows } of answers) {
        for (const row of rows) {
          const count = Number(row.count);
          if (count > 0) (parts[String(row[relation.field])] ??= []).push(`${t(relation.label)} ${count}`);
        }
      }
      setCounts(Object.fromEntries(Object.entries(parts).map(([id, labels]) => [id, labels.join(" · ")])));
    });
    return () => {
      live = false;
    };
  }, [relations, wanted]);
  return counts;
}
