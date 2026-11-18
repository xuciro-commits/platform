import { NumberInput } from "@astryxdesign/core/NumberInput";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
import { TextInput } from "@astryxdesign/core/TextInput";
import type { Api } from "../host";
import { t } from "../i18n";

export type InputStatus = { type: "error" | "success"; message?: string };
type ParameterInputProps = {
  parameter: Api.Field;
  described: Api.EntityInfo;
  value: unknown;
  status?: InputStatus;
  onChange: (value: unknown) => void;
  onEnter?: () => void;
};

const typed = new Set(["string", "longtext", "date", "datetime", "entity", "choice"]);
const numeric = new Set(["integer", "number"]);

export function enterable(parameter: Api.Field) {
  return Boolean(parameter.choices) || parameter.type === "boolean" || typed.has(parameter.type) || numeric.has(parameter.type);
}

export function settles(parameter: Api.Field) {
  return Boolean(parameter.choices) || parameter.type === "boolean" || numeric.has(parameter.type);
}

export function ParameterInput({ parameter, described, value, status, onChange, onEnter }: ParameterInputProps) {
  const field = described.fields.find((candidate) => candidate.name === parameter.name);
  const label = t(field?.title ?? parameter.name);
  const description = field?.help;
  if (parameter.choices) {
    return (
      <Selector
        label={label}
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
    return <Switch label={label} description={description} value={value === true} onChange={onChange} />;
  }
  if (numeric.has(parameter.type)) {
    return (
      <NumberInput
        label={label}
        description={description}
        isRequired={parameter.required}
        status={status}
        value={typeof value === "number" ? value : null}
        onChange={onChange}
      />
    );
  }
  return (
    <TextInput
      label={label}
      description={description}
      isRequired={parameter.required}
      status={status}
      value={String(value ?? "")}
      onChange={onChange}
      onEnter={onEnter}
    />
  );
}
