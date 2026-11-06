import { ArrowLeft } from "lucide-react";
import { createPortal } from "react-dom";
import { useContext, useLayoutEffect, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { ContentTabs } from "../layout/ContentTabs";
import {PanelContext, WorkspaceContext} from "./WorkspaceContext";

export type ResourceSection = { id: string; label: ReactNode; icon?: ReactNode; badge?: ReactNode; group?: string };

export function ResourceNavigation({ back, title, icon, meta, sections, value, onChange }: {
  back?: { label: string; onClick: () => void }; title: string; icon?: ReactNode; meta?: ReactNode;
  sections: ResourceSection[]; value: string; onChange: (id: string) => void;
}) {
  const api = useContext(WorkspaceContext);
  const panel = useContext(PanelContext);
  const main = !!panel?.startsWith("view:");
  const claim = api?.claimSidebar;
  useLayoutEffect(() => main && panel && claim ? claim(panel) : undefined, [main, panel, claim]);
  const target = api?.sidebarSlot;
  if (!main || api?.activePanel !== panel || !target) return null;
  const groups: { label?: string; items: ResourceSection[] }[] = [];
  for (const section of sections) {
    const last = groups.at(-1);
    if (last && last.label === section.group) last.items.push(section); else groups.push({ label: section.group, items: [section] });
  }
  return createPortal(<div className="grid gap-1">
    {back && <button type="button" onClick={back.onClick} className="flex min-w-0 items-center gap-1.5 rounded-md px-2 py-1 text-left text-xs text-muted hover:bg-row-hover hover:text-foreground [&_svg]:size-3.5 [&_svg]:shrink-0"><ArrowLeft /><span className="truncate">{back.label}</span></button>}
    <div className="flex min-w-0 items-center gap-2 px-2 py-1.5">
      {icon && <span className="grid size-6 shrink-0 place-items-center rounded-md bg-row-selected text-primary [&_svg]:size-3.5">{icon}</span>}
      <span className="grid min-w-0"><span className="truncate text-sm font-semibold">{title}</span>{meta && <span className="truncate font-mono text-2xs text-muted">{meta}</span>}</span>
    </div>
    <nav aria-label={title} className="grid">
      {groups.map((group, index) => <div key={`${group.label ?? ""}:${index}`} className="mb-1">
        {group.label && <div className="px-2 pb-1 pt-2 text-2xs font-semibold uppercase tracking-wider text-muted">{group.label}</div>}
        {group.items.map(section => <button key={section.id} type="button" onClick={() => onChange(section.id)} aria-current={section.id === value ? "page" : undefined}
          className={cn("flex min-h-7 w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm text-foreground hover:bg-row-hover [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted", section.id === value && "bg-row-selected font-medium text-primary [&_svg]:text-primary")}>
          {section.icon}<span className="min-w-0 flex-1 truncate">{section.label}</span>{section.badge != null && <span className="shrink-0 text-xs text-muted">{section.badge}</span>}
        </button>)}
      </div>)}
    </nav>
  </div>, target);
}

export function ResourceSections({ items, value, onChange, inline = false, ...navigation }: Omit<Parameters<typeof ResourceNavigation>[0], "sections"> & {
  inline?: boolean;
  items: (Omit<ResourceSection, "label"> & { title: ReactNode; content: ReactNode })[];
}) {
  const active = items.find(item => item.id === value) ?? items[0];
  if (inline) return <ContentTabs label={navigation.title} value={active?.id} onChange={onChange}
    items={items.map(({ id, title, content }) => ({ id, title, content }))} />;
  return <>
    <ResourceNavigation {...navigation} sections={items.map(({ content: _content, title, ...item }) => ({ ...item, label: title }))} value={active?.id ?? ""} onChange={onChange} />
    {active?.content}
  </>;
}
