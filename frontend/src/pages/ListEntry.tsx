import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { useToast } from "@astryxdesign/core/Toast";
import { useMemo, useState } from "react";
import { type Api, type Read, request, useRead } from "../host";
import { t } from "../i18n";
import { useSearch } from "../router";
import { reasonOf, send, useAct } from "./act";
import { ActionDialog } from "./ActionDialog";
import type { Values } from "./describe";
import { type ListColumn, type ListShape, ListView } from "./ListPage";
import { enterable } from "./ParameterInput";
import { newView, readView } from "./view";

export type RowAction = { schema: string; id?: string; fixed?: Values; initial?: Values; label?: string; revision?: number };
export type ListDefinition<Answer, Row> = Omit<ListShape<Answer, Row>, "columns" | "page" | "remote"> & {
  read: string;
  columns: ListColumn<Row>[] | { (answer: Answer): ListColumn<Row>[] };
  more?: (answer: Answer) => string | undefined;
  actions?: { create?: string; row?: (row: Row) => RowAction[] };
  query?: string;
};

type Later<Answer> = { base?: Read<Answer>; answers: Answer[]; loading: boolean };

function nameOf(payload: Values) {
  const words = String(payload.title ?? payload.name ?? "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40);
  return `${/^[a-z]/.test(words) ? words : `n${words && `-${words}`}`}-${crypto.randomUUID().slice(0, 6)}`;
}

export function listPage<Answer, Row>(definition: ListDefinition<Answer, Row>) {
  return function ListEntry() {
    const toast = useToast();
    const currentSearch = useSearch();
    const term = definition.query ? readView(new URLSearchParams(currentSearch).get("view"), newView([])).search.trim() : "";
    const path = term ? `${definition.read}${definition.read.includes("?") ? "&" : "?"}${definition.query}=${encodeURIComponent(term)}` : definition.read;
    const first = useRead<Answer>(path);
    const unsearched = useRead<Answer>(term ? definition.read : null);
    const [later, setLater] = useState<Later<Answer>>({ answers: [], loading: false });
    const own = later.base === first;
    const answers = first.status === "ready" ? [first.data, ...(own ? later.answers : [])] : [];
    const next = definition.more && answers.length ? definition.more(answers[answers.length - 1]) : undefined;
    const rows = answers.flatMap(definition.rows);
    const loadMore = () => {
      if (!next || (own && later.loading)) return;
      setLater({ base: first, answers: own ? later.answers : [], loading: true });
      request<Answer>(next).then(
        (answer) => setLater((previous) => (previous.base === first ? { base: first, answers: [...previous.answers, answer], loading: false } : previous)),
        (problem: unknown) => {
          setLater((previous) => ({ ...previous, loading: false }));
          toast({ body: reasonOf(problem), type: "error" });
        },
      );
    };
    const columns = useMemo(
      () => (typeof definition.columns === "function" ? (first.status === "ready" ? definition.columns(first.data) : []) : definition.columns),
      [first],
    );

    const catalog = useRead<Api.Action[]>(definition.actions ? "/v1/actions" : null);
    const action = (schema: string) => (catalog.status === "ready" ? catalog.data.find((candidate) => candidate.schema === schema) : undefined);
    const create = definition.actions?.create ? action(definition.actions.create) : undefined;
    const creatable = create && !create.payload.some((parameter) => parameter.required && !enterable(parameter));
    const [creating, setCreating] = useState(false);
    const act = useAct();
    const [refusal, setRefusal] = useState({ id: "", reason: "" });

    const rowActions = definition.actions?.row;
    const detail = rowActions || definition.detail ? (id: string) => {
      const row = rows.find((candidate) => definition.id(candidate) === id);
      const offered = row && rowActions ? rowActions(row).flatMap((offer) => {
        const found = action(offer.schema);
        return found ? [{ ...offer, action: found }] : [];
      }) : [];
      const name = (row && columns[0]?.text(row)) || id;
      return (
        <VStack gap={2}>
          {refusal.id === id && refusal.reason && <Banner status="error" title={t("Not saved")} description={refusal.reason} />}
          {offered.length > 0 && (
            <HStack gap={2} wrap="wrap">
              {offered.map((offer) => (
                <Button
                  key={offer.schema}
                  label={t(offer.label ?? offer.action.title)}
                  variant="secondary"
                  size="sm"
                  isDisabled={act.busy}
                  onClick={() =>
                    act.run({
                      action: offer.action,
                      id: offer.id ?? id,
                      revision: offer.revision,
                      fixed: offer.fixed,
                      initial: offer.initial,
                      label: t(offer.label ?? offer.action.title),
                      name,
                      reversible: false,
                      onRefused: (reason) => setRefusal({ id, reason }),
                    })
                  }
                />
              ))}
            </HStack>
          )}
          {definition.detail?.(id)}
        </VStack>
      );
    } : undefined;

    return (
      <>
        <ListView
          key={definition.read}
          definition={{
            ...definition,
            columns,
            rows: () => rows,
            detail,
            carryView: true,
            remote: definition.query ? "search" : undefined,
            unfiltered: unsearched.status === "ready" && !definition.more?.(unsearched.data) ? definition.rows(unsearched.data).length : undefined,
            page: definition.more ? { total: rows.length, loaded: rows.length, hasMore: Boolean(next), loading: own && later.loading, loadMore } : undefined,
          }}
          read={first}
          primary={creatable ? <Button label={t("New")} variant="primary" onClick={() => setCreating(true)} /> : undefined}
        />
        {act.element}
        {creating && create && (
          <ActionDialog action={create} heading={t(create.title)} confirm={t("Create")} onClose={() => setCreating(false)} onDone={() => setCreating(false)}
            perform={async (payload) => void (await send(create, typeof payload.id === "string" ? payload.id : nameOf(payload), payload))} />
        )}
      </>
    );
  };
}
