import { VStack } from "@astryxdesign/core/Layout";
import { type FocusEvent, type KeyboardEvent, useEffect, useRef, useState } from "react";
import type { Api } from "../host";
import { reasonOf } from "./act";
import { type InputStatus, ParameterInput, settles } from "./ParameterInput";

export type CellEdit<Row> = {
  parameter: Api.Field;
  described?: Api.EntityInfo;
  value: (row: Row) => unknown;
  name?: (row: Row) => string | undefined;
  save(row: Row, value: unknown): Promise<unknown>;
};

const blank = (value: unknown) => value == null || value === "";
const same = (left: unknown, right: unknown) => left === right || (blank(left) && blank(right));
const layers = "[role=listbox], [role=dialog], [role=menu], [role=grid], [role=tooltip]";

export function CellEditor<Row>({ edit, row, close }: { edit: CellEdit<Row>; row: Row; close: () => void }) {
  const initial = edit.value(row);
  const [draft, setDraft] = useState(initial);
  const latest = useRef(initial);
  const busy = useRef(false);
  const [status, setStatus] = useState<InputStatus>();
  const cell = useRef<HTMLElement>(null);
  useEffect(() => {
    const control = cell.current?.querySelector<HTMLElement>("input, textarea, button");
    control?.focus();
    if (control instanceof HTMLButtonElement) control.click();
  }, []);
  const commit = async (value: unknown) => {
    if (busy.current) return;
    if (same(value, initial)) return close();
    busy.current = true;
    try {
      await edit.save(row, value);
      close();
    } catch (problem) {
      setStatus({ type: "error", message: reasonOf(problem) });
    }
    busy.current = false;
  };
  const change = (value: unknown) => {
    latest.current = value;
    setDraft(value);
    if (settles(edit.parameter) || edit.parameter.type === "date") void commit(value);
  };
  const leave = (event: FocusEvent<HTMLElement>) => {
    const cell = event.currentTarget;
    setTimeout(() => {
      const now = document.activeElement;
      if (now && now !== document.body && (cell.contains(now) || now.closest(layers))) return;
      void commit(latest.current);
    });
  };
  const keys = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key === "Escape") {
      event.stopPropagation();
      close();
    }
  };
  return (
    <VStack ref={cell} onBlur={leave} onKeyDown={keys}>
      <ParameterInput parameter={edit.parameter} described={edit.described} value={draft} referenceName={edit.name?.(row)} status={status}
        onChange={change} onEnter={() => void commit(latest.current)} focused labelHidden />
    </VStack>
  );
}
