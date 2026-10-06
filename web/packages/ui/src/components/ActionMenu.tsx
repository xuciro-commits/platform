// A button that opens a list of commands (ADR-0052 §4: one "Open in…" and
// one overflow menu across the platform, instead of a row of ad-hoc buttons).
import { DropdownMenu } from 'radix-ui';
import type { ReactNode } from 'react';
import { Button } from '../primitives/button';
import type { ContextCommand } from './CommandMenu';

const panel = 'z-[100] max-h-80 min-w-44 overflow-auto rounded-md border border-border bg-surface p-1 text-sm shadow-lg';
const item = 'flex cursor-default items-center rounded px-2 py-1 outline-none data-[highlighted]:bg-row-selected data-[disabled]:pointer-events-none data-[disabled]:opacity-40';

export function ActionMenu({ label, commands, icon, size = 'sm', variant = 'ghost', children }: {
  label: string; commands: ContextCommand[]; icon?: ReactNode; size?: 'sm' | 'md'; variant?: 'ghost' | 'primary' | 'default'; children?: ReactNode;
}) {
  if (commands.length === 0) return null;
  return <DropdownMenu.Root>
    <DropdownMenu.Trigger asChild><Button size={size} variant={variant} aria-label={children ? undefined : label} title={label}>{icon}{children ?? label}</Button></DropdownMenu.Trigger>
    <DropdownMenu.Portal><DropdownMenu.Content aria-label={label} sideOffset={4} align="end" className={panel} onKeyDown={(event) => event.stopPropagation()}>
      {commands.map((command) => <DropdownMenu.Item key={command.id} disabled={command.disabled} onSelect={command.run}
        className={`${item} ${command.danger ? 'text-[var(--tone-danger)]' : ''} ${command.separatorBefore ? 'mt-1 border-t border-border pt-2' : ''}`}>
        {command.icon && <span className="mr-2 [&_svg]:size-3.5">{command.icon}</span>}<span className="flex-1">{command.label}</span>
        {command.shortcut && <span className="ml-4 text-[10px] text-muted">{command.shortcut}</span>}
      </DropdownMenu.Item>)}
    </DropdownMenu.Content></DropdownMenu.Portal>
  </DropdownMenu.Root>;
}
