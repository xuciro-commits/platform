import { DraftStatus, ResourceControls } from "../editor/workbench";
import { StructuredEditor } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useId } from "react";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";

type Draft = Api.Listener;
const empty = (): Draft => ({ id: "", revision: 0, created: { at: "" } as Api.Stamp, changed: { at: "" } as Api.Stamp, name: "", title: "", connector: "", path: "", secret: "", enabled: false, state: "draft" });
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function Listeners() {
  const { source, holds } = useHost(), { open } = useApplicationWorkspace();
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Listeners")} description={t("Only a builder or integrator can edit listeners.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Listeners")} help={t("Inbound webhooks that let an external system POST payloads to an existing connector input. The listener names its secret in the host's secret store; the credential itself never enters a definition. Publish to open the path; pause to close it.")}
      actions={<Button variant="primary" onClick={() => open({ view: "listener", params: { id: "new" } })}>{t("New listener")}</Button>} />
    <ResourceList source={source} type="build.listener" fields={["title", "name", "connector", "path", "enabled", "state"]} onOpen={(record) => open({ view: "listener", params: { id: record.id } })} />
  </div>;
}

export function ListenerEditor({ id }: { id: string }) {
  const { holds } = useHost(), { open } = useApplicationWorkspace();
  const fieldID = useId();
  const appsQuery = useReadQuery<Api.AppInfo[]>("/v1/apps");
  const inputs = (appsQuery.data ?? []).flatMap((app) => app.inputs).sort((a, b) => a.localeCompare(b));
  const { draft, structured, query, dirty, busy, error, patch, perform, save, discardChanges, transition } = useDefinitionDraft<Draft>({
    id, type: "build.listener", view: "listener", empty,
    failure: t("The listener could not be saved or loaded. Your draft is still here."),
    payload: (draft) => {
      const { name, title, connector, path, secret, enabled } = draft;
      return { name, title, connector, path, secret, enabled: !!enabled };
    },
  });
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Listeners")} description={t("Only a builder or integrator can edit listeners.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Listeners")} description={query.isError ? t("The listener could not be loaded.") : t("Loading…")} />;
  const inboundURL = draft.path ? `${location.origin}/v1/listeners/${draft.path}` : "";
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader crumbs={[{ label: t("Listeners"), onClick: () => open({ view: "listener" }) }]} title={draft.title || t("New listener")} status={<><DraftStatus state={draft.state} /></>} meta={draft.name ? <span className="font-mono">{draft.name}</span> : undefined} help={t("Point an external system at the inbound path and give it the shared secret as the X-Listener-Secret header. Each delivery is handed to the chosen connector input.")}
      actions={<ResourceControls type="build.listener" record={draft} dirty={dirty} busy={busy} route={{ view: "listener", params: { id } }} onCancel={discardChanges} onSave={() => perform(save)}
        operations={[
          ...(draft.state === "draft" ? [{ id: "publish", label: t("Publish"), disabled: dirty, run: () => void perform(() => transition("publish")) }] : []),
          ...(draft.state === "published" ? [{ id: "pause", label: t("Pause"), disabled: dirty, run: () => void perform(() => transition("pause")) }] : []),
        ]} />} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <StructuredEditor {...structured}><fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("Listener")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Listener name")}<Input disabled={draft.state === "published"} value={draft.name} placeholder="orders-inbound" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Listener title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Connector input")}<Select value={draft.connector} onChange={(e) => patch({ connector: e.target.value })}>
          <option value="">{t("Choose a connector input")}</option>
          {draft.connector && !inputs.includes(draft.connector) && <option value={draft.connector}>{draft.connector} · {t("Unavailable connector input")}</option>}
          {inputs.map((input) => <option key={input} value={input}>{input}</option>)}
        </Select>
          <span className="text-xs text-muted">{t("The declared connector input each delivery is handed to. Only inputs you may use are listed.")}</span>
          {appsQuery.isError && <span className="text-danger">{t("The connector inputs could not be loaded.")}</span>}
          {draft.connector && !appsQuery.isPending && !inputs.includes(draft.connector) && <span role="alert" className="text-danger">{t("This connector input is unavailable. Choose one you may use.")}</span>}</label>
        <Checkbox checked={!!draft.enabled} onChange={(enabled) => patch({ enabled })}>{t("Accept inbound deliveries")}</Checkbox>
      </Panel>
      <Panel title={t("Inbound delivery")} className="grid min-w-0 content-start gap-3">
        <div className={fieldClass}><label htmlFor={`${fieldID}-path`}>{t("Webhook path")}</label><Input id={`${fieldID}-path`} aria-describedby={`${fieldID}-path-help`} disabled={draft.state === "published"} value={draft.path} placeholder="orders" onChange={(e) => patch({ path: e.target.value })} />
          <p id={`${fieldID}-path-help`} className="text-xs text-muted">{t("The external system POSTs to this path with its shared secret in the X-Listener-Secret header.")}</p></div>
        {inboundURL && <div className="grid gap-1 text-xs"><span className="text-muted">{t("Inbound URL")}</span><div className="flex min-w-0 items-center gap-2"><code className="min-w-0 flex-1 truncate font-mono">{inboundURL}</code><Button size="sm" variant="ghost" onClick={() => void navigator.clipboard?.writeText(inboundURL)}>{t("Copy")}</Button></div></div>}
        <div className={fieldClass}><label htmlFor={`${fieldID}-secret`}>{t("Secret name")}</label><Input id={`${fieldID}-secret`} aria-describedby={`${fieldID}-secret-help`} value={draft.secret} placeholder="orders-inbound" onChange={(e) => patch({ secret: e.target.value })} />
          <p id={`${fieldID}-secret-help`} className="text-xs text-muted">{t("The credential's name in the host's secret store, never the credential. Its content is the value the external system must send as X-Listener-Secret.")}</p></div>
        <p className="flex flex-wrap items-center gap-2 text-xs"><Tag label={draft.enabled ? t("Accepting") : t("Closed")} tone={draft.enabled && draft.state === "published" ? "success" : "neutral"} /><span className="text-muted">{t("A listener accepts deliveries only when published and enabled.")}</span></p>
      </Panel>
    </fieldset></StructuredEditor>
  </div>;
}
