// The Platform: the system's own changes, files and releases. This is the
// human entry of self-hosting: a publisher reads a proposed change, approves
// it, watches the build the updater reports, activates the release and may
// cancel inside the window. Everything here is an ordinary decision on the
// system tenant, through the same host every other app uses.
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, Card, Input, PageHeader, Tag, Textarea, notify, t } from "@platform/ui";
import { useState } from "react";

type ChangeFile = { path: string; content?: string; digest: string; deleted?: boolean };
type Change = { id: string; title: string; rationale: string; origin: string; prompt?: string; agent?: string;
  state: string; approvedBy?: string; files?: ChangeFile[]; diagnostics?: string; release?: number };
type Release = { id: string; version: number; change: string; state: string; previous?: number;
  binary?: string; web?: string; toolchain?: string; diagnostics?: string };
type Activation = { id: string; release: number; from?: number; result: string; window?: number; evidence?: string; at?: string };
type SystemFile = { path: string; digest: string; content: string; size: number; revision: number; deleted?: boolean; by?: string };

const tone = (state: string) => (({ failed: "danger", rejected: "danger", aborted: "warning", approved: "info",
  published: "success", active: "success", requested: "info", superseded: "neutral" }) as const)[state as "failed"] ?? "neutral";

/** Prove a change: one decision, the reason a refusal gives is the message. */
function useDecide() {
  const { decide } = useHost();
  return async (schema: string, type: string, id: string, payload: unknown = {}) =>
    decide(schema, { type, id }, payload, { onRefused: (message) => notify.error(message) });
}

export function SystemChanges() {
  const changes = useRead<Change[]>("/v1/system-changes");
  const { can } = useHost();
  const decide = useDecide();
  const [open, setOpen] = useState<string>();
  const list = changes.data ?? [];
  const chosen = list.find((c) => c.id === open);
  return <>
    <PageHeader title={t("System changes")} help={t("Every iteration of the platform itself: what was proposed, by a person or a prompt, the files it freezes, and how it was built. A proposal is approved by a publisher, never by the agent that proposed it.")} />
    <div className="grid gap-4 lg:grid-cols-2">
      <Card className="grid content-start gap-2 p-3">
        {list.length === 0 ? <p className="text-sm text-muted">{t("No change was proposed yet.")}</p> : list.map((c) =>
          <div key={c.id} className={`grid gap-1 rounded border border-border p-2 text-sm ${open === c.id ? "bg-surface-raised" : ""}`}>
            <button type="button" className="flex items-center gap-2 text-left" onClick={() => setOpen(c.id)}>
              <span className="font-mono text-xs text-muted">{c.id}</span>
              <span className="font-medium">{c.title}</span>
              <Tag label={c.origin === "prompt" ? t("Prompt") : t("Interface")} tone="info" />
              <Tag label={c.state} tone={tone(c.state)} />
              <span className="ml-auto text-xs text-muted">{t("{count} files", { count: c.files?.length ?? 0 })}</span>
            </button>
            {open === c.id && <ChangeDetail change={c} decide={decide} can={can} />}
          </div>)}
      </Card>
      <Card className="grid content-start gap-3 p-3">
        <PromptEntry />
        <div className="text-xs text-muted">{t("A prompt runs the system architect on this tenant. It can read the system's files and propose one change; it cannot approve, build or activate anything.")}</div>
      </Card>
    </div>
    {chosen && chosen.files?.length ? <Card className="mt-4 grid gap-2 p-3">
      <h2 className="text-sm font-semibold">{t("Frozen files")}</h2>
      {chosen.files.map((f) => <details key={f.path} className="rounded border border-border p-2 text-xs">
        <summary className="cursor-pointer font-mono">{f.path}{f.deleted ? ` (${t("delete")})` : ""}</summary>
        {!f.deleted && <pre className="mt-2 max-h-80 overflow-auto whitespace-pre-wrap text-[11px]">{f.content ?? f.digest}</pre>}
      </details>)}
    </Card> : null}
  </>;
}

function ChangeDetail({ change, decide, can }: { change: Change; decide: (s: string, ty: string, id: string, p?: unknown) => Promise<boolean>; can: (schema: string) => boolean }) {
  return <div className="grid gap-2 pl-4 text-sm">
    <p className="text-muted">{change.rationale}</p>
    {change.prompt && <p className="rounded bg-surface-sunken p-2 font-mono text-xs">{change.prompt}</p>}
    {change.diagnostics && <pre className="max-h-40 overflow-auto rounded bg-surface-sunken p-2 text-[11px]">{change.diagnostics}</pre>}
    <div className="flex flex-wrap gap-2">
      {change.state === "draft" && can("system.change.approve") &&
        <Button size="sm" variant="primary" onClick={() => void decide("system.change.approve", "system.change", change.id)}>{t("Approve")}</Button>}
      {change.state === "published" && change.release &&
        <Button size="sm" variant="primary" onClick={() => void decide("system.release.activate", "system.release", String(change.release).padStart(4, "0"), { window: 30 })}>{t("Activate")}</Button>}
      {change.state === "approved" && <Tag label={t("Waiting for the updater's build")} tone="info" />}
      {change.state === "failed" && <Tag label={t("The build failed; propose a new change")} tone="danger" />}
    </div>
    {change.approvedBy && <p className="text-xs text-muted">{t("Approved by {who}", { who: change.approvedBy })}</p>}
  </div>;
}

function PromptEntry() {
  const { decide, can } = useHost();
  const [goal, setGoal] = useState("");
  if (!can("agent.run.start")) return <p className="text-sm text-muted">{t("Starting an agent needs a role in this app.")}</p>;
  return <div className="grid gap-2">
    <label className="text-sm font-medium" htmlFor="system-prompt">{t("Ask for a change")}</label>
    <Textarea id="system-prompt" rows={3} value={goal} placeholder={t("Change one behaviour of the platform: what should it do instead?")}
      onChange={(e) => setGoal(e.target.value)} />
    <Button size="sm" variant="primary" disabled={!goal.trim()} onClick={() => {
      const id = `run-${Date.now()}`;
      void decide("agent.run.start", { type: "agent.run", id }, { agent: "system.architect", goal },
        { onRefused: (message) => notify.error(message) }).then((ok) => { if (ok) setGoal(""); });
    }}>{t("Run the architect")}</Button>
    <p className="text-xs text-muted">{t("Every file it reads and writes is in its run log. The change it proposes waits for a publisher.")}</p>
  </div>;
}

export function SystemFiles() {
  const files = useRead<SystemFile[]>("/v1/system-files");
  const decide = useDecide();
  const list = files.data ?? [];
  const [path, setPath] = useState<string>("");
  const chosen = list.find((f) => f.path === path) ?? list[0];
  const [body, setBody] = useState("");
  const [dirty, setDirty] = useState(false);
  return <>
    <PageHeader title={t("System files")} help={t("The platform's own source, one record per path and content-addressed by sha256. Saving names the revision it expects, so two writers cannot silently overwrite each other. The kernel paths are refused before anything is stored.")} />
    <div className="grid gap-4 lg:grid-cols-[minmax(200px,1fr)_3fr]">
      <Card className="grid content-start gap-1 p-2">
        {list.length === 0 ? <p className="p-2 text-sm text-muted">{t("No file was saved yet.")}</p> : list.map((f) =>
          <button key={f.path} type="button" className={`flex items-center gap-2 rounded p-2 text-left font-mono text-xs ${chosen?.path === f.path ? "bg-surface-raised" : ""}`}
            onClick={() => { setPath(f.path); setBody(f.content); setDirty(false); }}>
            <span className={f.deleted ? "line-through" : ""}>{f.path}</span>
            <span className="ml-auto text-muted">r{f.revision}</span>
          </button>)}
        <Button size="sm" className="mt-2" onClick={() => { setPath(""); setBody(""); setDirty(true); }}>{t("New file")}</Button>
      </Card>
      <Card className="grid gap-2 p-3">
        <Input value={path || chosen?.path || ""} placeholder={t("path/in/the/repository")} onChange={(e) => { setPath(e.target.value); setDirty(true); }} />
        <Textarea rows={20} className="font-mono text-xs" value={dirty ? body : chosen?.content ?? body}
          onChange={(e) => { setBody(e.target.value); setDirty(true); }} />
        <div className="flex items-center gap-2">
          <Button size="sm" variant="primary" onClick={() => {
            void decide("system.file.save", "system.file", path || chosen?.path || "",
              { path: path || chosen?.path, content: body, revision: chosen?.revision ?? 0 }).then((ok) => { if (ok) setDirty(false); });
          }}>{t("Save as revision {revision}", { revision: (chosen?.revision ?? 0) + 1 })}</Button>
          {chosen && <Button size="sm" variant="danger" onClick={() => {
            void decide("system.file.save", "system.file", chosen.path, { path: chosen.path, revision: chosen.revision, deleted: true });
          }}>{t("Delete")}</Button>}
          <span className="ml-auto text-xs text-muted">{chosen ? `${t("revision")} ${chosen.revision} · ${chosen.digest.slice(0, 12)}` : ""}</span>
        </div>
        <p className="text-xs text-muted">{t("Saving a file does not publish it: a change freezes the files it touches, and the updater builds what a publisher approved.")}</p>
      </Card>
    </div>
  </>;
}

export function SystemReleases() {
  const releases = useRead<Release[]>("/v1/system-releases");
  const activations = useRead<Activation[]>("/v1/system-activations");
  const decide = useDecide();
  const { can } = useHost();
  const current = (releases.data ?? []).find((r) => r.state === "active");
  const pending = (activations.data ?? []).find((a) => a.result === "requested");
  return <>
    <PageHeader title={t("System releases")} help={t("Every built version of the platform, with the digest of its binary and workspace. A release is immutable: a fix is another change. Activation is a request to the updater, and a person may cancel it while the window is open.")} />
    {current && <Card className="mb-4 grid gap-1 p-3 text-sm">
      <h2 className="font-semibold">{t("Running release {version}", { version: current.version })}</h2>
      <span className="font-mono text-xs text-muted">{t("binary")} {current.binary ?? "—"} · {t("workspace")} {current.web || "—"} · {current.toolchain ?? ""}</span>
    </Card>}
    {pending && <Card className="mb-4 grid gap-2 p-3 text-sm">
      <span>{t("Release {version} is requested; the updater swaps in up to {window} seconds.", { version: pending.release, window: pending.window ?? 30 })}</span>
      {can("system.release.cancel") && <Button size="sm" variant="danger" onClick={() =>
        void decide("system.release.cancel", "system.release", String(pending.release).padStart(4, "0"), { reason: "cancelled in the Platform" })}>{t("Cancel the swap")}</Button>}
    </Card>}
    <Card className="grid gap-2 p-3">
      {(releases.data ?? []).map((r) => <div key={r.id} className="flex flex-wrap items-center gap-2 border-b border-border py-1 text-sm last:border-0">
        <span className="font-mono text-xs">{r.id}</span>
        <span>{r.change}</span>
        <Tag label={r.state} tone={tone(r.state)} />
        <span className="font-mono text-[11px] text-muted">{r.binary ?? t("definitions only")}</span>
        <div className="ml-auto flex gap-2">
          {r.state === "published" && can("system.release.activate") &&
            <Button size="sm" onClick={() => void decide("system.release.activate", "system.release", r.id, { window: 30 })}>{t("Activate")}</Button>}
          {r.state === "active" && r.previous && can("system.release.rollback") &&
            <Button size="sm" variant="danger" onClick={() => void decide("system.release.rollback", "system.release", r.id)}>{t("Roll back")}</Button>}
        </div>
      </div>)}
      {(releases.data ?? []).length === 0 && <p className="text-sm text-muted">{t("No release was built yet.")}</p>}
    </Card>
    <Card className="mt-4 grid gap-2 p-3">
      <h2 className="text-sm font-semibold">{t("Activations")}</h2>
      {(activations.data ?? []).map((a) => <div key={a.id} className="flex flex-wrap items-center gap-2 border-b border-border py-1 text-sm last:border-0">
        <span className="font-mono text-xs">{a.id}</span>
        <Tag label={t("release {version}", { version: a.release })} tone="info" />
        <Tag label={a.result} tone={tone(a.result)} />
        {a.evidence && <span className="font-mono text-[11px] text-muted">{a.evidence}</span>}
        {a.at && <span className="ml-auto text-xs text-muted">{new Date(a.at).toLocaleString()}</span>}
      </div>)}
      {(activations.data ?? []).length === 0 && <p className="text-sm text-muted">{t("Nothing was activated yet.")}</p>}
    </Card>
  </>;
}
