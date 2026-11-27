import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { Field } from "@astryxdesign/core/Field";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { ChevronDownIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useRef, useState } from "react";
import { type Api, HostRefusal, request, reread, reasonOf, send } from "../host";
import { t } from "../i18n";
import { beforeLeave } from "../router";
import type { Described, Section, Values } from "./describe";
import { ParameterInput } from "./ParameterInput";
import { type InputStatus, enterable, settles } from "./fieldRules";
import { SemanticValue } from "../semantic";
import { plain, present, shown } from "./values";
import { OneOfInput, ReferenceLink } from "./ReferenceField";
import { motionStyles } from "./motion";
import { PropertyField } from "./PropertyField";
import { MemberLabel } from "../semantic";

type Conflict = { name: string; before: unknown; mine: unknown; saved: unknown; revision: number };
export type Editor = {
  values: Values;
  stale?: boolean;
  conflict?: Conflict;
  resolve: (keep: "mine" | "saved") => void;
  status: Record<string, InputStatus>;
  saving: ReadonlySet<string>;
  change: (parameter: Api.Field, value: unknown) => void;
  settle: () => void;
  commit(name: string): Promise<void>;
  leave?: (discard: boolean) => void;
};

const blank = (value: unknown) => value == null || value === "";
const same = (left: unknown, right: unknown) => left === right || (blank(left) && blank(right)) ||
  (Array.isArray(left) && Array.isArray(right) && JSON.stringify(left) === JSON.stringify(right));

type Written = Promise<{ revision: number }>;
type WriteRecord = (patch: Values, revision: number) => Written;

export function useEditor(described: Described, id: string, values: Values, revision: number, writeRecord?: WriteRecord): Editor {
  const edit = described.edit;
  const canWrite = Boolean(edit || writeRecord);
  const [drafts, setDrafts] = useState<Values>({});
  const [status, setStatus] = useState<Record<string, InputStatus>>({});
  const [conflict, setConflict] = useState<Conflict>();
  const [saving, setSaving] = useState<ReadonlySet<string>>(new Set());
  const [leave, setLeave] = useState<Editor["leave"]>();
  const refused = useRef(new Set<string>());
  const reasons = useRef(new Map<string, string>());
  const latest = useRef(revision);
  const pending = useRef(drafts);
  const origins = useRef<Values>({});
  const queue = useRef(Promise.resolve());
  const sent = useRef<Values>({});
  const clearTimers = useRef<Map<string, number>>(new Map());
  const hasDraft = Object.keys(drafts).length > 0;
  if (!hasDraft && !conflict) latest.current = Math.max(latest.current, revision);
  pending.current = drafts;
  const stale = hasDraft && revision > latest.current;

  useEffect(() => () => {
    for (const timer of clearTimers.current.values()) window.clearTimeout(timer);
    clearTimers.current.clear();
  }, []);

  useEffect(() => {
    setDrafts((previous) => {
      const next = Object.fromEntries(Object.entries(previous).filter(([name, value]) => !same(value, values[name])));
      for (const name of Object.keys(origins.current)) if (!(name in next)) delete origins.current[name];
      return next;
    });
    setStatus((previous) => Object.fromEntries(Object.entries(previous).filter(([, entry]) => entry.type === "error" || entry.type === "success")));
  }, [values]);

  const write = async (name: string, value: unknown, before: unknown, together: Values = {}) => {
    if (!canWrite) return;
    try {
      const patch = { ...together, [name]: value };
      const accepted = writeRecord ? await writeRecord(patch, latest.current) : await send(edit!, id, patch, latest.current);
      latest.current = accepted.revision;
      const savedFields = [name, ...Object.keys(together)];
      for (const field of savedFields) {
        refused.current.delete(field);
        reasons.current.delete(field);
        delete origins.current[field];
        const existing = clearTimers.current.get(field);
        if (existing) window.clearTimeout(existing);
        clearTimers.current.set(field, window.setTimeout(() => {
          clearTimers.current.delete(field);
          setStatus((previous) => (previous[field]?.type === "success" ? Object.fromEntries(Object.entries(previous).filter(([key]) => key !== field)) : previous));
        }, 1050));
      }
      setStatus((previous) => ({ ...previous, ...Object.fromEntries(savedFields.map((sentField) => [sentField, { type: "success" as const }])) }));
    } catch (problem) {
      if (!(problem instanceof HostRefusal && problem.reasonCode === "stale_revision")) {
        if (same(pending.current[name], value)) {
          refused.current.add(name);
          reasons.current.set(name, reasonOf(problem));
          setStatus((previous) => ({ ...previous, [name]: { type: "error", message: reasonOf(problem) } }));
        }
        return;
      }
      const now = await request<Api.RecordView>(`/v1/records/${described.type}/${encodeURIComponent(id)}`);
      const nextRevision = (now.record as { revision: number }).revision;
      const saved = plain(now.values?.[name]);
      latest.current = nextRevision;
      if (same(saved, before)) return write(name, value, before, together);
      setConflict({ name, before, mine: value, saved, revision: nextRevision });
    }
  };

  const save = (name: string, value: unknown) => {
    const before = name in origins.current ? origins.current[name] : values[name];
    if (!canWrite || same(value, before) || (name in sent.current && sent.current[name] === value)) return;
    const drafted = (other: string) => other !== name && other in pending.current && !same(pending.current[other], values[other]);
    const form = { ...values, ...pending.current, [name]: value };
    const controller = described.field(name)?.when?.field;
    const together: Values = controller && drafted(controller) ? { [controller]: pending.current[controller] } : {};
    const field = described.field(name);
    if (field && !blank(value)) for (const sibling of described.members(field)) if (sibling.name !== name && !blank(values[sibling.name])) together[sibling.name] = "";
    const changedFields = [name, ...Object.keys(together)];
    for (const candidate of described.entity.fields) {
      if (candidate.when && changedFields.includes(candidate.when.field) && drafted(candidate.name) && described.shows(candidate, form)) together[candidate.name] = pending.current[candidate.name];
    }
    const missing = described.needs(form, changedFields).filter((candidate) => candidate.name !== name && !(candidate.name in together));
    if (missing.length) {
      const message = `${t("Also fill in")}: ${missing.map((candidate) => t(candidate.title)).join(", ")}`;
      refused.current.add(name);
      reasons.current.set(name, message);
      setStatus((previous) => ({ ...previous, [name]: { type: "error", message } }));
      return;
    }
    sent.current[name] = value;
    Object.assign(sent.current, together);
    setSaving((previous) => new Set(previous).add(name));
    queue.current = queue.current.then(() => write(name, value, before, together)).finally(() => setSaving((previous) => {
      const next = new Set(previous);
      next.delete(name);
      return next;
    }));
  };

  const resolve = (keep: "mine" | "saved") => {
    if (!conflict) return;
    setConflict(undefined);
    delete sent.current[conflict.name];
    delete origins.current[conflict.name];
    if (keep === "mine") queue.current = queue.current.then(() => write(conflict.name, conflict.mine, conflict.saved));
    else {
      setDrafts(({ [conflict.name]: _, ...rest }) => rest);
      reread();
    }
  };

  const text = new Set(((edit ?? (writeRecord ? described.create : undefined))?.payload ?? []).filter((parameter) => !settles(parameter, described.field(parameter.name))).map((parameter) => parameter.name));

  const settle = () => {
    for (const [name, value] of Object.entries(pending.current)) if (text.has(name)) save(name, value);
  };
  const commit = async (name: string) => {
    settle();
    await queue.current;
    const reason = reasons.current.get(name);
    if (reason) throw new Error(reason);
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
          for (const name of refused.current) {
            delete sent.current[name];
            delete origins.current[name];
          }
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
    stale,
    leave,
    status,
    saving,
    conflict,
    resolve,
    change: (parameter, value) => {
      refused.current.delete(parameter.name);
      reasons.current.delete(parameter.name);
      if (!(parameter.name in origins.current)) origins.current[parameter.name] = values[parameter.name];
      const field = described.field(parameter.name);
      const cleared = field && !blank(value) ? described.members(field).filter((sibling) => sibling.name !== parameter.name).map((sibling) => [sibling.name, ""]) : [];
      setDrafts((previous) => ({ ...previous, ...Object.fromEntries(cleared), [parameter.name]: value }));
      setStatus(({ [parameter.name]: _, ...rest }) => rest);
      if (settles(parameter, described.field(parameter.name))) save(parameter.name, value);
    },
    settle,
    commit,
  };
}

export function EditorConflict({ editor, described }: { editor: Editor; described: Described }) {
  const clash = editor.conflict;
  const clashing = clash && described.field(clash.name);
  if (!clash || !clashing) return null;
  return (
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
                  <SemanticValue field={clashing} value={clash.before} size="sm" surface="detail" interactive={false} />
                </MetadataListItem>
                <MetadataListItem label={t("Saved")}>
                  <SemanticValue field={clashing} value={clash.saved} size="sm" surface="detail" interactive={false} />
                </MetadataListItem>
                <MetadataListItem label={t("Yours")}>
                  <SemanticValue field={clashing} value={clash.mine} size="sm" surface="detail" interactive={false} />
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
  );
}

type FieldGroupProps = { described: Described; editor: Editor; sections: Section[]; direction: "horizontal-labels" | "vertical"; focusEmpty?: boolean; fold?: boolean; refs?: Api.RecordView["refs"]; restricted?: ReadonlySet<string> };

const styles = stylex.create({
  aligned: { gridTemplateColumns: { default: "minmax(0, 1fr) minmax(0, 3fr)", "@media (max-width: 480px)": "100%" } },
});

export function FieldGroup({ described, editor, sections, direction, focusEmpty, fold, refs, restricted }: FieldGroupProps) {
  const parameters = new Map((described.edit?.payload ?? []).filter(enterable).map((parameter) => [parameter.name, parameter]));
  const [folded] = useState(() => new Set(fold ? sections.flatMap((section) => section.fields)
    .filter((field) => parameters.has(field.name) && !field.required && !field.when && !field.oneOf && !restricted?.has(field.name) && present(field, editor.values[field.name]) === "")
    .map((field) => field.name) : []));
  const [more, setMore] = useState(false);
  const visible = (fields: Api.FieldInfo[]) =>
    fields.filter((field) => {
      if (restricted?.has(field.name)) return true;
      if (!described.shows(field, editor.values)) return false;
      if (field.oneOf) return described.members(field).some((member) => parameters.has(member.name) || !blank(editor.values[member.name]));
      if (parameters.has(field.name)) return true;
      const value = editor.values[field.name];
      const structured = field.type === "money" || (field.type as string) === "quantity" || field.type === "attachment" || field.type === "tags" || field.type === "references";
      return (structured || typeof value !== "object") && present(field, value) !== "";
    });
  const hidden = more ? 0 : sections.flatMap((section) => visible(section.fields)).filter((field) => folded.has(field.name)).length;
  const prepared = sections.map((section) => {
    const shownFields = visible(section.fields);
    const primary = shownFields.filter((field) => !folded.has(field.name));
    const extra = shownFields.filter((field) => folded.has(field.name));
    return { title: section.title, primary, extra };
  }).filter((section) => section.primary.length > 0 || section.extra.length > 0);
  if (prepared.length === 0 && hidden === 0) return null;
  const typed = (parameter?: Api.Field) => parameter && !parameter.choices && parameter.type !== "boolean" && parameter.type !== "attachment";
  const first = focusEmpty
    ? prepared.flatMap((section) => (more ? [...section.primary, ...section.extra] : section.primary)).find((field) => !restricted?.has(field.name) && typed(parameters.get(field.name)) && shown(editor.values[field.name]) === "")?.name
    : undefined;
  const renderField = (field: Api.FieldInfo) => {
    const parameter = parameters.get(field.name);
    const value = editor.values[field.name];
    const id = String(value ?? "");
    const referenceName = refs?.[field.name]?.[id];
    if (field.oneOf) {
      const members = described.members(field);
      const editable = members.flatMap((member) => parameters.get(member.name) ?? []);
      const names = Object.fromEntries(members.map((member) => [member.name, refs?.[member.name]?.[String(editor.values[member.name] ?? "")]]));
      if (editable.length === members.length) {
        return <OneOfInput key={field.name} title={t(field.oneOf)} members={members} values={editor.values} names={names}
          status={members.map((member) => editor.status[member.name]).find(Boolean)}
          onChange={(name, next) => editor.change(editable.find((parameter) => parameter.name === name)!, next)} />;
      }
      const linked = members.find((member) => !blank(editor.values[member.name]));
      return <Field key={field.name} label={t(field.oneOf)} inputID={`shown-${field.name}`}>
        {linked && names[linked.name] ? <ReferenceLink type={String(linked.ref)} id={String(editor.values[linked.name])} name={String(names[linked.name])} /> : <Text type="body">{present(field, editor.values[linked?.name ?? field.name])}</Text>}
      </Field>;
    }
    if (restricted?.has(field.name)) {
      return <Field key={field.name} label={t(field.title)} inputID={`restricted-${field.name}`}>
        <Text type="supporting" color="secondary">{t("Cannot view")}</Text>
      </Field>;
    }
    return <PropertyField key={field.name} field={field} parameter={parameter} described={described.entity} value={value} form={editor.values}
      name={referenceName} status={editor.status[field.name]} saving={editor.saving.has(field.name)} open={field.name === first}
      onChange={parameter && ((next: unknown) => editor.change(parameter, next))} onCommit={() => editor.commit(field.name)} />;
  };
  const layoutStyle = direction === "horizontal-labels" ? styles.aligned : undefined;
  return (
    <VStack gap={6} onBlur={editor.settle}>
      {editor.conflict && sections.some((section) => section.fields.some((field) => field.name === editor.conflict?.name)) && <EditorConflict editor={editor} described={described} />}
      {prepared.map((section) => {
        const onlyExtra = section.primary.length === 0;
        const block = (
          <VStack gap={3}>
            <Heading level={3} accessibilityLevel={2}>{t(section.title)}</Heading>
            {section.primary.length > 0 && (
              <FormLayout direction={direction} defaultOptionality="optional" xstyle={layoutStyle}>
                {section.primary.map(renderField)}
              </FormLayout>
            )}
            {section.extra.length > 0 && !onlyExtra && (
              <VStack
                ref={(node) => { if (node) node.inert = !more; }}
                aria-hidden={!more || undefined}
                xstyle={[motionStyles.collapseGrid, more && motionStyles.collapseOpen]}
              >
                <VStack xstyle={motionStyles.collapseInner}>
                  <FormLayout direction={direction} defaultOptionality="optional" xstyle={layoutStyle}>
                    {section.extra.map(renderField)}
                  </FormLayout>
                </VStack>
              </VStack>
            )}
            {onlyExtra && (
              <FormLayout direction={direction} defaultOptionality="optional" xstyle={layoutStyle}>
                {section.extra.map(renderField)}
              </FormLayout>
            )}
          </VStack>
        );
        if (!onlyExtra) return <VStack key={section.title}>{block}</VStack>;
        return (
          <VStack
            key={section.title}
            ref={(node) => { if (node) node.inert = !more; }}
            aria-hidden={!more || undefined}
            xstyle={[motionStyles.collapseGrid, more && motionStyles.collapseOpen]}
          >
            <VStack xstyle={motionStyles.collapseInner}>{block}</VStack>
          </VStack>
        );
      })}
      {folded.size > 0 && (
        <HStack hAlign="start">
          <Button
            variant="ghost"
            size="sm"
            icon={<HStack xstyle={[motionStyles.chevron, more && motionStyles.chevronOpen]}><Icon icon={ChevronDownIcon} size="sm" /></HStack>}
            label={more ? t("Hide empty fields") : `${t("Show empty fields")} · ${hidden}`}
            onClick={() => setMore(!more)}
          />
        </HStack>
      )}
    </VStack>
  );
}
