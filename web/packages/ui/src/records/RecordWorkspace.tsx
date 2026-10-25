import type { ReactNode } from "react";
import { Button } from "../primitives/button";
import { PageHeader } from "../components/PageHeader";
import { t } from "../i18n";
import { RecordList, type ListState, type RecordSource } from "./Records";
import type { RecordEditPort } from "./EditableRecordGrid";

/** Responsive list/detail task frame shared by code pages and builder preview. */
export function RecordWorkspace({ title, description, source, type, listFields, selected, onSelect, detail, actions, notice,
  listToolbar, onListSave, listInlineEdit, listInitial }: {
  title: string; description?: string; source: RecordSource; type: string; listFields?: string[];
  selected?: string; onSelect: (id?: string) => void; detail: (id: string) => ReactNode;
  actions?: ReactNode; notice?: ReactNode;
  /** The list's own operations: import where the page permits, saving where the member is. */
  listToolbar?: ReactNode; onListSave?: (state: ListState) => void; listInlineEdit?: RecordEditPort; listInitial?: ListState;
}) {
  return <>
    <PageHeader title={title} description={description} actions={actions} />
    {notice}
    <div className={selected ? "grid min-w-0 gap-4 lg:grid-cols-[minmax(20rem,45%)_minmax(0,1fr)]" : "grid min-w-0"}>
      <section aria-label={t("Records in this page")} className={selected ? "hidden min-w-0 lg:block" : "min-w-0"}>
        <RecordList source={source} type={type} fields={listFields} selectedId={selected} onOpen={(r) => onSelect(r.id)} height="calc(100dvh - 255px)"
          toolbar={listToolbar} onSave={onListSave} inlineEdit={listInlineEdit} initial={listInitial} />
      </section>
      {selected && <section aria-label={t("Selected record")} className="min-w-0">
          <Button size="sm" variant="ghost" className="mb-2" onClick={() => onSelect(undefined)}>{t("Back to list")}</Button>
          {detail(selected)}
      </section>}
    </div>
  </>;
}
