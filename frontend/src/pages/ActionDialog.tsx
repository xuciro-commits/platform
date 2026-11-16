import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem } from "@astryxdesign/core/Layout";
import { NumberInput } from "@astryxdesign/core/NumberInput";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { type Api, submit, useRead } from "../host";
import { t } from "../i18n";

type Values = Record<string, unknown>;
type Project = { id: string; title: string };
export type ActionDialogProps = {
  action: Api.Action;
  described: Api.EntityInfo;
  heading: string;
  confirm: string;
  id?: string;
  revision: number;
  initial?: Values;
  owned?: boolean;
  onClose: () => void;
  onDone: (id: string) => void;
};

const typed = new Set(["string", "longtext", "date", "datetime", "entity", "choice"]);
const numeric = new Set(["integer", "number"]);
const enterable = (parameter: Api.Field) => Boolean(parameter.choices) || parameter.type === "boolean" || typed.has(parameter.type) || numeric.has(parameter.type);

export function ActionDialog({ action, described, heading, confirm, id, revision, initial = {}, owned, onClose, onDone }: ActionDialogProps) {
  const [values, setValues] = useState<Values>(initial);
  const [project, setProject] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const projects = useRead<Api.RecordPage>(owned ? "/v1/records/build.app?limit=200" : null);
  const set = (name: string, value: unknown) => setValues((previous) => ({ ...previous, [name]: value }));
  const shownParameters = action.payload.filter(enterable);
  const blocked = action.payload.filter((parameter) => parameter.required && !enterable(parameter)).map((parameter) => parameter.name);
  const missing = shownParameters.some((parameter) => parameter.required && (values[parameter.name] == null || values[parameter.name] === ""));
  const wording = (parameter: Api.Field) => {
    const field = described.fields.find((candidate) => candidate.name === parameter.name);
    return { label: field?.title ?? parameter.name, description: field?.help };
  };

  const send = async () => {
    setBusy(true);
    setFailure("");
    const payload: Values = {};
    for (const parameter of shownParameters) {
      const value = values[parameter.name];
      if (value != null && value !== "" && value !== initial[parameter.name]) payload[parameter.name] = value;
    }
    if (owned) payload.project = project;
    const target = id ?? (typeof values.name === "string" ? values.name : `${action.idPrefix ?? ""}${crypto.randomUUID()}`);
    try {
      await submit(action, target, payload, revision);
      onDone(target);
    } catch (problem) {
      setFailure((problem instanceof Error && problem.message) || t("The platform did not accept this change."));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={onClose} width={620} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={heading} onOpenChange={onClose} />}
        content={
          <LayoutContent>
            <FormLayout defaultOptionality="optional">
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              {blocked.length > 0 && (
                <Banner status="warning" title={t("Some required inputs cannot be entered here yet")} description={blocked.join(", ")} />
              )}
              {owned && (
                <Selector
                  label={t("Project")}
                  isRequired
                  value={project}
                  onChange={setProject}
                  options={projects.status === "ready" ? (projects.data.records as Project[]).map((entry) => ({ value: entry.id, label: entry.title })) : []}
                />
              )}
              {shownParameters.map((parameter) => {
                const { label, description } = wording(parameter);
                if (parameter.choices) {
                  return (
                    <Selector
                      key={parameter.name}
                      label={t(label)}
                      description={description}
                      isRequired={parameter.required}
                      value={String(values[parameter.name] ?? "")}
                      onChange={(value) => set(parameter.name, value)}
                      options={parameter.choices.map((value) => ({ value, label: value }))}
                    />
                  );
                }
                if (parameter.type === "boolean") {
                  return (
                    <Switch
                      key={parameter.name}
                      label={t(label)}
                      description={description}
                      value={values[parameter.name] === true}
                      onChange={(value) => set(parameter.name, value)}
                    />
                  );
                }
                if (numeric.has(parameter.type)) {
                  return (
                    <NumberInput
                      key={parameter.name}
                      label={t(label)}
                      description={description}
                      isRequired={parameter.required}
                      value={typeof values[parameter.name] === "number" ? (values[parameter.name] as number) : null}
                      onChange={(value) => set(parameter.name, value)}
                    />
                  );
                }
                return (
                  <TextInput
                    key={parameter.name}
                    label={t(label)}
                    description={description}
                    isRequired={parameter.required}
                    value={String(values[parameter.name] ?? "")}
                    onChange={(value) => set(parameter.name, value)}
                  />
                );
              })}
            </FormLayout>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={confirm} variant="primary" isDisabled={busy || missing || blocked.length > 0 || (owned && !project)} onClick={send} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
