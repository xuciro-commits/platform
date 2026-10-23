import { useHost, useReadQuery as useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Card, Dialog, Input, PageHeader, Select, Tag, t } from "@platform/ui";
import { useEffect, useRef, useState } from "react";

const blank = (): Api.CredentialWrite => ({ id: "", title: "", kind: "api-key", source: "managed", bindings: [{ consumer: "ai/", origin: "" }], expectedVersion: 0 });
export function CredentialEditor({ open, onClose, initial, onSaved }: { open: boolean; onClose: () => void; initial?: Api.CredentialWrite; onSaved?: (name: string) => void }) {
  const { client, decide } = useHost();
  const [draft, setDraft] = useState<Api.CredentialWrite>(blank);
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [pending, setPending] = useState(false);
  const saved = useRef<{ write: Api.CredentialWrite; key?: string } | undefined>(undefined);
  useEffect(() => { if (open) { setDraft(initial ?? blank()); setValue(""); setError(""); setPending(false); saved.current = undefined; } }, [open, initial]);
  const save = async () => {
    setBusy(true); setError("");
    try {
      let write = saved.current?.write;
      if (!write) {
        write = { ...draft, reference: draft.source === "deployment" ? draft.reference : undefined };
        if (write.source === "managed") {
          // This online call bypasses the durable outbox. Only the resulting
          // authenticated ciphertext becomes an ordinary decision.
          const secret = value; setValue("");
          const result = await client.call<Api.CredentialWrite & {error?:{message?:string}}>("POST", "/v1/credentials/seal", { ...write, value: secret });
          if (!result.ok) { setError(result.body?.error?.message ?? t("Credential encryption failed.")); return; }
          write = result.body;
        }
        saved.current = { write };
      }
      const ok = await decide("platform.credential.save", { type: "platform.credential", id: write.id }, write, {
        originalKey: saved.current?.key,
        onRefused: setError,
        onOutcome: entry => {
          if (saved.current) saved.current.key = entry.submission.idempotencyKey;
          setPending(entry.state === "SUBMISSION_STATE_UNKNOWN" || entry.state === "SUBMISSION_STATE_PENDING" || entry.state === "SUBMISSION_STATE_SENDING");
          if (entry.state === "SUBMISSION_STATE_REJECTED" || entry.state === "SUBMISSION_STATE_CONFLICT") saved.current = undefined;
        },
      });
      if (ok) { onSaved?.(write.id); onClose(); }
    } finally { setBusy(false); }
  };
  const changeBinding = (index: number, change: Partial<Api.CredentialBinding>) => setDraft(current => ({ ...current, bindings: current.bindings.map((binding, i) => i === index ? { ...binding, ...change } : binding) }));
  const field = (label: string, key: "id" | "title" | "reference", disabled = false) => <label className="grid gap-1">{t(label)}<Input aria-label={t(label)} value={draft[key] ?? ""} disabled={disabled || busy || pending} onChange={event => setDraft({ ...draft, [key]: event.target.value })} /></label>;
  return <Dialog open={open} onOpenChange={next => { if (!next && !busy) { setValue(""); onClose(); } }} title={t(draft.expectedVersion ? "Edit credential" : "Add credential")}>
    <div className="grid gap-3 text-sm">
      {field("Credential name", "id", draft.expectedVersion > 0)}{field("Title", "title")}
      <label className="grid gap-1">{t("Credential type")}<Select aria-label={t("Credential type")} value={draft.kind} disabled={busy || pending} onChange={event => setDraft({ ...draft, kind: event.target.value })}><option value="api-key">API key</option><option value="bearer">Bearer token</option><option value="password">{t("Password")}</option></Select></label>
      <label className="grid gap-1">{t("Credential source")}<Select aria-label={t("Credential source")} value={draft.source} disabled={busy || pending} onChange={event => setDraft({ ...draft, source: event.target.value })}><option value="managed">{t("Platform managed")}</option><option value="deployment">{t("Deployment reference")}</option></Select></label>
      {draft.source === "deployment" ? field("Deployment secret name", "reference") : <label className="grid gap-1">{t("Secret value")}<Input type="password" autoComplete="new-password" aria-label={t("Secret value")} value={value} disabled={busy || pending} onChange={event => setValue(event.target.value)} /><span className="text-xs text-muted">{t(draft.expectedVersion ? "Leave empty to keep the current value. Saved values cannot be displayed." : "The value is encrypted before saving and cannot be displayed later.")}</span></label>}
      <fieldset disabled={busy || pending} className="grid gap-2"><legend>{t("Allowed uses")}</legend>{draft.bindings.map((binding, index) => {
        const [kind, ...id] = binding.consumer.split("/");
        return <div key={index} className="grid gap-2 rounded border p-2">
          <label className="grid gap-1">{t("Consumer type")}<Select aria-label={t("Consumer type")} value={kind} onChange={event => changeBinding(index, { consumer: `${event.target.value}/${id.join("/")}` })}><option value="ai">{t("AI provider")}</option><option value="connection">{t("Connection")}</option><option value="endpoint">{t("Endpoint")}</option></Select></label>
          <label className="grid gap-1">{t("Consumer ID")}<Input aria-label={t("Consumer ID")} value={id.join("/")} onChange={event => changeBinding(index, { consumer: `${kind}/${event.target.value}` })} /></label>
          <label className="grid gap-1">{t("Allowed origin")}<Input aria-label={t("Allowed origin")} placeholder="https://api.example.com" value={binding.origin} onChange={event => changeBinding(index, { origin: event.target.value })} /></label>
          {draft.bindings.length > 1 && <Button size="sm" onClick={() => setDraft({ ...draft, bindings: draft.bindings.filter((_, i) => i !== index) })}>{t("Remove use")}</Button>}
        </div>;
      })}{draft.bindings.length < 32 && <Button size="sm" onClick={() => setDraft({ ...draft, bindings: [...draft.bindings, { consumer: "connection/", origin: "" }] })}>{t("Add use")}</Button>}</fieldset>
      {error && <p role="alert" className="text-sm text-[var(--tone-danger)]">{error}</p>}
      {pending && <p className="text-xs text-muted">{t("The result is pending. Retry the original encrypted request before editing.")}</p>}
      <div className="flex justify-end gap-2"><Button disabled={busy} onClick={() => { setValue(""); onClose(); }}>{t("Cancel")}</Button><Button variant="primary" disabled={busy || !draft.id || !draft.title} onClick={() => void save()}>{t(pending ? "Retry" : "Save")}</Button></div>
    </div>
  </Dialog>;
}

export function Credentials() {
  const { decide } = useHost();
  const catalog = useRead<Api.CredentialCatalog>("/v1/credentials");
  const [editing, setEditing] = useState<Api.CredentialWrite>();
  return <>
    <PageHeader title={t("Credentials")} help={t("Manage encrypted credentials and deployment references. Applications use authorized bindings; saved values are never displayed.")} actions={<Button variant="primary" onClick={() => setEditing(blank())}>{t("Add credential")}</Button>} />
    {catalog.error && <p role="alert">{String(catalog.error)}</p>}
    {catalog.data && !catalog.data.managedAvailable && <p className="mb-3 text-sm text-muted">{t("Platform encryption is not configured. Deployment references remain available.")}</p>}
    <div className="grid gap-2">{catalog.data?.credentials.map(credential => <Card key={credential.id} className="grid gap-2 p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2"><strong>{credential.title}</strong><span className="font-mono text-xs">{credential.id}</span><Tag label={t(credential.state)} tone={credential.state === "active" ? "success" : "neutral"} /><Tag label={credential.kind} tone="info" /><span className="text-xs text-muted">{t(credential.source === "managed" ? "Platform managed" : "Deployment reference")} · {t("Version")} {credential.version}</span></div>
      <div className="grid gap-1 font-mono text-xs text-muted">{credential.bindings.map((binding, index) => <span key={index}>{binding.consumer} · {binding.origin}</span>)}</div>
      <div className="flex gap-2"><Button size="sm" onClick={() => setEditing({ id: credential.id, title: credential.title, kind: credential.kind, source: credential.source, reference: credential.reference, bindings: credential.bindings, expectedVersion: credential.version })}>{t("Edit")}</Button>{credential.state === "active" && <Button size="sm" variant="danger" onClick={() => void decide("platform.credential.disable", { type: "platform.credential", id: credential.id }, { expectedVersion: credential.version })}>{t("Disable")}</Button>}</div>
    </Card>)}</div>
    {!catalog.data?.credentials.length && !catalog.error && <p className="text-sm text-muted">{t("No credentials.")}</p>}
    <CredentialEditor open={!!editing} initial={editing} onClose={() => setEditing(undefined)} />
  </>;
}
