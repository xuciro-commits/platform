import type { ReactNode } from "react";

export function PageHeader({ title, description, actions, level, compact=false }: { title: string; description?: string; actions?: ReactNode;level?:1|2|3;compact?:boolean }) {
  const Heading=level===2?"h2":level===3?"h3":"h1";
  return (
    <div className={(compact?"":"mb-3 ")+"flex min-w-0 flex-col gap-3 sm:flex-row sm:items-end sm:justify-between"}>
      <div className="min-w-0">
        <Heading className={"break-words font-semibold tracking-tight "+(level===1?"text-xl":level===2?"text-base":level===3?"text-sm":"text-lg")}>{title}</Heading>
        {description && <p className="text-sm text-muted">{description}</p>}
      </div>
      {(!compact||actions)&&<div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}
