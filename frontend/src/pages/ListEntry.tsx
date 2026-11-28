import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { useToast } from "@astryxdesign/core/Toast";
import { lazy, type ReactNode, Suspense, useMemo, useRef, useState } from "react";
import { type Api, request, useRead, reasonOf, send } from "../host";
import { t } from "../i18n";
import { useSearch } from "../router";
import { type RecordHome, useRecordHome } from "../recordRoutes";
import { useAct } from "./act";
import { ActionDialog } from "./ActionDialog";
import type { Values } from "./describe";
import type { ImportTarget } from "./ImportWizard";

const ImportWizard = lazy(() => import("./ImportWizard").then((module) => ({ default: module.ImportWizard })));
import { type ListColumn, type ListShape, ListView } from "./ListPage";
import { enterable } from "./fieldRules";
import { newView, readView } from "./view";

export type RowAction = { schema: string; id?: string; fixed?: Values; initial?: Values; label?: string; revision?: number };
type Change<Row> = { schema: string; parameter: string; id: (row: Row) => string; fixed?: (row: Row) => Values; value: (row: Row) => unknown };
type EntryColumn<Row> = ListColumn<Row> & { change?: Change<Row> };
export type ListDefinition<Answer, Row> = Omit<ListShape<Answer, Row>, "columns" | "page" | "remote" | "detail" | "link"> & {
  read: string;
  link?: (row: Row, recordHref: RecordHome) => string;
  columns: EntryColumn<Row>[] | { (answer: Answer): EntryColumn<Row>[] };
  more?: (answer: Answer) => string | undefined;
  actions?: { create?: string; row?: (row: Row) => RowAction[] };
  createLabel?: string;
  secondaryCreate?: { schema: string; label: string; creator: (props: { onClose: () => void }) => ReactNode };
  creator?: (props: { onClose: () => void }) => ReactNode;
  detail?: (id: string, row?: Row) => ReactNode;
  import?: (answer: Answer, action: (schema: string) => Api.Action | undefined) => ImportTarget | undefined;
  export?: string;
  query?: string;
};

type Later<Answer> = { path?: string; answers: Answer[]; loading: boolean };
const quoteCell = (cell: string) => `"${cell.replaceAll("\"", "\"\"")}"`;

function nameOf(payload: Values) {
  const words = String(payload.title ?? payload.name ?? "").toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40);
  return `${/^[a-z]/.test(words) ? words : `n${words && `-${words}`}`}-${crypto.randomUUID().slice(0, 6)}`;
}

export function listPage<Answer, Row>(definition: ListDefinition<Answer, Row>) {
  return function ListEntry() {
    const toast = useToast();
    const recordHref = useRecordHome();
    const link = definition.link;
    const currentSearch = useSearch();
    const term = definition.query ? readView(new URLSearchParams(currentSearch).get("view"), newView([])).search.trim() : "";
    const path = term ? `${definition.read}${definition.read.includes("?") ? "&" : "?"}${definition.query}=${encodeURIComponent(term)}` : definition.read;
    const first = useRead<Answer>(path);
    const unsearched = useRead<Answer>(term ? definition.read : null);
    const [later, setLater] = useState<Later<Answer>>({ answers: [], loading: false });
    const own = later.path === path;
    const answers = first.status === "ready" ? [first.data, ...(own ? later.answers : [])] : [];
    const next = definition.more && answers.length ? definition.more(answers[answers.length - 1]) : undefined;
    const rows = useMemo(() => {
      const seen = new Set<string>();
      const out: Row[] = [];
      for (const answer of answers) {
        for (const row of definition.rows(answer)) {
          const key = definition.id(row);
          if (seen.has(key)) continue;
          seen.add(key);
          out.push(row);
        }
      }
      return out;
    }, [first, own, later.answers]);
    const loadMore = () => {
      if (!next || (own && later.loading)) return;
      const requested = path;
      setLater({ path: requested, answers: own ? later.answers : [], loading: true });
      request<Answer>(next).then(
        (answer) => setLater((previous) => (previous.path === requested ? { path: requested, answers: [...previous.answers, answer], loading: false } : previous)),
        (problem: unknown) => {
          setLater((previous) => (previous.path === requested ? { ...previous, loading: false } : previous));
          toast({ body: reasonOf(problem), type: "error" });
        },
      );
    };
    const declared = useMemo(
      () => (typeof definition.columns === "function" ? (first.status === "ready" ? definition.columns(first.data) : []) : definition.columns),
      [first],
    );
    const catalog = useRead<Api.Action[]>(definition.actions || definition.import || declared.some((column) => column.change) ? "/v1/actions" : null);
    const action = (schema: string) => (catalog.status === "ready" ? catalog.data.find((candidate) => candidate.schema === schema) : undefined);
    const columns = useMemo(() => declared.map(({ change, ...column }): ListColumn<Row> => {
      const found = change && action(change.schema);
      const parameter = found?.payload.find((candidate) => candidate.name === change?.parameter);
      return change && found && parameter
        ? { ...column, edit: { parameter, value: change.value, save: (row, value) => send(found, change.id(row), { ...change.fixed?.(row), [change.parameter]: value }) } }
        : column;
    }), [declared, catalog]);
    const create = definition.actions?.create ? action(definition.actions.create) : undefined;
    const creatable = create ? !create.payload.some((parameter) => parameter.required && !enterable(parameter)) : Boolean(definition.creator && !definition.actions?.create && first.status === "ready");
    const importTarget = first.status === "ready" && definition.import ? definition.import(first.data, action) : undefined;
    const [creating, setCreating] = useState(false);
    const [secondaryCreating, setSecondaryCreating] = useState(false);
    const secondary = definition.secondaryCreate;
    const secondaryAllowed = Boolean(secondary && action(secondary.schema));
    const [importing, setImporting] = useState(false);
    const exportRef = useRef<(() => { headers: string[]; rows: string[][] }) | null>(null);
    const act = useAct();
    const [refusal, setRefusal] = useState({ id: "", reason: "" });

    const exportList = () => {
      const snapshot = exportRef.current?.();
      if (!snapshot || !definition.export) return;
      const csv = "\uFEFF" + [snapshot.headers, ...snapshot.rows].map((line) => line.map(quoteCell).join(",")).join("\r\n");
      const href = URL.createObjectURL(new Blob([csv], { type: "text/csv;charset=utf-8" }));
      Object.assign(document.createElement("a"), { href, download: `${definition.export}.csv` }).click();
      setTimeout(() => URL.revokeObjectURL(href), 1000);
    };

    const moreItems = [
      ...(importTarget ? [{ id: "import-rows", label: t("Import"), onClick: () => setImporting(true) }] : []),
      ...(definition.export ? [{ id: "export-rows", label: t("Export as CSV"), onClick: exportList }] : []),
    ];

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
              {offered.map((offer, index) => (
                <Button
                  key={`${offer.schema}:${index}`}
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
          {definition.detail?.(id, row)}
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
            link: link ? (row) => link(row, recordHref) : undefined,
            rows: () => rows,
            detail,
            carryView: true,
            remote: definition.query ? "search" : undefined,
            unfiltered: unsearched.status === "ready" && !definition.more?.(unsearched.data) ? definition.rows(unsearched.data).length : undefined,
            page: definition.more ? { total: rows.length, loaded: rows.length, hasMore: Boolean(next), loading: own && later.loading, loadMore } : undefined,
          }}
          read={first}
          exportRef={definition.export ? exportRef : undefined}
          primary={creatable || secondaryAllowed || moreItems.length > 0 ? (
            <HStack gap={2} vAlign="center">
              {secondaryAllowed && secondary && <Button label={t(secondary.label)} variant="secondary" onClick={() => setSecondaryCreating(true)} />}
              {creatable && <Button label={t(definition.createLabel ?? "New")} variant="primary" onClick={() => setCreating(true)} />}
              {moreItems.length > 0 && <MoreMenu label={t("More actions")} alignment="end" items={moreItems} />}
            </HStack>
          ) : undefined}
        />
        {importing && importTarget && <Suspense fallback={null}><ImportWizard target={importTarget} onClose={() => setImporting(false)} /></Suspense>}
        {act.element}
        {secondaryCreating && secondaryAllowed && secondary && secondary.creator({ onClose: () => setSecondaryCreating(false) })}
        {creating && creatable && definition.creator && definition.creator({ onClose: () => setCreating(false) })}
        {creating && create && !definition.creator && (
          <ActionDialog action={create} heading={t(create.title)} confirm={t("Create")} onClose={() => setCreating(false)} onDone={() => setCreating(false)}
            perform={async (payload) => void (await send(create, typeof payload.id === "string" ? payload.id : nameOf(payload), payload))} />
        )}
      </>
    );
  };
}
