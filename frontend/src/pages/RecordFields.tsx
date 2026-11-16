import { FormLayout } from "@astryxdesign/core/FormLayout";
import { VStack } from "@astryxdesign/core/Layout";
import { useEffect, useRef, useState } from "react";
import { type Api, submit } from "../host";
import { t } from "../i18n";
import { type InputStatus, ParameterInput, enterable, settles } from "./ParameterInput";

type Values = Record<string, unknown>;
type RecordFieldsProps = { described: Api.EntityInfo; edit: Api.Action; id: string; values: Values; revision: number };

const blank = (value: unknown) => value == null || value === "";
const same = (left: unknown, right: unknown) => left === right || (blank(left) && blank(right));

export function RecordFields({ described, edit, id, values, revision }: RecordFieldsProps) {
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
    if (same(value, values[name]) || (name in sent.current && sent.current[name] === value)) return;
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

  const text = new Set(edit.payload.filter((parameter) => !settles(parameter)).map((parameter) => parameter.name));
  const settle = () => {
    for (const [name, value] of Object.entries(pending.current)) if (text.has(name)) save(name, value);
  };

  return (
    <VStack onBlur={settle}>
      <FormLayout direction="horizontal-labels" defaultOptionality="optional">
        {edit.payload.filter(enterable).map((parameter) => (
          <ParameterInput
            key={parameter.name}
            parameter={parameter}
            described={described}
            value={parameter.name in drafts ? drafts[parameter.name] : values[parameter.name]}
            status={status[parameter.name]}
            onChange={(value) => {
              setDrafts((previous) => ({ ...previous, [parameter.name]: value }));
              setStatus(({ [parameter.name]: _, ...rest }) => rest);
              if (settles(parameter)) save(parameter.name, value);
            }}
            onEnter={settle}
          />
        ))}
      </FormLayout>
    </VStack>
  );
}
