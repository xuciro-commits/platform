import { Button, FlowView, t, useWorkspace, type FlowDefinition, type FlowInstanceData } from "@platform/ui";
import { ChainGraph, useHost, useOpenRecord, useReadQuery } from "../index";

/** One runtime record view for every member; original reads/actions authorize access. */
export function FlowInstanceView({ id }: { id: string }) {
  const { open } = useWorkspace(), openRecord = useOpenRecord(), { decide, can } = useHost();
  const view = useReadQuery<{ record: FlowInstanceData }>(`/v1/records/flow.instance/${encodeURIComponent(id)}`, 3000);
  const flows = useReadQuery<FlowDefinition[]>("/v1/flows").data ?? [];
  const instance = view.data?.record;
  if (view.isError) return <p role="alert" className="text-sm text-muted">{t("This flow run is unavailable to you.")}</p>;
  if (!instance) return <p className="text-sm text-muted">{t("Loading")} {id}…</p>;
  const definition = flows.find((flow) => flow.id === instance.flow && flow.version === instance.version);
  const target = { type: "flow.instance", id: instance.id };
  const live = !["done", "compensated", "canceled"].includes(instance.state);
  const next = flows.some((flow) => flow.id === instance.flow && flow.version > instance.version);
  return <div className="grid max-w-4xl gap-3">
    <div className="flex flex-wrap gap-2"><Button variant="ghost" onClick={() => open({ view: "inbox" }, { window: "beside" })}>{t("Back to inbox")}</Button>
      {instance.subject && <Button variant="ghost" onClick={() => openRecord(instance.subject!)}>{t("Open related record")}</Button>}
    </div>
    {live && <div className="flex gap-2">
      {can("flow.instance.retry") && <Button size="sm" onClick={() => void decide("flow.instance.retry", target, {})}>{t("Retry")}</Button>}
      {can("flow.instance.move") && next && <Button size="sm" onClick={() => void decide("flow.instance.move", target, {})}>{t("Move to the next version")}</Button>}
      {can("flow.instance.cancel") && <Button size="sm" variant="danger" onClick={() => void decide("flow.instance.cancel", target, {})}>{t("Cancel")}</Button>}
    </div>}
    <ChainGraph of={`flow.instance/${instance.id}`} />
    <FlowView definition={definition} instance={instance} actions={(token) => live && can("flow.instance.skip") && ["stuck", "retry", "undo"].includes(token.waits ?? "")
      ? <Button size="sm" variant="ghost" onClick={() => void decide("flow.instance.skip", target, { token: token.id })}>{t("Skip")}</Button> : null} />
  </div>;
}
