import { useHost } from "@platform/app";
import { Button, Checkbox, Select, t, useWorkspace } from "@platform/ui";
import { useMemo } from "react";
import type { Access, ObjectRecord } from "../ontology/object-model";
import type { OwnedResource } from "./project";

// Roles & scope designer (ADR-0057 F, ADR-0066): every role the project's
// objects name, against every object — what each reads and may do. Edits go
// straight to the object drafts through the original build.object.edit; the
// object's own Permissions tab stays the place for field-level access.
const levels = ["all", "below", "unit", "own", "none"] as const;

export function ProjectRoles({ owned }: { owned: OwnedResource[] }) {
  const host = useHost();
  const { open } = useWorkspace();
  const objects = useMemo(() => owned.filter((item) => item.kind.kind === "object" && item.record).map((item) => ({ item, record: item.record as unknown as ObjectRecord })), [owned]);
  const roles = useMemo(() => [...new Set(objects.flatMap(({ record }) => (record.access ?? []).map((a) => a.role)))].sort(), [objects]);
  const can = host.can("build.object.edit");
  const write = (record: ObjectRecord, access: Access[]) =>
    void host.decide("build.object.edit", { type: "build.object", id: record.id }, { ...record, access }, { expectedRevision: record.revision });
  const setLevel = (record: ObjectRecord, role: string, read: Access["read"]) => {
    const current = record.access ?? [];
    const next = current.some((a) => a.role === role)
      ? current.map((a) => a.role === role ? (read === "none" ? { ...a, read, create: false, edit: false, archive: false } : { ...a, read }) : a)
      : [...current, { role, read, create: read !== "none", edit: read !== "none" }];
    write(record, next);
  };
  const setVerb = (record: ObjectRecord, role: string, key: "create" | "edit" | "archive", on: boolean) =>
    write(record, (record.access ?? []).map((a) => a.role === role ? { ...a, [key]: on } : a));
  if (!objects.length) return <p className="p-4 text-sm text-muted">{t("This project has no object drafts yet; their roles appear here.")}</p>;
  return <div className="grid content-start gap-4 p-4">
    <div>
      <h2 className="text-sm font-semibold">{t("Roles across the application")}</h2>
      <p className="text-xs text-muted">{t("Each cell: which records the role reads (all · below · unit · own · none) and whether it may create, edit or archive. Objects without roles are open to builders and users alike. Field-level access and the scope fields live on each object.")}</p>
    </div>
    {!roles.length && <p className="text-sm text-muted">{t("No roles yet. Add a role on an object's Permissions tab; it then appears here for every object.")}</p>}
    {roles.length > 0 && <div className="overflow-auto rounded-md border border-border">
      <table className="w-full text-sm">
        <thead><tr className="text-left text-xs text-muted"><th className="px-2 py-1 font-medium">{t("Object")}</th>{roles.map((role) => <th key={role} className="px-2 py-1 font-mono font-medium">{role}</th>)}</tr></thead>
        <tbody>{objects.map(({ item, record }) => <tr key={record.id} className="border-t border-border align-top">
          <td className="px-2 py-1.5"><Button variant="row" type="button" className="font-medium hover:underline" onClick={() => open({ view: "object-type", params: { id: record.id, tab: "permissions", access: "true" } })}>{item.title}</Button>
            <div className="text-[11px] text-muted">{record.scope?.unit ? t("unit: {field} · {structure}", { field: record.scope.unit, structure: record.scope.structure ?? "—" }) : (record.access ?? []).some((a) => a.read === "unit" || a.read === "below") ? <span className="text-danger">{t("needs scope fields")}</span> : ""}</div></td>
          {roles.map((role) => {
            const a = (record.access ?? []).find((x) => x.role === role);
            return <td key={role} className="px-2 py-1.5">
              <Select value={a?.read ?? ""} disabled={!can} onChange={(e) => e.target.value && setLevel(record, role, e.target.value as Access["read"])}>
                <option value="">{(record.access ?? []).length ? t("none") : t("open")}</option>{levels.map((l) => <option key={l} value={l}>{t(l)}</option>)}</Select>
              {a && a.read !== "none" && <div className="mt-1 flex gap-2 text-[11px]">{(["create", "edit", "archive"] as const).map((key) =>
                <Checkbox key={key} checked={!!a[key]} disabled={!can} onChange={(on) => setVerb(record, role, key, on)}>{t(key)}</Checkbox>)}</div>}
            </td>;
          })}
        </tr>)}</tbody>
      </table>
    </div>}
  </div>;
}
