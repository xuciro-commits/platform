import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Dialog, Notice, PropertyList, t } from "@platform/ui";
import { useState } from "react";

export function PackageOwnership({ id, version, onClose }: { id: string; version: string; onClose: () => void }) {
  const { decide } = useHost();
  const query = useReadQuery<Api.PackageOwnership>(`/v1/packages/${encodeURIComponent(id)}/ownership?version=${encodeURIComponent(version)}`);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const plan = query.data;
  const transfer = async () => {
    if (!plan || busy) return;
    setBusy(true); setError("");
    try {
      if (plan.createProject && !await decide("build.app.create", { type: "build.app", id: plan.project }, { name: plan.name, title: plan.title }, { quiet: true, onRefused: setError })) return;
      if (!await decide("build.resource.rehome", { type: "build.resource", id: plan.entries[0]!.id }, { entries: plan.entries.map(({ id, project, managedBy, version }) => ({ id, project, managedBy, version })) }, { quiet: true, onRefused: setError })) return;
      await query.refetch();
    } finally { setBusy(false); }
  };
  return <Dialog open title={t("Transfer resource ownership")} onOpenChange={open => { if (!open && !busy) onClose(); }} wide>
    <div className="grid gap-3 text-sm">
      <p>{t("Transfer existing definitions to this product before installing it. Resource identities, business data, connections and history stay. Install each product separately after transferring all shared resources; retire the previous product last.")}</p>
      {query.isLoading && <p role="status">{t("Loading…")}</p>}
      {query.isError && <Notice tone="danger" message={query.error.message} />}
      {plan && <><PropertyList items={[[t("Product version"), `${plan.id}@${plan.version}`], [t("Project"), plan.project]]} />
        {plan.problems.map(problem => <Notice key={problem} tone="danger" message={t(problem)} />)}
        <ul aria-label={t("Resources to transfer")} className="max-h-80 overflow-auto divide-y divide-border">{plan.entries.map(entry => <li key={entry.id} className="grid gap-1 py-2 text-xs"><strong>{entry.ref.name}</strong><span className="text-muted">{entry.from || t("Customer project")} → {entry.managedBy}</span></li>)}</ul>
        {!plan.entries.length && <p role="status">{t("Ownership is ready. Continue with installation in Marketplace.")}</p>}
      </>}
      {error && <Notice role="alert" tone="danger" message={error} />}
      <div className="flex justify-end gap-2"><Button disabled={busy} onClick={onClose}>{t("Close")}</Button><Button variant="primary" disabled={busy || !plan?.entries.length || !!plan.problems.length} onClick={() => void transfer()}>{t("Transfer ownership")}</Button></div>
    </div>
  </Dialog>;
}
