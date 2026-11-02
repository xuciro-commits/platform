import { useEffect, useState } from "react";
import { Button } from "../primitives/button";
import { Select } from "../primitives/input";
import { t } from "../i18n";
import { RecordLookup } from "./RecordLookup";
import type { RecordSource } from "./Records";
type Attachment = { mediaSet: string; item: string };
type Item = { hash: string; name: string };
const reads = new WeakMap<RecordSource, Map<string, Promise<Item[]>>>();
function mediaItems(source: RecordSource, id: string, item?: string): Promise<Item[]> {
  let pending = reads.get(source);
  if (!pending) { pending = new Map(); reads.set(source, pending); }
  const key = `${id}:${item ?? ""}`;
  let read = pending.get(key);
  if (!read) {
    read = (async () => {
      const current = await source.get("build.mediaset", id);
      const items = [...((current.record.items as Item[]) ?? [])];
      if (item && !items.some(current => current.hash === item)) {
        let offset = 0;
        for (;;) {
          const page = await source.list("build.mediasetversion", { domain: [["mediaSet", "=", id]], sort: ["-version"], offset, limit: 200 });
          for (const record of page.records) items.push(...((record.items as Item[]) ?? []));
          offset += page.records.length;
          if (items.some(current => current.hash === item) || !page.records.length || offset >= page.total) break;
        }
      }
      return items;
    })().finally(() => pending.delete(key));
    pending.set(key, read);
  }
  return read;
}
export function MediaAttachment({ source, value, onChange, id }: { source: RecordSource; value?: Attachment; onChange?: (value?: Attachment) => void; id?: string }) {
  const [items, setItems] = useState<Item[]>([]), [error, setError] = useState("");
  useEffect(() => { let active = true; setItems([]); setError(""); if (value?.mediaSet) void mediaItems(source, value.mediaSet, onChange ? undefined : value.item).then(next => { if (active) setItems(next); }, () => { if (active) setError(t("Media set could not be loaded.")); }); return () => { active = false; }; }, [source, value?.mediaSet, value?.item, !!onChange]);
  if (!onChange) {
    const name = items.find(item => item.hash === value?.item)?.name ?? value?.item ?? "—";
    return <span>{source.downloadMedia && value?.item ? <Button size="sm" variant="ghost" onClick={() => void source.downloadMedia!(value.mediaSet, value.item).then(file => { const url = URL.createObjectURL(file.blob); const link = document.createElement("a"); link.href = url; link.download = file.name || name; link.click(); URL.revokeObjectURL(url); }, () => setError(t("File could not be downloaded.")))}>{name}</Button> : name}{error && <span role="alert">{error}</span>}</span>;
  }
  return <div className="grid gap-2"><RecordLookup id={id} source={source} type="build.mediaset" value={value?.mediaSet} ariaLabel={t("Media set")} onChange={mediaSet => onChange(mediaSet ? { mediaSet, item: "" } : undefined)} />{value?.mediaSet && <Select aria-label={t("Media item")} value={value.item} onChange={event => onChange({ ...value, item: event.target.value })}><option value="">{t("Choose a file")}</option>{items.map(item => <option key={item.hash} value={item.hash}>{item.name}</option>)}</Select>}{error && <span role="alert">{error}</span>}</div>;
}
