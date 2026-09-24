import type { ReactNode } from "react";
import { Card } from "../primitives/card";

/** Label/value pairs; values may be any node (tags, links, numbers). */
export function PropertyList({ items }: { items: [label: string, value: ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1.5 text-sm">
      {items.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-muted">{label}</dt>
          <dd className="min-w-0 truncate">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** One entity at a glance: identity, state, key properties, actions. */
export function EntityCard({ title, subtitle, status, properties, actions }: {
  title: ReactNode; subtitle?: ReactNode; status?: ReactNode; properties: [string, ReactNode][]; actions?: ReactNode;
}) {
  return (
    <Card className="flex flex-col gap-3 p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-base font-semibold">{title}</div>
          {subtitle && <div className="truncate font-mono text-xs text-muted">{subtitle}</div>}
        </div>
        {status}
      </div>
      <PropertyList items={properties} />
      {actions && <div className="flex flex-wrap gap-2 border-t border-border pt-3">{actions}</div>}
    </Card>
  );
}
