import { ButtonGroup as AstryxButtonGroup } from "@astryxdesign/core/ButtonGroup";
import type { ReactNode } from "react";
import { Button } from "../primitives/button";

export function SegmentedChoice({
  label,
  value,
  options,
  compact = false,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  options: readonly { value: string; label: string; icon?: ReactNode; disabled?: boolean }[];
  compact?: boolean;
  disabled?: boolean;
  onChange?: (value: string) => void;
}) {
  const active = options.find((option) => option.value === value && !option.disabled) ?? options.find((option) => !option.disabled);
  return (
    <AstryxButtonGroup label={label} size={compact ? "sm" : "md"}>
      {options.map((option) => (
        <Button
          key={option.value}
          size={compact ? "sm" : "md"}
          variant={active?.value === option.value ? "primary" : "default"}
          aria-pressed={active?.value === option.value}
          disabled={disabled || !onChange || option.disabled}
          onClick={() => onChange?.(option.value)}
        >
          {option.icon ?? undefined}{option.label}
        </Button>
      ))}
    </AstryxButtonGroup>
  );
}