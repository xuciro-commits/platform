import { Card as AstryxCard } from "@astryxdesign/core/Card";
import { createContext, useContext, type HTMLAttributes, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { ContextHelp } from "../components/ContextHelp";
import { t } from "../i18n";

export function Card({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <AstryxCard {...props} data-ui="card" padding={0}>
      <div data-ui="card-body" className={className}>
        {children}
      </div>
    </AstryxCard>
  );
}

const CardDepth = createContext(false);

export function PlainPanels({ children }: { children: ReactNode }) {
  return <CardDepth.Provider value>{children}</CardDepth.Provider>;
}

export function PanelGroup({ className, children, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      data-ui="panel-group"
      className={cn(
        "grid min-w-0 gap-px overflow-hidden rounded-md border border-border bg-border text-sm [&>[data-ui=section]]:min-w-0 [&>[data-ui=section]]:bg-panel [&>[data-ui=section]]:p-3",
        className,
      )}
      {...props}
    >
      <PlainPanels>{children}</PlainPanels>
    </div>
  );
}

export function Panel({
  title,
  description,
  help,
  actions,
  className,
  children,
  variant,
  ...props
}: Omit<HTMLAttributes<HTMLElement>, "title"> & {
  title?: ReactNode;
  description?: ReactNode;
  help?: ReactNode;
  actions?: ReactNode;
  variant?: "card" | "plain";
}) {
  const inside = useContext(CardDepth);
  const resolved = variant ?? (inside ? "plain" : "card");
  const body = (
    <>
      {(title || actions || help) && (
        <div className="mb-2 flex flex-wrap items-center gap-2">
          {title && <h2 className="text-sm font-semibold">{title}</h2>}
          {help && (
            <ContextHelp title={typeof title === "string" ? title : (props["aria-label"] ?? t("Details"))}>
              {help}
            </ContextHelp>
          )}
          {actions && <span className="ml-auto flex flex-wrap items-center gap-2">{actions}</span>}
        </div>
      )}
      {description && <p className="mb-2 max-w-prose text-xs text-muted">{description}</p>}
      {children}
    </>
  );

  if (resolved === "card") {
    return (
      <CardDepth.Provider value>
        <AstryxCard {...props} data-ui="card" className={cn("text-sm", className)} padding={3}>
          {body}
        </AstryxCard>
      </CardDepth.Provider>
    );
  }

  return (
    <section data-ui="section" className={className} {...props}>
      {body}
    </section>
  );
}
