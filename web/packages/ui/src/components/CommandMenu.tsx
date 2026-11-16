import { ContextMenu, type ContextMenuOption } from "@astryxdesign/core/ContextMenu";
import { DropdownMenu, type DropdownMenuOption } from "@astryxdesign/core/DropdownMenu";
import { useId, type ReactNode } from "react";
import { MoreHorizontal } from "lucide-react";
import { Button } from "../primitives/button";

export type ContextCommand = { id: string; label: string; disabled?: boolean; reason?: string; icon?: ReactNode; shortcut?: string; danger?: boolean; separatorBefore?: boolean; run: () => void };

function menuItems(commands: ContextCommand[]): DropdownMenuOption[] {
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
      endContent: command.shortcut ? <span className="text-2xs text-muted">{command.shortcut}</span> : undefined,
      onClick: () => command.run(),
    });
  }
  return items;
}

export function CommandMenu({ children, label, commands, focusKey, compact = false }: { children: ReactNode; label: string; commands: ContextCommand[]; focusKey?: string; compact?: boolean }) {
  const trigger = useId();
  const items = menuItems(commands);
  return <ContextMenu label={label} items={items as ContextMenuOption[]} menuWidth={176}>
    <div className="flex min-w-0 items-center">
      <div className="min-w-0 flex-1">{children}</div>
      <DropdownMenu
        items={items}
        alignment="end"
        menuMaxHeight={320}
        renderTrigger={(props) => <Button {...props} id={trigger} variant="ghost" size="sm" className={compact ? "h-5 w-5 rounded-sm p-0 text-inherit" : undefined} aria-label={label} title={label} data-command-focus={focusKey}><MoreHorizontal /></Button>}
      />
    </div>
  </ContextMenu>;
}
