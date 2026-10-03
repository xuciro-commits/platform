import type { ReactNode } from "react";
import { Card } from "../primitives/card";

/** Label/value pairs; values may be any node (tags, links, numbers). */
export function PropertyList({ items,columns=1,itemKeys }: {itemKeys?:string[]; items: [label: string, value: ReactNode][];columns?:1|2|3|4 }) {
  const layout={1:"grid-cols-1",2:"grid-cols-1 @sm/properties:grid-cols-2",3:"grid-cols-1 @sm/properties:grid-cols-2 @lg/properties:grid-cols-3",4:"grid-cols-1 @sm/properties:grid-cols-2 @lg/properties:grid-cols-3 @xl/properties:grid-cols-4"}[columns]??"grid-cols-1";
  return (
    <div className="@container/properties"><dl className={`@container grid gap-3 text-sm ${layout}`}>
      {items.map(([label, value],index) => (
        <div key={itemKeys?.[index]??label} className={`grid min-w-0 grid-cols-1 gap-x-4 gap-y-1 ${columns===1?"@sm:grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)]":""}`}>
          <dt className="break-words text-muted">{label}</dt>
          <dd className="min-w-0 break-words">{value}</dd>
        </div>
      ))}
    </dl></div>
  );
}

/** One entity at a glance: identity, state, key properties, actions. */
export function EntityCard({ title, subtitle, status, properties, actions,tone,propertyKeys }: {
  tone?:"neutral"|"info"|"success"|"warning"|"danger";propertyKeys?:string[];
  title: ReactNode; subtitle?: ReactNode; status?: ReactNode; properties: [string, ReactNode][]; actions?: ReactNode;
}) {
  return (
    <Card className="flex flex-col gap-3 p-3" style={tone?{borderLeftWidth:3,borderLeftColor:`var(--tone-${tone})`}:undefined}>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="truncate text-base font-semibold">{title}</div>
          {subtitle && <div className="truncate font-mono text-xs text-muted">{subtitle}</div>}
        </div>
        {status}
      </div>
      <PropertyList items={properties} itemKeys={propertyKeys}/>
      {actions && <div className="flex flex-wrap gap-2 border-t border-border pt-3">{actions}</div>}
    </Card>
  );
}
