import { Button, Panel, FlowRun, t, useWorkspace, type FlowDefinition, type FlowInstanceData } from "@platform/ui";
import { ChainGraph, useHost, useOpenRecord, useReadQuery } from "../index";

import { useRunControl } from "./use-run-control";

export function FlowInstanceView({ id, application, project }: { id: string; application?: string; project?: string }) {
  const { open } = useWorkspace(), openRecord = useOpenRecord(), { can } = useHost();
  const actions = useRunControl("flow.instance", id);
  const view = useReadQuery<{ record: FlowInstanceData }>(`/v1/records/flow.instance/${encodeURIComponent(id)}`);
  const flows = useReadQuery<FlowDefinition[]>("/v1/flows").data ?? [];
  const instance = view.data?.record;
  if (view.isError) return <p role="alert" className="text-sm text-muted">{t("This flow run is unavailable to you.")}</p>;
  if (!instance) return <p className="text-sm text-muted">{t("Loading")} {id}…</p>;
  const definition = flows.find((flow) => flow.id === instance.flow && flow.version === instance.version);
  const live = !["done", "compensated", "canceled"].includes(instance.state);
  const next = flows.some((flow) => flow.id === instance.flow && flow.version > instance.version);
  return <div className="grid max-w-4xl gap-3">
    <div className="flex flex-wrap gap-2">
      {project && <Button variant="ghost" onClick={() => open({ view: "project", params: { id: project, section: "runs" } })}>{t("Back to project")}</Button>}
      {!project && application && <Button variant="ghost" onClick={() => open({ view: "application-runs", params: { app: application.split(":")[0] ?? "", name: application.split(":").slice(1).join(":"), application } })}>{t("Back to application runs")}</Button>}
      <Button variant="ghost" onClick={() => open({ view: "inbox" }, { window: "beside" })}>{t("Back to inbox")}</Button>
      {instance.subject && <Button variant="ghost" onClick={() => openRecord(instance.subject!)}>{t("Open related record")}</Button>}
    </div>
    {actions.error && <Panel role="alert" className="text-sm text-danger">{actions.error}</Panel>}
    {actions.pending && <Panel role="status" className="grid gap-2 text-sm"><p>{t("A runtime decision is unconfirmed. Retry the original operation; leaving does not cancel accepted work.")}</p>
      <code>{actions.pending.schema}</code><Button disabled={actions.busy} onClick={() => void actions.retry()}>{t("Retry original operation")}</Button></Panel>}
    {live && <div className="flex gap-2">
      {can("flow.instance.retry") && <Button size="sm" disabled={actions.busy || !!actions.pending} onClick={() => void actions.control("flow.instance.retry")}>{t("Retry")}</Button>}
      {can("flow.instance.move") && next && <Button size="sm" disabled={actions.busy || !!actions.pending} onClick={() => void actions.control("flow.instance.move")}>{t("Move to the next version")}</Button>}
      {can("flow.instance.cancel") && <Button size="sm" variant="danger" disabled={actions.busy || !!actions.pending} onClick={() => void actions.control("flow.instance.cancel")}>{t("Cancel")}</Button>}
    </div>}
    <ChainGraph of={`flow.instance/${instance.id}`} />
    <FlowRun definition={definition} instance={instance} actions={(token) => live && can("flow.instance.skip") && ["stuck", "retry", "undo"].includes(token.waits ?? "")
      ? <Button size="sm" variant="ghost" disabled={actions.busy || !!actions.pending} onClick={() => void actions.control("flow.instance.skip", { token: token.id })}>{t("Skip")}</Button> : null} />
  </div>;
}
