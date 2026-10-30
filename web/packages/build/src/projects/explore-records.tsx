import { objectSetPath, type Api } from "@platform/kernel";
import { useHost } from "@platform/app";
import { DataTable, columnsFor, entityFrom, humanizeKernelError, t, type EntityRecord } from "@platform/ui";
import { useEffect, useMemo, useState } from "react";
import { ObjectSetEditor } from "../editor/ObjectSetEditor";

const pageSize = 25;

export function ExploreRecords({ object }: { object: string }) {
  const { client, source, entities } = useHost();
  const info = entities.find(entity => entity.type === object);
  const [set, setSet] = useState<Api.ObjectSet>({ object });
  const [answer, setAnswer] = useState<{ key: string; scope: unknown; page?: Api.RecordPage; error?: string }>();
  const key = JSON.stringify(set);
  useEffect(() => { setSet({ object }); }, [object]);
  useEffect(() => {
    if (!set.object && !set.union && !set.intersect && !set.subtract && !set.around) return;
    let active = true;
    const handle = setTimeout(() => {
      client.get<Api.RecordPage>(objectSetPath("load", { set, limit: pageSize }), true).then(
        page => { if (active) setAnswer({ key, scope: source.scope, page }); },
        error => { if (active) setAnswer({ key, scope: source.scope, error: humanizeKernelError(error instanceof Error ? error.message : String(error)) }); },
      );
    }, 300);
    return () => { active = false; clearTimeout(handle); };
  }, [key, source.scope]);
  const entity = useMemo(() => info ? entityFrom(info, {}, source) : undefined, [info, source]);
  const current = answer?.key === key && answer.scope === source.scope ? answer : undefined;
  const columns = useMemo(() => entity && info ? [{ id: "id", header: "ID", accessorKey: "id", meta: { width: 110 } }, ...columnsFor(entity, info.fields.filter(field => !field.readOnly).slice(0, 3).map(field => field.name))] : [], [entity, info]);
  return <div className="grid min-w-0 gap-2 px-1 text-xs">
    <ObjectSetEditor draftKey={`explore:${object}`} value={set} onChange={setSet} interfaces={undefined} />
    {current?.error ? <p role="alert" className="text-danger">{current.error}</p>
      : <DataTable data={(current?.page?.records ?? []) as EntityRecord[]} columns={columns as never} getRowId={(row: EntityRecord) => row.id} height={Math.min(280, 40 + (current?.page?.records.length ?? 0) * 28)} searchable={false} loading={!current}
        empty={t("No records match this set.")} />}
    {current?.page && current.page.total > current.page.records.length && <p className="text-muted">{t("Showing the first {shown} of {total} records.", { shown: current.page.records.length, total: current.page.total.toLocaleString() })}</p>}
  </div>;
}
