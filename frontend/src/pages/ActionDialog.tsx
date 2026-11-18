import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { ParameterInput, enterable } from "./ParameterInput";

type Values = Record<string, unknown>;
type Applied = Promise<void>;
type Project = { id: string; title: string };
export type ActionDialogProps = {
  action: Api.Action;
  described: Api.EntityInfo;
  heading: string;
  confirm: string;
  id?: string;
  revision: number;
  initial?: Values;
  fixed?: Values;
  owned?: boolean;
  quick?: boolean;
  onClose: () => void;
  onDone: (id: string) => void;
  perform?: (payload: Values) => Applied;
};


export function ActionDialog({ action, described, heading, confirm, id, revision, initial = {}, fixed = {}, owned, quick, onClose, onDone, perform }: ActionDialogProps) {
  const [values, setValues] = useState<Values>(initial);
  const [project, setProject] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const projects = useRead<Api.RecordPage>(owned ? "/v1/records/build.app?limit=200" : null);
  const set = (name: string, value: unknown) => setValues((previous) => ({ ...previous, [name]: value }));
  const shownParameters = action.payload.filter((parameter) => !(parameter.name in fixed) && enterable(parameter) && (!quick || parameter.required || parameter.name === described.display));
  const blocked = action.payload.filter((parameter) => parameter.required && !(parameter.name in fixed) && !enterable(parameter)).map((parameter) => parameter.name);
  const missing = shownParameters.some((parameter) => parameter.required && (values[parameter.name] == null || values[parameter.name] === ""));

  const apply = async () => {
    setBusy(true);
    setFailure("");
    const payload: Values = { ...fixed };
    for (const parameter of shownParameters) {
      const value = values[parameter.name];
      if (value != null && value !== "" && value !== initial[parameter.name]) payload[parameter.name] = value;
    }
    if (owned) payload.project = project;
    const target = id ?? (typeof values.name === "string" ? values.name : `${action.idPrefix ?? ""}${crypto.randomUUID()}`);
    try {
      if (perform) await perform(payload);
      else await send(action, target, payload, revision);
      onDone(target);
    } catch (problem) {
      setFailure(reasonOf(problem));
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
              {shownParameters.map((parameter) => (
                <ParameterInput
                  key={parameter.name}
                  parameter={parameter}
                  described={described}
                  value={values[parameter.name]}
                  onChange={(value) => set(parameter.name, value)}
                />
              ))}
            </FormLayout>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={confirm} variant="primary" isDisabled={busy || missing || blocked.length > 0 || (owned && !project)} onClick={apply} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
