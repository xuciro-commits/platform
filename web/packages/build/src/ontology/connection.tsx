import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useId } from "react";
import type { Api } from "@platform/kernel";
import { useHost } from "@platform/app";
import { MarkingField, integrates } from "./marking";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";

// Connections (ADR-0070): the external systems a tenant reads through - a kind,
// an address and the name of a secret the host holds. Check reaches the system
// once from the host; sources then read through a ready connection.
type Draft = Api.Connection;
const empty = (): Draft => ({ id: "", revision: 0, created: { at: "" } as Api.Stamp, changed: { at: "" } as Api.Stamp, name: "", title: "", kind: "http", address: "", state: "draft" });
const fieldClass = "grid min-w-0 gap-1 text-xs";

const connectionKinds = [
  { id: "http", title: "HTTP", placeholder: "https://api.example.com" },
  { id: "odata", title: "OData", placeholder: "https://erp.example.com/odata/v4" },
  { id: "postgres", title: "PostgreSQL", placeholder: "postgres://reader@db.example.com:5432/warehouse?sslmode=require" },
] as const;

export function Connections() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (!integrates(role("build"))) return <PageHeader title={t("Connections")} description={t("Only a builder or integrator can edit connections.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Connections")} description={t("The systems this tenant reads from. A connection names its credential in the host's secret store; the credential itself never enters a definition or a release.")}
      actions={<Button onClick={() => open({ view: "connection", params: { id: "new" } })}>{t("New connection")}</Button>} />
    <ResourceList source={source} type="build.connection" fields={["title", "name", "kind", "address", "state"]} onOpen={(record) => open({ view: "connection", params: { id: record.id } })} />
  </div>;
}

export function ConnectionEditor({ id }: { id: string }) {
  const { role } = useHost(), { open } = useApplicationWorkspace();
  const fieldID = useId();
  const { draft, query, dirty, busy, error, inputs, patch, perform, save, review, discardChanges, transition } = useDefinitionDraft<Draft>({
    id, type: "build.connection", kind: "connection", view: "connection", empty,
    failure: t("The connection could not be saved or loaded. Your draft is still here."),
    payload: draft => {
      const { name, title, kind, address, secret, allowPrivate, marking } = draft;
      return { name, title, kind, address, secret: secret ?? "", allowPrivate: !!allowPrivate, marking: marking ?? "" };
    },
  });
  if (!integrates(role("build"))) return <PageHeader title={t("Connections")} description={t("Only a builder or integrator can edit connections.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Connections")} description={query.isError ? t("The connection could not be loaded.") : t("Loading…")} />;
  const last = draft.last;
  const selected = connectionKinds.find(option => option.id === draft.kind) ?? connectionKinds[0];
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New connection")} description={t("Check confirms the saved connection before release.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "connection" })}>{t("Connections")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || !!inputs?.invalid || !dirty && id !== "new"} onClick={() => void perform(save)}>{t("Save connection")}</Button>
        <Button variant="primary" disabled={busy || !!inputs?.invalid || !!draft.requested} onClick={() => void perform(() => transition("check"))}>{draft.requested ? t("Checking…") : t("Check")}</Button>
        {role("build") === "builder" && <Button disabled={busy || !!inputs?.invalid} onClick={() => void perform(review)}>{t("Review and publish…")}</Button>}
        {draft.state === "ready" && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("retire"))}>{t("Retire")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("System")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Connection name")}<Input disabled={draft.state === "ready"} value={draft.name} placeholder="sap" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Connection title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Kind")}<Select value={draft.kind} onChange={(e) => patch({ kind: e.target.value as Api.Connection["kind"] })}>
          {connectionKinds.map(option => <option key={option.id} value={option.id}>{t(option.title)}</option>)}
        </Select></label>
        <div className={fieldClass}><label htmlFor={`${fieldID}-address`}>{t("Address")}</label><Input id={`${fieldID}-address`} aria-describedby={`${fieldID}-address-help`} value={draft.address} placeholder={selected.placeholder} onChange={(e) => patch({ address: e.target.value })} />
          <p id={`${fieldID}-address-help`} className="text-[11px] text-muted">{draft.kind === "postgres" ? t("A postgres:// URL without the password; the secret is the password.") : t("The service root; sources give a path or an entity set beneath it.")}</p></div>
        <div className={fieldClass}><label htmlFor={`${fieldID}-secret`}>{t("Secret name")}</label><Input id={`${fieldID}-secret`} aria-describedby={`${fieldID}-secret-help`} value={draft.secret ?? ""} placeholder="sap-reader" onChange={(e) => patch({ secret: e.target.value })} />
          <p id={`${fieldID}-secret-help`} className="text-[11px] text-muted">{t("Looked up by the host (PLATFORM_SECRETS_DIR or PLATFORM_SECRET_<NAME>). For http and OData its content is the Authorization header's value, such as Basic … or Bearer ….")}</p></div>
        <Checkbox checked={!!draft.allowPrivate} onChange={(allowPrivate) => patch({ allowPrivate })}>{t("Allow http and private addresses (on-premise systems)")}</Checkbox>
        <MarkingField value={draft.marking ?? ""} onChange={(marking) => patch({ marking: marking as Api.Connection["marking"] })} help={t("Everything read through it is at least this; datasets loaded from it are raised to it.")} />
      </Panel>
      <Panel title={t("Last check")} className="grid min-w-0 content-start gap-2">
        {!last ? <p className="text-xs text-muted">{t("Not checked yet.")}</p> : <>
          <p className="flex flex-wrap items-center gap-2 text-xs"><Tag label={last.ok ? t("Reachable") : t("Failed")} tone={last.ok ? "success" : "danger"} /><span>{new Date(last.at).toLocaleString()}</span>{last.detail && <span className="font-mono">{last.detail}</span>}</p>
          {last.error && <p className="text-xs text-danger">{last.error}</p>}
        </>}
      </Panel>
    </fieldset>
  </div>;
}
