import {
  Button, DataTable, FilterBar, PageHeader, RecordForm, Sheet, applyFilters, columnsFor, defineEntity, field, notify, type Filter,
} from "@platform/ui";
import { Plus } from "lucide-react";
import { useState } from "react";

// Manufacturing master data declared once with field types: the grid, inline
// editing, filters and the form all follow from this declaration.
type Material = {
  code: string; name: string; kind: string; certifications: string[]; unitCost: number; onHand: number; reorderAt: number;
  leadTime: number; supplier?: string; datasheet?: string; phone?: string; lastAudit?: string; quality?: number;
  active: boolean; drawings?: { name: string; url: string }[]; usedIn?: string; updated?: string;
};

export const material = defineEntity<Material>({
  name: "Material", primary: "code",
  fields: {
    code: field.barcode({ label: "Code", required: true, readOnly: true }),
    name: field.text({ label: "Name", required: true }),
    kind: field.singleSelect({ label: "Kind", required: true, options: [
      { value: "raw", label: "Raw material", tone: "info" }, { value: "part", label: "Purchased part", tone: "success" },
      { value: "wip", label: "Semi-finished", tone: "warning" }] }),
    certifications: field.multiSelect({ label: "Certifications", options: [
      { value: "rohs", label: "RoHS", tone: "success" }, { value: "reach", label: "REACH", tone: "info" }, { value: "iatf", label: "IATF 16949", tone: "neutral" }] }),
    unitCost: field.currency({ label: "Unit cost", currency: "CNY" }),
    onHand: field.number({ label: "On hand", unit: "pcs", min: 0 }),
    reorderAt: field.number({ label: "Reorder at", unit: "pcs", min: 0 }),
    coverage: field.formula({ label: "Coverage", compute: (m: Material) => (m.reorderAt ? Math.min(1, m.onHand / (m.reorderAt * 2)) : undefined),
      as: field.percent({ label: "" }) }),
    leadTime: field.duration({ label: "Lead time" }),
    supplier: field.email({ label: "Supplier" }),
    phone: field.phone({ label: "Phone" }),
    datasheet: field.url({ label: "Datasheet" }),
    lastAudit: field.date({ label: "Last audit" }),
    quality: field.rating({ label: "Supplier rating" }),
    active: field.checkbox({ label: "Active" }),
    drawings: field.attachment({ label: "Drawings", accept: ".pdf,.dxf,.step" }),
    usedIn: field.link({ label: "Used in", to: (id) => ({ view: "workOrder", params: { id } }) }),
    updated: field.timestamp({ label: "Updated", of: (m: Material) => m.updated }),
  },
});

const seed: Material[] = Array.from({ length: 60 }, (_, i) => ({
  code: `690${String(1_000_000_000 + i * 7919).slice(0, 10)}`, name: ["Aluminium ingot A356", "Valve seat DN25", "NBR gasket 40x3", "Pump shaft blank", "Bearing 6204-2RS"][i % 5]! + ` #${i + 1}`,
  kind: ["raw", "part", "part", "wip", "part"][i % 5]!, certifications: [["rohs", "reach"], ["rohs"], [], ["iatf"], ["rohs", "iatf"]][i % 5]!,
  unitCost: [21.5, 3.8, 0.24, 46, 7.9][i % 5]!, onHand: (i * 37) % 900, reorderAt: [200, 150, 1000, 50, 300][i % 5]!, leadTime: [2880, 720, 240, 4320, 1440][i % 5]!,
  supplier: `buyer${i % 4}@supplier.example`, phone: `+86 512 6${String(1000000 + i * 131).slice(0, 7)}`, datasheet: `https://datasheets.example/m/${i}`,
  lastAudit: `2026-0${(i % 9) + 1}-1${i % 9}`, quality: (i % 5) + 1, active: i % 7 !== 0,
  drawings: i % 3 === 0 ? [{ name: `DWG-${i}.pdf`, url: `https://files.example/DWG-${i}.pdf` }] : [],
  usedIn: `WO-${String((i * 13) % 1000 + 1).padStart(6, "0")}`, updated: `2026-09-2${i % 5}T0${i % 9}:15`,
}));

export function Materials() {
  const [rows, setRows] = useState(seed);
  const [filters, setFilters] = useState<Filter[]>([{ field: "kind", operator: "is", arg: "part" }]);
  const [creating, setCreating] = useState(false);
  return (
    <>
      <PageHeader title="Materials" description="Every field type once: double-click a cell to edit it, filters follow each field's type."
        actions={<Button variant="primary" onClick={() => setCreating(true)}><Plus />New material</Button>} />
      <div className="mb-2"><FilterBar entity={material} filters={filters} onChange={setFilters} /></div>
      <DataTable data={applyFilters(material, rows, filters)} columns={columnsFor(material)} getRowId={(m) => m.code} height="calc(100dvh - 230px)"
        onCellEdit={(row, key, value) => {
          setRows((all) => all.map((m) => (m === row ? { ...m, [key]: value, updated: new Date().toISOString().slice(0, 16) } : m)));
          notify.success(`${row.code}: ${material.fields[key]!.label} updated`);
        }} />
      <Sheet open={creating} onOpenChange={setCreating} title="New material">
        <RecordForm entity={material} keys={["name", "kind", "certifications", "unitCost", "onHand", "reorderAt", "leadTime", "supplier", "datasheet", "active", "drawings"]}
          defaultValues={{ kind: "part", active: true, certifications: [] }} onCancel={() => setCreating(false)}
          onSubmit={(values) => {
            const code = `690${Date.now()}`.slice(0, 13);
            setRows((all) => [{ code, onHand: 0, reorderAt: 0, unitCost: 0, leadTime: 0, certifications: [], active: true, ...values } as Material, ...all]);
            setCreating(false);
            notify.success(`Material ${code} created`);
          }} />
      </Sheet>
    </>
  );
}
