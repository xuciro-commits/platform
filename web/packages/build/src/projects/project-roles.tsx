import { useHost } from "@platform/app";
import { Button, Checkbox, DataTable, Select, t, type ColumnDef } from "@platform/ui";
import { useApplicationWorkspace } from "./application-scope";
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
  const { open } = useApplicationWorkspace();
  const objects = useMemo(() => owned.filter((item) => item.kind.kind === "object" && item.record).map((item) => ({ item, record: item.record as unknown as ObjectRecord })), [owned]);
  const roles = useMemo(() => [...new Set(objects.flatMap(({ record }) => (record.access ?? []).map((a) => a.role)))].sort(), [objects]);
  const can = host.can("build.object.edit");
  const write = (record: ObjectRecord, access: Access[]) =>
    void host.decide("build.object.edit", { type: "build.object", id: record.id }, { access }, { expectedRevision: record.revision });
  const setLevel = (record: ObjectRecord, role: string, read: Access["read"]) => {
    const current = record.access ?? [];
    const next = current.some((a) => a.role === role)
      ? current.map((a) => a.role === role ? (read === "none" ? { ...a, read, create: false, edit: false, archive: false } : { ...a, read }) : a)
      : [...current, { role, read, create: read !== "none", edit: read !== "none" }];
    write(record, next);
  };
  const setVerb = (record: ObjectRecord, role: string, key: "create" | "edit" | "archive", on: boolean) =>
    write(record, (record.access ?? []).map((a) => a.role === role ? { ...a, [key]: on } : a));
  const columns: ColumnDef<(typeof objects)[number]>[] = [
    { id: "object", header: t("Object"), meta: { width: 240, pin: "left" }, cell: ({ row }) => {
      const { item, record } = row.original;
      return <div>
        <Button variant="row" type="button" className="font-medium hover:underline" onClick={() => open({ view: "object-type", params: { id: record.id, tab: "permissions", access: "true" } })}>{item.title}</Button>
        <div className="text-[11px] text-muted">{record.scope?.unit ? t("unit: {field} · {structure}", { field: record.scope.unit, structure: record.scope.structure ?? "—" }) : (record.access ?? []).some((a) => a.read === "unit" || a.read === "below") ? <span className="text-danger">{t("needs scope fields")}</span> : ""}</div>
      </div>;
    } },
    ...roles.map((role): ColumnDef<(typeof objects)[number]> => ({ id: `role:${role}`, header: role, meta: { width: 240 }, cell: ({ row }) => {
      const { record } = row.original;
      const access = (record.access ?? []).find((item) => item.role === role);
      return <div className="grid gap-1 py-1">
        <Select aria-label={`${row.original.item.title} · ${role} · ${t("Read")}`} value={access?.read ?? ""} disabled={!can} onChange={(event) => event.target.value && setLevel(record, role, event.target.value as Access["read"])}>
          <option value="">{(record.access ?? []).length ? t("none") : t("open")}</option>{levels.map((level) => <option key={level} value={level}>{t(level)}</option>)}
        </Select>
        {access && access.read !== "none" && <div className="flex gap-2 text-[11px]">{(["create", "edit", "archive"] as const).map((key) =>
          <Checkbox key={key} checked={!!access[key]} disabled={!can} onChange={(on) => setVerb(record, role, key, on)}>{t(key)}</Checkbox>)}</div>}
      </div>;
    } })),
  ];
  if (!objects.length) return <p className="p-4 text-sm text-muted">{t("This project has no object drafts yet; their roles appear here.")}</p>;
  return <div className="grid content-start gap-4 p-4">
    <div>
      <h2 className="text-sm font-semibold">{t("Roles across the application")}</h2>
      <p className="text-xs text-muted">{t("Each cell: which records the role reads (all · below · unit · own · none) and whether it may create, edit or archive. Objects without roles are open to builders and users alike. Field-level access and the scope fields live on each object.")}</p>
    </div>
    {!roles.length && <p className="text-sm text-muted">{t("No roles yet. Add a role on an object's Permissions tab; it then appears here for every object.")}</p>}
    {roles.length > 0 && <DataTable data={objects} columns={columns} getRowId={({ record }) => record.id} searchable={false} rowHeight={76} height={Math.min(480, 30 + objects.length * 76)} />}
  </div>;
}
