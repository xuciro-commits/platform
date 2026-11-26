import { VStack } from "@astryxdesign/core/Layout";
import { useEffect, useMemo, useRef } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { send } from "./act";
import { describe, type Described, numeric, type Section, type Values, withRestrictedFields } from "./describe";
import type { ListColumn } from "./ListPage";
import { enterable } from "./ParameterInput";
import { FieldGroup, useEditor } from "./RecordFields";
import { money, plain, present } from "./values";

type Row = Record<string, unknown>;
type Kept = { id: string; revision: number };
type Performed = Promise<{ id?: string; revision: number }>;

export type ContributedAction = {
  key: string;
  action: Api.Action;
  described: Api.EntityInfo;
  id: string;
  revision: number;
  verdict: Api.RecordAction;
  perform: (payload: Values) => Performed;
};

export function extensionsOf(targetType: string, entities: Api.EntityInfo[], catalog: Api.Action[]): Described[] {
  if (targetType.startsWith("build.") || targetType.startsWith("platform.") || targetType === "enterprise.element") return [];
  return entities
    .filter((entity) => entity.extends === targetType && entity.type !== targetType)
    .map((entity) => describe(entity, catalog));
}

function canAutoCreate(ext: Described) {
  return Boolean(ext.create && !ext.create.payload.some((parameter) => parameter.required && parameter.name !== "base"));
}

function extensionSectionsOf(detail: Described): Section[] {
  const withoutBase = (fields: Api.FieldInfo[]) => fields.filter((field) => field.name !== "base");
  const sections: Section[] = detail.sections
    .map((section) => ({
      title: section.title === "Details" ? detail.entity.title : section.title,
      fields: withoutBase(section.fields),
    }))
    .filter((section) => section.fields.length > 0);
  const railFields = withoutBase(detail.aside);
  if (railFields.length > 0) {
    const existing = sections.find((section) => section.title === detail.entity.title);
    if (existing) existing.fields.push(...railFields);
    else sections.push({ title: detail.entity.title, fields: railFields });
  }
  return sections;
}

function ExtensionSlot({
  ext,
  baseId,
  catalog,
  showFields,
  onActions,
}: {
  ext: Described;
  baseId: string;
  catalog: Api.Action[];
  showFields: boolean;
  onActions: (type: string, actions: ContributedAction[]) => void;
}) {
  const domain = encodeURIComponent(JSON.stringify([["base", "=", baseId]]));
  const page = useRead<Api.RecordPage>(`/v1/records/${encodeURIComponent(ext.type)}?domain=${domain}&limit=1&actions=1`);
  const firstRow = page.status === "ready" ? (page.data.records[0] as Kept | undefined) : undefined;
  const extId = firstRow?.id ? String(firstRow.id) : "";
  const view = useRead<Api.RecordView>(extId ? `/v1/records/${encodeURIComponent(ext.type)}/${encodeURIComponent(extId)}` : null);

  const createdRef = useRef<{ id: string; revision: number } | null>(null);
  useEffect(() => {
    createdRef.current = null;
  }, [baseId, ext.type]);
  useEffect(() => {
    if (extId && view.status === "ready") {
      createdRef.current = { id: extId, revision: (view.data.record as Kept).revision };
    }
  }, [extId, view]);

  const values = useMemo(
    () => (view.status === "ready" ? Object.fromEntries(Object.entries(view.data.values ?? {}).map(([name, value]) => [name, plain(value)])) : {}),
    [view],
  );
  const restrictedFields = view.status === "ready" ? view.data.restrictedFields : undefined;
  const restricted = useMemo(() => new Set(view.status === "ready" ? view.data.restricted : undefined), [view]);
  const detail = useMemo(
    () => (restrictedFields?.length ? describe(withRestrictedFields(ext.entity, restrictedFields), catalog) : ext),
    [catalog, ext, restrictedFields],
  );

  const autoCreate = !extId && canAutoCreate(detail);
  const editableDetail = useMemo<Described>(() => {
    if (detail.edit || !autoCreate || !detail.create) return detail;
    const payload = detail.create.payload.filter((parameter) => parameter.name !== "base");
    const syntheticEdit: Api.Action = { ...detail.create, schema: `${detail.type}.edit`, payload };
    return {
      ...detail,
      edit: syntheticEdit,
      editable: new Set(payload.filter(enterable).map((parameter) => parameter.name)),
    };
  }, [autoCreate, detail]);

  const revision = view.status === "ready" ? (view.data.record as Kept).revision : (firstRow?.revision ?? 0);
  const writeRecord = useMemo(() => {
    if (extId) {
      return detail.edit
        ? async (patch: Values, expected: number) => {
            const accepted = await send(detail.edit!, extId, patch, expected);
            return { revision: accepted.revision };
          }
        : undefined;
    }
    if (!autoCreate || !detail.create) return undefined;
    return async (patch: Values, expected: number) => {
      const existing = createdRef.current;
      if (existing && detail.edit) {
        const accepted = await send(detail.edit, existing.id, patch, expected || existing.revision);
        createdRef.current = { id: existing.id, revision: accepted.revision };
        return { revision: accepted.revision };
      }
      const target = detail.entity.naming ? "" : crypto.randomUUID();
      const accepted = await send(detail.create!, target, { base: baseId, ...patch }, 0);
      const actualId = accepted.id || target;
      createdRef.current = { id: actualId, revision: accepted.revision };
      reread();
      return { revision: accepted.revision };
    };
  }, [autoCreate, baseId, detail, extId]);

  const editor = useEditor(editableDetail, extId || baseId, values, revision, writeRecord);
  const sections = useMemo(() => extensionSectionsOf(editableDetail), [editableDetail]);

  const customActions = useMemo(() => {
    const standard = new Set([`${detail.type}.create`, `${detail.type}.edit`, `${detail.type}.archive`, `${detail.type}.restore`]);
    return [
      ...detail.steps.map((step) => step.action),
      ...detail.other.filter((action) => !standard.has(action.schema)),
    ];
  }, [detail]);

  const contributed = useMemo<ContributedAction[]>(() => {
    if (page.status !== "ready") return [];
    if (extId) {
      if (view.status !== "ready") return [];
      const verdicts = new Map((view.data.actions ?? []).map((entry) => [entry.schema, entry]));
      const currentRev = (view.data.record as Kept).revision;
      return customActions.flatMap((action) => {
        const verdict = verdicts.get(action.schema);
        if (!verdict) return [];
        return [{
          key: `${detail.type}:${action.schema}`,
          action,
          described: detail.entity,
          id: extId,
          revision: currentRev,
          verdict,
          perform: async (payload) => {
            const accepted = await send(action, extId, payload, currentRev);
            reread();
            return { id: extId, revision: accepted.revision };
          },
        }];
      });
    }
    if (!autoCreate || !detail.create) return [];
    return customActions.map((action) => ({
      key: `${detail.type}:${action.schema}`,
      action,
      described: detail.entity,
      id: "",
      revision: 0,
      verdict: { schema: action.schema, allowed: true },
      perform: async (payload) => {
        const current = createdRef.current;
        let targetId = current?.id ?? "";
        let targetRev = current?.revision ?? 0;
        if (!targetId) {
          const seedId = detail.entity.naming ? "" : crypto.randomUUID();
          const created = await send(detail.create!, seedId, { base: baseId }, 0);
          targetId = created.id || seedId;
          targetRev = created.revision;
          createdRef.current = { id: targetId, revision: targetRev };
        }
        const accepted = await send(action, targetId, payload, targetRev);
        createdRef.current = { id: targetId, revision: accepted.revision };
        reread();
        return { id: targetId, revision: accepted.revision };
      },
    }));
  }, [autoCreate, baseId, customActions, detail, extId, page.status, view]);

  useEffect(() => {
    onActions(ext.type, contributed);
    return () => onActions(ext.type, []);
  }, [contributed, ext.type, onActions]);

  if (!showFields || page.status !== "ready") return null;
  if (!extId && !autoCreate) return null;
  const refs = view.status === "ready" ? view.data.refs : undefined;
  return (
    <FieldGroup
      described={editableDetail}
      editor={editor}
      sections={sections}
      direction="horizontal-labels"
      fold={false}
      refs={refs}
      restricted={restricted}
    />
  );
}

export function ExtensionHost({
  extensions,
  baseId,
  catalog,
  showFields,
  onActions,
}: {
  extensions: Described[];
  baseId: string;
  catalog: Api.Action[];
  showFields: boolean;
  onActions: (type: string, actions: ContributedAction[]) => void;
}) {
  if (extensions.length === 0) return null;
  return (
    <VStack gap={6}>
      {extensions.map((ext) => (
        <ExtensionSlot
          key={ext.type}
          ext={ext}
          baseId={baseId}
          catalog={catalog}
          showFields={showFields}
          onActions={onActions}
        />
      ))}
    </VStack>
  );
}

export type ExtensionMap = Record<string, Api.RecordPage | undefined>;

function ExtensionPageLoader({
  type,
  baseIds,
  onPage,
}: {
  type: string;
  baseIds: string[];
  onPage: (type: string, page: Api.RecordPage | undefined) => void;
}) {
  const domain = useMemo(
    () => (baseIds.length > 0 ? encodeURIComponent(JSON.stringify([["base", "in", baseIds]])) : ""),
    [baseIds],
  );
  const path = domain ? `/v1/records/${encodeURIComponent(type)}?domain=${domain}&limit=${Math.max(100, baseIds.length)}` : null;
  const read = useRead<Api.RecordPage>(path);
  useEffect(() => {
    if (!domain) {
      onPage(type, { records: [], total: 0 });
      return;
    }
    if (read.status === "ready") onPage(type, read.data);
  }, [domain, onPage, read, type]);
  return null;
}

export function ExtensionListLoaders({
  extensions,
  activeTypes,
  baseIds,
  onPage,
}: {
  extensions: Described[];
  activeTypes: ReadonlySet<string>;
  baseIds: string[];
  onPage: (type: string, page: Api.RecordPage | undefined) => void;
}) {
  return extensions
    .filter((ext) => activeTypes.has(ext.type))
    .map((ext) => <ExtensionPageLoader key={ext.type} type={ext.type} baseIds={baseIds} onPage={onPage} />);
}

export function extensionListColumns(
  extensions: Described[],
  pages: ExtensionMap,
  baseHeaders: ReadonlySet<string>,
): ListColumn<Row>[] {
  return extensions.flatMap((ext) => {
    const page = pages[ext.type];
    const byBase = new Map<string, Row>();
    for (const item of (page?.records ?? []) as Row[]) {
      if (item.base != null) byBase.set(String(item.base), item);
    }
    const refs = page?.refs;
    const autoCreate = canAutoCreate(ext);
    return ext.table
      .filter((field) => field.name !== "base")
      .map((field): ListColumn<Row> => {
        const key = `${ext.type}:${field.name}`;
        const header = baseHeaders.has(field.title) ? `${t(ext.entity.title)} · ${t(field.title)}` : field.title;
        const editParam = ext.edit?.payload.find((candidate) => candidate.name === field.name)
          ?? (autoCreate ? ext.create?.payload.find((candidate) => candidate.name === field.name) : undefined);
        const canCellEdit = Boolean(editParam && enterable(editParam) && !field.readOnly && !field.oneOf && (ext.edit || autoCreate));
        const kind = field.type === "choice" ? ("enum" as const)
          : numeric.has(field.type) ? ("number" as const)
          : field.type === "date" ? ("date" as const)
          : field.type === "datetime" ? ("time" as const)
          : field.type === "member" ? ("member" as const)
          : ("string" as const);
        return {
          key,
          header,
          kind,
          choices: field.choiceTitles ?? field.choices,
          boolean: field.type === "boolean",
          chip: false,
          hiddenByDefault: true,
          text: (row) => {
            const extRow = byBase.get(String(row.id));
            if (!extRow) return "";
            const raw = extRow[field.name];
            const label = refs?.[field.name]?.[String(raw)];
            if (field.type === "member") return raw ? label ?? t("Unnamed member") : "";
            return label || present(field, raw);
          },
          reference: field.type === "reference" && field.ref ? (row) => {
            const extRow = byBase.get(String(row.id));
            const targetId = extRow?.[field.name];
            return targetId ? { type: String(field.ref), id: String(targetId) } : undefined;
          } : undefined,
          value: numeric.has(field.type) ? (row) => {
            const extRow = byBase.get(String(row.id));
            if (!extRow) return undefined;
            return field.type === "money" ? money(extRow[field.name])?.amount : plain(extRow[field.name]);
          } : undefined,
          edit: canCellEdit && editParam ? {
            parameter: editParam,
            described: ext.entity,
            value: (row) => byBase.get(String(row.id))?.[field.name],
            name: (row) => {
              const extRow = byBase.get(String(row.id));
              return extRow ? refs?.[field.name]?.[String(extRow[field.name])] : undefined;
            },
            save: async (row, value) => {
              const extRow = byBase.get(String(row.id));
              if (extRow && ext.edit) {
                await send(ext.edit, String(extRow.id), { [field.name]: value }, Number(extRow.revision));
                reread();
                return;
              }
              if (autoCreate && ext.create) {
                const seedId = ext.entity.naming ? "" : crypto.randomUUID();
                await send(ext.create, seedId, { base: String(row.id), [field.name]: value }, 0);
                reread();
              }
            },
          } : undefined,
        };
      });
  });
}
