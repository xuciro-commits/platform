import { Button } from "@astryxdesign/core/Button";
import { Field } from "@astryxdesign/core/Field";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Spinner } from "@astryxdesign/core/Spinner";
import { Text } from "@astryxdesign/core/Text";
import { VisuallyHidden } from "@astryxdesign/core/VisuallyHidden";
import { useClickableContainer, usePressFeedback } from "@astryxdesign/core/hooks";
import { radiusVars, sizeVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import { focusOutlineStyles, interactionOverlayStyles } from "@astryxdesign/core/utils";
import { ArrowTurnDownRightIcon, CalculatorIcon, CheckIcon, LockClosedIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { type FocusEvent, type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { type Api, reasonOf } from "../host";
import { t } from "../i18n";
import { SemanticValue } from "../semantic";
import { iconField } from "./icons";
import { motionStyles } from "./motion";
import { ParameterInput } from "./ParameterInput";
import type { InputStatus } from "./fieldRules";
import type { Dot } from "./Transitions";

const layers = "[role=listbox], [role=dialog], [role=menu], [role=grid], [role=tooltip]";
const direct = new Set(["boolean", "attachment", "lines", "member[]", "action[]"]);
const blank = (value: unknown) => value == null || value === "";
const sources = {
  formula: { icon: CalculatorIcon, text: "Calculated by a formula" },
  from: { icon: ArrowTurnDownRightIcon, text: "Filled in from other fields; you can change it" },
  action: { icon: LockClosedIcon, text: "Changed only by actions" },
};

const styles = stylex.create({
  surface: {
    minHeight: sizeVars["--size-element-md"],
    paddingInline: spacingVars["--spacing-2"],
    borderRadius: radiusVars["--radius-element"],
    minWidth: 0,
  },
  value: { alignSelf: "stretch", display: "flex", alignItems: "center", minWidth: 0 },
});

export function picks(parameter: Api.Field, field?: Api.FieldInfo) {
  return Boolean(parameter.choices || parameter.ref) || iconField(parameter.name, parameter.type) || ["choice", "reference", "member", "date"].includes(field?.type ?? parameter.type);
}

export function PropertyValue({
  field,
  value,
  name,
  dot,
  tone,
  recordType,
  recordId,
  recordRevision,
}: {
  field: Api.FieldInfo;
  value: unknown;
  name?: string;
  dot?: Dot;
  tone?: string;
  recordType?: string;
  recordId?: string;
  recordRevision?: number;
}) {
  return (
    <SemanticValue
      field={field}
      value={value}
      name={name}
      dot={dot}
      tone={tone}
      size="md"
      surface="detail"
      recordType={recordType}
      recordId={recordId}
      recordRevision={recordRevision}
    />
  );
}

export type CellEdit<Row> = {
  parameter: Api.Field;
  described?: Api.EntityInfo;
  value: (row: Row) => unknown;
  name?: (row: Row) => string | undefined;
  save(row: Row, value: unknown): Promise<unknown>;
};

export function CellProperty<Row>({ edit, row, close, onTab }: { edit: CellEdit<Row>; row: Row; close: () => void; onTab?: (shift: boolean) => void }) {
  const value = edit.value(row);
  const field = edit.described?.fields.find((candidate) => candidate.name === edit.parameter.name) ??
    { name: edit.parameter.name, title: edit.parameter.label ?? edit.parameter.name, type: "text" as const };
  return <PropertyField field={field} parameter={edit.parameter} described={edit.described} value={value} name={edit.name?.(row)} labelHidden open
    onCommit={(next) => (blank(next) && blank(value)) || next === value ? undefined : edit.save(row, next)} onClose={close} onTab={onTab} />;
}

type PropertyFieldProps = {
  field: Api.FieldInfo;
  parameter?: Api.Field;
  described?: Api.EntityInfo;
  value: unknown;
  form?: Record<string, unknown>;
  name?: string;
  dot?: Dot;
  tone?: string;
  hint?: string;
  status?: InputStatus;
  saving?: boolean;
  labelHidden?: boolean;
  open?: boolean;
  recordId?: string;
  recordRevision?: number;
  onChange?: (value: unknown) => void;
  onCommit?(value: unknown): Promise<unknown> | void;
  onClose?: () => void;
  onTab?: (shift: boolean) => void;
};

export function PropertyField(props: PropertyFieldProps) {
  const { field, described, value, form, name, status, saving, labelHidden, onChange } = props;
  const parameter = props.parameter && !field.readOnly ? props.parameter : undefined;
  const label = t(field.title);
  const id = useId();
  const [editing, setEditing] = useState(Boolean(props.open));
  const [draft, setDraft] = useState(value);
  const [failure, setFailure] = useState("");
  const initial = useRef(value);
  const latest = useRef(value);
  const busy = useRef(false);
  const box = useRef<HTMLDivElement>(null);
  const surface = useRef<HTMLDivElement>(null);
  const control = useRef<HTMLButtonElement>(null);
  const pressable = usePressFeedback();
  const begin = () => {
    initial.current = value;
    latest.current = value;
    setDraft(value);
    setFailure("");
    setEditing(true);
  };
  const clickable = useClickableContainer({ containerRef: surface, interactiveRef: control, onClick: begin, disabled: !parameter });
  const shownStatus = failure ? { type: "error" as const, message: failure } : status;
  const open = Boolean(parameter) && (editing || shownStatus?.type === "error");

  useEffect(() => {
    if (status?.type !== "error") setFailure("");
  }, [status]);

  useEffect(() => {
    if (!editing) return;
    const target = box.current?.querySelector<HTMLElement>("input, textarea, button");
    target?.focus();
    if (target instanceof HTMLButtonElement) target.click();
  }, [editing]);

  const finish = () => {
    setEditing(false);
    props.onClose?.();
  };
  const leave = async (next: unknown) => {
    if (busy.current) return;
    busy.current = true;
    try {
      await props.onCommit?.(next);
      setFailure("");
      finish();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      busy.current = false;
    }
  };
  const change = (next: unknown) => {
    latest.current = next;
    setDraft(next);
    setFailure("");
    onChange?.(next);
    if (parameter && picks(parameter, field)) void leave(next);
  };
  const blur = (event: FocusEvent<HTMLElement>) => {
    const inside = event.currentTarget;
    setTimeout(() => {
      const now = document.activeElement;
      if (now && now !== document.body && (inside.contains(now) || now.closest(layers))) return;
      void leave(latest.current);
    });
  };
  const keys = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Tab" && props.onTab) {
      event.preventDefault();
      event.stopPropagation();
      const shift = event.shiftKey;
      void leave(latest.current).then(() => props.onTab?.(shift));
      return;
    }
    if (event.key !== "Escape") return;
    event.stopPropagation();
    latest.current = initial.current;
    setFailure("");
    onChange?.(initial.current);
    finish();
  };

  if (parameter && direct.has(parameter.type)) {
    return <ParameterInput parameter={parameter} described={described} value={value} form={form} status={shownStatus} labelHidden={labelHidden}
      onChange={(next) => { onChange?.(next); void leave(next); }} />;
  }
  const savedCheck = !saving && shownStatus?.type === "success";
  const shown = open && parameter ? (
    <VStack ref={box} onBlur={blur} onKeyDown={keys} xstyle={shownStatus?.type === "error" ? motionStyles.shakeError : undefined}>
      <ParameterInput parameter={parameter} described={described} value={editing ? draft : value} form={form} referenceName={name} status={shownStatus}
        labelHidden onChange={change} onEnter={() => void leave(latest.current)} />
    </VStack>
  ) : (
    <HStack ref={surface} id={id} gap={2} vAlign="center" xstyle={parameter ? [styles.surface, motionStyles.overlayBadge, interactionOverlayStyles.backgroundColor, focusOutlineStyles.focusWithin] : [styles.surface, motionStyles.overlayBadge]}
      {...(parameter ? { ...pressable, onClick: clickable.onClick, onMouseUp: clickable.onMouseUp, onPointerDown: clickable.onPointerDown } : {})}>
      <StackItem size="fill" role="group" aria-label={label} xstyle={styles.value}>
        {props.hint && blank(value) ? (
          <Text type="body" color="secondary">{props.hint}</Text>
        ) : (
          <PropertyValue
            field={field}
            value={value}
            name={name}
            dot={props.dot}
            tone={props.tone}
            recordType={described?.type}
            recordId={props.recordId ?? (typeof form?.id === "string" ? form.id : undefined)}
            recordRevision={props.recordRevision}
          />
        )}
      </StackItem>
      {saving && <Spinner size="sm" />}
      {savedCheck && (
        <HStack xstyle={[motionStyles.overlayCheck, motionStyles.popEnter]}>
          <Icon icon={CheckIcon} color="success" size="sm" label={t("Saved")} />
        </HStack>
      )}
      {parameter && <VisuallyHidden><Button ref={control} label={`${t("Edit")} ${label}`} onClick={begin} /></VisuallyHidden>}
    </HStack>
  );
  const source = field.provenance && sources[field.provenance];
  const sourceField = field.fromField ? described?.fields.find((candidate) => candidate.name === field.fromField) : undefined;
  const fromNote = sourceField ? t("From {field}").replace("{field}", t(sourceField.title)) : undefined;
  const about = [field.help && t(field.help), fromNote, source && t(source.text)].filter(Boolean).join(" ");
  const content = fromNote ? <VStack gap={0}>{shown}<Text type="supporting" color="secondary">{fromNote}</Text></VStack> : shown;
  return labelHidden ? shown : <Field label={label} inputID={id} labelIcon={source?.icon} labelTooltip={about || undefined}>{content}</Field>;
}
