// Lineage (ADR-0052 batch P4): what an installed asset is built from, drawn
// from `Definition.requires` — pages over object types, applications over
// pages, queries and functions over the types they read. One asset at a time,
// or every asset of an app when nothing is chosen.
import { assetKey, useDefinitions, type AssetRef, type Definition } from "@platform/app";
import { PageHeader, RelationCanvas, relationNodeClasses, Select, useWorkspace, type RelationEdge, type RelationNode, t } from "@platform/ui";
import { useMemo, useState } from "react";

// The kit's class table decides the glyph, the caption word and the default
// tone (ADR-0090 D1); this lineage keeps one override — a plain object type is
// the mutable thing here, so it stays amber beside its published siblings.
const toneOverrides: Record<string, RelationNode["tone"]> = { object: "warning" };
const kindLabel = (kind: string) => t(relationNodeClasses[kind]?.title ?? kind);
const titleOf = (d: Definition) => d.entity?.title ?? d.action?.title ?? d.page?.title ?? d.application?.title ?? d.ref.name;

export function Lineage({ ref: requested }: { ref?: string }) {
  const { data, isPending, error } = useDefinitions();
  const { open } = useWorkspace();
  const definitions = useMemo(() => data ?? [], [data]);
  const [picked, setPicked] = useState<string>();
  const apps = useMemo(() => [...new Set(definitions.map((d) => d.ref.app))].sort(), [definitions]);
  const [app, setApp] = useState<string>();
  const focus = picked ?? requested ?? "";
  const scopeApp = app ?? (focus ? focus.split("/")[0] : apps[0]) ?? "";
  const { nodes, edges } = useMemo(() => {
    const byKey = new Map(definitions.map((d) => [assetKey(d.ref), d] as const));
    const keep = new Set<string>();
    if (focus && byKey.has(focus)) {
      // Everything the chosen asset requires, transitively, and everything that requires it.
      const down = (key: string) => { if (keep.has(key)) return; keep.add(key); for (const r of byKey.get(key)?.requires ?? []) down(assetKey(r)); };
      down(focus);
      for (const d of definitions) if (d.requires.some((r) => assetKey(r) === focus)) keep.add(assetKey(d.ref));
    } else {
      for (const d of definitions) if (d.ref.app === scopeApp) { keep.add(assetKey(d.ref)); for (const r of d.requires) keep.add(assetKey(r)); }
    }
    const nodes: RelationNode[] = [...keep].map((key) => {
      const d = byKey.get(key);
      const [appId, kind, name] = key.split("/");
      const caption = `${kindLabel(kind ?? "")} · ${appId}${d?.version ? ` · ${d.version}` : ""}`;
      // The class decides the glyph and default tone; only the override rides
      // alongside, and a kind the kit has never heard of stays neutral.
      return { id: key, label: d ? titleOf(d) : name ?? key, class: kind || undefined,
        tone: kind && !relationNodeClasses[kind] ? "neutral" : toneOverrides[kind ?? ""], caption, detail: caption };
    });
    const edges: RelationEdge[] = [];
    for (const key of keep) for (const r of byKey.get(key)?.requires ?? []) { const to = assetKey(r); if (keep.has(to)) edges.push({ id: `${to}>${key}`, source: to, target: key, directed: true }); }
    return { nodes, edges };
  }, [definitions, focus, scopeApp]);
  const openAsset = (key: string) => {
    const d = definitions.find((x) => assetKey(x.ref) === key);
    if (!d) return;
    const ref: AssetRef = d.ref;
    if (d.page) return open({ view: "page", params: { app: ref.app, kind: ref.kind, name: ref.name } });
    open({ view: "definition", params: { app: ref.app, kind: ref.kind, name: ref.name } });
  };
  if (error) return <p role="alert" className="text-sm text-danger">{t("Definitions could not be loaded.")}</p>;
  if (isPending) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  return <>
    <PageHeader title={t("Lineage")} description={t("What each installed asset is built from, declared in its own definition: applications over pages, pages over object types, functions and queries over the types they read. It proves impact — what a change to an asset reaches — not history: no record moved here. Runtime data flow is on the object's Data lineage panel. Click a node to open it.")}
      actions={<>
        <Select aria-label={t("App")} value={scopeApp} className="w-40" onChange={(e) => { setApp(e.target.value); setPicked(""); }}>{apps.map((id) => <option key={id} value={id}>{id}</option>)}</Select>
        <Select aria-label={t("Asset")} value={focus} className="w-72" onChange={(e) => setPicked(e.target.value)}>
          <option value="">{t("Every asset of {app}", { app: scopeApp })}</option>
          {definitions.filter((d) => d.ref.app === scopeApp).map((d) => <option key={assetKey(d.ref)} value={assetKey(d.ref)}>{kindLabel(d.ref.kind)} · {titleOf(d)}</option>)}
        </Select>
      </>} />
    {nodes.length === 0 ? <p className="text-sm text-muted">{t("No definitions available.")}</p>
      : <RelationCanvas layout="tree-right" nodes={nodes} edges={edges} selected={focus || undefined} height={Math.max(360, (typeof window !== "undefined" ? window.innerHeight : 900) - 220)} label={t("Lineage")} storeKey={`lineage:${focus || `app:${scopeApp}`}`} onSelect={(id) => id && openAsset(id)} />}
  </>;
}
