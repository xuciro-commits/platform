import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { DateInput } from "@astryxdesign/core/DateInput";
import { FileInput } from "@astryxdesign/core/FileInput";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { NumberInput } from "@astryxdesign/core/NumberInput";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Switch } from "@astryxdesign/core/Switch";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { type ComponentProps, useState } from "react";
import { type Api, request, requestBlob, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { MemberInput } from "./MemberField";
import { ReferenceInput } from "./ReferenceField";

export type InputStatus = { type: "error" | "success"; message?: string };
type ParameterInputProps = {
  parameter: Api.Field;
  described: Api.EntityInfo;
  value: unknown;
  status?: InputStatus;
  onChange: (value: unknown) => void;
  onEnter?: () => void;
  focused?: boolean;
  referenceName?: string;
  labelHidden?: boolean;
  quiet?: boolean;
};

const typed = new Set(["string", "longtext", "date", "datetime", "entity", "reference", "choice", "member"]);
const numeric = new Set(["integer", "number"]);
type Attachment = { mediaSet?: string; item?: string };

export function enterable(parameter: Api.Field) {
  return Boolean(parameter.choices) || parameter.type === "boolean" || parameter.type === "attachment" || typed.has(parameter.type) || numeric.has(parameter.type);
}

export function settles(parameter: Api.Field, field?: Api.FieldInfo) {
  return Boolean(parameter.ref || parameter.choices) || field?.type === "reference" || field?.type === "member" || parameter.type === "member" || parameter.type === "reference" || parameter.type === "boolean" || parameter.type === "attachment" || numeric.has(parameter.type);
}

function AttachmentInput({ parameter, value, onChange, label, description }: { parameter: Api.Field; value: unknown; onChange: (value: unknown) => void; label: string; description?: string }) {
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
    <VStack gap={2}>
      <Text type="body" weight="bold">{label}</Text>
      {description && <Text type="supporting" color="secondary">{description}</Text>}
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
      {parameter.required && !attachment?.item && <Text type="supporting" color="secondary">{t("Required")}</Text>}
    </VStack>
  );
}

const styles = stylex.create({
  quiet: {
    borderColor: { default: "transparent", ":hover": colorVars["--color-border"], ":focus-within": colorVars["--color-border-emphasized"] },
    backgroundColor: { default: "transparent", ":hover": colorVars["--color-overlay-hover"], ":focus-within": "transparent" },
  },
});

export function ParameterInput({ parameter, described, value, status, onChange, onEnter, focused, referenceName, labelHidden, quiet }: ParameterInputProps) {
  const control = quiet && !status ? styles.quiet : undefined;
  const field = described.fields.find((candidate) => candidate.name === parameter.name);
  const label = t(field?.title ?? parameter.label ?? parameter.name);
  const description = field?.help;
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
    return <Switch label={label} isLabelHidden={labelHidden} description={description} value={value === true} onChange={onChange} />;
  }
  if (parameter.type === "date") {
    return (
      <DateInput
        label={label}
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
