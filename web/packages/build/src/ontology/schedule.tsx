import { useReadQuery } from "@platform/app";
import { Button, Checkbox, DataTable, Input, PageHeader, Panel, PropertyList, Select, StructuredEditor, t } from "@platform/ui";
import { ResourceControls } from "../editor/workbench";
import type { Api } from "@platform/api";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { PERIODS, periodLabel } from "../automate/workflow-model";

type Draft = { id: string; revision: number; name: string; title: string; pipeline: string; every: string; enabled: boolean };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", pipeline: "", every: "", enabled: false });
export function Schedules({ pipeline }: { pipeline?: string }) {
  const { open } = useApplicationWorkspace();
  const schedules = useReadQuery<Api.PipelineSchedule[]>("/v1/schedules", 15000);
  const pipelines = useReadQuery<{ records: { id: string; title: string }[] }>("/v1/records/build.pipeline?limit=200");
  const stamp = (value?: string) => value ? new Date(value).toLocaleString() : "—";
  return <div className="grid gap-3"><PageHeader title={t("Schedules")} actions={<Button variant="primary" onClick={() => open({ view: "schedule", params: { id: "new" } })}>{t("New schedule")}</Button>} />
    {schedules.isError ? <Panel role="alert">{t("Schedules are unavailable to you.")}</Panel> : <DataTable data={(schedules.data ?? []).filter(schedule => !pipeline || schedule.pipeline === pipeline)} getRowId={row => row.id} empty={schedules.isPending ? t("Loading…") : t("Nothing here yet.")} onRowClick={row => open({ view: "schedule", params: { id: row.id } })} columns={[
      { id: "title", header: t("Schedule"), accessorFn: row => row.title || row.name },
      { id: "pipeline", header: t("Pipeline"), accessorFn: row => row.pipeline, cell: context => <Button size="sm" variant="ghost" onClick={event => { event.stopPropagation(); open({ view: "pipeline", params: { id: context.row.original.pipeline } }); }}>{pipelines.data?.records.find(pipeline => pipeline.id === context.row.original.pipeline)?.title ?? context.row.original.pipeline}</Button> },
      { id: "every", header: t("Every"), accessorFn: row => row.every, cell: context => periodLabel(String(context.getValue())) },
      { id: "lastBuild", header: t("Last build"), accessorFn: row => row.lastBuild, cell: context => stamp(context.getValue() as string | undefined) },
      { id: "nextDue", header: t("Next due"), accessorFn: row => row.nextDue, cell: context => stamp(context.getValue() as string | undefined) },
      { id: "enabled", header: t("Enabled"), accessorFn: row => row.enabled, cell: context => t(context.getValue() ? "Yes" : "No") },
    ]} />}</div>;
}
export function ScheduleEditor({ id }: { id: string }) {
  const { open } = useApplicationWorkspace();
  const { draft, structured, query, dirty, busy, error, patch, perform, save, discardChanges } = useDefinitionDraft<Draft>({ id, type: "build.schedule", view: "schedule", empty, failure: t("The schedule could not be saved or loaded. Your draft is still here."), payload: ({ name, title, pipeline, every, enabled }) => ({ name, title, pipeline, every, enabled }) });
  const pipelines = useReadQuery<{ records: { id: string; title: string }[] }>("/v1/records/build.pipeline?limit=200");
  const schedules = useReadQuery<Api.PipelineSchedule[]>("/v1/schedules", 15000, id !== "new");
  const schedule = schedules.data?.find(item => item.id === id);
  if (id !== "new" && !draft.id) return <PageHeader title={t("Schedules")} description={query.isError ? t("The schedule could not be loaded.") : t("Loading…")} />;
  return <div className="grid gap-3"><PageHeader crumbs={[{ label: t("Schedules"), onClick: () => open({ view: "schedules" }) }]} title={draft.title || t("New schedule")} actions={<ResourceControls type="build.schedule" record={draft} dirty={dirty} busy={busy} route={{ view: "schedule", params: { id } }} onCancel={discardChanges} onSave={() => perform(save)} />} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    {draft.id && <Panel title={t("Schedule")}><PropertyList items={[[t("Last build"), schedule?.lastBuild ? new Date(schedule.lastBuild).toLocaleString() : "—"], [t("Next due"), schedule?.nextDue ? new Date(schedule.nextDue).toLocaleString() : "—"]]} />{schedules.isError && <p role="alert">{t("Schedules are unavailable to you.")}</p>}</Panel>}
    <StructuredEditor {...structured}><Panel title={t("Schedule")} className="grid gap-3"><label className="grid gap-1 text-xs">{t("Name")}<Input value={draft.name} onChange={event => patch({ name: event.target.value })} /></label><label className="grid gap-1 text-xs">{t("Title")}<Input value={draft.title} onChange={event => patch({ title: event.target.value })} /></label><label className="grid gap-1 text-xs">{t("Pipeline")}<Select value={draft.pipeline} onChange={event => patch({ pipeline: event.target.value })}><option value="">{t("Choose a pipeline")}</option>{pipelines.data?.records.map(pipeline => <option key={pipeline.id} value={pipeline.id}>{pipeline.title}</option>)}</Select></label><label className="grid gap-1 text-xs">{t("Every")}<Select value={draft.every} onChange={event => patch({ every: event.target.value })}><option value="">{t("Choose a period")}</option>{PERIODS.map(period => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label><Checkbox checked={draft.enabled} onChange={enabled => patch({ enabled })}>{t("Enabled")}</Checkbox></Panel></StructuredEditor></div>;
}
