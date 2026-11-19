import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { DateInput } from "@astryxdesign/core/DateInput";
import { Field } from "@astryxdesign/core/Field";
import { FileInput } from "@astryxdesign/core/FileInput";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { NumberInput } from "@astryxdesign/core/NumberInput";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Switch } from "@astryxdesign/core/Switch";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { type ComponentProps, useId, useState } from "react";
import { type Api, request, requestBlob, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { MemberInput } from "./MemberField";
import { money } from "./values";
import { ReferenceInput } from "./ReferenceField";

export type InputStatus = { type: "error" | "success"; message?: string };
type ParameterInputProps = {
  parameter: Api.Field;
  described?: Api.EntityInfo;
  value: unknown;
  form?: Record<string, unknown>;
  status?: InputStatus;
  onChange: (value: unknown) => void;
  onEnter?: () => void;
  focused?: boolean;
  referenceName?: string;
  labelHidden?: boolean;
  quiet?: boolean;
};

const typed = new Set(["string", "longtext", "date", "datetime", "entity", "reference", "choice", "member"]);
const numeric = new Set(["integer", "number", "money"]);
const multiple = new Set(["member[]", "action[]"]);
type Attachment = { mediaSet?: string; item?: string };

export function enterable(parameter: Api.Field): boolean {
  if (parameter.type === "lines") return Boolean(parameter.fields?.length && parameter.fields.every(enterable));
  return Boolean(parameter.choices) || parameter.type === "boolean" || parameter.type === "attachment" || typed.has(parameter.type) || numeric.has(parameter.type) || multiple.has(parameter.type);
}

export function settles(parameter: Api.Field, field?: Api.FieldInfo) {
  return Boolean(parameter.ref || parameter.choices) || field?.type === "reference" || field?.type === "member" || parameter.type === "member" || parameter.type === "reference" || parameter.type === "boolean" || parameter.type === "attachment" || numeric.has(parameter.type) || multiple.has(parameter.type);
}

export function missingValue(parameter: Api.Field, value: unknown): boolean {
  if (value == null || value === "") return Boolean(parameter.required);
  if (parameter.type === "lines" && Array.isArray(value)) {
    return (Boolean(parameter.required) && value.length === 0) ||
      value.some((row) => parameter.fields?.some((field) => missingValue(field, row[field.name])));
  }
  return parameter.type === "action[]" && Boolean(parameter.required) && Array.isArray(value) && value.length === 0;
}

function MultipleInput({ parameter, value, form, label, description, status, onChange, labelHidden, control }: ParameterInputProps & {
  label: string;
  description?: string;
  control?: ComponentProps<typeof MultiSelector>["controlXstyle"];
}) {
  const members = parameter.type === "member[]";
  const app = String(form?.app ?? "").trim();
  const directory = useRead<Api.MemberOption[]>(members ? "/v1/member-directory" : null);
  const catalog = useRead<Api.Action[]>(!members && app ? "/v1/actions" : null);
  const read = members ? directory : catalog;
  const selected = Array.isArray(value) ? value as string[] : [];
  const options = members
    ? (directory.status === "ready" ? directory.data.map((member) => ({ value: member.id, label: member.name })) : [])
    : (catalog.status === "ready" ? catalog.data.filter((action) => action.schema.startsWith(app + "."))
      .map((action) => ({ value: action.schema, label: `${t(action.title)} · ${action.schema}` })) : []);
  for (const id of selected) {
    if (!options.some((option) => option.value === id)) options.push({ value: id, label: members ? t("Unnamed member") : id });
  }
  return <MultiSelector
    label={label}
    description={description}
    isRequired={parameter.required}
    isLabelHidden={labelHidden}
    isLoading={read.status === "loading"}
    isDisabled={!members && !app}
    disabledMessage={!members && !app ? t("Enter an app first") : undefined}
    status={status ?? (read.status === "refused" || read.status === "error" ? { type: "error", message: read.reason } : undefined)}
    controlXstyle={control}
    options={options}
    value={selected}
    onChange={onChange}
    triggerDisplay="labels"
    presentation="adaptive"
    hasSearch
    placeholder={t("Select…")}
    searchPlaceholder={t("Search")}
    emptyText={t("No results")}
    emptySearchText={t("No results")}
  />;
}

function LinesInput({ parameter, value, label, description, status, onChange, labelHidden }: ParameterInputProps & { label: string; description?: string }) {
  const id = useId();
  const rows = Array.isArray(value) ? value as Record<string, unknown>[] : [];
  return <Field label={label} inputID={id} labelID={`${id}-label`} isGroupLabel isLabelHidden={labelHidden}
    isRequired={parameter.required} description={description} descriptionID={description ? `${id}-description` : undefined} status={status} statusVariant="detached">
    <VStack id={id} role="group" aria-labelledby={`${id}-label`} aria-describedby={description ? `${id}-description` : undefined} gap={3}>
      {rows.map((row, index) => (
        <VStack key={index} gap={2}>
          <FormLayout defaultOptionality="optional">
            {parameter.fields?.map((column) => <ParameterInput key={column.name} parameter={column} value={row[column.name]} form={row}
              onChange={(value) => onChange(rows.map((entry, i) => i === index ? { ...entry, [column.name]: value } : entry))} />)}
          </FormLayout>
          <HStack hAlign="start">
            <Button label={t("Remove row")} variant="ghost" size="sm" onClick={() => onChange(rows.filter((_, i) => i !== index))} />
          </HStack>
        </VStack>
      ))}
      <HStack hAlign="start">
        <Button label={t("Add a row")} variant="secondary" size="sm" onClick={() => onChange([...rows, {}])} />
      </HStack>
    </VStack>
  </Field>;
}

function AttachmentInput({ parameter, value, onChange, label, description }: { parameter: Api.Field; value: unknown; onChange: (value: unknown) => void; label: string; description?: string }) {
  const id = useId();
  const attachment = value as Attachment | undefined;
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const media = useRead<Api.RecordView>(attachment?.mediaSet ? `/v1/records/build.mediaset/${encodeURIComponent(attachment.mediaSet)}` : null);
  const projects = useRead<Api.RecordPage>(attachment?.mediaSet ? null : "/v1/records/build.app?limit=200");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [selectedProject, setProject] = useState<string>();
  const projectOptions = projects.status === "ready"
    ? (projects.data.records as { id: string; title: string }[]).map((entry) => ({ value: entry.id, label: entry.title }))
    : [];
  const project = selectedProject ?? projectOptions[0]?.value ?? "";
  const loading = catalog.status === "loading" || (attachment?.mediaSet ? media.status : projects.status) === "loading";
  const items = media.status === "ready" ? (media.data.record as { items?: { hash: string; name: string }[] }).items ?? [] : [];
  const current = items.find((item) => item.hash === attachment?.item);
  const actions = catalog.status === "ready" ? catalog.data : [];
  const create = actions.find((action) => action.schema === "build.mediaset.create");
  const add = actions.find((action) => action.schema === "build.mediaset.add");

  const upload = async (file: File) => {
    if (busy || !create || !add) return;
    setBusy(true);
    setFailure("");
    try {
      const uploaded = await request<Api.Upload>(`/v1/files?${new URLSearchParams({ name: file.name })}`, { body: file });
      let mediaSet = attachment?.mediaSet;
      let revision = media.status === "ready" ? (media.data.record as { revision: number }).revision : 0;
      if (!mediaSet) {
        mediaSet = `MEDIA-${crypto.randomUUID()}`;
        const created = await send(create, mediaSet, { name: mediaSet.toLowerCase(), title: label, project }, 0);
        revision = created.revision;
      }
      await send(add, mediaSet, { ...uploaded }, revision);
      onChange({ mediaSet, item: uploaded.hash });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const download = async () => {
    if (!attachment?.item) return;
    setFailure("");
    try {
      const href = URL.createObjectURL(await requestBlob(`/v1/files/${encodeURIComponent(attachment.item)}`));
      Object.assign(document.createElement("a"), { href, download: current?.name ?? attachment.item }).click();
      setTimeout(() => URL.revokeObjectURL(href), 1000);
    } catch (problem) {
      setFailure(reasonOf(problem));
    }
  };

  return (
    <Field label={label} inputID={id} labelID={`${id}-label`} isGroupLabel description={description}
      descriptionID={description ? `${id}-description` : undefined} isRequired={parameter.required}>
      <FormLayout direction="vertical">
        <VStack gap={2} id={id} role="group" aria-labelledby={`${id}-label`} aria-describedby={description ? `${id}-description` : undefined}>
          {failure && <Banner status="error" title={t("The file operation did not finish")} description={failure} />}
          {attachment?.item && (
            <HStack gap={2} vAlign="center">
              <StackItem size="fill">
                <Text type="body">{current?.name ?? attachment.item}</Text>
              </StackItem>
              <Button label={t("Download")} variant="secondary" onClick={download} />
            </HStack>
          )}
          {!attachment?.mediaSet && (
            <Selector
              label={t("Project")}
              value={project}
              onChange={setProject}
              options={projectOptions}
            />
          )}
          {loading ? <Skeleton height={28} /> : (
            <FileInput
              label={attachment?.item ? t("Replace file") : t("Choose file")}
              placeholder={t("Choose file")}
              value={null}
              mode="dropzone"
              isLoading={busy}
              isDisabled={busy || !create || !add || (attachment?.mediaSet ? media.status !== "ready" : !project)}
              onChange={(file) => { if (file instanceof File) void upload(file); }}
            />
          )}
        </VStack>
      </FormLayout>
    </Field>
  );
}

function MoneyInput({ parameter, value, label, description, status, onChange, labelHidden, control, focused }: ParameterInputProps & {
  label: string;
  description?: string;
  control?: ComponentProps<typeof NumberInput>["controlXstyle"];
}) {
  const me = useRead<Api.MeView>("/v1/me");
  const given = money(value);
  const currency = given?.currency || (me.status === "ready" ? me.data.currency : "");
  return (
    <NumberInput
      label={label}
      isLabelHidden={labelHidden}
      description={description}
      isRequired={parameter.required}
      status={status}
      units={currency || null}
      isDisabled={!currency}
      value={given ? given.amount / 100 : null}
      formatValue={(number) => number.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
      controlXstyle={control}
      onChange={(number) => onChange(Number.isFinite(number) ? { amount: Math.round(number * 100), currency } : "")}
      hasAutoFocus={focused}
    />
  );
}

const styles = stylex.create({
  quiet: {
    borderColor: { default: "transparent", ":hover": colorVars["--color-border"], ":focus-within": colorVars["--color-border-emphasized"] },
    backgroundColor: { default: "transparent", ":hover": colorVars["--color-overlay-hover"], ":focus-within": "transparent" },
  },
});

export function ParameterInput({ parameter, described, value, form, status, onChange, onEnter, focused, referenceName, labelHidden, quiet }: ParameterInputProps) {
  const [switchID, setSwitchID] = useState("");
  const control = quiet && !status ? styles.quiet : undefined;
  const field = described?.fields.find((candidate) => candidate.name === parameter.name);
  const label = t(field?.title ?? parameter.label ?? parameter.name);
  const description = field?.help;
  if (multiple.has(parameter.type)) {
    return <MultipleInput parameter={parameter} value={value} form={form} label={label} description={description ?? t(parameter.description)}
      status={status} onChange={onChange} labelHidden={labelHidden} control={control} />;
  }
  if (parameter.type === "lines") {
    return <LinesInput parameter={parameter} value={value} label={label} description={description ?? t(parameter.description)}
      status={status} onChange={onChange} labelHidden={labelHidden} />;
  }
  const reference = parameter.ref ?? (field?.type === "reference" ? field.ref : undefined);
  if (reference) {
    return <ReferenceInput type={reference} id={String(value ?? "")} name={referenceName} label={label} description={description}
      required={parameter.required} focused={focused} status={status} onChange={onChange} control={control} />;
  }
  if (field?.type === "member" || parameter.type === "member") {
    return <MemberInput id={String(value ?? "")} name={referenceName} label={label} description={description}
      isRequired={parameter.required} isLabelHidden={labelHidden} hasAutoFocus={focused} status={status} onChange={onChange} controlXstyle={control} />;
  }
  if (parameter.choices) {
    return (
      <Selector
        label={label}
        isLabelHidden={labelHidden}
        description={description}
        isRequired={parameter.required}
        status={status}
        controlXstyle={control}
        value={String(value ?? "")}
        onChange={onChange}
        options={parameter.choices.map((choice) => ({ value: choice, label: t(field?.choiceTitles?.[field.choices?.indexOf(choice) ?? -1] ?? choice) }))}
      />
    );
  }
  if (parameter.type === "attachment") {
    return <AttachmentInput parameter={parameter} value={value} onChange={onChange} label={label} description={description} />;
  }
  if (parameter.type === "boolean") {
    return (
      <Field label={label} inputID={switchID} isLabelHidden={labelHidden} description={description}>
        <Switch ref={(input) => { if (input) setSwitchID(input.id); }} label="" isLabelHidden value={value === true} onChange={onChange} />
      </Field>
    );
  }
  if (parameter.type === "date") {
    return (
      <DateInput
        label={label}
        aria-label={label}
        isLabelHidden={labelHidden}
        description={description}
        isRequired={parameter.required}
        status={status}
        controlXstyle={control}
        format="date"
        value={typeof value === "string" && value ? value as ComponentProps<typeof DateInput>["value"] : undefined}
        onChange={(date) => onChange(date ?? "")}
      />
    );
  }
  if (parameter.type === "money") {
    return <MoneyInput parameter={parameter} value={value} label={label} description={description} status={status} onChange={onChange}
      labelHidden={labelHidden} control={control} focused={focused} />;
  }
  if (numeric.has(parameter.type)) {
    return (
      <NumberInput
        isIntegerOnly={parameter.type === "integer"}
        label={label}
        isLabelHidden={labelHidden}
        description={description}
        isRequired={parameter.required}
        status={status}
        value={typeof value === "number" ? value : null}
        formatValue={(number) => number.toLocaleString()}
        controlXstyle={control}
        onChange={onChange}
        hasAutoFocus={focused}
      />
    );
  }
  return (
    <TextInput
      label={label}
      isLabelHidden={labelHidden}
      description={description}
      isRequired={parameter.required}
      status={status}
      value={String(value ?? "")}
      controlXstyle={control}
      onChange={onChange}
      onEnter={onEnter}
      hasAutoFocus={focused}
    />
  );
}
