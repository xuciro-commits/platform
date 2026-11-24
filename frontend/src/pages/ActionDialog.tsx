import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { motionStyles } from "./motion";
import { ParameterInput, enterable, missingValue } from "./ParameterInput";
import { OneOfInput } from "./ReferenceField";
import { grouped } from "./describe";

type Values = Record<string, unknown>;
type Applied = Promise<void>;
type Project = { id: string; title: string; name?: string };
export type ActionDialogProps = {
  action: Api.Action;
  described?: Api.EntityInfo;
  heading: string;
  confirm: string;
  id?: string;
  revision?: number;
  initial?: Values;
  fixed?: Values;
  owned?: boolean;
  quick?: boolean;
  only?: string[];
  onClose: () => void;
  onDone: (id: string) => void;
  perform?: (payload: Values) => Applied;
};

function todayInZone(timeZone?: string) {
  try {
    return new Intl.DateTimeFormat("en-CA", {
      timeZone: timeZone || undefined,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(new Date());
  } catch {
    return new Date().toISOString().slice(0, 10);
  }
}

function resolveDefault(raw: unknown, me?: Api.MeView): unknown {
  if (raw === "member") return me?.principalId || undefined;
  if (raw === "today") return todayInZone(me?.account?.effective?.timezone);
  if (raw === "now") return new Date().toISOString();
  if (typeof raw === "string" && raw.startsWith("setting:")) return undefined;
  return raw;
}

export function ActionDialog({ action, described, heading, confirm, id, revision, initial = {}, fixed = {}, owned, quick, only, onClose, onDone, perform }: ActionDialogProps) {
  const me = useRead<Api.MeView>("/v1/me");
  const touched = useRef<Set<string>>(new Set(Object.keys(initial)));
  const formRef = useRef<HTMLDivElement>(null);
  const [values, setValues] = useState<Values>(() => {
    const out: Values = {};
    for (const field of action.payload) {
      if (field.default == null) continue;
      const resolved = resolveDefault(field.default, me.status === "ready" ? me.data : undefined);
      if (resolved !== undefined) out[field.name] = resolved;
    }
    return { ...out, ...initial };
  });
  const effectiveZone = me.status === "ready" ? me.data.account?.effective?.timezone ?? "" : "";
  useEffect(() => {
    if (me.status !== "ready") return;
    setValues((previous) => {
      let changed = false;
      const next = { ...previous };
      for (const field of action.payload) {
        if (field.default == null || touched.current.has(field.name)) continue;
        const resolved = resolveDefault(field.default, me.data);
        if (resolved !== undefined && next[field.name] !== resolved) {
          next[field.name] = resolved;
          changed = true;
        }
      }
      return changed ? next : previous;
    });
  }, [me.status, me.status === "ready" ? me.data.principalId : "", effectiveZone, action]);
  const [project, setProject] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const projects = useRead<Api.RecordPage>(owned ? "/v1/records/build.app?limit=200" : null);
  const projectList = projects.status === "ready" ? (projects.data.records as Project[]) : [];
  const singleProject = owned && projectList.length === 1 ? projectList[0] : undefined;
  useEffect(() => {
    if (singleProject && !project) setProject(singleProject.id);
  }, [singleProject, project]);
  const effectiveProject = singleProject ? singleProject.id : project;

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const root = formRef.current;
      if (!root) return;
      const focusable = root.querySelector<HTMLElement>(
        "input:not([disabled]):not([type=hidden]), textarea:not([disabled]), [role=combobox]:not([aria-disabled=true])",
      );
      focusable?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  const set = (name: string, value: unknown) => {
    touched.current.add(name);
    setValues((previous) => {
      const next = { ...previous, [name]: value };
      if (name === "app" && String(value ?? "").trim() !== String(previous.app ?? "").trim()) {
        for (const parameter of action.payload) if (parameter.type === "action[]") delete next[parameter.name];
      }
      return next;
    });
  };
  const shownParameters = action.payload.filter((parameter) => !(parameter.name in fixed) && enterable(parameter) && (!quick || parameter.required || parameter.name === described?.display || initial[parameter.name] !== undefined))
    .flatMap((parameter) => (!only ? [parameter] : only.includes(parameter.name) ? [{ ...parameter, required: true }] : []));
  const blocked = action.payload.filter((parameter) => parameter.required && !(parameter.name in fixed) && !enterable(parameter)).map((parameter) => parameter.name);
  const missing = shownParameters.some((parameter) => missingValue(parameter, values[parameter.name]));
  const canSubmit = !busy && !missing && blocked.length === 0 && (!owned || Boolean(effectiveProject));

  const apply = async () => {
    if (!canSubmit) return;
    setBusy(true);
    setFailure("");
    const payload: Values = { ...fixed };
    for (const parameter of shownParameters) {
      const value = values[parameter.name];
      if (value != null && value !== "") payload[parameter.name] = value;
    }
    if (owned) payload.project = effectiveProject;
    const space = projectList.find((entry) => entry.id === effectiveProject)?.name;
    const named = typeof values.name === "string" ? (space && space !== "workspace" ? `${space}.${values.name}` : values.name) : undefined;
    const autoNamed = !id && Boolean(described?.naming) && action.schema === `${described?.type}.create`;
    const target = autoNamed ? "" : (id ?? named ?? `${action.idPrefix ?? ""}${crypto.randomUUID()}`);
    try {
      let created = target;
      if (perform) await perform(payload);
      else {
        const accepted = await send(action, target, payload, revision);
        if (accepted.id) created = accepted.id;
      }
      onDone(created);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && canSubmit) {
      event.preventDefault();
      void apply();
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
            <VStack ref={formRef} onKeyDown={onKeyDown}>
              <FormLayout defaultOptionality="optional">
                {failure && <VStack xstyle={[motionStyles.bannerEnter, motionStyles.shakeError]}><Banner status="error" title={t("Not saved")} description={failure} /></VStack>}
                {blocked.length > 0 && (
                  <Banner status="warning" title={t("Some required inputs cannot be entered here yet")} description={blocked.join(", ")} />
                )}
                {owned && !singleProject && (
                  <Selector
                    label={t("Project")}
                    isRequired
                    value={project}
                    onChange={setProject}
                    options={projectList.map((entry) => ({ value: entry.id, label: entry.title }))}
                  />
                )}
                {shownParameters.map((parameter) => {
                  const field = described?.fields.find((candidate) => candidate.name === parameter.name);
                  const members = field && described ? grouped(described.fields, field) : [];
                  if (field?.oneOf) {
                    if (members.some((member) => member.name in fixed) || shownParameters.find((other) => members.some((member) => member.name === other.name))?.name !== parameter.name) return null;
                    return <OneOfInput key={parameter.name} title={t(field.oneOf)} members={members.filter((member) => shownParameters.some((other) => other.name === member.name))}
                      values={values} onChange={(name, next) => setValues((previous) => ({ ...previous, ...Object.fromEntries(members.map((member) => [member.name, ""])), [name]: next }))} />;
                  }
                  return <ParameterInput
                    key={parameter.name}
                    parameter={parameter}
                    described={described}
                    value={values[parameter.name]}
                    form={{ ...values, ...fixed }}
                    onChange={(value) => set(parameter.name, value)}
                  />;
                })}
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={confirm} variant="primary" isDisabled={!canSubmit} isLoading={busy} onClick={apply} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
