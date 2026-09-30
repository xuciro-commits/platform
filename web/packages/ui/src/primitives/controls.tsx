import { useRef, useState, type FormHTMLAttributes, type ReactNode } from "react";
import { ChevronDown, ChevronRight } from "lucide-react";
import { cn } from "../lib/cn";
import { Button, type ButtonProps } from "./button";

/** A row of values a person switches on and off: the fields a widget shows, the
 *  actions it offers, the tags of a record. One owner for the pattern (rule 11). */
export function Toggles({ options, value = [], onChange, empty, className }: {
  options: { value: string; label: string }[];
  value?: string[];
  onChange: (value: string[]) => void;
  /** What to say when there is nothing to choose from. */
  empty?: string;
  className?: string;
}) {
  if (options.length === 0 && empty) return <span className={cn("text-xs text-muted", className)}>{empty}</span>;
  return (
    <span className={cn("flex flex-wrap gap-1", className)}>
      {options.map((option) => {
        const on = value.includes(option.value);
        return (
          <button key={option.value} type="button" aria-pressed={on}
            onClick={() => onChange(on ? value.filter((x) => x !== option.value) : [...value, option.value])}
            className={cn("rounded-sm border px-1.5 py-0.5 text-xs", on ? "border-primary bg-row-selected" : "border-border text-muted hover:bg-row-hover")}>
            {option.label}
          </button>
        );
      })}
    </span>
  );
}

/** A checkbox with its label; the label is the click target. */
export function Checkbox({ checked, onChange, children, className, disabled }:
  { checked: boolean; onChange: (checked: boolean) => void; children?: ReactNode; className?: string; disabled?: boolean }) {
  return (
    <label className={cn("flex items-center gap-2", disabled && "opacity-50", className)}>
      <input type="checkbox" className="accent-[var(--primary)]" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      {children}
    </label>
  );
}

/** A form that submits on Enter and on its submit button, without reloading the page. */
export function Form({ onSubmit, ...props }: Omit<FormHTMLAttributes<HTMLFormElement>, "onSubmit"> & { onSubmit: () => void }) {
  return <form {...props} onSubmit={(e) => { e.preventDefault(); onSubmit(); }} />;
}

/** A row that opens to show more: a run's step, a citation's passage. */
export function Disclosure({ summary, children, open, defaultOpen = false, onToggle, className }:
  { summary: ReactNode; children: ReactNode; open?: boolean; defaultOpen?: boolean; onToggle?: (open: boolean) => void; className?: string }) {
  const [own, setOwn] = useState(defaultOpen);
  const shown = open ?? own;
  return (
    <div className={className}>
      <button type="button" aria-expanded={shown} className="flex w-full items-baseline gap-2 rounded-md px-1 py-0.5 text-left hover:bg-row-hover"
        onClick={() => { onToggle ? onToggle(!shown) : setOwn(!shown); }}>
        {shown ? <ChevronDown className="size-3 shrink-0 self-center" /> : <ChevronRight className="size-3 shrink-0 self-center" />}
        {summary}
      </button>
      {shown && <div className="pl-5">{children}</div>}
    </div>
  );
}

/** A button that picks a file. */
export function FilePicker({ accept, onFile, children, ...button }:
  Omit<ButtonProps, "onClick"> & { accept?: string; onFile: (file: File) => void }) {
  const input = useRef<HTMLInputElement>(null);
  return (
    <>
      <input ref={input} type="file" accept={accept} className="hidden" onChange={(e) => {
        const file = e.target.files?.[0];
        e.target.value = "";
        if (file) onFile(file);
      }} />
      <Button {...button} onClick={() => input.current?.click()}>{children}</Button>
    </>
  );
}

/** A tree whose rows are selected: organisation units, anything with parents. */
export function Tree<T>({ roots, children, row, selected, onSelect, id }: {
  roots: T[]; children: (node: T) => T[]; row: (node: T) => ReactNode; id: (node: T) => string;
  selected?: string; onSelect: (node: T) => void;
}) {
  const Node = ({ node, depth }: { node: T; depth: number }) => (
    <>
      <button type="button" onClick={() => onSelect(node)} style={{ paddingLeft: 8 + depth * 18 }} aria-selected={selected === id(node)}
        className={cn("flex w-full items-center gap-2 rounded-sm py-1 pr-2 text-left text-sm hover:bg-row-hover", selected === id(node) && "bg-row-selected")}>
        {row(node)}
      </button>
      {children(node).map((c) => <Node key={id(c)} node={c} depth={depth + 1} />)}
    </>
  );
  return <div role="tree">{roots.map((r) => <Node key={id(r)} node={r} depth={0} />)}</div>;
}
