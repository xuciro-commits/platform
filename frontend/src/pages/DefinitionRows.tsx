import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { Selector } from "@astryxdesign/core/Selector";
import { Table, TableBody, TableCell, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Typeahead } from "@astryxdesign/core/Typeahead";
import { useRef, useState } from "react";
import type { Api } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import type { Described, Values } from "./describe";
import { ParameterInput, enterable } from "./ParameterInput";
import { present, shown } from "./values";

type DefinitionRow = Values & { name?: string; title?: string; from?: string[]; transaction?: Api.BusinessTxn };
type Slot = "fields" | "states" | "actions";
const fromStates = "from";
const slots: Slot[] = ["fields", "states", "actions"];
const metadata = {
  fields: { title: "Fields", add: "Add field", show: ["name", "title", "type", "group"], edit: ["name", "title", "type", "choices", "ref", "required", "search", "group", "aside", "when"] },
  states: { title: "States", add: "Add state", show: ["name", "title", "tone"], edit: ["name", "title", "tone", "description"] },
  actions: { title: "Transitions", add: "Add transition", show: ["name", "title", fromStates, "to"], edit: ["name", "title", fromStates, "to", "description"] },
};

function rowsOf(source: Values, slot: Slot): DefinitionRow[] {
  return Array.isArray(source[slot]) ? source[slot] : [];
}

function targetOf(row: DefinitionRow) {
  const transaction = row.transaction;
  if (!transaction || transaction.steps.length !== 1) return;
  const [step] = transaction.steps;
  const change = step.changes?.[0];
  if (step.kind !== "update" || step.target?.id !== "$subject" || step.when ||
    step.changes?.length !== 1 || change?.field !== "state") return;
  const value = change.expr?.lit?.value ?? (change.from?.startsWith("=") ? change.from.slice(1) : undefined);
  return typeof value === "string" ? value : undefined;
}

export function stateProblem(source: Values) {
  const states = rowsOf(source, "states");
  if (states.length < 2 || (Array.isArray(source.actionTypes) && source.actionTypes.length)) return "";
  const transitions = rowsOf(source, "actions").map((row) => ({ from: row.from ?? [], to: targetOf(row) }));
  if (transitions.some((row) => row.to === undefined)) return "";
  const reached = new Set([states[0].name]);
  for (let pass = 1; pass < states.length; pass++) {
    for (const step of transitions) if (step.from.some((from) => reached.has(from))) reached.add(step.to);
  }
  const missing = states.filter((state) => !reached.has(state.name)).map((state) => state.title ?? state.name);
  return missing.length ? `${t("These states cannot be reached from the initial state:")} ${missing.join(", ")}` : "";
}

export function DefinitionRows({ described, source, entities, id, revision }: { described: Described; source: Values; entities: Api.EntityInfo[]; id: string; revision: number }) {
  const states = rowsOf(source, "states");
  const bound = Array.isArray(source.actionTypes) && source.actionTypes.length > 0;
  return slots.map((slot) => {
    const field = described.field(slot);
    if (!field) return null;
    const action = described.edit?.payload.find((parameter) => parameter.name === slot);
    return (
      <DefinitionTable key={slot} slot={slot} field={field} rows={rowsOf(source, slot)} states={states} entities={entities}
        subject={`build.${source.name}`}
        bound={slot === "actions" && bound}
        save={action && described.edit ? (rows) => send(described.edit!, id, { [slot]: rows }, revision) : undefined} />
    );
  });
}

type SavedRows = Promise<unknown>;
type SaveRows = (rows: DefinitionRow[]) => SavedRows;

function DefinitionTable({ slot, field, rows, states, entities, subject, bound, save }: {
  slot: Slot; field: Api.FieldInfo; rows: DefinitionRow[]; states: DefinitionRow[]; entities: Api.EntityInfo[];
  subject: string; bound: boolean; save?: SaveRows;
}) {
  const schema = metadata[slot];
  const title = t(schema.title);
  const [editing, setEditing] = useState<{ index: number; row: DefinitionRow }>();
  const [removing, setRemoving] = useState<number>();
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const dragged = useRef<number | undefined>(undefined);
  const fields = field.fields?.length ? field.fields : slot === "actions" ? [
    { name: "name", title: "Name", type: "text", required: true },
    { name: "title", title: "What people call it", type: "text", required: true },
    { name: fromStates, title: "From states", type: "text", required: true },
    { name: "description", title: "Description", type: "text" },
  ] as Api.FieldInfo[] : [];
  const label = (name: string) => name === "to" ? t("To state") : name === fromStates ? t("From states") : t(fields.find((column) => column.name === name)?.title ?? name);
  const canWrite = Boolean(save && !bound);
  const canMove = slot === "fields" && canWrite && !busy;
  const applying = async (next: DefinitionRow[]) => {
    if (!save || busy) return;
    setBusy(true);
    setFailure("");
    try {
      await save(next);
      setEditing(undefined);
      setRemoving(undefined);
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
  const move = (index: number, to: number) => {
    if (!canMove || index === to) return;
    const next = [...rows];
    const [row] = next.splice(index, 1);
    next.splice(to, 0, row);
    void applying(next);
  };
  const show = (row: DefinitionRow, name: string) => {
    if (name === "to") return targetOf(row) ?? "";
    if (name === "type" || name === "tone") return t(shown(row[name]));
    const column = fields.find((item) => item.name === name);
    return column ? present(column, row[name]) : shown(row[name]);
  };
  return (
    <VStack gap={3}>
      <HStack gap={2} vAlign="center">
        <StackItem size="fill"><Heading level={3}>{title}</Heading></StackItem>
        {canWrite && <Button label={t(schema.add)} variant="secondary" isDisabled={busy || slot === "actions" && !states.length} onClick={() => { setFailure(""); setEditing({ index: rows.length, row: {} }); }} />}
      </HStack>
      {bound && <Text type="supporting">{t("Transitions use the linked action types.")}</Text>}
      {failure && !editing && removing === undefined && <Banner status="error" title={t("Not saved")} description={failure} />}
      {rows.length > 0 && (
        <Table aria-label={title} density="compact">
          <TableHeader><TableRow>
            {schema.show.map((name) => <TableHeaderCell key={name}>{label(name)}</TableHeaderCell>)}
            {canWrite && <TableHeaderCell>{t("Actions")}</TableHeaderCell>}
          </TableRow></TableHeader>
          <TableBody>
            {rows.map((row, index) => (
              <TableRow key={row.name ?? index} draggable={canMove}
                onDragStart={(event) => { if (canMove) { dragged.current = index; event.dataTransfer.setData("text/plain", String(index)); } }}
                onDragEnd={() => { dragged.current = undefined; }}
                onDragOver={(event) => { if (canMove && dragged.current !== undefined) event.preventDefault(); }}
                onDrop={(event) => {
                  if (!canMove || dragged.current === undefined) return;
                  event.preventDefault();
                  move(dragged.current, index);
                  dragged.current = undefined;
                }}>
                {schema.show.map((name) => <TableCell key={name}>{show(row, name)}</TableCell>)}
                {canWrite && (
                  <TableCell><MoreMenu label={`${t("Row actions")}: ${row.title ?? row.name}`} items={[
                    { label: t("Edit"), isDisabled: busy || slot === "actions" && !targetOf(row), onClick: () => { setFailure(""); setEditing({ index, row }); } },
                    ...(canMove ? [
                      { label: t("Move up"), isDisabled: index === 0, onClick: () => move(index, index - 1) },
                      { label: t("Move down"), isDisabled: index === rows.length - 1, onClick: () => move(index, index + 1) },
                    ] : []),
                    { label: t("Remove"), isDisabled: busy, onClick: () => { setFailure(""); setRemoving(index); } },
                  ]} /></TableCell>
                )}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      {editing && (
        <DefinitionForm slot={slot} row={editing.row} field={field} states={states} entities={entities} subject={subject}
          title={editing.index === rows.length ? t(schema.add) : t("Edit row")} busy={busy} failure={failure} label={label}
          close={() => !busy && setEditing(undefined)}
          save={(row) => {
            const next = [...rows];
            next[editing.index] = row;
            void applying(next);
          }} />
      )}
      {removing !== undefined && (
        <AlertDialog isOpen title={`${t("Remove from draft")}: ${rows[removing]?.title ?? rows[removing]?.name}`}
          description={failure || t("The installed type changes only when you publish.")}
          actionLabel={t("Remove")} isActionLoading={busy}
          onOpenChange={(open) => !open && !busy && setRemoving(undefined)}
          onAction={() => applying(rows.filter((_, index) => index !== removing))} />
      )}
    </VStack>
  );
}

function DefinitionForm({ slot, row, field, states, entities, subject, title, busy, failure, label, close, save }: {
  slot: Slot; row: DefinitionRow; field: Api.FieldInfo; states: DefinitionRow[]; entities: Api.EntityInfo[]; subject: string;
  title: string; busy: boolean; failure: string; label: (name: string) => string; close: () => void; save: (row: DefinitionRow) => void;
}) {
  const [values, setValues] = useState(row);
  const [to, setTo] = useState(targetOf(row) ?? "");
  const set = (name: string, value: unknown) => setValues((previous) => ({ ...previous, [name]: value }));
  const columns = field.fields?.length ? field.fields : slot === "actions" ? [
    { name: "name", title: "Name", type: "text", required: true },
    { name: "title", title: "What people call it", type: "text", required: true },
    { name: fromStates, title: "From states", type: "text", required: true },
    { name: "description", title: "Description", type: "text" },
  ] as Api.FieldInfo[] : [];
  const inputs = metadata[slot].edit.flatMap((name) => {
    const column = columns.find((item) => item.name === name);
    if (!column) return [];
    const type = column.type === "text" ? "string" : column.type;
    const parameter = { name, type, choices: column.choices, description: column.title, required: column.required };
    return enterable(parameter) ? [parameter] : [];
  });
  const described = { fields: columns } as Api.EntityInfo;
  const stateOptions = states.flatMap((state) => state.name ? [{ value: state.name, label: t(state.title ?? state.name) }] : []);
  const selectedType = entities.find((entity) => entity.type === values.ref);
  const missing = !values.name || !values.title || (slot === "fields" && (!values.type || values.type === "reference" && !values.ref)) ||
    (slot === "actions" && (!values.from?.length || !to));
  const apply = () => {
    const next = { ...values };
    if (slot === "fields" && next.type !== "reference") delete next.ref;
    if (slot === "actions" && to !== targetOf(row)) {
      next.transaction = {
        ...row.transaction,
        subject,
        steps: [{ name: "update", kind: "update", target: { type: subject, id: "$subject" },
          changes: [{ field: "state", expr: { op: "", kind: "choice", lit: { value: to } } }] }],
      };
    }
    save(next);
  };
  return (
    <Dialog isOpen onOpenChange={close} purpose="form">
      <Layout header={<DialogHeader title={title} onOpenChange={close} />} content={
        <LayoutContent padding={4}>
          <FormLayout defaultOptionality="optional">
            {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
            {inputs.filter((parameter) => parameter.name !== "ref").map((parameter) => (
              <ParameterInput key={parameter.name} parameter={parameter} described={described} value={values[parameter.name]} onChange={(value) => set(parameter.name, value)} />
            ))}
            {slot === "fields" && values.type === "reference" && (
              <Typeahead label={t("Refers to")} isRequired
                value={typeof values.ref === "string" && values.ref ? { id: values.ref, label: selectedType?.title ?? values.ref } : null}
                onChange={(next) => set("ref", next?.id ?? "")}
                searchSource={{
                  bootstrap: () => [],
                  search: (query) => entities.filter((entity) => `${entity.title} ${entity.type}`.toLowerCase().includes(query.toLowerCase()))
                    .map((entity) => ({ id: entity.type, label: t(entity.title) })),
                }} />
            )}
            {slot === "actions" && (
              <>
                <MultiSelector label={label(fromStates)} options={stateOptions} value={values.from ?? []} onChange={(value) => set(fromStates, value)} />
                <Selector label={label("to")} options={stateOptions} value={to} onChange={setTo} isRequired />
              </>
            )}
          </FormLayout>
        </LayoutContent>
      } footer={
        <LayoutFooter padding={4}>
          <HStack gap={2} hAlign="end">
            <Button label={t("Cancel")} variant="secondary" onClick={close} />
            <Button label={t("Save")} variant="primary" isDisabled={busy || missing} onClick={apply} />
          </HStack>
        </LayoutFooter>
      } />
    </Dialog>
  );
}
