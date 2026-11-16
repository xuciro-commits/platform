import {InputDraftProvider,InputProblems,useInputDrafts} from "../fields/draft";
import {t} from "../i18n";
import { useRef, useState, type FormHTMLAttributes, type ReactNode } from "react";
import { CheckboxInput } from "@astryxdesign/core/CheckboxInput";
import { Collapsible } from "@astryxdesign/core/Collapsible";
import { Switch as AstryxSwitch } from "@astryxdesign/core/Switch";
import { ToggleButton } from "@astryxdesign/core/ToggleButton";
import { TreeList, type TreeListItemData } from "@astryxdesign/core/TreeList";
import { HStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { cn } from "../lib/cn";
import { Button, type ButtonProps } from "./button";

export function Toggles({ options, value = [], onChange, empty, className, disabled, isDisabled }: {
  options: { value: string; label: string }[];
  value?: string[];
  onChange: (value: string[]) => void;
  empty?: string;
  className?: string;
  disabled?:boolean;
  isDisabled?:(value:string)=>boolean;
}) {
  if (options.length === 0 && empty) return <Text type="supporting" className={className}>{empty}</Text>;
  return (
    <HStack gap={1} wrap="wrap" className={className}>
      {options.map((option) => {
        const on = value.includes(option.value);
        return <ToggleButton key={option.value} label={option.label} size="sm" isPressed={on}
          isDisabled={disabled || isDisabled?.(option.value)}
          onPressedChange={(pressed) => onChange(pressed ? [...value, option.value] : value.filter((x) => x !== option.value))} />;
      })}
    </HStack>
  );
}

function nodeText(node: ReactNode): string {
  if (node === null || node === undefined || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(nodeText).join("");
  if (typeof node === "object" && "props" in node) return nodeText((node as { props?: { children?: ReactNode } }).props?.children);
  return "";
}
function isScreenReaderOnly(node: ReactNode): boolean {
  if (Array.isArray(node)) return node.every(isScreenReaderOnly);
  if (node && typeof node === "object" && "props" in node) {
    const props = (node as { props?: { className?: unknown; children?: ReactNode } }).props;
    return typeof props?.className === "string" && props.className.split(/\s+/).includes("sr-only");
  }
  return false;
}

export function Checkbox({ id, checked, onChange, children, className, disabled, ariaLabel }:
  { id?: string; checked: boolean; onChange: (checked: boolean) => void; children?: ReactNode; className?: string; disabled?: boolean; ariaLabel?:string }) {
  const text = nodeText(children).trim();
  const hideLabel = !text || isScreenReaderOnly(children);
  return <CheckboxInput
    id={id}
    label={text || ariaLabel || "Checkbox"}
    isLabelHidden={hideLabel}
    value={checked}
    onChange={onChange}
    isDisabled={disabled}
    aria-label={ariaLabel}
    className={cn("text-xs", className)}
  />;
}

export function Switch({checked,onChange,label,ariaLabel,disabled,className}:{
  checked:boolean;onChange:(checked:boolean)=>void;label:string;ariaLabel?:string;disabled?:boolean;className?:string;
}) {
  return <AstryxSwitch label={label || ariaLabel || "Switch"} isLabelHidden={!label} value={checked} onChange={onChange} isDisabled={disabled} aria-label={ariaLabel} size="sm" className={cn("text-xs",className)} />;
}

export function Form({ onSubmit, ...props }: Omit<FormHTMLAttributes<HTMLFormElement>, "onSubmit"> & { onSubmit: () => void }) {
  return <InputDraftProvider isolated scope="form"><FormBody {...props} onSubmit={onSubmit}/></InputDraftProvider>;
}
function FormBody({onSubmit,children,...props}:Omit<FormHTMLAttributes<HTMLFormElement>,"onSubmit">&{onSubmit:()=>void}){
 const inputs=useInputDrafts(),lock=useRef(false),[busy,setBusy]=useState(false),[failure,setFailure]=useState("");
 return <form {...props} onSubmit={async event=>{event.preventDefault();if(lock.current||inputs?.invalid)return;lock.current=true;setBusy(true);setFailure("");inputs?.reject();inputs?.pending(1);try{await onSubmit();}catch(error){setFailure(error instanceof Error?error.message:t("The request was not confirmed. Your input is retained."));}finally{lock.current=false;setBusy(false);inputs?.pending(-1);}}}><fieldset disabled={busy} className="contents">{children}</fieldset><InputProblems/>{failure&&<p role="alert" className="text-xs text-danger">{failure}</p>}</form>;
}

export function Disclosure({ summary, children, open, defaultOpen = false, onToggle, className }:
  { summary: ReactNode; children: ReactNode; open?: boolean; defaultOpen?: boolean; onToggle?: (open: boolean) => void; className?: string }) {
  return <Collapsible className={className} trigger={summary} defaultIsOpen={defaultOpen} isOpen={open} onOpenChange={onToggle} chevronPosition="start">
    <div className="pl-5">{children}</div>
  </Collapsible>;
}

export function FilePicker({ accept, onFile, children, ...button }:
  Omit<ButtonProps, "onClick"> & { accept?: string; onFile: (file: File) => void }) {
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      <input ref={input} type="file" accept={accept} disabled={button.disabled} className="hidden" onChange={(e) => {
        const file = e.target.files?.[0];
        e.target.value = "";
        if (file) onFile(file);
      }} />
      <Button {...button} onClick={() => input.current?.click()}>{children}</Button>
    </>
  );
}

export function Tree<T>({ roots, children, row, selected, onSelect, id }: {
  roots: T[]; children: (node: T) => T[]; row: (node: T) => ReactNode; id: (node: T) => string;
  selected?: string; onSelect: (node: T) => void;
}) {
  const item = (node: T): TreeListItemData => ({
    id: id(node), label: row(node), isSelected: selected === id(node), isExpanded: true,
    onClick: () => onSelect(node), children: children(node).map(item),
  });
  return <TreeList items={roots.map(item)} density="compact" variant="lineGuides" />;
}
