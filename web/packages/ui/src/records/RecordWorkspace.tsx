import type { ReactNode } from "react";
import { Button } from "../primitives/button";
import { PageHeader } from "../components/PageHeader";
import { t } from "../i18n";
import { RecordList, type RecordSource } from "./Records";

/** Responsive list/detail task frame shared by code pages and builder preview. */
export function RecordWorkspace({ title, description, source, type, listFields, selected, onSelect, detail, actions, notice }: {
  title: string; description?: string; source: RecordSource; type: string; listFields?: string[];
  selected?: string; onSelect: (id?: string) => void; detail: (id: string) => ReactNode;
  actions?: ReactNode; notice?: ReactNode;
}) {
  return <>
    <PageHeader title={title} description={description} actions={actions} />
    {notice}
    <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(20rem,45%)_minmax(0,1fr)]">
      <section aria-label={t("Records in this page")} className={selected ? "hidden min-w-0 lg:block" : "min-w-0"}>
        <RecordList source={source} type={type} fields={listFields} onOpen={(r) => onSelect(r.id)} height="calc(100dvh - 255px)" />
      </section>
      <section aria-label={t("Selected record")} className={!selected ? "hidden min-w-0 lg:block" : "min-w-0"}>
        {selected ? <>
          <Button size="sm" variant="ghost" className="mb-2 lg:hidden" onClick={() => onSelect(undefined)}>{t("Back to list")}</Button>
          {detail(selected)}
        </> : <p className="rounded-md border border-border bg-surface p-6 text-sm text-muted">{t("Select a record to see its details.")}</p>}
      </section>
    </div>
  </>;
}
