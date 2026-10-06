// Object Explorer (ADR-0052 §3.4, batch P3): every object type the member may
// read, and its records with the host's search, sort and pages. One place to
// browse the Ontology as data; the definition itself opens in the Ontology app.
import { useHost, useOpenRecord, OpenIn } from "@platform/app";
import { Button, Input, PageHeader, RecordList, cn, t } from "@platform/ui";
import { Compass } from "lucide-react";
import { useState } from "react";

export function ObjectExplorer({ type: requested }: { type?: string }) {
  const { source, entities } = useHost();
  const openRecord = useOpenRecord();
  const [filter, setFilter] = useState("");
  const [picked, setPicked] = useState<string>();
  const chosen = picked ?? requested ?? entities[0]?.type ?? "";
  const entity = entities.find((e) => e.type === chosen);
  const shown = entities.filter((e) => !filter || `${e.title} ${e.type}`.toLowerCase().includes(filter.toLowerCase()))
    .sort((a, b) => a.type.localeCompare(b.type));
  const groups = new Map<string, typeof shown>();
  for (const e of shown) { const app = e.type.split(".")[0] ?? ""; groups.set(app, [...(groups.get(app) ?? []), e]); }
  if (entities.length === 0) return <>
    <PageHeader title={t("Object Explorer")} />
    <p className="text-sm text-muted">{t("No entity types in apps you hold a role in.")}</p>
  </>;
  return <div className="grid h-[calc(100dvh-120px)] grid-cols-[260px_minmax(0,1fr)] gap-4">
    <aside className="grid content-start gap-2 overflow-hidden border-r border-border pr-3">
      <div className="flex items-center gap-2 text-sm font-semibold"><Compass className="size-4" />{t("Object types")}<span className="ml-auto text-xs font-normal text-muted">{entities.length}</span></div>
      <Input type="search" aria-label={t("Filter object types")} placeholder={t("Filter…")} value={filter} onChange={(e) => setFilter(e.target.value)} />
      <nav aria-label={t("Object types")} className="grid gap-2 overflow-auto">
        {[...groups.entries()].map(([app, list]) => <div key={app} className="grid gap-0.5">
          <div className="px-2 pt-1 text-[11px] font-medium uppercase tracking-wide text-muted">{app}</div>
          {list.map((e) => <Button key={e.type} variant="row" aria-current={e.type === chosen ? "true" : undefined}
            className={cn("grid", e.type === chosen && "bg-row-selected font-medium")} onClick={() => setPicked(e.type)}>
            <span className="truncate">{e.title}</span><span className="truncate font-mono text-[11px] text-muted">{e.type}</span>
          </Button>)}
        </div>)}
      </nav>
    </aside>
    <div className="min-w-0">
      <PageHeader title={entity?.title ?? chosen} description={entity?.description || chosen}
        actions={<OpenIn type={chosen} exclude={["explorer"]} />} />
      <RecordList source={source} type={chosen} height="calc(100dvh - 250px)" onOpen={(r) => openRecord({ type: chosen, id: r.id })} />
    </div>
  </div>;
}

