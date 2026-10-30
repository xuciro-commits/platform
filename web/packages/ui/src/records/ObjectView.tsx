import { MoreHorizontal } from "lucide-react";
import type { ReactNode } from "react";
import type { Api } from "@platform/kernel";
import { ActionMenu } from "../components/ActionMenu";
import type { ContextCommand } from "../components/CommandMenu";
import { IconGlyph } from "../components/IconPicker";
import { Tag, toneNames, type Tone } from "../components/StatusTag";
import { t } from "../i18n";
import { cn } from "../lib/cn";
import { Button } from "../primitives/button";

export type HeaderPill = { key: string; label: string; tone: Tone; icon?: string; title?: string };

export const toneOf = (value?: string): Tone => (toneNames as readonly string[]).includes(value ?? "") ? value as Tone : "neutral";

export function ObjectIcon({ icon, color, className }: { icon?: string; color?: string; className?: string }) {
  const tone = toneOf(color);
  return <span data-ui="object-icon" data-tone={tone} aria-hidden className={cn("grid size-8 shrink-0 place-items-center rounded-md border", className)}
    style={{ color: `var(--tone-${tone})`, borderColor: `color-mix(in oklch, var(--tone-${tone}) 35%, transparent)`, background: `color-mix(in oklch, var(--tone-${tone}) 10%, transparent)` }}>
    <IconGlyph name={icon} />
  </span>;
}

export function FormatPill({ format, text, title }: { format: Api.FieldFormat; text: string; title?: string }) {
  const label = format.label || text;
  return <span title={title} className="inline-flex"><Tag label={label || "—"} tone={toneOf(format.tone)} icon={format.icon} /></span>;
}

export function HeaderCommands({ commands, menu, disabled, children }: { commands: ContextCommand[]; menu: ContextCommand[]; disabled: boolean; children?: ReactNode }) {
  const inline = commands.length > 3 ? commands.slice(0, 1) : commands;
  const overflow = [...(commands.length > 3 ? commands.slice(1) : []), ...menu].map(command => ({ ...command, disabled: disabled || command.disabled }));
  return <>
    {inline.map((command, index) => <Button key={command.id} size="sm" variant={index === 0 ? "primary" : "default"} disabled={disabled || command.disabled} onClick={command.run}>{command.label}</Button>)}
    {children}
    {overflow.length > 0 && <ActionMenu label={t("More actions")} commands={overflow} icon={<MoreHorizontal />}>{t("More")}</ActionMenu>}
  </>;
}

export function ObjectHeader({ icon, color, title, subtitle, pills, tags, status, actions, sticky }: {
  icon?: string; color?: string; title: string; subtitle: ReactNode; pills: HeaderPill[]; tags?: ReactNode; status?: ReactNode; actions?: ReactNode; sticky?: boolean;
}) {
  return <header className={cn("flex min-w-0 flex-wrap items-start gap-x-3 gap-y-2 rounded-md border border-border bg-surface px-3 py-2", sticky && "sticky top-0 z-10")}>
    <ObjectIcon icon={icon} color={color} />
    <div className="grid min-w-0 flex-1 gap-1">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <h1 className="min-w-0 break-words text-lg font-semibold">{title}</h1>
        {pills.map(pill => <span key={pill.key} title={pill.title} className="inline-flex"><Tag label={pill.label} tone={pill.tone} icon={pill.icon} /></span>)}
        {tags}
        {status}
      </div>
      <p className="min-w-0 break-words font-mono text-xs text-muted">{subtitle}</p>
    </div>
    {actions && <div className="ml-auto flex min-w-0 flex-wrap items-center gap-1">{actions}</div>}
  </header>;
}
