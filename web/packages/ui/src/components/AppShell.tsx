import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export type NavItem = { id: string; label: string; icon?: ReactNode; badge?: ReactNode };

/** Application frame: product name, navigation, context (tenant, principal), content. */
export function AppShell({ product, nav, active, onNavigate, context, children }: {
  product: string; nav: NavItem[]; active: string; onNavigate: (id: string) => void; context?: ReactNode; children: ReactNode;
}) {
  return (
    <div className="grid h-dvh grid-cols-[200px_1fr] grid-rows-[40px_1fr] max-md:grid-cols-1">
      <header className="col-span-full flex items-center gap-3 border-b border-border bg-surface px-3">
        <span className="text-sm font-semibold tracking-tight">{product}</span>
        <div className="ml-auto flex items-center gap-2">{context}</div>
      </header>
      <nav aria-label="Main" className="border-r border-border bg-surface p-2 max-md:hidden">
        {nav.map((item) => (
          <button key={item.id} type="button" onClick={() => onNavigate(item.id)}
            aria-current={item.id === active ? "page" : undefined}
            className={cn("flex h-7 w-full items-center gap-2 rounded-md px-2 text-sm hover:bg-row-hover [&_svg]:size-3.5",
              item.id === active && "bg-row-selected font-medium")}>
            {item.icon}{item.label}<span className="ml-auto">{item.badge}</span>
          </button>
        ))}
      </nav>
      <main className="min-w-0 overflow-auto p-4">{children}</main>
    </div>
  );
}

export function PageHeader({ title, description, actions }: { title: string; description?: string; actions?: ReactNode }) {
  return (
    <div className="mb-3 flex items-end justify-between gap-3">
      <div>
        <h1 className="text-lg font-semibold tracking-tight">{title}</h1>
        {description && <p className="text-sm text-muted">{description}</p>}
      </div>
      <div className="flex gap-2">{actions}</div>
    </div>
  );
}
