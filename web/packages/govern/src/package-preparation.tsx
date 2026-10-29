import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Checkbox, Disclosure, PageHeader, Panel, PropertyList, Tag, t, useWorkspace } from "@platform/ui";
import { useState } from "react";

function PreparationReadProblem({ error, retry }: { error: Error; retry: () => void }) {
  const { can } = useHost(), { open } = useWorkspace();
  const forbidden = /HTTP 403\b/.test(error.message);
  return <div role="alert" className="grid gap-2 text-sm text-danger">
    <p>{t(forbidden ? "This account cannot prepare application drafts in this workspace. Continue with a builder account, or ask an administrator to assign the builder role." : "Application requirements could not be loaded.")}</p>
    {forbidden ? <Disclosure summary={t("Technical details")}><p className="break-words text-xs">{error.message}</p></Disclosure> : <p className="break-words">{error.message}</p>}
    <div className="flex flex-wrap gap-2"><Button size="sm" onClick={retry}>{t("Retry")}</Button>{forbidden && can("platform.member.grant") && <Button size="sm" onClick={() => open({ view: "members" })}>{t("Members")}</Button>}</div>
  </div>;
}

export function PackageRequirements({ id, version }: { id: string; version: string }) {
  const { me } = useHost();
  const read = useReadQuery<Api.PackagePreparation>(`/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}`, undefined, !!version);
  if (read.isError) return <PreparationReadProblem error={read.error} retry={() => void read.refetch()} />;
  if (!read.data) return <p role="status" className="text-sm text-muted">{t("Loading application requirements…")}</p>;
  const plan = read.data;
  return <div className="grid gap-2 text-sm" aria-label={t("Before using this package")}>
    {me.tenantId === "system" && <p role="status">{t("This is the system self-hosting workspace. To publish a business application, switch to a business workspace using the account menu. System definitions require an approved Platform change.")}</p>}
    <p className="font-medium">{t("Before using this package")}</p>
    <p>{t("Applications: {names}", { names: plan.applications.map(app => app.title).join(", ") })}</p>
    <p>{plan.connections.length ? t("Configure {count} external connections in this environment.", { count: plan.connections.length }) : t("No external connection is required.")}</p>
    {plan.ai.length > 0 && <p>{t("Configure a model after installation to use AI features. Evaluate the actual configuration when needed.")}</p>}
    <p>{t("Installation makes the package available. Prepare and activate its applications before using them.")}</p>
  </div>;
}

export function PackagePreparation({ id, version }: { id: string; version: string }) {
  const host = useHost(), { client, decide, can } = host, { open } = useWorkspace();
  const systemScope = host.me.tenantId === "system";
  const query = useReadQuery<Api.PackagePreparation>(`/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}`, undefined, !!version);
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
  const missingDrafts = !!plan?.applications.some(app => !app.draftId);
  const allPublished = !!plan?.applications.length && plan.applications.every(app => app.published);
  const setupEntries = plan?.setup?.steps.map((step, index) => setupRun ? host.outbox.find(entry => entry.submission.tenantId === client.connection.tenant && entry.submission.principalId === client.connection.principal && entry.submission.schema?.name === step.schema && entry.submission.target?.id === step.target?.id && entry.submission.idempotencyKey?.endsWith(`:${setupRun}:${index}`)) : undefined) ?? [];
  const setupComplete = setupEntries.length > 0 && setupEntries.every(entry => entry?.state === "SUBMISSION_STATE_CONFIRMED");
  const missingSetupRoles = !!plan?.setup?.roles.some(required => !plan.roles.some(item => item.app === required.app && item.role === required.role && item.available && item.held));
  const setupStatus = (state?: string) => state === "SUBMISSION_STATE_CONFIRMED" ? "Completed" : ["SUBMISSION_STATE_PENDING", "SUBMISSION_STATE_SENDING", "SUBMISSION_STATE_UNKNOWN"].includes(state ?? "") ? "Awaiting confirmation" : state ? "Needs attention" : "Not started";
  return <div className="grid min-w-0 grid-cols-1 gap-5 break-words">
    <PageHeader title={t("Prepare package applications")} description={`${id} · ${version}`} actions={<div className="flex flex-wrap gap-2"><Button disabled={busy} onClick={() => void query.refetch()}>{t("Refresh readiness")}</Button><Button onClick={() => open({ view: "apps" })}>{t("Packages")}</Button></div>} />
    {systemScope && <Panel role="status" className="grid gap-2 text-sm">
      <p>{t("This is the system self-hosting workspace. To publish a business application, switch to a business workspace using the account menu. System definitions require an approved Platform change.")}</p>
      <div><Button onClick={() => open({ view: "self-hosting" })}>{t("Open self-hosting")}</Button></div>
    </Panel>}
    {query.isError && <PreparationReadProblem error={query.error} retry={() => void query.refetch()} />}
    {query.isLoading && <p role="status">{t("Loading application requirements…")}</p>}
    {error && <Panel role="alert" className="break-words text-danger">{error}</Panel>}
    {plan && <>
      <Panel className="grid min-w-0 grid-cols-1 gap-3" title={t("Install and create drafts")}>
        <p className="text-sm text-muted">{t("Follow the remaining steps here. Editing definitions is optional; using the application starts after activation.")}</p>
        {!plan.installed && <div className="flex flex-wrap items-center gap-2"><p className="text-sm">{t(plan.installationState === "draining" ? "Finish uninstalling this package in Packages before preparing applications." : "Install this exact package version to begin.")}</p>{plan.installationState !== "draining" && (can(plan.installationState === "active" ? "platform.package.upgrade" : "platform.package.install") ? <Button variant="primary" disabled={busy} onClick={() => void install()}>{plan.installationState === "active" ? t("Switch to {v}", { v: version }) : t("Install package")}</Button> : <p>{t("Ask an administrator to install this package.")}</p>)}</div>}
        <p className="text-sm text-muted">{t("Existing drafts keep your edits. Changing the package version does not overwrite them.")}</p>
        {plan.installed && missingDrafts && <Button variant="primary" disabled={busy} onClick={() => void create()}>{t("Create application drafts")}</Button>}
        {plan.applications.map(app => <div key={app.ref.name} className="flex flex-wrap items-center gap-2 border-t border-border pt-3">
          <strong className="mr-auto">{app.title}</strong><Tag label={t(app.state === "withdrawn" ? "Entry withdrawn" : app.published ? "Published" : app.draftId ? "Draft" : "Not created")} />
          {app.draftId && <Button disabled={busy} onClick={() => open({ view: "project", params: { id: app.draftId! } })}>{t("Edit application")}</Button>}
        </div>)}
      </Panel>
      <Panel title={t("Publish applications")} className="grid min-w-0 grid-cols-1 gap-3">
        <p className="text-sm text-muted">{t("Publish the complete application independently. Configure external services afterward for the features you use.")}</p>
        <div className="grid gap-2 border-t border-border pt-3">
          <p className="text-sm text-muted">{t("Review the exact candidate and its checks before activation. Return here and refresh readiness after publishing.")}</p>
          {plan.applications.map(app => <div key={app.ref.name} className="flex flex-wrap items-center gap-2 text-sm"><span className="mr-auto font-medium">{app.title}</span><Tag label={t(app.published ? "Published" : "Not published")} />{app.draftId && <Button variant={app.published ? "default" : "primary"} disabled={busy || !plan.installed} onClick={() => open(systemScope ? { view: "self-hosting" } : { view: "release-review", params: { kind: "app", id: app.draftId!, application: app.draftId!, applicationName: app.ref.name } })}>{t(systemScope ? "Open self-hosting" : app.published ? "Review release" : "Publish application")}</Button>}</div>)}
        </div>
      </Panel>
      <Panel title={t("Configure services when needed")} className="grid min-w-0 grid-cols-1 gap-3">
        {!plan.connections.length && !plan.ai.length && <p className="text-sm">{t("This package has no external service requirements.")}</p>}
        {plan.connections.map(connection => <div key={connection.ref.name} className="grid gap-2 border-b border-border pb-3 text-sm">
          <div className="flex flex-wrap gap-2"><strong>{connection.title || connection.ref.name}</strong><Tag label={t(connection.ready ? "Ready" : "Needs configuration")} tone={connection.ready ? "success" : "warning"} /></div>
          <p>{t(connection.credential ? "Provide this environment's service address and credential, then check the connection." : "Provide this environment's service address, then check the connection.")}</p>
          <div><Button disabled={!connection.draftId} onClick={() => open({ view: "connection", params: { id: connection.draftId! } })}>{t("Configure connection")}</Button></div>
        </div>)}
        {plan.ai.map(item => <div key={`${item.ref.kind}/${item.ref.name}`} className="flex flex-wrap items-center gap-2 text-sm"><span className="mr-auto">{item.ref.name}{item.model ? ` · ${item.model}` : ""}</span><Tag label={t(item.ready ? "Model available" : "Needs a model")} tone={item.ready ? "success" : "warning"} />{item.problem && <p className="w-full text-xs text-danger">{item.problem}</p>}{item.ref.kind === "evaluation-set" && item.draftId && <Button onClick={() => open({ view: "evaluation-set", params: { id: item.draftId! } })}>{t("Configure evaluation")}</Button>}</div>)}
        {plan.ai.length > 0 && <div className="flex flex-wrap gap-2"><Button onClick={() => open({ view: "ai-configuration",params:{fromPackage:id,fromVersion:version} })}>{t("Manage AI configuration")}</Button><p className="text-xs text-muted">{t("Configure models and run evaluations explicitly when you need the AI features.")}</p></div>}
      </Panel>
      <Panel title={t("Assign business access")} className="grid min-w-0 grid-cols-1 gap-3">
        <p className="text-sm">{t("Grant business roles explicitly to the people who will use the application. Package installation does not grant access.")}</p>
        {!plan.roles.length && <p className="text-sm">{t("This package declares no business role requirements.")}</p>}
        <ul className="grid gap-2 text-sm">{plan.roles.map(item => <li key={`${item.app}/${item.role}`} className="flex flex-wrap items-center gap-2"><span className="mr-auto">{item.app} · {item.role} · {t(item.held ? "You hold this role" : "Not held by you")}</span>{!item.available ? <p className="w-full text-muted">{t("Publish the application before assigning this role.")}</p> : !item.held && can("platform.member.grant") && <Button size="sm" disabled={busy} onClick={() => void perform(async () => { await decide("platform.member.grant", { type: "platform.member", id: host.me.principalId }, { app: item.app, role: item.role }, { onRefused: setError }); })}>{t("Grant this role to me")}</Button>}</li>)}</ul>
        {plan.roles.some(item => item.available) && (can("platform.member.grant") ? <div><Button disabled={busy} onClick={() => open({ view: "members" })}>{t("Assign business roles")}</Button></div> : plan.roles.some(item => item.available && !item.held) ? <p className="text-sm text-muted">{t("Ask an administrator to assign the required business roles, then refresh readiness.")}</p> : null)}
      </Panel>
      <Panel title={t("Initial data and application access")} className="grid min-w-0 grid-cols-1 gap-3">
        {!allPublished && <p className="text-sm text-muted">{t("Activate all package applications before running initial data requests.")}</p>}
        {plan.setup?.steps.length ? <div className="grid gap-3"><h3 className="text-sm font-medium">{t("Optional initial data")}</h3>
          <p className="text-sm">{t("Initial data is optional. Review and run these requests only when you need the sample data. Application access does not require running them.")}</p>
          <ol className="grid gap-2 text-sm">{plan.setup.steps.map((step, index) => <li key={step.name}><Disclosure summary={<span className="flex flex-wrap items-center gap-2"><span>{index + 1}. {step.name}</span><Tag label={t(setupStatus(setupEntries[index]?.state))} /></span>}><PropertyList items={[[t("Action"), step.schema], [t("Target"), step.target?.id ?? ""]]} /><pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs">{step.payload}</pre>{setupEntries[index]?.reason && <p className="text-danger">{setupEntries[index]!.reason}</p>}</Disclosure></li>)}</ol>
          <Checkbox checked={reviewedSetup} onChange={setReviewedSetup}>{t("I reviewed the initial data requests.")}</Checkbox>
          {!setupComplete && missingSetupRoles && <p className="text-sm text-muted">{t("Your initialization roles must be available and assigned before continuing.")}</p>}
          {setupRun && !setupComplete && <p className="text-sm text-muted">{t("Continue checks the original requests. Confirmed steps are skipped; unanswered steps keep their original inputs and keys.")}</p>}
          {setupComplete ? <p role="status">{t("Initial data is ready.")}</p> : <Button disabled={busy || !plan.installed || !allPublished || !reviewedSetup || missingSetupRoles} onClick={() => void initialize()}>{t(setupRun ? "Continue initialization" : "Create initial data")}</Button>}
        </div> : <p className="text-sm">{t("This package requires no initial data requests.")}</p>}
        <div className="grid gap-2 border-t border-border pt-3">{plan.applications.map(app => app.published && app.pages.length > 0 && app.state !== "withdrawn" ? <div key={app.ref.name} className="flex flex-wrap items-center gap-2"><span className="mr-auto font-medium text-sm">{app.title}</span><Button variant="primary" disabled={busy} onClick={() => openApp(app)}>{t("Open application")}</Button></div> : null)}</div>
      </Panel>
    </>}
  </div>;
}
