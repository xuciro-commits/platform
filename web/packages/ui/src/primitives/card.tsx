import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "../lib/cn";

export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("rounded-md border border-border bg-surface", className)} {...props} />;
}

/** A card with a heading: the one way a page groups a part of itself under a title. */
export function Panel({ title, description, actions, className, children, ...props }:
  Omit<HTMLAttributes<HTMLElement>, "title"> & { title?: ReactNode; description?: ReactNode; actions?: ReactNode }) {
  return (
    <section className={cn("rounded-md border border-border bg-surface p-3 text-sm", className)} {...props}>
      {(title || actions) && (
        <div className="mb-2 flex items-center gap-2">
          {title && <h2 className="text-sm font-semibold">{title}</h2>}
          {actions && <span className="ml-auto flex items-center gap-2">{actions}</span>}
        </div>
      )}
      {description && <p className="mb-2 text-xs text-muted">{description}</p>}
      {children}
    </section>
  );
}
