import { NumberInput } from "@astryxdesign/core/NumberInput";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
import { TextInput } from "@astryxdesign/core/TextInput";
import type { Api } from "../host";
import { t } from "../i18n";
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
};

const typed = new Set(["string", "longtext", "date", "datetime", "entity", "reference", "choice"]);
const numeric = new Set(["integer", "number"]);

export function enterable(parameter: Api.Field) {
  return Boolean(parameter.choices) || parameter.type === "boolean" || typed.has(parameter.type) || numeric.has(parameter.type);
}

export function settles(parameter: Api.Field, field?: Api.FieldInfo) {
  return Boolean(parameter.ref || parameter.choices) || field?.type === "reference" || parameter.type === "reference" || parameter.type === "boolean" || numeric.has(parameter.type);
}

export function ParameterInput({ parameter, described, value, status, onChange, onEnter, focused, referenceName, labelHidden }: ParameterInputProps) {
  const field = described.fields.find((candidate) => candidate.name === parameter.name);
  const label = t(field?.title ?? parameter.name);
  const description = field?.help;
  const reference = parameter.ref ?? (field?.type === "reference" ? field.ref : undefined);
  if (reference) {
    return <ReferenceInput type={reference} id={String(value ?? "")} name={referenceName} label={label} description={description}
      required={parameter.required} focused={focused} status={status} onChange={onChange} />;
  }
  if (parameter.choices) {
    return (
      <Selector
        label={label}
        isLabelHidden={labelHidden}
        description={description}
        isRequired={parameter.required}
        status={status}
        value={String(value ?? "")}
        onChange={onChange}
        options={parameter.choices.map((choice) => ({ value: choice, label: field?.choiceTitles?.[field.choices?.indexOf(choice) ?? -1] ?? choice }))}
      />
    );
  }
  if (parameter.type === "boolean") {
    return <Switch label={label} isLabelHidden={labelHidden} description={description} value={value === true} onChange={onChange} />;
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
      onChange={onChange}
      onEnter={onEnter}
      hasAutoFocus={focused}
    />
  );
}
