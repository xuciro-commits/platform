import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import type { ReactNode } from "react";

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
    <SegmentedControl
      label={label}
      value={active?.value ?? value}
      onChange={(next) => onChange?.(next)}
      size={compact ? "sm" : "md"}
      layout="hug"
      isDisabled={disabled || !onChange}
    >
      {options.map((option) => (
        <SegmentedControlItem
          key={option.value}
          value={option.value}
          label={option.label}
          icon={option.icon}
          isDisabled={option.disabled}
        />
      ))}
    </SegmentedControl>
  );
}
