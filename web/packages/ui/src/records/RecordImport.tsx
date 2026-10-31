import { useState } from "react";
import { Button } from "../primitives/button";
import { Dialog } from "../primitives/dialog";
import { FilePicker } from "../primitives/controls";
import { humanizeKernelError } from "../lib/errors";
import { t } from "../i18n";
import type { Api } from "@platform/kernel";
import type { RecordSource } from "./Records";

export function RecordImportDialog({ source, type, open, onOpenChange, onImported }: {
  source: RecordSource; type: string; open: boolean; onOpenChange: (open: boolean) => void;
  onImported?: (applied: number, refused: number) => void;
}) {
  const [csv, setCsv] = useState<string>();
  const [preview, setPreview] = useState<Api.ImportRow[]>();
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string>();
  const run = async (apply: boolean) => {
    if (!source.importCSV || csv === undefined) return;
    setBusy(true); setNotice(undefined);
    try {
      const rows = await source.importCSV(type, csv, !apply);
      if (apply) {
        const refused = rows.filter((row) => row.error).length;
        onOpenChange(false); setCsv(undefined); setPreview(undefined);
        onImported?.(rows.length - refused, refused);
        return;
      }
      setPreview(rows);
    } catch (error) {
      setNotice(humanizeKernelError(error instanceof Error ? error.message : String(error)));
    } finally { setBusy(false); }
  };
  return <Dialog open={open} onOpenChange={onOpenChange} title={t("Import CSV")}>
    <div className="grid gap-3 p-1 text-sm">
      <p className="text-muted">{t("A CSV whose header names this type's fields, with an id column. Every row is checked first, then created or edited as you.")}</p>
      <FilePicker accept=".csv,text/csv" onFile={(file) => void file.text().then((text) => { setCsv(text); setPreview(undefined); setNotice(undefined); })} />
      {notice && <p role="alert" className="text-xs text-danger">{notice}</p>}
      {preview && (() => {
        const refused = preview.filter((row) => row.error);
        return <div className="grid gap-2">
          <p role="status">{t("{total} rows checked: {ok} apply cleanly, {refused} are refused.", { total: preview.length, ok: preview.length - refused.length, refused: refused.length })}</p>
          {refused.length > 0 && <ul className="grid max-h-40 gap-1 overflow-auto rounded border border-border p-2 text-xs">
            {refused.slice(0, 20).map((row, index) => <li key={index} className="break-words">{t("Row {row} ({id}): {reason}", { row: row.row, id: row.id, reason: row.error?.detail ?? row.error?.message ?? row.outcome })}</li>)}
          </ul>}
          <div className="flex gap-2">
            <Button size="sm" variant="primary" loading={busy} disabled={refused.length === preview.length} onClick={() => void run(true)}>{t("Import {ok} rows", { ok: preview.length - refused.length })}</Button>
            <Button size="sm" variant="ghost" onClick={() => void run(false)}>{t("Check again")}</Button>
          </div>
        </div>;
      })()}
      {csv !== undefined && !preview && <Button size="sm" loading={busy} onClick={() => void run(false)}>{t("Check rows")}</Button>}
    </div>
  </Dialog>;
}
