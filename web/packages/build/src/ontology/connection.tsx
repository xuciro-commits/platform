import { StructuredEditor } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useEffect, useId, useRef, useState } from "react";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { MarkingField, integrates } from "./marking";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";
import { ProtocolBindingEditor, ProtocolSamples } from "./ProtocolBindingEditor";

// Connections (ADR-0023): the external systems a tenant reads through - a kind,
// an address and the name of a secret the host holds. Check reaches the system
// once from the host; sources then read through a ready connection.
type Draft = Api.Connection;
const empty = (): Draft => ({ id: "", revision: 0, created: { at: "" } as Api.Stamp, changed: { at: "" } as Api.Stamp, name: "", title: "", kind: "http", address: "", state: "draft" });
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function Connections() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (!integrates(role("build"))) return <PageHeader title={t("Connections")} description={t("Only a builder or integrator can edit connections.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Connections")} help={t("The systems this tenant reads from. A connection names its credential in the host's secret store; the credential itself never enters a definition or a release.")}
      actions={<Button onClick={() => open({ view: "connection", params: { id: "new" } })}>{t("New connection")}</Button>} />
    <ResourceList source={source} type="build.connection" fields={["title", "name", "kind", "address", "state"]} onOpen={(record) => open({ view: "connection", params: { id: record.id } })} />
  </div>;
}

export function ConnectionEditor({ id }: { id: string }) {
  const { client, role } = useHost(), { open } = useApplicationWorkspace();
  const fieldID = useId();
  const kinds = useReadQuery<{ items: Api.ConnectionKind[] }>("/v1/connection-kinds");
  const { draft, structured, query, dirty, busy, error, inputs, patch, perform, save, review, discardChanges, transition, createID } = useDefinitionDraft<Draft>({
    id, type: "build.connection", kind: "connection", view: "connection", empty,
    failure: t("The connection could not be saved or loaded. Your draft is still here."),
    payload: draft => {
      const { name, title, kind, address, secret, allowPrivate, marking, protocol } = draft;
      return { name, title, kind, address, secret: secret ?? "", allowPrivate: !!allowPrivate, marking: marking ?? "", protocol: protocol ?? null };
    },
  });
  const [preview, setPreview] = useState<{ check: Api.ConnectionCheck; signature: string }>();
  const previewElement = useRef<HTMLDivElement>(null);
  const signature = JSON.stringify([draft.kind, draft.address, draft.secret, draft.allowPrivate, draft.protocol]);
  useEffect(() => { if (preview) previewElement.current?.scrollIntoView({ behavior: "smooth", block: "start" }); }, [preview]);
  const previewRead = async () => {
    const { kind, address, secret, allowPrivate, protocol } = draft;
    const response = await client.call<Api.ConnectionCheck>("POST", "/v1/connections/preview", { id: draft.id || createID, kind, address, secret, allowPrivate, protocol });
    if (!response.ok) throw Error(apiErrorMessage(response.body) ?? t("The connection could not be saved or loaded. Your draft is still here."));
    setPreview({ check: response.body, signature });
  };
  if (!integrates(role("build"))) return <PageHeader title={t("Connections")} description={t("Only a builder or integrator can edit connections.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Connections")} description={query.isError ? t("The connection could not be loaded.") : t("Loading…")} />;
  const last = draft.last;
  const options = kinds.data?.items ?? [];
  const selected = options.find(option => draft.protocol
    ? option.package === draft.protocol.package && option.version === draft.protocol.version && option.digest === draft.protocol.digest && option.profile?.id === draft.protocol.profile
    : !option.package && option.kind === draft.kind);
  const chooseKind = (id: string) => {
    const option = options.find(item => item.id === id); if (!option) return;
    patch({ kind: option.kind as Api.Connection["kind"], protocol: option.package && option.profile ? { package: option.package, version: option.version!, digest: option.digest!, profile: option.profile.id, configuration: { connection: {}, devices: [], points: [], commands: [], seed: 1 } } : undefined });
    setPreview(undefined);
  };
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New connection")} help={t("Choose a built-in connection or an installed protocol package. Preview reads the current draft; Check confirms the saved connection before release.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "connection" })}>{t("Connections")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || !!inputs?.invalid || !dirty && id !== "new"} onClick={() => void perform(save)}>{t("Save connection")}</Button>
        {draft.protocol && <Button disabled={busy || !selected} onClick={() => void perform(previewRead)}>{t("Read-only preview")}</Button>}
        <Button variant="primary" disabled={busy || !!inputs?.invalid || !!draft.requested} onClick={() => void perform(() => transition("check"))}>{draft.requested ? t("Checking…") : t("Check")}</Button>
        {role("build") === "builder" && <Button disabled={busy || !!inputs?.invalid} onClick={() => void perform(review)}>{t("Review and publish…")}</Button>}
        {draft.state === "ready" && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("retire"))}>{t("Retire")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <StructuredEditor {...structured}><fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("System")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Connection name")}<Input disabled={draft.state === "ready"} value={draft.name} placeholder="sap" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Connection title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Kind")}<Select value={selected?.id ?? ""} disabled={kinds.isPending} onChange={(e) => chooseKind(e.target.value)}>
          {!selected && <option value="">{draft.protocol ? t("The pinned protocol version is unavailable.") : t("Choose a connection kind")}</option>}
          {options.map(option => <option key={option.id} value={option.id}>{t(option.title)}{option.package ? ` · ${option.version}` : ""}</option>)}
        </Select></label>
        {kinds.isError && <p role="alert" className="text-xs text-danger">{t("Connection kinds could not be loaded.")} <Button size="sm" onClick={() => void kinds.refetch()}>{t("Retry")}</Button></p>}
        {draft.protocol && !selected && !kinds.isPending && <p role="alert" className="break-words text-xs text-danger">{t("Install this exact protocol version on Packages before editing or checking its configuration.")} {draft.protocol.package} · {draft.protocol.version}</p>}
        <div className={fieldClass}><label htmlFor={`${fieldID}-address`}>{draft.protocol ? t("Gateway address") : t("Address")}</label><Input id={`${fieldID}-address`} aria-describedby={`${fieldID}-address-help`} value={draft.address} placeholder={selected?.addressPlaceholder} onChange={(e) => patch({ address: e.target.value })} />
          <p id={`${fieldID}-address-help`} className="text-[11px] text-muted">{draft.protocol ? t("The protected HTTP address of the independent gateway. Device addresses are configured below.") : draft.kind === "postgres" ? t("A postgres:// URL without the password; the secret is the password.") : t("The service root; sources give a path or an entity set beneath it.")}</p></div>
        <div className={fieldClass}><label htmlFor={`${fieldID}-secret`}>{t("Secret name")}</label><Input id={`${fieldID}-secret`} aria-describedby={`${fieldID}-secret-help`} value={draft.secret ?? ""} placeholder="sap-reader" onChange={(e) => patch({ secret: e.target.value })} />
          <p id={`${fieldID}-secret-help`} className="text-[11px] text-muted">{draft.protocol ? t("The gateway credential is read from the host secret store. Keep credentials out of device settings and point files.") : t("Looked up by the host (PLATFORM_SECRETS_DIR or PLATFORM_SECRET_<NAME>). For http and OData its content is the Authorization header's value, such as Basic … or Bearer ….")}</p></div>
        <Checkbox checked={!!draft.allowPrivate} onChange={(allowPrivate) => patch({ allowPrivate })}>{t("Allow http and private addresses (on-premise systems)")}</Checkbox>
        <MarkingField value={draft.marking ?? ""} onChange={(marking) => patch({ marking: marking as Api.Connection["marking"] })} help={t("Everything read through it is at least this; datasets loaded from it are raised to it.")} />
      </Panel>
      <Panel title={t("Last check")} className="grid min-w-0 content-start gap-2">
        {!last ? <p className="text-xs text-muted">{t("Not checked yet.")}</p> : <>
          <p className="flex flex-wrap items-center gap-2 text-xs"><Tag label={last.ok ? t("Reachable") : t("Failed")} tone={last.ok ? "success" : "danger"} /><span>{new Date(last.at).toLocaleString()}</span>{last.detail && <span className="font-mono">{last.detail}</span>}</p>
          {last.error && <p className="text-xs text-danger">{last.error}</p>}
        </>}
      </Panel>
      {draft.protocol && selected?.profile && <ProtocolBindingEditor key={`${selected.id}:${draft.id}`} value={draft.protocol} profile={selected.profile} onChange={protocol => patch({ protocol })} />}
      {preview && <Panel title={t("Read-only preview")} aria-label={t("Read-only preview")} className="grid min-w-0 gap-3 lg:col-span-2">
        <div ref={previewElement} className="flex flex-wrap items-center gap-2 text-xs"><Tag label={preview.check.ok ? t("Read confirmed") : t("Failed")} tone={preview.check.ok ? "success" : "danger"} /><span>{new Date(preview.check.at).toLocaleString()}</span>{selected?.profile?.simulation && <Tag label={t("Simulation")} tone="warning" />}</div>
        {preview.signature !== signature && <p role="status" className="text-xs text-warning">{t("The draft changed after this preview. Preview again to confirm its current values.")}</p>}
        {preview.check.error && <p role="alert" className="break-words text-xs text-danger">{preview.check.error}</p>}
        {preview.check.detail && <p className="break-words text-xs text-muted">{preview.check.detail}</p>}
        <ProtocolSamples samples={preview.check.samples ?? []} />
        <p className="text-xs text-muted">{t("Preview reads only. It does not publish the connection, start continuous sampling or send device commands.")}</p>
      </Panel>}
    </fieldset></StructuredEditor>
  </div>;
}
