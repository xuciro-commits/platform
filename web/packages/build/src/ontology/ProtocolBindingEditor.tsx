import { useState } from "react";
import type { Api } from "@platform/kernel";
import { PayloadFields } from "@platform/app";
import { Button, DataTable, Disclosure, Input, Panel, Select, Tag, t, type ColumnDef } from "@platform/ui";

const fieldClass = "grid min-w-0 gap-1 text-xs";
const pointTypes = ["decimal", "integer", "number", "boolean", "text", "enum"];
const pointFields = (): Api.Field[] => [
  { name: "id", description: t("Signal ID"), type: "string", required: true },
  { name: "address", description: t("Point address"), type: "string", required: true },
  { name: "type", description: t("Value type"), type: "string", choices: pointTypes, required: true },
  { name: "dataType", description: t("Driver data type"), type: "string" },
  { name: "unit", description: t("Unit"), type: "string" },
  { name: "intervalMS", description: t("Sample interval (ms)"), type: "integer", required: true, constraints: { min: 100, max: 86_400_000 } },
  { name: "scale", description: t("Scale"), type: "string" },
  { name: "offset", description: t("Offset"), type: "string" },
];
const translated = (fields: Api.Field[] = []) => fields.map(field => ({ ...field, description: t(field.description || field.name), group: field.group ? t(field.group) : undefined }));

type ImportRow = { line: number; point: Api.DriverPoint; errors: string[] };

function csvRows(source: string): string[][] {
  const rows: string[][] = []; let row: string[] = [], cell = "", quoted = false;
  for (let at = 0; at < source.length; at++) {
    const char = source[at]!;
    if (char === '"') {
      if (quoted && source[at + 1] === '"') { cell += '"'; at++; }
      else quoted = !quoted;
    } else if (!quoted && (char === "," || char === "\n" || char === "\r")) {
      row.push(cell); cell = "";
      if (char !== ",") { if (row.some(value => value.trim())) rows.push(row); row = []; if (char === "\r" && source[at + 1] === "\n") at++; }
    } else cell += char;
  }
  if (quoted) throw Error(t("A quoted CSV value is not closed."));
  row.push(cell); if (row.some(value => value.trim())) rows.push(row);
  return rows;
}

function importedPoints(source: string, json: boolean): ImportRow[] {
  let values: unknown[];
  if (json) { const parsed: unknown = JSON.parse(source); if (!Array.isArray(parsed)) throw Error(t("The JSON point table must be an array.")); values = parsed; }
  else {
    const [header, ...rows] = csvRows(source.replace(/^\uFEFF/, ""));
    if (!header?.length) throw Error(t("The point table is empty."));
    const names = header.map(name => name.trim());
    if (new Set(names).size !== names.length) throw Error(t("CSV column names must be unique."));
    values = rows.map((row, index) => {
      if (row.length !== names.length) throw Error(t("CSV row {row} has {actual} values; expected {expected}.", { row: index + 2, actual: row.length, expected: names.length }));
      return Object.fromEntries(names.map((name, index) => [name, row[index] ?? ""]));
    });
  }
  if (!values.length) throw Error(t("The point table is empty."));
  const seen = new Set<string>();
  return values.map((value, index) => {
    const errors: string[] = [], row = value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
    for (const name of Object.keys(row)) if (!["id", "device", "address", "type", "dataType", "unit", "scale", "offset", "intervalMS"].includes(name)) errors.push(t("Unknown point column: {field}", { field: name }));
    const text = (name: string, required = false) => { const value = row[name]; if (value !== undefined && typeof value !== "string") { errors.push(t("{field} must be text.", { field: name })); return ""; } const result = String(value ?? "").trim(); if (required && !result) errors.push(t("{field} is required.", { field: name })); return result; };
    const id = text("id", true), device = text("device", true), address = text("address", true), type = text("type", true);
    if (id && seen.has(id)) errors.push(t("Duplicate signal ID: {id}", { id })); seen.add(id);
    if (type && !pointTypes.includes(type)) errors.push(t("Unsupported point value type: {type}", { type }));
    const intervalMS = row.intervalMS === undefined || row.intervalMS === "" ? 1000 : Number(row.intervalMS);
    if (!Number.isInteger(intervalMS) || intervalMS < 100 || intervalMS > 86_400_000) errors.push(t("Sample interval must be 100–86400000 ms."));
    const point = { id, device, address, type, dataType: text("dataType"), unit: text("unit"), scale: text("scale"), offset: text("offset"), intervalMS };
    return { line: index + (json ? 1 : 2), point, errors };
  });
}

/** The package supplies connection/device fields; common point and command
 * configuration stays in the original Connection definition. */
export function ProtocolBindingEditor({ value, profile, onChange }: { value: Api.DriverBinding; profile: Api.DriverProfile; onChange: (value: Api.DriverBinding) => void }) {
  const configuration = value.configuration;
  const devices = configuration.devices ?? [], points = configuration.points ?? [], commands = configuration.commands ?? [];
  const [selected, setSelected] = useState(0), [rows, setRows] = useState<ImportRow[]>([]), [importError, setImportError] = useState("");
  const change = (patch: Partial<Api.DriverConfiguration>) => { onChange({ ...value, configuration: { ...configuration, ...patch } }); };
  const point = points[selected];
  const setPoint = (next: Api.DriverPoint) => change({ points: points.map((point, index) => index === selected ? next : point), commands: commands.map(command => command.point === point?.id ? { ...command, point: next.id } : command) });
  const columns: ColumnDef<Api.DriverPoint & { row: number }>[] = [
    { accessorKey: "id", header: t("Signal ID"), meta: { pin: "left", width: 160 } },
    { accessorKey: "device", header: t("Device ID"), meta: { width: 140 } },
    { accessorKey: "address", header: t("Point address"), meta: { width: 180 } },
    { accessorKey: "type", header: t("Value type"), meta: { width: 100 }, cell: cell => t(cell.getValue<string>()) },
    { accessorKey: "unit", header: t("Unit"), meta: { width: 80 } },
  ];
  const importFile = async (file: File) => {
    setRows([]); setImportError("");
    try {
      if (file.size > 256 * 1024) throw Error(t("The point table must be smaller than 256 KiB."));
      setRows(importedPoints(await file.text(), file.name.toLowerCase().endsWith(".json")));
    } catch (error) { setImportError(error instanceof Error ? error.message : t("The point table could not be read.")); }
  };
  const importColumns: ColumnDef<ImportRow>[] = [
    { accessorKey: "line", header: t("Row"), meta: { width: 55 } },
    { id: "id", accessorFn: row => row.point.id, header: t("Signal ID"), meta: { width: 160 } },
    { id: "device", accessorFn: row => row.point.device, header: t("Device ID"), meta: { width: 130 } },
    { id: "address", accessorFn: row => row.point.address, header: t("Point address"), meta: { width: 160 } },
    { id: "errors", accessorFn: row => row.errors.join(" · "), header: t("Import issues"), meta: { width: 300 }, cell: cell => <span className="text-danger">{cell.getValue<string>() || t("Ready")}</span> },
  ];
  return <div className="grid min-w-0 gap-3 lg:col-span-2">
    <Panel title={t("Protocol configuration")} className="grid min-w-0 gap-3">
      <div className="flex flex-wrap items-center gap-2 text-xs"><Tag label={profile.simulation ? t("Simulation") : t("Device protocol")} tone={profile.simulation ? "warning" : "neutral"} /><span>{profile.title}</span><span className="break-all font-mono text-muted">{value.package} · {value.version}</span></div>
      {profile.description && <p className="text-xs text-muted">{t(profile.description)}</p>}
      <p className="text-xs text-muted">{t("Control loops stay in the device, gateway or PLC. The platform dispatches declared commands and reads their status.")}</p>
      <div className="grid min-w-0 gap-3 sm:grid-cols-2"><PayloadFields fields={translated(profile.connectionFields)} values={configuration.connection ?? {}} onChange={connection => change({ connection })} /></div>
      {profile.simulation && <div className="grid min-w-0 gap-3 sm:grid-cols-2">
        <label className={fieldClass}>{t("Simulation scenario")}<Select value={configuration.scenario ?? ""} onChange={event => change({ scenario: event.target.value })}><option value="">{t("Default scenario")}</option>{profile.scenarios?.map(scenario => <option key={scenario} value={scenario}>{t(scenario)}</option>)}</Select></label>
        <label className={fieldClass}>{t("Simulation seed")}<Input type="number" step="1" value={configuration.seed ?? 1} onChange={event => change({ seed: Number(event.target.value) })} /></label>
      </div>}
      <Disclosure summary={t("Pinned protocol artifact")}><p className="break-all font-mono text-xs">{value.digest}</p></Disclosure>
    </Panel>
    <Panel title={t("Devices")} className="grid min-w-0 gap-3">
      {!devices.length && <p className="text-xs text-muted">{t("Add a device before configuring its points.")}</p>}
      {devices.map((device, index) => {
        const patch = (changes: Partial<Api.DriverDevice>) => change({ devices: devices.map((old, at) => at === index ? { ...old, ...changes } : old), ...(changes.id !== undefined ? { points: points.map(point => point.device === device.id ? { ...point, device: changes.id! } : point) } : {}) });
        return <fieldset key={index} className="grid min-w-0 gap-3 rounded border border-border p-3 sm:grid-cols-2"><legend className="px-1 text-xs font-medium">{device.title || device.id || t("Device {n}", { n: index + 1 })}</legend>
          <label className={fieldClass}>{t("Device ID")}<Input value={device.id} onChange={event => patch({ id: event.target.value })} /></label>
          <label className={fieldClass}>{t("Device title")}<Input value={device.title} onChange={event => patch({ title: event.target.value })} /></label>
          <PayloadFields fields={translated(profile.deviceFields)} values={device.settings ?? {}} onChange={settings => patch({ settings })} />
          <div className="sm:col-span-2"><Button size="sm" disabled={points.some(point => point.device === device.id)} onClick={() => change({ devices: devices.filter((_, at) => at !== index) })}>{t("Remove device")}</Button></div>
        </fieldset>;
      })}
      <div><Button size="sm" onClick={() => change({ devices: [...devices, { id: "", title: "", settings: {} }] })}>{t("Add device")}</Button></div>
    </Panel>
    <Panel title={t("Point table")} className="grid min-w-0 gap-3">
      <p className="text-xs text-muted">{t("Select a point to edit it. Addresses are checked by the installed driver during read-only preview.")}</p>
      <DataTable data={points.map((point, row) => ({ ...point, row }))} columns={columns} getRowId={row => String(row.row)} selectedId={String(selected)} onRowClick={row => setSelected(row.row)} height={Math.min(300, Math.max(100, points.length * 30 + 40))} empty={t("No points configured.")} />
      <div><Button size="sm" disabled={!devices.length} onClick={() => { setSelected(points.length); change({ points: [...points, { id: "", device: devices[0]!.id, address: "", type: "decimal", intervalMS: 1000 }] }); }}>{t("Add point")}</Button></div>
      {point && <fieldset className="grid min-w-0 gap-3 rounded border border-border p-3 sm:grid-cols-2"><legend className="px-1 text-xs font-medium">{t("Edit point {n}", { n: selected + 1 })}</legend>
        <label className={fieldClass}>{t("Point device")}<Select value={point.device} onChange={event => setPoint({ ...point, device: event.target.value })}><option value="">{t("Choose a device")}</option>{devices.map((device, index) => <option key={index} value={device.id}>{device.title || device.id || t("Device {n}", { n: index + 1 })}</option>)}</Select></label>
        <PayloadFields fields={pointFields()} values={{ ...point }} onChange={values => setPoint(values as Api.DriverPoint)} />
        <p className="text-xs text-muted sm:col-span-2">{t("Scaled value = decoded value × scale + offset. Empty scale and offset mean 1 and 0.")}</p>
        <div className="sm:col-span-2"><Button size="sm" disabled={commands.some(command => command.point === point.id)} onClick={() => { change({ points: points.filter((_, index) => index !== selected) }); setSelected(Math.max(0, selected - 1)); }}>{t("Remove point")}</Button></div>
      </fieldset>}
      <Disclosure summary={t("Import a point table")}>
        <div className="grid min-w-0 gap-3">
          <p className="text-xs text-muted">{t("Import CSV columns id, device, address, type, dataType, unit, scale, offset, intervalMS, or a JSON array with the same fields. The default interval is 1000 ms. Review rows before replacing the current table.")}</p>
          <label className={fieldClass}>{t("Point table file")}<Input type="file" accept=".csv,.json,text/csv,application/json" onChange={event => { const file = event.target.files?.[0]; if (file) void importFile(file); event.target.value = ""; }} /></label>
          {importError && <p role="alert" className="text-xs text-danger">{importError}</p>}
          {!!rows.length && <>
            <DataTable data={rows} columns={importColumns} getRowId={row => String(row.line)} height={260} />
            <p className="text-xs text-muted">{t("{rows} rows · {errors} with issues", { rows: rows.length, errors: rows.filter(row => row.errors.length).length })}</p>
            <div className="flex flex-wrap gap-2"><Button size="sm" disabled={rows.some(row => row.errors.length)} onClick={() => { change({ points: rows.map(row => row.point) }); setSelected(0); setRows([]); }}>{t("Replace points with this table")}</Button><Button size="sm" onClick={() => { setRows([]); setImportError(""); }}>{t("Cancel import")}</Button></div>
          </>}
        </div>
      </Disclosure>
    </Panel>
    {profile.operations.includes("execute") ? <Panel title={t("Named commands")} className="grid min-w-0 gap-3">
      <p className="text-xs text-muted">{t("Commands can only target declared points. Business actions use Dispatch to request them; preview never sends a command.")}</p>
      {commands.map((command, index) => {
        const patch = (changes: Partial<Api.DriverCommand>) => change({ commands: commands.map((old, at) => at === index ? { ...old, ...changes } : old) });
        return <fieldset key={index} className="grid min-w-0 gap-3 rounded border border-border p-3 sm:grid-cols-2"><legend className="px-1 text-xs font-medium">{command.id || t("Command {n}", { n: index + 1 })}</legend>
          <label className={fieldClass}>{t("Command ID")}<Input value={command.id} onChange={event => patch({ id: event.target.value })} /></label>
          <label className={fieldClass}>{t("Command point")}<Select value={command.point} onChange={event => patch({ point: event.target.value })}><option value="">{t("Choose a point")}</option>{points.map((point, at) => <option key={at} value={point.id}>{point.id || t("Point {n}", { n: at + 1 })}</option>)}</Select></label>
          <label className={fieldClass}>{t("Minimum command value")}<Input value={command.min ?? ""} onChange={event => patch({ min: event.target.value })} /></label>
          <label className={fieldClass}>{t("Maximum command value")}<Input value={command.max ?? ""} onChange={event => patch({ max: event.target.value })} /></label>
          <div className="sm:col-span-2"><Button size="sm" onClick={() => change({ commands: commands.filter((_, at) => at !== index) })}>{t("Remove command")}</Button></div>
        </fieldset>;
      })}
      <div><Button size="sm" disabled={!points.length} onClick={() => change({ commands: [...commands, { id: "", point: points[0]!.id }] })}>{t("Add command")}</Button></div>
    </Panel> : <p className="text-xs text-muted">{t("This profile supports reading only. Device commands are unavailable.")}</p>}
  </div>;
}

export function ProtocolSamples({ samples }: { samples: Api.DriverSample[] }) {
  const columns: ColumnDef<Api.DriverSample & { row: number }>[] = [
    { accessorKey: "signal", header: t("Signal ID"), meta: { pin: "left", width: 150 } },
    { accessorKey: "value", header: t("Value"), cell: cell => String(cell.getValue() ?? "—"), meta: { width: 85 } },
    { accessorKey: "quality", header: t("Quality"), cell: cell => t(cell.getValue<string>()), meta: { width: 85 } },
    { accessorKey: "unit", header: t("Unit"), meta: { width: 70 } },
    { accessorKey: "device", header: t("Device ID"), meta: { width: 140 } },
    { accessorKey: "at", header: t("Sample time"), cell: cell => new Date(cell.getValue<string>()).toLocaleString(), meta: { width: 175 } },
    { accessorKey: "reason", header: t("Reason"), meta: { width: 220 } },
  ];
  return <DataTable data={samples.map((sample, row) => ({ ...sample, row }))} columns={columns} getRowId={sample => String(sample.row)} height={Math.min(300, Math.max(100, samples.length * 30 + 40))} empty={t("No sample values were returned.")} />;
}
