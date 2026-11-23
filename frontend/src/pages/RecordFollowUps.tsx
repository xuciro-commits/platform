import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Text } from "@astryxdesign/core/Text";
import { PlusIcon, RectangleGroupIcon } from "@heroicons/react/24/outline";
import { type ReactNode, useMemo, useState } from "react";
import { useRecordHref } from "../apps/entries";
import { type Api, useRead, useReads } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { stepIntent, useAct } from "./act";
import { type Described, describe } from "./describe";
import { DotMark } from "./DotMark";
import { iconOf } from "./icons";
import { type Dot, dots } from "./Transitions";
import { plain, present } from "./values";

type Row = Record<string, unknown> & { id: string; revision: number; created?: { at?: string; by?: string }; changed?: { at?: string } };
type Source = { described: Described; field: string };

export type FollowUp = {
  key: string;
  at: string;
  by: string;
  icon: ReturnType<typeof iconOf>;
  kind: string;
  title: string;
  href: string;
  state?: { label: string; dot: Dot };
  due?: string;
  text?: string;
  actions: ReactNode;
};

export function useFollowUps(described: Described, id: string, entities: Api.EntityInfo[], catalog: Api.Action[]) {
  const href = useRecordHref();
  const act = useAct();
  const [creating, setCreating] = useState<Source>();
  const [failure, setFailure] = useState("");
  const sources = useMemo<Source[]>(() => entities.flatMap((entity) => {
    const field = entity.fields.find((candidate) => candidate.type === "reference" && candidate.ref === described.type && candidate.oneOf);
    return field ? [{ described: describe(entity, catalog), field: field.name }] : [];
  }), [entities, catalog, described.type]);
  const reads = useReads<Api.RecordPage>(sources.map(({ described: source, field }) =>
    `/v1/records/${encodeURIComponent(source.type)}?${new URLSearchParams({ limit: "50", domain: JSON.stringify([[field, "=", id]]) })}`));
  const directory = useRead<Api.MemberOption[]>(sources.length ? "/v1/member-directory" : null);
  const people: Record<string, string> = directory.status === "ready" ? Object.fromEntries(directory.data.map((member) => [member.id, member.name])) : {};
  const items: FollowUp[] = [];
  if (reads.status === "ready") {
    sources.forEach(({ described: source }, index) => {
      const page = reads.data[index];
      const due = source.entity.fields.find((field) => field.type === "date" || field.type === "datetime");
      const body = source.entity.fields.find((field) => field.type === "longtext" && !field.readOnly);
      for (const row of page.records as Row[]) {
        const values = Object.fromEntries(Object.entries(row).map(([key, value]) => [key, plain(value)]));
        const lifecycle = source.lifecycle;
        const current = lifecycle ? String(values[lifecycle.field] ?? "") : "";
        const state = lifecycle && (lifecycle.states.length > 1) ? source.state(values) : undefined;
        const allowed = (schema: string) => page.actions?.[row.id]?.some((action) => action.schema === schema && action.allowed);
        const name = source.name(values, row.id);
        const steps = source.steps.filter(({ transition, action }) => transition.from.includes(current) && allowed(action.schema));
        items.push({
          key: `${source.type}/${row.id}`,
          at: row.changed?.at ?? row.created?.at ?? "",
          by: row.created?.by ?? "",
          icon: iconOf(source.entity.icon, RectangleGroupIcon),
          kind: t(source.entity.title),
          title: name,
          href: href(source.type, row.id),
          state: state && { label: t(state.title), dot: dots[(state.tone ?? "neutral") as keyof typeof dots] ?? dots.neutral },
          due: due && values[due.name] ? present(due, values[due.name]) : undefined,
          text: body && values[body.name] ? String(values[body.name]) : undefined,
          actions: steps.length ? (
            <HStack gap={1} wrap="wrap">
              {steps.map((step) => (
                <Button key={step.action.schema} label={t(step.transition.title)} variant="secondary" size="sm"
                  onClick={() => act.run(stepIntent(source, step, current, { id: row.id, revision: row.revision, name }, { onRefused: setFailure }))} />
              ))}
            </HStack>
          ) : null,
        });
      }
    });
  }
  const adders = sources.filter((source) => source.described.create).map((source) => (
    <Button key={source.described.type} label={`${t("Add")} ${t(source.described.entity.title)}`} variant="secondary" size="sm"
      icon={<Icon icon={PlusIcon} size="sm" />} onClick={() => setCreating(source)} />
  ));
  const element = (
    <>
      {act.element}
      {creating?.described.create && (
        <ActionDialog action={creating.described.create} described={creating.described.entity} heading={t(creating.described.create.title)}
          confirm={t("Create")} revision={0} fixed={{ [creating.field]: id }} onClose={() => setCreating(undefined)} onDone={() => setCreating(undefined)} />
      )}
    </>
  );
  return { items, adders, element, people, failure, loading: reads.status === "loading" };
}

export function FollowUpBody({ item }: { item: FollowUp }) {
  return (
    <>
      <HStack gap={2} vAlign="center" wrap="wrap">
        <Link href={item.href} maxLines={1}>{item.title}</Link>
        {item.state && (
          <HStack gap={1} vAlign="center">
            <DotMark dot={item.state.dot} label={item.state.label} />
            <Text type="supporting" color="secondary">{item.state.label}</Text>
          </HStack>
        )}
        {item.due && <Text type="supporting" color="secondary">{item.due}</Text>}
      </HStack>
      {item.text && <Text type="body" color="secondary" maxLines={3}>{item.text}</Text>}
      {item.actions}
    </>
  );
}
