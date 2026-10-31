import { createContext, useContext, type HTMLAttributes, type ReactNode } from "react";
import { cn } from "../lib/cn";
import {ContextHelp} from '../components/ContextHelp';
import {t} from '../i18n';

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-ui="card" className={cn("rounded-md border border-border bg-surface", className)} {...props} />;
}

const CardDepth = createContext(false);

export function PlainPanels({ children }: { children: ReactNode }) {
  return <CardDepth.Provider value>{children}</CardDepth.Provider>;
}

export function PanelGroup({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div data-ui="panel-group" className={cn("grid min-w-0 gap-px overflow-hidden rounded-md border border-border bg-border text-sm [&>[data-ui=section]]:min-w-0 [&>[data-ui=section]]:bg-surface [&>[data-ui=section]]:p-3", className)} {...props}><PlainPanels>{children}</PlainPanels></div>;
}

export function Panel({ title, description, help, actions, className, children, variant, ...props }:
  Omit<HTMLAttributes<HTMLElement>, "title"> & { title?: ReactNode; description?: ReactNode; help?:ReactNode; actions?: ReactNode; variant?: "card" | "plain" }) {
  const inside = useContext(CardDepth);
  variant = variant ?? (inside ? "plain" : "card");
  const body = (
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
  return variant === "card" ? <CardDepth.Provider value>{body}</CardDepth.Provider> : body;
}
