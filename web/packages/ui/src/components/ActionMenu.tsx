import { DropdownMenu, type DropdownMenuOption } from "@astryxdesign/core/DropdownMenu";
import { useContext, type ReactNode } from "react";
import { DefaultButtonSize } from "../primitives/button";
import { cn } from "../lib/cn";
import type { ContextCommand } from "./CommandMenu";

export function ActionMenu({ label, commands, icon, size, variant = "ghost", className, children }: {
  label: string; commands: ContextCommand[]; icon?: ReactNode; size?: "sm" | "md" | "lg"; variant?: "ghost" | "primary" | "default"; className?: string; children?: ReactNode;
}) {
  const inherited = useContext(DefaultButtonSize);
  if (commands.length === 0) return null;
  const iconOnly = !!icon && !children;
  const resolvedSize = size ?? (inherited === "lg" ? "lg" : "sm");
  const items: DropdownMenuOption[] = [];
  for (const command of commands) {
    if (command.separatorBefore) items.push({ type: "divider" });
    items.push({
      id: command.id,
      label: command.label,
      icon: command.icon,
      description: command.reason,
      isDisabled: command.disabled,
      variant: command.danger ? "destructive" : "default",
      onClick: () => command.run(),
    });
  }
  return <DropdownMenu
    button={{
      label,
      variant: variant === "default" ? "secondary" : variant,
      size: resolvedSize,
      icon: icon ?? undefined,
      isIconOnly: iconOnly,
      children: iconOnly ? undefined : (children ?? label),
      className: cn(iconOnly && "aspect-square px-0", className),
      tooltip: children ? undefined : label,
    }}
    items={items}
    alignment="end"
    menuMaxHeight={320}
    className={cn(iconOnly && "aspect-square", className)}
  />;
}
