import type { Host } from "@platform/app";
import { Button, Dialog, formatDateTime, t, type EntityRecord, type RecordView } from "@platform/ui";
import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { Value } from "./diff";

type Row = { name: string; title: string; theirs: unknown; mine: unknown; changed: boolean };
type Keep = "mine" | "theirs";
export type EditConflict = { title: string; by: string; at: string; rows: Row[]; resolve: (keep: Keep) => void };

const Settlers = createContext<Map<string, () => void> | null>(null);
const same = (left: unknown, right: unknown) => JSON.stringify(left ?? null) === JSON.stringify(right ?? null);

export function ConflictSettlers({ settlers, children }: { settlers: Map<string, () => void>; children: ReactNode }) {
  return <Settlers.Provider value={settlers}>{children}</Settlers.Provider>;
}

export function useConflictSettle(type: string, id: string | undefined, busy: boolean, settle?: () => void) {
  const settlers = useContext(Settlers), latest = useRef(settle), [pending, setPending] = useState(false);
  latest.current = settle;
  const available = !!settle;
  useEffect(() => {
    if (!settlers || !id || !available) return;
    const key = `${type}/${id}`, request = () => setPending(true);
    settlers.set(key, request);
    return () => { if (settlers.get(key) === request) settlers.delete(key); };
  }, [settlers, type, id, available]);
  useEffect(() => {
    if (!pending || busy) return;
    setPending(false); latest.current?.();
  }, [pending, busy]);
}

export async function editConflict(host: Host, type: string, id: string, expected: number, payload: unknown): Promise<Omit<EditConflict, "resolve"> & { revision: number } | undefined> {
  let view: RecordView | undefined, current: EntityRecord | undefined;
  for (let attempt = 0; attempt < 8; attempt++) {
    view = await host.source.get(type, id).catch(() => undefined) as RecordView | undefined;
    current = view?.record as EntityRecord | undefined;
    if (!current || current.revision !== expected) break;
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  if (!view || !current || current.revision === expected) return;
  const theirs = new Set((view?.history ?? []).slice(0, Math.max(0, current.revision - expected)).flatMap(change => change.fields.map(field => field.field)));
  const mine = (payload ?? {}) as Record<string, unknown>, fields = host.source.entity(type)?.fields ?? [];
  const names = [...new Set([...theirs, ...Object.keys(mine).filter(name => !same(mine[name], current[name]))])];
  const rows = names.map(name => ({ name, title: fields.find(field => field.name === name)?.title || name, theirs: current[name], mine: Object.hasOwn(mine, name) ? mine[name] : current[name], changed: Object.hasOwn(mine, name) && !same(mine[name], current[name]) }));
  return { title: String(current.title ?? current.name ?? id), by: current.changed?.by ?? "", at: current.changed?.at ?? "", rows, revision: current.revision };
}

export function ConflictDialog({ conflict }: { conflict?: EditConflict }) {
  const [busy, setBusy] = useState(false);
  const choose = (keep: Keep) => { setBusy(true); conflict?.resolve(keep); };
  useEffect(() => setBusy(false), [conflict]);
  return <Dialog open={!!conflict} wide title={t("Someone else saved this first")} onOpenChange={next => { if (!next && !busy) choose("theirs"); }}>
    {conflict && <div className="grid gap-3 text-sm">
      <p>{t("{who} saved {name} at {at}, after you started editing. Choose which version to keep; nothing is overwritten until you choose.", { who: conflict.by || t("Another builder"), name: conflict.title, at: formatDateTime(conflict.at) })}</p>
      <table className="w-full table-fixed text-xs" aria-label={t("Conflicting fields")}>
        <thead><tr className="text-left text-muted"><th className="w-40 py-1 font-normal">{t("Field")}</th><th className="py-1 font-normal">{t("Their version")}</th><th className="py-1 font-normal">{t("Your version")}</th></tr></thead>
        <tbody>{conflict.rows.map(row => <tr key={row.name} className="border-t border-border align-top">
          <td className="py-1.5 pr-2 text-muted">{row.title}</td>
          <td className="py-1.5 pr-2"><Value value={row.theirs} /></td>
          <td className="py-1.5">{row.changed ? <Value value={row.mine} className="font-medium" /> : <span className="text-muted">{t("Same as theirs")}</span>}</td>
        </tr>)}</tbody>
      </table>
      <div className="flex justify-end gap-2">
        <Button disabled={busy} onClick={() => choose("theirs")}>{t("Keep theirs")}</Button>
        <Button variant="primary" loading={busy} onClick={() => choose("mine")}>{t("Keep mine")}</Button>
      </div>
    </div>}
  </Dialog>;
}
