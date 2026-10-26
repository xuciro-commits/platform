import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/cn";
import {ContextHelp} from '../components/ContextHelp';
import {t} from '../i18n';

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-ui="card" className={cn("rounded-md border border-border bg-surface", className)} {...props} />;
}

/** A card with a heading: the one way a page groups a part of itself under a title. */
export function Panel({ title, description, help, actions, className, children, variant="card", ...props }:
  Omit<HTMLAttributes<HTMLElement>, "title"> & { title?: ReactNode; description?: ReactNode; help?:ReactNode; actions?: ReactNode; variant?: "card" | "plain" }) {
  return (
    <section data-ui={variant==="plain"?"section":"card"} className={cn(variant==="card"&&"rounded-md border border-border bg-surface p-3 text-sm", className)} {...props}>
      {(title || actions || help) && (
        <div className="mb-2 flex flex-wrap items-center gap-2">
          {title && <h2 className="text-sm font-semibold">{title}</h2>}
          {help&&<ContextHelp title={typeof title==='string'?title:props['aria-label']??t('Details')}>{help}</ContextHelp>}
          {actions && <span className="ml-auto flex flex-wrap items-center gap-2">{actions}</span>}
        </div>
      )}
      {description && <p className="mb-2 max-w-prose text-xs text-muted">{description}</p>}
      {children}
    </section>
  );
}
