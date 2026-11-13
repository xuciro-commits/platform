import type { Api } from "@platform/api";
import { Button, Dialog, Input, Select, Textarea, notify, t, type ButtonProps } from "@platform/ui";
import { useState } from "react";
import { useHost, useReadQuery } from "../index";

type RequestAccessProps = {
  project?: string;
  app?: string;
  role?: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRequested?: () => void;
};

const projectRoles = ["viewer", "editor", "discoverer"];
const roleTitles: Record<string, string> = { viewer: "Viewer", editor: "Editor", discoverer: "Discoverer", builder: "Builder", publisher: "Publisher" };

export function RequestAccessDialog({ project, app, role: initialRole, open, onOpenChange, onRequested }: RequestAccessProps) {
  const { decide, me } = useHost();
  const apps = useReadQuery<Api.AppInfo[]>("/v1/apps", undefined, open && !project);
  const appRoles = apps.data?.find(item => item.id === app)?.roles ?? [];
  const roles = project ? projectRoles : appRoles.filter(item => item !== "admin");
  const [role, setRole] = useState(initialRole ?? roles[0] ?? "");
  const [reason, setReason] = useState("");
  const [until, setUntil] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const chosen = roles.includes(role) ? role : roles[0] ?? "";
  const admin = me.apps.some(item => item.id === "platform" && item.role === "admin") && !project;
  const close = (next: boolean) => {
    if (!next) {
      setReason("");
      setUntil("");
      setError("");
    }
    onOpenChange(next);
  };
  const submit = async () => {
    if (!chosen || !reason.trim()) return;
    setBusy(true);
    setError("");
    const accepted = await decide("platform.access.request", { type: "platform.access-request", id: crypto.randomUUID() }, {
      scope: project ? "project" : "role", project: project ?? "", app: project ? "" : app ?? "", role: chosen, reason: reason.trim(), until,
    }, { quiet: true, onRefused: setError });
    setBusy(false);
    if (!accepted) return;
    notify.success(t(project ? "Access request sent to the project owners." : "Access request sent to the platform administrators."));
    close(false);
    onRequested?.();
  };
  const grant = async () => {
    if (!chosen || !app) return;
    setBusy(true);
    setError("");
    const accepted = await decide("platform.member.grant", { type: "platform.member", id: me.principalId }, {
      app, role: chosen, until, reason: reason.trim(),
    }, { quiet: true, onRefused: setError });
    setBusy(false);
    if (!accepted) return;
    notify.success(t("Role granted."));
    close(false);
    onRequested?.();
  };
  return <Dialog open={open} onOpenChange={close} title={t(project ? "Request project access" : "Request application access")}>
    <div className="grid gap-3 text-sm">
      <p className="text-muted">{t(project
        ? "The project's owners decide this request. Approval applies the project role immediately; no new sign-in is needed."
        : "The platform administrators decide this request. Approval applies the business role immediately; no new sign-in is needed.")}</p>
      <p className="font-mono text-xs">{project ? `project ${project}` : app}</p>
      <label className="grid gap-1">{t("Role")}
        <Select aria-label={t("Role")} value={chosen} disabled={busy || !roles.length} onChange={event => setRole(event.target.value)}>
          {roles.map(item => <option key={item} value={item}>{t(roleTitles[item] ?? item)}</option>)}
        </Select>
      </label>
      <label className="grid gap-1">{t("Reason")}
        <Textarea aria-label={t("Reason")} rows={3} value={reason} disabled={busy} onChange={event => setReason(event.target.value)} placeholder={t("What will you do with this access?")} />
      </label>
      <label className="grid gap-1">{t("Until (optional)")}
        <Input type="date" aria-label={t("Until (optional)")} value={until} disabled={busy} onChange={event => setUntil(event.target.value)} />
      </label>
      {error && <p role="alert" className="text-danger">{error}</p>}
      <div className="flex justify-end gap-2">
        <Button onClick={() => close(false)}>{t("Cancel")}</Button>
        {admin && <Button disabled={busy || !chosen || !reason.trim()} onClick={() => void grant()}>{t("Grant this role to me")}</Button>}
        <Button variant="primary" disabled={busy || !chosen || !reason.trim()} onClick={() => void submit()}>{t("Send request")}</Button>
      </div>
    </div>
  </Dialog>;
}

export function RequestAccessButton({ project, app, role, label, size = "sm", variant = "default" }: Pick<RequestAccessProps, "project" | "app" | "role"> & {
  label?: string;
  size?: ButtonProps["size"];
  variant?: ButtonProps["variant"];
}) {
  const [open, setOpen] = useState(false);
  return <>
    <Button size={size} variant={variant} onClick={() => setOpen(true)}>{label ?? t("Request access")}</Button>
    <RequestAccessDialog project={project} app={app} role={role} open={open} onOpenChange={setOpen} />
  </>;
}
