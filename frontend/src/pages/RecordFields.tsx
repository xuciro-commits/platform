import { Field } from "@astryxdesign/core/Field";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { VStack } from "@astryxdesign/core/Layout";
import { Heading, Text } from "@astryxdesign/core/Text";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useRef, useState } from "react";
import { type Api, submit } from "../host";
import { t } from "../i18n";
import type { Described, Section, Values } from "./describe";
import { type InputStatus, ParameterInput, enterable, settles } from "./ParameterInput";
import { shown } from "./values";

export type Editor = {
  values: Values;
  status: Record<string, InputStatus>;
  change: (parameter: Api.Field, value: unknown) => void;
  settle: () => void;
};

const blank = (value: unknown) => value == null || value === "";
const same = (left: unknown, right: unknown) => left === right || (blank(left) && blank(right));

export function useEditor(edit: Api.Action | undefined, id: string, values: Values, revision: number): Editor {
  const [drafts, setDrafts] = useState<Values>({});
  const [status, setStatus] = useState<Record<string, InputStatus>>({});
  const latest = useRef(revision);
  const pending = useRef(drafts);
  const queue = useRef(Promise.resolve());
  const sent = useRef<Values>({});
  latest.current = Math.max(latest.current, revision);
  pending.current = drafts;

  useEffect(() => {
    setDrafts((previous) => Object.fromEntries(Object.entries(previous).filter(([name, value]) => !same(value, values[name]))));
    setStatus((previous) => Object.fromEntries(Object.entries(previous).filter(([, entry]) => entry.type === "error")));
  }, [values]);

  const save = (name: string, value: unknown) => {
    if (!edit || same(value, values[name]) || (name in sent.current && sent.current[name] === value)) return;
    sent.current[name] = value;
    queue.current = queue.current.then(async () => {
      try {
        const accepted = await submit(edit, id, { [name]: value }, latest.current);
        latest.current = accepted.revision;
        setStatus((previous) => ({ ...previous, [name]: { type: "success" } }));
      } catch (problem) {
        const message = (problem instanceof Error && problem.message) || t("The platform did not accept this change.");
        setStatus((previous) => ({ ...previous, [name]: { type: "error", message } }));
      }
    });
  };

  const text = new Set((edit?.payload ?? []).filter((parameter) => !settles(parameter)).map((parameter) => parameter.name));

  return {
    values: { ...values, ...drafts },
    status,
    change: (parameter, value) => {
      setDrafts((previous) => ({ ...previous, [parameter.name]: value }));
      setStatus(({ [parameter.name]: _, ...rest }) => rest);
      if (settles(parameter)) save(parameter.name, value);
    },
    settle: () => {
      for (const [name, value] of Object.entries(pending.current)) if (text.has(name)) save(name, value);
    },
  };
}

type FieldGroupProps = { described: Described; editor: Editor; sections: Section[]; direction: "horizontal-labels" | "vertical" };

const styles = stylex.create({
  aligned: { gridTemplateColumns: { default: "minmax(0, 1fr) minmax(0, 3fr)", "@media (max-width: 480px)": "100%" } },
});

export function FieldGroup({ described, editor, sections, direction }: FieldGroupProps) {
  const parameters = new Map((described.edit?.payload ?? []).filter(enterable).map((parameter) => [parameter.name, parameter]));
  const visible = (fields: Api.FieldInfo[]) =>
    fields.filter((field) => {
      if (!described.shows(field, editor.values)) return false;
      if (parameters.has(field.name)) return true;
      const value = editor.values[field.name];
      return typeof value !== "object" && shown(value) !== "";
    });
  const shownSections = sections.map((section) => ({ ...section, fields: visible(section.fields) })).filter((section) => section.fields.length > 0);
  if (shownSections.length === 0) return null;
  return (
    <VStack gap={6} onBlur={editor.settle}>
      {shownSections.map((section) => (
        <VStack key={section.title} gap={3}>
          <Heading level={3}>{t(section.title)}</Heading>
          <FormLayout direction={direction} defaultOptionality="optional" xstyle={direction === "horizontal-labels" ? styles.aligned : undefined}>
            {section.fields.map((field) => {
              const parameter = parameters.get(field.name);
              return parameter ? (
                <ParameterInput
                  key={field.name}
                  parameter={parameter}
                  described={described.entity}
                  value={editor.values[field.name]}
                  status={editor.status[field.name]}
                  onChange={(value) => editor.change(parameter, value)}
                  onEnter={editor.settle}
                />
              ) : (
                <Field key={field.name} label={t(field.title)} inputID={`shown-${field.name}`}>
                  <Text type="body">{shown(editor.values[field.name])}</Text>
                </Field>
              );
            })}
          </FormLayout>
        </VStack>
      ))}
    </VStack>
  );
}
