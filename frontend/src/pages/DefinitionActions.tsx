import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { CheckboxInput } from "@astryxdesign/core/CheckboxInput";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, VStack } from "@astryxdesign/core/Layout";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { type Api, request, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import type { Values } from "./describe";

export type ActionTypeRow = {
  id: string;
  revision: number;
  name: string;
  title: string;
  object: string;
  state: string;
  definition?: { name?: string; title?: string; from?: string[]; roles?: string[] };
};

type DefinitionRow = Values & { name?: string; title?: string; role?: string; read?: string; create?: boolean; edit?: boolean; archive?: boolean };

const cleanName = (raw: string) => raw.toLowerCase().replace(/[^a-z0-9]+/g, "");

function exprKindOf(type?: string) {
  if (type === "integer") return "integer";
  if (type === "decimal") return "decimal";
  if (type === "boolean") return "boolean";
  if (type === "date") return "date";
  if (type === "datetime") return "datetime";
  if (type === "choice") return "choice";
  if (type === "reference" || type === "member") return "reference";
  return "text";
}

export function TransitionDialog({ subject, objectId, objectRevision, states, catalog, close, done }: {
  subject: string;
  objectId: string;
  objectRevision: number;
  states: DefinitionRow[];
  catalog: Api.Action[];
  close: () => void;
  done: () => void;
}) {
  const domain = encodeURIComponent(JSON.stringify([["object", "=", subject]]));
  const available = useRead<Api.RecordPage>(`/v1/records/build.actiontype?domain=${domain}&limit=200`);
  const candidates = available.status === "ready" ? (available.data.records as ActionTypeRow[]) : [];
  const [selected, setSelected] = useState("");
  const [name, setName] = useState("activate");
  const [title, setTitle] = useState("");
  const [from, setFrom] = useState(String(states[0]?.name ?? ""));
  const [to, setTo] = useState(String(states[1]?.name ?? ""));
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const createAction = catalog.find((action) => action.schema === "build.actiontype.create");
  const publishAction = catalog.find((action) => action.schema === "build.actiontype.publish");
  const objectEdit = catalog.find((action) => action.schema === "build.object.edit");
  const stateOptions = states.flatMap((state) => (state.name ? [{ value: state.name, label: t(state.title ?? state.name) }] : []));
  const current = candidates.find((candidate) => candidate.id === selected);
  const actionName = current?.name ?? `${subject}.${name}`;
  const missing = !publishAction || (!current && (!createAction || !name || !title || from === to)) || (!current && !objectEdit);

  const apply = async () => {
    setBusy(true);
    setFailure("");
    try {
      const action = current;
      if (!action) {
        if (!createAction || !publishAction) throw new Error(t("This action is no longer available."));
        const actionId = `ACTION-${crypto.randomUUID()}`;
        const transaction = {
          subject,
          steps: [{
            name,
            kind: "update",
            target: { type: subject, id: "$subject" },
            changes: [{ field: "state", expr: { op: "", kind: "choice", lit: { value: to } } }],
          }],
        };
        const created = await send(createAction, actionId, {
          name: actionName,
          object: subject,
          definition: { name, title, from: [from], transaction },
        }, 0);
        await send(publishAction, actionId, {}, created.revision);
      } else if (action.state !== "published") {
        if (!publishAction) throw new Error(t("This action is no longer available."));
        await send(publishAction, action.id, {}, action.revision);
      }
      if (!current || !objectEdit) {
        done();
        return;
      }
      const object = await request<Api.RecordView>(`/v1/records/build.object/${encodeURIComponent(objectId)}`);
      const record = object.record as { revision?: number; actionTypes?: Api.AssetBinding[] };
      const bindings = record.actionTypes ?? [];
      if (!bindings.some((binding) => binding.ref.name === actionName)) {
        await send(objectEdit, objectId, {
          actionTypes: [...bindings, { ref: { app: "build", kind: "action", name: actionName }, sourceVersion: "" }],
        }, record.revision ?? objectRevision);
      }
      done();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={close} purpose="form">
      <Layout header={<DialogHeader title={t("Create transition")} onOpenChange={close} />} content={
        <LayoutContent padding={4}>
          <FormLayout defaultOptionality="optional">
            {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
            {available.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
            <Selector
              label={t("Action type")}
              value={selected}
              onChange={setSelected}
              options={[
                { value: "", label: t("Create a new action type") },
                ...candidates.map((candidate) => ({ value: candidate.id, label: `${candidate.title || candidate.name} · ${t(candidate.state === "published" ? "Published" : "Draft")}` })),
              ]}
            />
            {current ? (
              <Text type="body">{current.title}</Text>
            ) : (
              <>
                <TextInput label={t("Name")} value={name} onChange={(next) => setName(cleanName(next))} />
                <TextInput label={t("What people call it")} value={title} onChange={setTitle} />
                <Selector label={t("From state")} value={from} onChange={setFrom} options={stateOptions} />
                <Selector label={t("To state")} value={to} onChange={setTo} options={stateOptions} />
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

export function ActionTypeDialog({ subject, fields, states, access, catalog, close, done }: {
  subject: string;
  fields: DefinitionRow[];
  states: DefinitionRow[];
  access: DefinitionRow[];
  catalog: Api.Action[];
  close: () => void;
  done: () => void;
}) {
  const writableFields = fields.filter((field) => field.name && field.name !== "base" && !field.readOnly);
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [targetField, setTargetField] = useState(String(writableFields[0]?.name ?? ""));
  const [mode, setMode] = useState<"input" | "fixed">("input");
  const [fixedValue, setFixedValue] = useState("");
  const [roles, setRoles] = useState<string[]>([]);
  const [from, setFrom] = useState<string[]>(() => states.flatMap((state) => (state.name ? [state.name] : [])));
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);

  const createAction = catalog.find((action) => action.schema === "build.actiontype.create");
  const publishAction = catalog.find((action) => action.schema === "build.actiontype.publish");
  const chosenField = writableFields.find((field) => field.name === targetField) ?? writableFields[0];
  const roleOptions = access.flatMap((entry) => (entry.role ? [{ value: String(entry.role), label: String(entry.role) }] : []));
  const stateOptions = states.flatMap((state) => (state.name ? [{ value: state.name, label: t(state.title ?? state.name) }] : []));

  const missing = !createAction || !publishAction || !name || !title.trim() || !chosenField?.name ||
    (mode === "fixed" && !fixedValue.trim()) || (states.length > 0 && from.length === 0);

  const apply = async () => {
    if (missing || !createAction || !publishAction || !chosenField?.name) return;
    setBusy(true);
    setFailure("");
    try {
      const actionId = `ACTION-${crypto.randomUUID()}`;
      const fieldName = String(chosenField.name);
      const fieldType = String(chosenField.type || "text");
      const inputs = mode === "input" ? [{
        name: fieldName,
        title: String(chosenField.title || fieldName),
        type: fieldType,
        required: true,
        ...(chosenField.choices ? { choices: String(chosenField.choices) } : {}),
        ...(chosenField.ref ? { ref: String(chosenField.ref) } : {}),
      }] : [];
      const parsedLiteral: unknown = fieldType === "integer" ? Number.parseInt(fixedValue, 10)
        : fieldType === "decimal" ? Number(fixedValue)
        : fieldType === "boolean" ? fixedValue.trim() === "true"
        : fixedValue.trim();
      const change = mode === "input"
        ? { field: fieldName, from: fieldName }
        : { field: fieldName, expr: { op: "", kind: exprKindOf(fieldType), lit: { value: parsedLiteral } } };
      const definition: Record<string, unknown> = {
        name,
        title: title.trim(),
        from: states.length > 0 ? from : [],
        ...(inputs.length > 0 ? { inputs } : {}),
        ...(roles.length > 0 ? { roles } : {}),
        transaction: {
          subject,
          steps: [{
            name,
            kind: "update",
            target: { type: subject, id: "$subject" },
            changes: [change],
          }],
        },
      };
      const created = await send(createAction, actionId, {
        name: `${subject}.${name}`,
        object: subject,
        definition,
      }, 0);
      await send(publishAction, actionId, {}, created.revision);
      done();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={close} purpose="form">
      <Layout header={<DialogHeader title={t("Create action")} onOpenChange={close} />} content={
        <LayoutContent padding={4}>
          <VStack gap={3}>
            <SegmentedControl
              label={t("How it sets the field")}
              value={mode}
              onChange={(next) => setMode(next as "input" | "fixed")}
            >
              <SegmentedControlItem value="input" label={t("Ask for input")} />
              <SegmentedControlItem value="fixed" label={t("Set a fixed value")} />
            </SegmentedControl>
            <FormLayout defaultOptionality="optional">
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              <TextInput label={t("API name")} isRequired value={name} onChange={(next) => setName(cleanName(next))} />
              <TextInput label={t("What people call it")} isRequired value={title} onChange={setTitle} />
              <Selector
                label={t("Field to update")}
                isRequired
                value={chosenField?.name ? String(chosenField.name) : ""}
                onChange={setTargetField}
                options={writableFields.map((field) => ({ value: String(field.name), label: t(String(field.title || field.name)) }))}
              />
              {mode === "fixed" && (
                <TextInput label={t("Value")} isRequired value={fixedValue} onChange={setFixedValue} />
              )}
              {states.length > 0 && (
                <MultiSelector label={t("From states")} options={stateOptions} value={from} onChange={setFrom} />
              )}
              {roleOptions.length > 0 && (
                <MultiSelector
                  label={t("Taken by")}
                  options={roleOptions}
                  value={roles}
                  onChange={setRoles}
                  placeholder={t("Select…")}
                />
              )}
            </FormLayout>
          </VStack>
        </LayoutContent>
      } footer={
        <LayoutFooter padding={4}>
          <HStack gap={2} hAlign="end">
            <Button label={t("Cancel")} variant="secondary" onClick={close} />
            <Button label={t("Save")} variant="primary" isDisabled={busy || missing} isLoading={busy} onClick={apply} />
          </HStack>
        </LayoutFooter>
      } />
    </Dialog>
  );
}

const readScopes = [
  { value: "all", label: "All records" },
  { value: "own", label: "Own records" },
  { value: "team", label: "Team records" },
  { value: "unit", label: "Unit records" },
  { value: "below", label: "Unit and below" },
  { value: "none", label: "No access" },
];

export function AccessDialog({ row, title, busy, failure, close, save }: {
  row: DefinitionRow;
  title: string;
  busy: boolean;
  failure: string;
  close: () => void;
  save: (row: DefinitionRow) => void;
}) {
  const [role, setRole] = useState(String(row.role ?? ""));
  const [read, setRead] = useState(String(row.read || "all"));
  const [canCreate, setCanCreate] = useState(Boolean(row.create));
  const [canEdit, setCanEdit] = useState(Boolean(row.edit));
  const [canArchive, setCanArchive] = useState(Boolean(row.archive));
  const noRead = read === "none";

  const apply = () => {
    save({
      role,
      read,
      ...(noRead || !canCreate ? {} : { create: true }),
      ...(noRead || !canEdit ? {} : { edit: true }),
      ...(noRead || !canArchive ? {} : { archive: true }),
    });
  };

  return (
    <Dialog isOpen onOpenChange={close} purpose="form">
      <Layout header={<DialogHeader title={title} onOpenChange={close} />} content={
        <LayoutContent padding={4}>
          <FormLayout defaultOptionality="optional">
            {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
            <TextInput label={t("Role")} isRequired value={role} onChange={(next) => setRole(cleanName(next))} />
            <Selector
              label={t("Read")}
              isRequired
              value={read}
              onChange={setRead}
              options={readScopes.map((scope) => ({ value: scope.value, label: t(scope.label) }))}
            />
            <CheckboxInput label={t("Create")} value={!noRead && canCreate} isDisabled={noRead} onChange={setCanCreate} />
            <CheckboxInput label={t("Edit")} value={!noRead && canEdit} isDisabled={noRead} onChange={setCanEdit} />
            <CheckboxInput label={t("Archive")} value={!noRead && canArchive} isDisabled={noRead} onChange={setCanArchive} />
          </FormLayout>
        </LayoutContent>
      } footer={
        <LayoutFooter padding={4}>
          <HStack gap={2} hAlign="end">
            <Button label={t("Cancel")} variant="secondary" onClick={close} />
            <Button label={t("Save")} variant="primary" isDisabled={busy || !role} isLoading={busy} onClick={apply} />
          </HStack>
        </LayoutFooter>
      } />
    </Dialog>
  );
}
