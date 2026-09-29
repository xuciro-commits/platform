// A builder-only review and immutable candidate save. Saving does not install
// a definition, activate a release or run a preview sandbox.
import { useHost, useReadQuery } from "@platform/app";
import { Button, Card, PageHeader, Select, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useState } from "react";

type Kind = "object" | "page" | "app";
type Record = { id: string; title: string; name: string; state: string };
const kinds: { kind: Kind; type: string; label: string }[] = [
  { kind: "object", type: "build.object", label: "Objects" },
  { kind: "page", type: "build.page", label: "Pages" },
  { kind: "app", type: "build.app", label: "Applications" },
];

export function ReleaseReview() {
  const { client, role } = useHost();
  const [kind, setKind] = useState<Kind>("object");
  const [id, setId] = useState("");
  const [review, setReview] = useState<Api.ReleasePreview>();
  const [candidateKey, setCandidateKey] = useState("");
  const [savedID, setSavedID] = useState("");
  const [activeID, setActiveID] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const selected = kinds.find((item) => item.kind === kind)!;
  const query = useReadQuery<{ records: Record[] }>(`/v1/records/${selected.type}?limit=1000`);
  const records = query.data?.records ?? [];
  if (role("build") !== "builder") {
    return <PageHeader title={t("Release review")} description={t("Only a builder can review complete release definitions.")} />;
  }
  const inspect = async () => {
    setBusy(true);
    setError("");
    setReview(undefined);
    setSavedID("");
    setActiveID("");
    try {
      const result = await client.call<Api.ReleasePreview>("POST", "/v1/releases/preview", { kind, id });
      if (!result.ok) setError((result.body as Api.ReleasePreview & { error?: string }).error ?? t("Release review could not be loaded."));
      else {
        setReview(result.body);
        setCandidateKey(crypto.randomUUID());
      }
    } catch {
      setError(t("Release review could not be loaded."));
    } finally {
      setBusy(false);
    }
  };
  const save = async () => {
    if (!review?.candidateId || !candidateKey) return;
    setBusy(true);
    setError("");
    try {
      const result = await client.call<Api.ReleaseSaved>("POST", "/v1/releases/candidates",
        { kind, id, candidateId: review.candidateId, key: candidateKey } satisfies Api.ReleaseSaveRequest);
      if (!result.ok) setError((result.body as Api.ReleaseSaved & { error?: string }).error ?? t("Candidate could not be saved."));
      else setSavedID(result.body.id);
    } catch {
      setError(t("Candidate could not be saved."));
    } finally {
      setBusy(false);
    }
  };
  // Activation succeeds only when operators already run exactly these bytes.
  const activate = async () => {
    if (!savedID) return;
    setBusy(true);
    setError("");
    try {
      const result = await client.call<Api.ReleaseActive>("POST", "/v1/releases/active",
        { candidateId: savedID, key: crypto.randomUUID() } satisfies Api.ReleaseActivateRequest);
      if (!result.ok) setError((result.body as Api.ReleaseActive & { error?: string }).error ?? t("Release could not be activated."));
      else setActiveID(result.body.id);
    } catch {
      setError(t("Release could not be activated."));
    } finally {
      setBusy(false);
    }
  };
  const changed = (label: string, refs?: Api.AssetRef[]) => (
    <div>
      <h3 className="text-xs font-semibold">{t(label)} · {refs?.length ?? 0}</h3>
      <ul className="mt-1 grid gap-1 font-mono text-xs">
        {(refs ?? []).map((ref) => <li key={`${ref.app}/${ref.kind}/${ref.name}`}>{ref.app}/{ref.kind}/{ref.name}</li>)}
      </ul>
    </div>
  );
  return <div className="grid gap-3">
    <PageHeader title={t("Release review")} description={t("Compare a saved draft, then save its exact candidate bytes. Saving does not activate it for operators.")} />
    <Card className="grid gap-3 p-3">
      <label className="grid gap-1 text-xs">{t("Definition kind")}
        <Select value={kind} onChange={(event) => { setKind(event.target.value as Kind); setId(""); setReview(undefined); setSavedID(""); setActiveID(""); setError(""); }}>
          {kinds.map((item) => <option key={item.kind} value={item.kind}>{t(item.label)}</option>)}
        </Select>
      </label>
      <label className="grid gap-1 text-xs">{t("Saved draft")}
        <Select value={id} onChange={(event) => { setId(event.target.value); setReview(undefined); setSavedID(""); setActiveID(""); setError(""); }}>
          <option value="">{t("Choose a saved draft")}</option>
          {records.map((record) => <option key={record.id} value={record.id}>{record.title || record.name} · {record.state}</option>)}
        </Select>
      </label>
      {query.isError && <p role="alert" className="text-sm text-danger">{t("Release review could not be loaded.")}</p>}
      <Button disabled={!id || busy} onClick={inspect}>{busy ? t("Checking…") : t("Check draft and dependencies")}</Button>
    </Card>
    {error && <Card className="p-3 text-sm text-danger" role="alert">{error}</Card>}
    {review && <Card className="grid gap-3 p-3" aria-live="polite">
      <div className="text-sm font-semibold">{review.diagnostic ? t("Candidate rejected") : t("Candidate ready for review")}</div>
      {review.currentId && <p className="break-all text-xs">{t("Installed candidate")}: <code>{review.currentId}</code></p>}
      {review.candidateId && <p className="break-all text-xs">{t("Draft candidate")}: <code>{review.candidateId}</code></p>}
      {review.diagnostic && <p role="alert" className="text-sm text-danger">{review.diagnostic}</p>}
      {!review.diagnostic && <div className="grid gap-3 sm:grid-cols-3">
        {changed("Added", review.added)}
        {changed("Changed", review.changed)}
        {changed("Removed", review.removed)}
      </div>}
      {!review.diagnostic && review.candidateId && <div className="grid gap-2">
        <Button disabled={busy || Boolean(savedID)} onClick={save}>{busy ? t("Saving…") : t("Save immutable candidate")}</Button>
        {savedID && !activeID && <p className="break-all text-sm" role="status">{t("Candidate saved; not active for operators.")} <code>{savedID}</code></p>}
        {savedID && <Button variant="secondary" disabled={busy || activeID === savedID} onClick={activate}>{t("Activate release")}</Button>}
        {activeID && <p className="break-all text-sm" role="status">{t("Release active for operators.")} <code>{activeID}</code></p>}
      </div>}
    </Card>}
  </div>;
}
