import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Field } from "@astryxdesign/core/Field";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useRef, useState } from "react";
import { type Api, HostRefusal, request, reread } from "../host";
import { t } from "../i18n";
import { beforeLeave } from "../router";
import { reasonOf, send } from "./act";
import type { Described, Section, Values } from "./describe";
import { type InputStatus, ParameterInput, enterable, settles } from "./ParameterInput";
import { plain, present, shown } from "./values";

type Conflict = { name: string; before: unknown; mine: unknown; saved: unknown; revision: number };
export type Editor = {
  values: Values;
  conflict?: Conflict;
  resolve: (keep: "mine" | "saved") => void;
  status: Record<string, InputStatus>;
  change: (parameter: Api.Field, value: unknown) => void;
  settle: () => void;
  leave?: (discard: boolean) => void;
};

const blank = (value: unknown) => value == null || value === "";
const same = (left: unknown, right: unknown) => left === right || (blank(left) && blank(right));

export function useEditor(edit: Api.Action | undefined, id: string, values: Values, revision: number): Editor {
  const [drafts, setDrafts] = useState<Values>({});
  const [status, setStatus] = useState<Record<string, InputStatus>>({});
  const [conflict, setConflict] = useState<Conflict>();
  const [leave, setLeave] = useState<Editor["leave"]>();
  const refused = useRef(new Set<string>());
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

  const write = async (name: string, value: unknown, before: unknown) => {
    if (!edit) return;
    try {
      const accepted = await send(edit, id, { [name]: value }, latest.current);
      latest.current = accepted.revision;
      setStatus((previous) => ({ ...previous, [name]: { type: "success" } }));
    } catch (problem) {
      if (!(problem instanceof HostRefusal && problem.reasonCode === "stale_revision")) {
        if (same(pending.current[name], value)) {
          refused.current.add(name);
          setStatus((previous) => ({ ...previous, [name]: { type: "error", message: reasonOf(problem) } }));
        }
        return;
      }
      const now = await request<Api.RecordView>(`/v1/records/${edit.target}/${encodeURIComponent(id)}`);
      const revision = (now.record as { revision: number }).revision;
      const saved = plain(now.values?.[name]);
      latest.current = revision;
      if (same(saved, before)) return write(name, value, before);
      setConflict({ name, before, mine: value, saved, revision });
    }
  };

  const save = (name: string, value: unknown) => {
    if (!edit || same(value, values[name]) || (name in sent.current && sent.current[name] === value)) return;
    sent.current[name] = value;
    const before = values[name];
    queue.current = queue.current.then(() => write(name, value, before));
  };

  const resolve = (keep: "mine" | "saved") => {
    if (!conflict) return;
    setConflict(undefined);
    delete sent.current[conflict.name];
    if (keep === "mine") queue.current = queue.current.then(() => write(conflict.name, conflict.mine, conflict.saved));
    else {
      setDrafts(({ [conflict.name]: _, ...rest }) => rest);
      reread();
    }
  };

  const text = new Set((edit?.payload ?? []).filter((parameter) => !settles(parameter)).map((parameter) => parameter.name));

  const settle = () => {
    for (const [name, value] of Object.entries(pending.current)) if (text.has(name)) save(name, value);
  };
  const flush = useRef(settle);
  flush.current = settle;
  useEffect(() => beforeLeave(async () => {
    flush.current();
    await queue.current;
    if (!refused.current.size) return true;
    return new Promise<boolean>((done) => {
      setLeave(() => (discard: boolean) => {
        setLeave(undefined);
        if (discard) {
          for (const name of refused.current) delete sent.current[name];
          refused.current.clear();
          setDrafts({});
          setStatus({});
        }
        done(discard);
      });
    });
  }), []);

  return {
    values: { ...values, ...drafts },
    leave,
    status,
    conflict,
    resolve,
    change: (parameter, value) => {
      refused.current.delete(parameter.name);
      setDrafts((previous) => ({ ...previous, [parameter.name]: value }));
      setStatus(({ [parameter.name]: _, ...rest }) => rest);
      if (settles(parameter)) save(parameter.name, value);
    },
    settle,
  };
}

type FieldGroupProps = { described: Described; editor: Editor; sections: Section[]; direction: "horizontal-labels" | "vertical"; focusEmpty?: boolean };

const styles = stylex.create({
  aligned: { gridTemplateColumns: { default: "minmax(0, 1fr) minmax(0, 3fr)", "@media (max-width: 480px)": "100%" } },
});

export function FieldGroup({ described, editor, sections, direction, focusEmpty }: FieldGroupProps) {
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
  const typed = (parameter?: Api.Field) => parameter && !parameter.choices && parameter.type !== "boolean";
  const first = focusEmpty
    ? shownSections.flatMap((section) => section.fields).find((field) => typed(parameters.get(field.name)) && shown(editor.values[field.name]) === "")?.name
    : undefined;
  const clash = editor.conflict && sections.some((section) => section.fields.some((field) => field.name === editor.conflict?.name)) ? editor.conflict : undefined;
  const clashing = clash ? described.field(clash.name) : undefined;
  return (
    <VStack gap={6} onBlur={editor.settle}>
      {clash && clashing && (
        <Dialog isOpen onOpenChange={() => editor.resolve("saved")} width={520} purpose="form">
          <Layout
            padding={4}
            header={<DialogHeader title={t("Someone saved this first")} onOpenChange={() => editor.resolve("saved")} />}
            content={
              <LayoutContent>
                <VStack gap={3}>
                  <Text type="body">{t("Choose the value to keep. Nothing is overwritten until you choose.")}</Text>
                  <Heading level={4}>{t(clashing.title)}</Heading>
                  <MetadataList>
                    <MetadataListItem label={t("Before")}>
                      <Text type="body">{present(clashing, clash.before)}</Text>
                    </MetadataListItem>
                    <MetadataListItem label={t("Saved")}>
                      <Text type="body">{present(clashing, clash.saved)}</Text>
                    </MetadataListItem>
                    <MetadataListItem label={t("Yours")}>
                      <Text type="body">{present(clashing, clash.mine)}</Text>
                    </MetadataListItem>
                  </MetadataList>
                </VStack>
              </LayoutContent>
            }
            footer={
              <LayoutFooter>
                <HStack gap={2} vAlign="center">
                  <StackItem size="fill" />
                  <Button label={t("Keep saved")} variant="secondary" onClick={() => editor.resolve("saved")} />
                  <Button label={t("Use mine")} variant="primary" onClick={() => editor.resolve("mine")} />
                </HStack>
              </LayoutFooter>
            }
          />
        </Dialog>
      )}
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
                  focused={field.name === first}
                />
              ) : (
                <Field key={field.name} label={t(field.title)} inputID={`shown-${field.name}`}>
                  <Text type="body">{present(field, editor.values[field.name])}</Text>
                </Field>
              );
            })}
          </FormLayout>
        </VStack>
      ))}
    </VStack>
  );
}
