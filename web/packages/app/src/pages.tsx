// Bounded page composition (ADR-0032 13b). The descriptor comes from the
// host's installed definition registry. Live operation and the local preview
// use the same list/detail layout and the UI kit's record components.
import {
  Button, Dialog, Panel, RecordPage, RecordWorkspace, type EntityInfo, type EntityRecord, type FieldInfo,
  type RecordPageData, type RecordSource, type RecordView, t,
} from "@platform/ui";
import { useMemo, useState } from "react";
import { NewActions, PayloadFields } from "./actions";
import { RecordDetail, assetKey, useHost, type Definition } from "./index";

type PageDefinition = Definition & { page: NonNullable<Definition["page"]> };

/** One read-only, permission-filtered descriptor returned by the host. */
export function isPageDefinition(definition: Definition | undefined): definition is PageDefinition {
  return !!definition?.page && definition.ref.kind === "page" && definition.page.layout === "list-detail";
}

/** A code page operates through the current member's record/action contracts. */
export function PageWorkspace({ definition }: { definition: PageDefinition }) {
  const { source } = useHost();
  const [selected, setSelected] = useState<string>();
  const page = definition.page;
  const allowed = page.actions.map((ref) => ref.name);
  return <RecordWorkspace title={page.title} description={page.description} source={source} type={page.object.name} listFields={page.listFields}
    selected={selected} onSelect={setSelected} actions={<NewActions type={page.object.name} allowed={allowed} />}
    detail={(id) => <RecordDetail type={page.object.name} id={id} fields={page.detailFields} allowed={allowed} />} />;
}

const previewValue = (field: FieldInfo, index: number): unknown => {
  if (field.example) return field.type === "integer" || field.type === "decimal" ? Number(field.example) : field.example;
  if (field.type === "choice") return field.choices?.[0] ?? "";
  if (field.type === "reference") return `${field.ref?.split(".").pop()?.toUpperCase() ?? "REF"}-001`;
  if (field.type === "references" || field.type === "tags" || field.type === "lines") return [];
  if (field.type === "integer" || field.type === "decimal") return index + 1;
  if (field.type === "money") return { amount: (index + 1) * 2500, currency: "CNY" };
  if (field.type === "boolean") return index === 0;
  if (field.type === "date") return `2026-10-0${index + 1}`;
  if (field.type === "datetime") return `2026-10-0${index + 1}T09:00`;
  return `${field.title} ${index + 1}`;
};

// Entirely local data and reads. A preview cannot invoke the host's writes,
// model endpoints, file uploads, effects or production record reads.
function previewSource(info: EntityInfo): RecordSource {
  const records: EntityRecord[] = [0, 1, 2].map((index) => ({
    id: `SAMPLE-00${index + 1}`, revision: 1,
    created: { by: "preview", at: "2026-10-01T09:00:00Z" }, changed: { by: "preview", at: "2026-10-01T09:00:00Z" },
    ...Object.fromEntries(info.fields.map((field) => [field.name, previewValue(field, index)])),
  }));
  return {
    entity: (type) => type === info.type ? info : undefined,
    list: async (_type, query): Promise<RecordPageData> => {
      const matching = records.filter((record) => !query.search || JSON.stringify(record).toLowerCase().includes(query.search.toLowerCase()));
      return { total: matching.length, records: matching.slice(query.offset ?? 0, (query.offset ?? 0) + (query.limit ?? 100)) };
    },
    get: async (_type, id): Promise<RecordView> => {
      const record = records.find((r) => r.id === id);
      if (!record) throw new Error(t("Preview record is unavailable."));
      return { record, history: [], related: [], linked: [], activity: [], processes: [], approvals: [], tasks: [], files: [], comments: [], following: false };
    },
  };
}

/** Local, read-only builder preview of the exact installed page descriptor. */
export function PagePreview({ definition, definitions }: { definition: PageDefinition; definitions: Definition[] }) {
  const page = definition.page;
  const object = definitions.find((d) => assetKey(d.ref) === assetKey(page.object));
  const info = object?.entity;
  const source = useMemo(() => info && previewSource(info), [info]);
  const [selected, setSelected] = useState<string | undefined>("SAMPLE-001");
  const [action, setAction] = useState<Definition>();
  const [values, setValues] = useState<Record<string, unknown>>({});
  if (!info || !source) return <p role="alert">{t("This definition is unavailable.")}</p>;
  const actions = page.actions.flatMap((ref) => {
    const found = definitions.find((d) => assetKey(d.ref) === assetKey(ref));
    return found?.action ? [found] : [];
  });
  return <>
    <RecordWorkspace title={page.title} description={page.description} source={source} type={info.type} listFields={page.listFields}
      selected={selected} onSelect={setSelected}
      notice={<Panel role="status" className="mb-3 text-xs text-muted">{t("Preview uses sample data. Actions do not run.")}</Panel>}
      actions={<div className="flex flex-wrap gap-1">{actions.map((a) => <Button key={assetKey(a.ref)} size="sm" onClick={() => { setValues({}); setAction(a); }}>{a.action!.title}</Button>)}</div>}
      detail={(id) => <RecordPage source={source} type={info.type} id={id} fields={page.detailFields} />} />
    <Dialog open={!!action} onOpenChange={(open) => !open && setAction(undefined)} title={action?.action?.title ?? t("Action preview")}>
      {action?.action && <div className="grid gap-3">
        <p className="text-xs text-muted">{assetKey(action.ref)} · {t("Preview only")}</p>
        <PayloadFields fields={action.action.payload} values={values} onChange={setValues} preview />
        <div className="flex justify-end gap-2"><Button onClick={() => setAction(undefined)}>{t("Close")}</Button>
          <Button variant="primary" disabled>{action.action.title}</Button></div>
      </div>}
    </Dialog>
  </>;
}
