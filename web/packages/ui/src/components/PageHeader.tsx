import { ChevronRight } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import {ContextHelp} from './ContextHelp';
import {DefaultButtonSize} from '../primitives/button';

export type PageCrumb = { label: string; onClick?: () => void };

export function PageHeader({ title, description, help, actions, level, compact=false, crumbs, icon, status, meta }: {
  title: string; description?: string; help?:ReactNode; actions?: ReactNode;level?:1|2|3;compact?:boolean;
  crumbs?: PageCrumb[]; icon?: ReactNode; status?: ReactNode; meta?: ReactNode;
}) {
  const Heading=level===2?"h2":level===3?"h3":"h1";
  return (
    <header className={(compact?"":"mb-3 ")+"grid min-w-0 gap-1"}>
      {crumbs && crumbs.length > 0 && <nav aria-label="Breadcrumb" className="flex min-w-0 flex-wrap items-center gap-1 text-xs text-muted">
        {crumbs.map((crumb, index) => <Fragment key={`${crumb.label}:${index}`}>
          {crumb.onClick ? <button type="button" onClick={crumb.onClick} className="rounded-sm hover:text-foreground hover:underline">{crumb.label}</button> : <span>{crumb.label}</span>}
          <ChevronRight aria-hidden className="size-3" />
        </Fragment>)}
      </nav>}
      <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-3">
          {icon && <span className="shrink-0 [&>svg]:size-5 [&>svg]:text-primary">{icon}</span>}
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-x-2 gap-y-1"><Heading className={"min-w-0 break-words font-semibold tracking-tight "+(level===1?"text-xl":level===2?"text-base":level===3?"text-sm":"text-lg")}>{title}</Heading>{status}{help&&<ContextHelp title={title}>{help}</ContextHelp>}</div>
            {meta && <div className="mt-0.5 truncate text-xs text-muted">{meta}</div>}
          </div>
        </div>
        {(!compact||actions)&&<div className="flex flex-wrap items-center gap-2"><DefaultButtonSize.Provider value="lg">{actions}</DefaultButtonSize.Provider></div>}
      </div>
      {description && <p className="max-w-prose text-sm text-muted">{description}</p>}
    </header>
  );
}
