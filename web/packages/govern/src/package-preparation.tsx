import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Checkbox, Disclosure, PageHeader, Panel, PropertyList, Tag, t, useWorkspace } from "@platform/ui";
import { useState } from "react";

export function PackageRequirements({ id, version }: { id: string; version: string }) {
  const { role } = useHost();
  const read = useReadQuery<Api.PackagePreparation>(`/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}`, undefined, !!version && role("build") === "builder");
  if (role("build") !== "builder") return <p className="text-sm">{t("A builder prepares the applications after package installation.")}</p>;
  if (read.isError) return <p role="alert" className="text-sm text-danger">{t("Application requirements could not be loaded.")}</p>;
  if (!read.data) return <p role="status" className="text-sm text-muted">{t("Loading application requirements…")}</p>;
  const plan = read.data;
  return <div className="grid gap-2 text-sm" aria-label={t("Before using this package")}>
    <p className="font-medium">{t("Before using this package")}</p>
    <p>{t("Applications: {names}", { names: plan.applications.map(app => app.title).join(", ") })}</p>
    <p>{plan.connections.length ? t("Configure {count} external connections in this environment.", { count: plan.connections.length }) : t("No external connection is required.")}</p>
    {plan.ai.length > 0 && <p>{t("AI features require an available model and passing release evaluations.")}</p>}
    <p>{t("Installation makes the package available. Prepare and activate its applications before using them.")}</p>
  </div>;
}

export function PackagePreparation({ id, version }: { id: string; version: string }) {
  const host = useHost(), { client, decide, can, role } = host, { open } = useWorkspace();
  const query = useReadQuery<Api.PackagePreparation>(`/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}`, undefined, role("build") === "builder");
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [reviewedSetup, setReviewedSetup] = useState(false);
  const scope = `package-task:${client.connection.server}:${client.connection.tenant}:${client.connection.principal}:${id}@${version}`;
  const [setupRun, setSetupRun] = useState(() => localStorage.getItem(`${scope}:setup`) ?? "");
  const perform = async (work: () => Promise<void>) => { if (busy) return; setBusy(true); setError(""); try { await work(); await query.refetch(); } catch (e) { setError(e instanceof Error ? e.message : t("The operation could not be completed.")); } finally { setBusy(false); } };
  const install = () => perform(async () => {
    const schema = query.data?.installationState === "active" ? "platform.package.upgrade" : "platform.package.install";
    const accepted = await decide(schema, { type: "platform.package", id }, { id, version }, { onRefused: setError });
    if (!accepted) return;
    await query.refetch();
  });
  const create = () => perform(async () => {
    const storage = `${scope}:create`, key = localStorage.getItem(storage) ?? crypto.randomUUID();
    localStorage.setItem(storage, key);
    const result = await client.call<Api.PackageDraftResult>("POST", `/v1/packages/${encodeURIComponent(id)}/drafts`, { version, key });
    if (!result.ok) throw Error(apiErrorMessage(result.body) ?? t("Application drafts could not be created."));
    localStorage.removeItem(storage);
  });
  const plan = query.data;
  const initialize = () => perform(async () => {
    if (!plan?.setup || !reviewedSetup) return;
    const run = setupRun || crypto.randomUUID();
    localStorage.setItem(`${scope}:setup`, run); setSetupRun(run);
    const digest = [...new Uint8Array(await crypto.subtle.digest("SHA-256", new TextEncoder().encode(JSON.stringify(plan.setup))))].map(b => b.toString(16).padStart(2, "0")).join("").slice(0, 24);
    await client.refreshDeclarations();
    for (let index = 0; index < plan.setup.steps.length; index++) {
      const step = plan.setup.steps[index]!, key = `setup:${digest}:${run}:${index}`, payload = JSON.parse(step.payload);
      const existing = client.authorities.outbox.find(entry => entry.submission.tenantId === client.connection.tenant && entry.submission.principalId === client.connection.principal && entry.submission.idempotencyKey === key);
      if (existing?.state === "SUBMISSION_STATE_CONFIRMED") continue;
      const action = host.catalog.find(action => action.schema === step.schema);
      if (!action) throw Error(t("Initialization step {name} needs an action unavailable to your current role.", { name: step.name }));
      const target = { type: step.target!.type!, id: step.target!.id! };
      if (!existing) client.draft(step.schema, target, payload, [], undefined, action.schemaVersion, undefined, key);
      const accepted = await decide(step.schema, target, payload, { originalKey: key, quiet: true, onRefused: reason => setError(t("Initialization step {name}: {reason}", { name: step.name, reason })) });
      if (!accepted) return;
    }
  });
  const openApp = (app: Api.PackageApplication) => open({ view: "page", params: { app: app.ref.app, kind: "page", name: app.pages[0]!, application: `${app.ref.app}:${app.ref.name}` } });
  const missingConfiguration = !!plan && (plan.connections.some(connection => !connection.ready) || plan.ai.some(item => !item.ready));
  const missingDrafts = !!plan?.applications.some(app => !app.draftId);
  const allPublished = !!plan?.applications.length && plan.applications.every(app => app.published);
  const setupStates = plan?.setup?.steps.map((_, index) => host.outbox.find(entry => entry.submission.idempotencyKey?.endsWith(`:${setupRun}:${index}`))?.state) ?? [];
  const setupComplete = setupStates.length > 0 && setupStates.every(state => state === "SUBMISSION_STATE_CONFIRMED");
  return <div className="grid min-w-0 grid-cols-1 gap-5 break-words">
    <PageHeader title={t("Prepare package applications")} description={`${id} · ${version}`} actions={<div className="flex flex-wrap gap-2"><Button disabled={busy} onClick={() => void query.refetch()}>{t("Refresh readiness")}</Button><Button onClick={() => open({ view: "apps" })}>{t("Packages")}</Button></div>} />
    {query.isError && <Panel role="alert" className="grid gap-2"><p>{t("Application requirements could not be loaded.")}</p><Button onClick={() => void query.refetch()}>{t("Retry")}</Button></Panel>}
    {query.isLoading && <p role="status">{t("Loading application requirements…")}</p>}
    {error && <Panel role="alert" className="break-words text-danger">{error}</Panel>}
    {plan && <>
      <Panel className="grid min-w-0 grid-cols-1 gap-3" title={t("Applications in this package")}>
        <p className="text-sm text-muted">{t("Follow the remaining steps here. Editing definitions is optional; using the application starts after activation.")}</p>
        {!plan.installed && <div className="flex flex-wrap items-center gap-2"><p className="text-sm">{t(plan.installationState === "draining" ? "Finish uninstalling this package in Packages before preparing applications." : "Install this exact package version to begin.")}</p>{plan.installationState !== "draining" && (can(plan.installationState === "active" ? "platform.package.upgrade" : "platform.package.install") ? <Button variant="primary" disabled={busy} onClick={() => void install()}>{plan.installationState === "active" ? t("Switch to {v}", { v: version }) : t("Install package")}</Button> : <p>{t("Ask an administrator to install this package.")}</p>)}</div>}
        <p className="text-sm text-muted">{t("Existing drafts keep your edits. Changing the package version does not overwrite them.")}</p>
        {plan.installed && missingDrafts && <Button variant="primary" disabled={busy} onClick={() => void create()}>{t("Create application drafts")}</Button>}
        {plan.applications.map(app => <div key={app.ref.name} className="flex flex-wrap items-center gap-2 border-t border-border pt-3">
          <strong className="mr-auto">{app.title}</strong><Tag label={t(app.state === "withdrawn" ? "Entry withdrawn" : app.published ? "Published" : app.draftId ? "Draft" : "Not created")} />
          {app.draftId && <><Button disabled={busy} onClick={() => open({ view: "project", params: { id: app.draftId! } })}>{t("Edit application")}</Button><Button variant={app.published ? "default" : "primary"} disabled={busy || missingConfiguration} onClick={() => open({ view: "release-review", params: { kind: "app", id: app.draftId!, application: app.draftId!, applicationName: app.ref.name } })}>{t(app.published ? "Review release" : "Publish application")}</Button></>}
          {app.published && app.pages.length > 0 && app.state !== "withdrawn" && <Button variant="primary" onClick={() => openApp(app)}>{t("Open application")}</Button>}
        </div>)}
      </Panel>
      <Panel title={t("Connections and AI")} className="grid min-w-0 grid-cols-1 gap-3">
        {!plan.connections.length && !plan.ai.length && <p className="text-sm">{t("No external configuration is required. Continue to publication.")}</p>}
        {plan.connections.map(connection => <div key={connection.ref.name} className="grid gap-2 border-b border-border pb-3 text-sm">
          <div className="flex flex-wrap gap-2"><strong>{connection.title || connection.ref.name}</strong><Tag label={t(connection.ready ? "Ready" : "Needs configuration")} tone={connection.ready ? "success" : "warning"} /></div>
          <p>{t(connection.credential ? "Provide this environment's service address and credential, then check the connection." : "Provide this environment's service address, then check the connection.")}</p>
          <div><Button disabled={!connection.draftId} onClick={() => open({ view: "connection", params: { id: connection.draftId! } })}>{t("Configure connection")}</Button></div>
        </div>)}
        {plan.ai.map(item => <div key={`${item.ref.kind}/${item.ref.name}`} className="flex flex-wrap items-center gap-2 text-sm"><span className="mr-auto">{item.ref.name}{item.model ? ` · ${item.model}` : ""}</span><Tag label={t(item.ready ? "Model available" : "Needs a model")} tone={item.ready ? "success" : "warning"} />{item.ref.kind === "evaluation-set" && item.draftId && <Button onClick={() => open({ view: "evaluation-set", params: { id: item.draftId! } })}>{t("Configure evaluation")}</Button>}</div>)}
        {plan.ai.length > 0 && <div className="flex flex-wrap gap-2"><Button onClick={() => open({ view: "ai-providers" })}>{t("Providers and models")}</Button><Button onClick={() => open({ view: "app-settings" })}>{t("Choose application and agent models")}</Button><p className="text-xs text-muted">{t("Run the required evaluations in release review after saving the candidate.")}</p></div>}
      </Panel>
      <Panel title={t("Access and initial data")} className="grid min-w-0 grid-cols-1 gap-3">
        <p className="text-sm">{t("Grant business roles explicitly to the people who will use the application. Package installation does not grant access.")}</p>
        <ul className="grid gap-2 text-sm">{plan.roles.map(item => <li key={`${item.app}/${item.role}`} className="flex flex-wrap items-center gap-2"><span className="mr-auto">{item.app} · {item.role} · {t(item.held ? "You hold this role" : "Not held by you")}</span>{!item.held && can("platform.member.grant") && <Button size="sm" disabled={busy} onClick={() => void perform(async () => { await decide("platform.member.grant", { type: "platform.member", id: host.me.principalId }, { app: item.app, role: item.role }, { onRefused: setError }); })}>{t("Grant this role to me")}</Button>}</li>)}</ul>
        <div><Button onClick={() => open({ view: "members" })}>{t("Assign business roles")}</Button></div>
        {plan.setup && <div className="grid gap-3 border-t border-border pt-3">
          <p className="text-sm">{t("After activation, review and run this package's initial data requests. Each step uses your current permissions; existing business records are not overwritten.")}</p>
          <ol className="grid gap-2 text-sm">{plan.setup.steps.map((step, index) => <li key={step.name}><Disclosure summary={`${index + 1}. ${step.name}`}><PropertyList items={[[t("Action"), step.schema], [t("Target"), step.target?.id ?? ""]]} /><pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs">{step.payload}</pre></Disclosure></li>)}</ol>
          <Checkbox checked={reviewedSetup} onChange={setReviewedSetup}>{t("I reviewed the initial data requests.")}</Checkbox>
          {setupComplete ? <p role="status">{t("Initial data is ready.")}</p> : <Button disabled={busy || !allPublished || !reviewedSetup || plan.setup.roles.some(required => !plan.roles.find(role => role.app === required.app && role.role === required.role)?.held)} onClick={() => void initialize()}>{t(setupRun ? "Continue initialization" : "Create initial data")}</Button>}
        </div>}
      </Panel>
    </>}
  </div>;
}
