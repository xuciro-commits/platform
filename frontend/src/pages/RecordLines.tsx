import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Table, TableBody, TableCell, TableFooter, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TrashIcon } from "@heroicons/react/24/outline";
import { type ClipboardEvent, type KeyboardEvent, useRef, useState } from "react";
import { type Api, reasonOf, reread, useRead } from "../host";
import { currentCurrency, t } from "../i18n";
import { useAct } from "./act";
import { type Described, type Values } from "./describe";
import { enterable } from "./fieldRules";
import { PropertyField } from "./PropertyField";
import { EditorConflict, useEditor } from "./RecordFields";
import { SemanticValue } from "../semantic";
import { importChoice, importRowErrors, postImportCsv } from "./workbook";

type Line = Values & { id: string; revision: number };
type TextMap = Record<string, string>;
type DraftLine = { key: string; values: Values; raw: TextMap; errors: TextMap };
type CellPos = { row: string; field: string };

function parsePasted(field: Api.FieldInfo, text: string, currency: string): unknown {
  const trimmed = text.trim();
  if (!trimmed) return "";
  if (field.type === "integer") {
    return /^-?\d+$/.test(trimmed) ? Number(trimmed) : undefined;
  }
  if (field.type === "decimal") {
    const num = Number(trimmed);
    return Number.isFinite(num) ? num : undefined;
  }
  if (field.type === "money") {
    const amount = Number(trimmed.split(" ")[0]);
    return Number.isFinite(amount) ? { amount: Math.round(amount * 100), currency } : undefined;
  }
  if (field.type === "boolean") {
    const low = trimmed.toLowerCase();
    if (low === "true" || low === "1" || low === "yes") return true;
    if (low === "false" || low === "0" || low === "no") return false;
    return undefined;
  }
  if (field.type === "choice") {
    const options = field.choices?.map((key, index) => {
      const title = field.choiceTitles?.[index] ?? key;
      return { key, labels: [title, t(title)] };
    });
    return importChoice(trimmed, options);
  }
  return trimmed;
}

function cellToCsv(field: Api.FieldInfo, value: unknown, raw?: string): string {
  if (raw !== undefined) return raw;
  if (value == null || value === "") return "";
  if (field.type === "money" && typeof value === "object" && value !== null && "amount" in value) {
    const money = value as { amount: number; currency?: string };
    const major = (money.amount / 100).toString();
    return money.currency ? `${major} ${money.currency}` : major;
  }
  if (field.type === "boolean") return value ? "true" : "false";
  if (Array.isArray(value)) return value.map((item) => String(item)).join(";");
  return String(value);
}

function emptyDraft(): DraftLine {
  return { key: `draft-${crypto.randomUUID()}`, values: {}, raw: {}, errors: {} };
}

function hasContent(draft: DraftLine, editableFields: Api.FieldInfo[]): boolean {
  return editableFields.some((field) => {
    const raw = draft.raw[field.name];
    if (raw !== undefined && raw.trim() !== "") return true;
    const val = draft.values[field.name];
    return val !== undefined && val !== null && val !== "";
  });
}

export function RecordLines({ parent, id, set, child, readOnly }: { parent: string; id: string; set: Api.RecordSet; child: Described; readOnly?: boolean }) {
  const read = useRead<Api.RecordSetResult>(`/v1/record-sets/${parent}/${encodeURIComponent(id)}/${encodeURIComponent(set.name)}`);
  const me = useRead<Api.MeView>("/v1/me");
  const [drafts, setDrafts] = useState<DraftLine[]>([]);
  const draftsRef = useRef<DraftLine[]>([]);
  const [active, setActive] = useState<CellPos | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [refusal, setRefusal] = useState("");
  const [banner, setBanner] = useState("");
  const act = useAct(child);

  const updateDrafts = (next: DraftLine[] | ((prev: DraftLine[]) => DraftLine[])) => {
    setDrafts((prev) => {
      const resolved = typeof next === "function" ? next(prev) : next;
      draftsRef.current = resolved;
      return resolved;
    });
  };

  const via = child.entity.fields.find((field) => field.ref === parent && field.inverse === set.from);
  const create = !readOnly && via ? child.create : undefined;
  const editAction = !readOnly ? child.edit : undefined;
  const fields = child.table.filter((field) => field.name !== via?.name && !field.aside);
  const createParams = new Map((create?.payload ?? []).filter(enterable).map((p) => [p.name, p]));
  const editParams = new Map((editAction?.payload ?? []).filter(enterable).map((p) => [p.name, p]));
  const editableFields = fields.filter((field) => !field.readOnly && (createParams.has(field.name) || editParams.has(field.name)));
  const archive = !readOnly ? child.action(`${child.type}.archive`) : undefined;
  const title = t(child.plural);
  const answer = read.status === "ready" ? read.data : null;
  const savedRows = (answer?.records ?? []) as Line[];
  const withheld = new Map(answer?.authorization?.elements.map((item) => [item.record, item.decision.withheld ?? []]));
  const currency = me.status === "ready" && me.data.currency ? me.data.currency : currentCurrency();
  const allRowKeys = [...savedRows.map((row) => row.id), ...drafts.map((row) => row.key)];

  const addDraftRow = (focusField?: string) => {
    if (!create || editableFields.length === 0) return;
    const next = emptyDraft();
    updateDrafts((prev) => [...prev, next]);
    setActive({ row: next.key, field: focusField ?? editableFields[0].name });
  };

  const moveTab = (rowKey: string, fieldName: string, shift: boolean) => {
    const colIdx = editableFields.findIndex((f) => f.name === fieldName);
    const rowIdx = allRowKeys.indexOf(rowKey);
    if (colIdx < 0 || rowIdx < 0) return;
    const step = shift ? -1 : 1;
    const nextCol = colIdx + step;
    if (nextCol >= 0 && nextCol < editableFields.length) {
      setActive({ row: rowKey, field: editableFields[nextCol].name });
      return;
    }
    const nextRowKey = allRowKeys[rowIdx + step];
    if (nextRowKey) {
      setActive({ row: nextRowKey, field: shift ? editableFields[editableFields.length - 1].name : editableFields[0].name });
      return;
    }
    if (!shift && create) addDraftRow(editableFields[0].name);
  };

  const moveEnter = (rowKey: string, fieldName: string) => {
    const rowIdx = allRowKeys.indexOf(rowKey);
    if (rowIdx < 0) return;
    const nextRowKey = allRowKeys[rowIdx + 1];
    if (nextRowKey) {
      setActive({ row: nextRowKey, field: fieldName });
      return;
    }
    if (create) addDraftRow(fieldName);
  };

  const submitDrafts = async () => {
    if (!create || !via || submitting) return;
    const nonEmpty = draftsRef.current.filter((draft) => hasContent(draft, editableFields));
    if (nonEmpty.length === 0) {
      updateDrafts([]);
      setBanner("");
      return;
    }
    setSubmitting(true);
    setRefusal("");
    setBanner("");
    try {
      const csvRows = nonEmpty.map((draft) => {
        const record: Record<string, string> = { [via.name]: id };
        for (const field of editableFields) {
          record[field.name] = cellToCsv(field, draft.values[field.name], draft.raw[field.name]);
        }
        return record;
      });
      const preview = await postImportCsv(child.type, csvRows, true);
      const errorsByKey = new Map<string, Record<string, string>>();
      let totalErrors = 0;
      let firstBad: CellPos | null = null;
      nonEmpty.forEach((draft, index) => {
        const result = preview[index];
        if (!result || result.outcome === "ok") return;
        const cellErrors = importRowErrors(result);
        if (Object.keys(cellErrors).length === 0 && editableFields[0]) {
          cellErrors[editableFields[0].name] = result.error?.message || t("Not saved");
        }
        errorsByKey.set(draft.key, cellErrors);
        const badFields = Object.keys(cellErrors);
        totalErrors += badFields.length;
        if (!firstBad && badFields[0]) {
          const matchedField = editableFields.find((f) => f.name === badFields[0])?.name ?? editableFields[0]?.name;
          if (matchedField) firstBad = { row: draft.key, field: matchedField };
        }
      });
      if (errorsByKey.size > 0) {
        updateDrafts(nonEmpty.map((draft) => ({ ...draft, errors: errorsByKey.get(draft.key) ?? {} })));
        setBanner(`${totalErrors} ${t("cells need fixing")}`);
        if (firstBad) setActive(firstBad);
        return;
      }
      const committed = await postImportCsv(child.type, csvRows, false);
      const remaining: DraftLine[] = [];
      let commitBad: CellPos | null = null;
      nonEmpty.forEach((draft, index) => {
        const result = committed[index];
        if (result?.outcome === "ok") return;
        const cellErrors = result ? importRowErrors(result) : {};
        if (Object.keys(cellErrors).length === 0 && editableFields[0]) {
          cellErrors[editableFields[0].name] = result?.error?.message || t("Not saved");
        }
        remaining.push({ ...draft, errors: cellErrors });
        if (!commitBad) {
          const badField = Object.keys(cellErrors)[0];
          const matched = editableFields.find((f) => f.name === badField)?.name ?? editableFields[0]?.name;
          if (matched) commitBad = { row: draft.key, field: matched };
        }
      });
      updateDrafts(remaining);
      if (remaining.length === 0) {
        setActive(null);
        setBanner("");
      } else {
        setBanner(`${remaining.length} ${t("need fixing")}`);
        if (commitBad) setActive(commitBad);
      }
      reread();
    } catch (problem) {
      setRefusal(reasonOf(problem));
    } finally {
      setSubmitting(false);
    }
  };

  const handlePaste = (event: ClipboardEvent<HTMLElement>) => {
    if (!create || !via || editableFields.length === 0) return;
    const text = event.clipboardData.getData("text/plain");
    if (!text || (!text.includes("\t") && !text.includes("\n"))) return;
    event.preventDefault();
    event.stopPropagation();
    const lines = text.replace(/\r?\n$/, "").split(/\r?\n/).map((line) => line.split("\t"));
    if (lines.length === 0) return;
    const activeCol = active ? editableFields.findIndex((f) => f.name === active.field) : -1;
    const startCol = activeCol >= 0 ? activeCol : 0;
    const prev = draftsRef.current;
    const activeDraftIdx = active ? prev.findIndex((d) => d.key === active.row) : -1;
    const startRow = activeDraftIdx >= 0 ? activeDraftIdx : prev.length;
    const next = prev.map((d) => ({ ...d, values: { ...d.values }, raw: { ...d.raw }, errors: { ...d.errors } }));
    while (next.length < startRow + lines.length) next.push(emptyDraft());
    lines.forEach((cells, r) => {
      const target = next[startRow + r];
      cells.forEach((cellText, c) => {
        const field = editableFields[startCol + c];
        if (!field) return;
        target.raw[field.name] = cellText;
        target.values[field.name] = parsePasted(field, cellText, currency);
        delete target.errors[field.name];
      });
    });
    updateDrafts(next);
    setBanner("");
    setRefusal("");
    setActive({ row: next[startRow].key, field: editableFields[startCol].name });
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && draftsRef.current.length > 0) {
      event.preventDefault();
      event.stopPropagation();
      void submitDrafts();
    }
  };

  const changeDraftCell = (key: string, fieldName: string, value: unknown) => {
    updateDrafts((prev) => prev.map((draft) => {
      if (draft.key !== key) return draft;
      const nextRaw = { ...draft.raw };
      delete nextRaw[fieldName];
      const nextErrors = { ...draft.errors };
      delete nextErrors[fieldName];
      return { ...draft, values: { ...draft.values, [fieldName]: value }, raw: nextRaw, errors: nextErrors };
    }));
  };

  const removeDraftRow = (key: string) => {
    updateDrafts((prev) => prev.filter((draft) => draft.key !== key));
    if (active?.row === key) setActive(null);
  };

  const hasRows = savedRows.length > 0 || drafts.length > 0;
  const showActions = Boolean(archive || drafts.length > 0);

  return (
    <VStack gap={3} onPaste={handlePaste} onKeyDown={handleKeyDown}>
      <HStack vAlign="center" gap={2}>
        <StackItem size="fill"><Heading level={3} accessibilityLevel={2}>{title}</Heading></StackItem>
        {drafts.length > 0 && (
          <HStack gap={2} vAlign="center">
            <Text type="supporting">{`${drafts.length} ${t("Pending rows")}`}</Text>
            <Button label={t("Discard")} variant="ghost" size="sm" isDisabled={submitting} onClick={() => { updateDrafts([]); setBanner(""); setActive(null); }} />
            <Button label={t("Submit rows")} variant="primary" size="sm" isLoading={submitting} onClick={() => void submitDrafts()} />
          </HStack>
        )}
        {answer && create && <Button label={t("Add a row")} variant="secondary" onClick={() => addDraftRow()} />}
      </HStack>
      {read.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
      {read.status === "error" && <Banner status="error" title={t("Rows could not be loaded")} description={read.reason} />}
      {read.status === "refused" && <Banner status="warning" title={t("You cannot see these rows")} description={read.reason} />}
      {refusal && <Banner status="error" title={t("Not done")} description={refusal} />}
      {banner && <Banner status="error" title={banner} />}
      {answer?.partial && <Banner status="warning" title={t("Some rows are unavailable")} />}
      {answer && !hasRows && (
        <EmptyState headingLevel={4} title={t("No rows yet")} description={create ? t("Paste from spreadsheet or press Enter to add rows") : undefined} />
      )}
      {answer && hasRows && (
        <Table aria-label={title} density="compact">
          <TableHeader>
            <TableRow>
              {fields.map((field) => <TableHeaderCell key={field.name}>{t(field.title)}</TableHeaderCell>)}
              {showActions && <TableHeaderCell>{t("Actions")}</TableHeaderCell>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {savedRows.map((row) => (
              <LineRow
                key={row.id}
                row={row}
                child={readOnly ? { ...child, edit: undefined, editable: new Set<string>() } : child}
                fields={fields}
                withheld={withheld.get(row.id)}
                refs={answer.refs}
                activeField={active?.row === row.id ? active.field : undefined}
                onTab={(fieldName, shift) => moveTab(row.id, fieldName, shift)}
                onEnter={(fieldName) => moveEnter(row.id, fieldName)}
                onSubmit={drafts.length > 0 ? () => void submitDrafts() : undefined}
                onClose={(fieldName) => setActive((cur) => (cur?.row === row.id && cur.field === fieldName ? null : cur))}
                archive={archive && (() => act.run({
                  action: archive, id: row.id, revision: row.revision, label: t("Archive row"), name: child.name(row, row.id),
                  reversible: false, onRefused: setRefusal,
                }))}
                showActions={showActions}
              />
            ))}
            {drafts.map((draft) => (
              <DraftRow
                key={draft.key}
                draft={draft}
                child={child}
                fields={fields}
                createParams={createParams}
                activeField={active?.row === draft.key ? active.field : undefined}
                onChange={(fieldName, value) => changeDraftCell(draft.key, fieldName, value)}
                onTab={(fieldName, shift) => moveTab(draft.key, fieldName, shift)}
                onEnter={(fieldName) => moveEnter(draft.key, fieldName)}
                onSubmit={() => void submitDrafts()}
                onClose={(fieldName) => setActive((cur) => (cur?.row === draft.key && cur.field === fieldName ? null : cur))}
                onRemove={() => removeDraftRow(draft.key)}
                showActions={showActions}
              />
            ))}
          </TableBody>
          {answer.sums && (
            <TableFooter>
              <TableRow>
                {fields.map((field, index) => (
                  <TableCell key={field.name}>
                    {index === 0 && <Text type="supporting" weight="medium">{t("Totals")}</Text>}
                    <SemanticValue field={field} value={answer.sums?.[field.name]} size="sm" surface="cell" interactive={false} />
                  </TableCell>
                ))}
                {showActions && <TableCell />}
              </TableRow>
            </TableFooter>
          )}
        </Table>
      )}
      {act.element}
    </VStack>
  );
}

function LineRow({
  row,
  child,
  fields,
  withheld,
  refs,
  activeField,
  onTab,
  onEnter,
  onSubmit,
  onClose,
  archive,
  showActions,
}: {
  row: Line;
  child: Described;
  fields: Api.FieldInfo[];
  withheld?: string[];
  refs?: Api.RecordSetResult["refs"];
  activeField?: string;
  onTab: (field: string, shift: boolean) => void;
  onEnter: (field: string) => void;
  onSubmit?: () => void;
  onClose: (field: string) => void;
  archive?: () => void;
  showActions: boolean;
}) {
  const editor = useEditor(child, row.id, row, row.revision);
  return (
    <TableRow>
      {fields.map((field, index) => {
        const parameter = child.edit?.payload.find((item) => item.name === field.name && enterable(item));
        return (
          <TableCell key={field.name}>
            {index === 0 && <EditorConflict editor={editor} described={child} />}
            {withheld?.includes(field.name) ? <Text type="supporting">{t("Cannot view")}</Text>
              : (
                <PropertyField
                  field={field}
                  parameter={parameter}
                  described={child.entity}
                  value={editor.values[field.name]}
                  form={editor.values}
                  name={refs?.[field.name]?.[String(editor.values[field.name] ?? "")]}
                  status={editor.status[field.name]}
                  saving={editor.saving.has(field.name)}
                  labelHidden
                  open={activeField === field.name}
                  onChange={parameter && ((value) => editor.change(parameter, value))}
                  onCommit={() => editor.commit(field.name)}
                  onTab={(shift) => onTab(field.name, shift)}
                  onEnter={() => onEnter(field.name)}
                  onSubmit={onSubmit}
                  onClose={() => onClose(field.name)}
                />
              )}
          </TableCell>
        );
      })}
      {showActions && (
        <TableCell>
          {archive && <IconButton label={t("Archive row")} tooltip={t("Archive row")} variant="ghost" size="sm" icon={<Icon icon={TrashIcon} size="sm" />} onClick={archive} />}
        </TableCell>
      )}
    </TableRow>
  );
}

function DraftRow({
  draft,
  child,
  fields,
  createParams,
  activeField,
  onChange,
  onTab,
  onEnter,
  onSubmit,
  onClose,
  onRemove,
  showActions,
}: {
  draft: DraftLine;
  child: Described;
  fields: Api.FieldInfo[];
  createParams: Map<string, Api.Field>;
  activeField?: string;
  onChange: (field: string, value: unknown) => void;
  onTab: (field: string, shift: boolean) => void;
  onEnter: (field: string) => void;
  onSubmit: () => void;
  onClose: (field: string) => void;
  onRemove: () => void;
  showActions: boolean;
}) {
  return (
    <TableRow>
      {fields.map((field) => {
        const parameter = field.readOnly ? undefined : createParams.get(field.name);
        const error = draft.errors[field.name];
        const value = draft.values[field.name] !== undefined ? draft.values[field.name] : draft.raw[field.name];
        return (
          <TableCell key={field.name}>
            <PropertyField
              field={field}
              parameter={parameter}
              described={child.entity}
              value={value}
              form={draft.values}
              name={field.type === "reference" && typeof draft.raw[field.name] === "string" ? draft.raw[field.name] : undefined}
              status={error ? { type: "error", message: error } : undefined}
              labelHidden
              open={activeField === field.name}
              onChange={parameter && ((next) => onChange(field.name, next))}
              onCommit={(next) => { if (parameter && next !== value) onChange(field.name, next); }}
              onTab={(shift) => onTab(field.name, shift)}
              onEnter={() => onEnter(field.name)}
              onSubmit={onSubmit}
              onClose={() => onClose(field.name)}
            />
          </TableCell>
        );
      })}
      {showActions && (
        <TableCell>
          <IconButton label={t("Remove row")} tooltip={t("Remove row")} variant="ghost" size="sm" icon={<Icon icon={TrashIcon} size="sm" />} onClick={onRemove} />
        </TableCell>
      )}
    </TableRow>
  );
}
