// A composed page (ADR-0035): sections laid out in order, each holding one
// widget bound to what this tenant has. A table says which record is selected;
// a detail and the actions read it. Every widget renders through the owner that
// already has it — the kit's record list and record page, the action catalog,
// the aggregate chart — so a code page and a composed page look and behave the
// same, and nothing here interprets data of its own.
import { Card, Chart, Panel, RecordList, cn, t, type ChartSpec, type Encoding, type EntityRecord } from "@platform/ui";
import { useState, type ReactNode } from "react";
import { NewActions, RecordActions } from "./actions";
import { RecordDetail, useHost, type Definition } from "./index";

type Page = NonNullable<Definition["page"]>;
type Section = NonNullable<Page["sections"]>[number];

/** What a section is bound to, and what the page has selected. */
type Bound = { page: Page; section: Section; selected?: EntityRecord; onSelect: (record?: EntityRecord) => void; live: boolean };

const objectOf = (page: Page, section: Section) => section.object?.name || page.object.name;

/** The records of an object, as a list; selecting one fills the rest of the page. */
function TableWidget({ page, section, onSelect, selected }: Bound) {
  const { source } = useHost();
  const type = objectOf(page, section);
  return (
    <RecordList source={source} type={type} fields={section.fields} height={320}
      onOpen={(record) => onSelect(record.id === selected?.id ? undefined : record)} />
  );
}

/** The record the page has selected, with the fields the builder chose. */
function DetailWidget({ page, section, selected, live }: Bound) {
  const type = objectOf(page, section);
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see it here.")}</p>;
  return <RecordDetail type={type} id={selected.id} fields={section.fields} allowed={live ? undefined : []} />;
}

/** The actions the builder chose, on what is selected (Workshop's button group). */
function ActionsWidget({ page, section, selected, live }: Bound) {
  const type = objectOf(page, section);
  const allowed = (section.actions ?? []).map((ref) => ref.name);
  if (!live) return <p className="text-sm text-muted">{t("Actions do not run while you compose.")}</p>;
  return (
    <div className="flex flex-wrap gap-2">
      <NewActions type={type} allowed={allowed} />
      {selected
        ? <RecordActions type={type} record={selected} allowed={allowed} />
        : <span className="self-center text-sm text-muted">{t("Select a record to act on it.")}</span>}
    </div>
  );
}

/** An aggregate of the object: grouped and measured, drawn by the kit (ADR-0019). */
function chartSpec(page: Page, section: Section, kpi: boolean): ChartSpec {
  const [aggregate, field] = (section.measure ?? "count").split(":");
  const value: Encoding = { field, type: "quantitative", aggregate: aggregate as Encoding["aggregate"] };
  const [group, timeUnit] = (section.group ?? "").split(":");
  const by: Encoding = { field: group, type: timeUnit ? "temporal" : "nominal", timeUnit: timeUnit as Encoding["timeUnit"] };
  return {
    title: kpi ? section.title : undefined,
    data: { entity: objectOf(page, section) },
    mark: kpi ? "kpi" : "bar",
    encoding: kpi ? { y: value } : { x: by, y: value },
  };
}

function ChartWidget({ page, section, kpi }: Bound & { kpi: boolean }) {
  const { source } = useHost();
  const aggregate = source.aggregate;
  return <Chart spec={chartSpec(page, section, kpi)} frame={false} height={kpi ? 120 : 240}
    source={aggregate ? { aggregate, revision: source.revision } : undefined} />;
}

/** One section: its title, and the widget it holds. */
export function SectionView(bound: Bound) {
  const { section } = bound;
  const body: ReactNode = (() => {
    switch (section.widget) {
      case "table": return <TableWidget {...bound} />;
      case "detail": return <DetailWidget {...bound} />;
      case "actions": return <ActionsWidget {...bound} />;
      case "chart": return <ChartWidget {...bound} kpi={false} />;
      case "metric": return <ChartWidget {...bound} kpi />;
      case "text": return <p className="whitespace-pre-wrap text-sm">{section.text}</p>;
      default: return <p role="alert" className="text-sm text-danger">{t("This widget is unavailable.")}</p>;
    }
  })();
  return (
    <Card className={cn("grid content-start gap-2 p-3", section.width === "half" ? "md:col-span-1" : "md:col-span-2")}>
      {section.title && section.widget !== "metric" && <h3 className="text-sm font-semibold">{section.title}</h3>}
      {body}
    </Card>
  );
}

/**
 * A composed page as people use it: the sections in order, sharing what is
 * selected. `live` false is the builder's canvas — the same widgets over the
 * same records, with nothing that writes.
 */
export function ComposedPage({ page, live = true, notice }: { page: Page; live?: boolean; notice?: ReactNode }) {
  const [selected, setSelected] = useState<EntityRecord>();
  return (
    <div className="grid gap-3">
      {notice}
      <div className="grid gap-3 md:grid-cols-2">
        {(page.sections ?? []).map((section, i) => (
          <SectionView key={i} page={page} section={section} selected={selected} onSelect={setSelected} live={live} />
        ))}
      </div>
      {(page.sections ?? []).length === 0 && <Panel role="status" className="text-sm text-muted">{t("Nothing is on this page yet.")}</Panel>}
    </div>
  );
}

/** Whether a page is composed of sections (ADR-0035) rather than the list-detail shorthand. */
export const isComposed = (page: Page) => page.layout === "composed";

