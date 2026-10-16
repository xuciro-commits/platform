import { useCallback, useMemo, useState } from "react";
import { RelationCanvas, relationNodeClasses, t, type RelationEdge, type RelationNode } from "@platform/ui";
import { assetKey, definitionPresentation, useHost, type AssetRef } from "../index";
import { useDependencies } from "./dependencies";

/** Draw the host's authorized forward/reverse result; never resolve a second graph. */
export function DependencyGraph({ asset, app, height = 360, labels, onSelect, onOpen }: { asset?: AssetRef; app?: string; height?: number; labels?: Record<string, string>; onSelect?: (ref: AssetRef) => void; onOpen?: (ref: AssetRef) => void }) {
  const { definitions } = useHost();
  const query = useDependencies(asset);
  const focus = asset ? assetKey(asset) : "";
  const descriptors = query.data?.descriptors ?? [];
  const { nodes, edges } = useMemo(() => {
    const byKey = new Map(descriptors.map(item => [assetKey(item.ref), item]));
    const titles = new Map(definitions.map(item => [assetKey(item.ref), item]));
    const keep = new Set<string>(focus
      ? [focus, ...(query.data?.requires ?? []).map(assetKey), ...(query.data?.usedBy ?? []).map(assetKey)]
      : descriptors.filter(item => !app || item.ref.app === app).flatMap(item => [assetKey(item.ref), ...item.requires.map(assetKey)]));
    const nodes: RelationNode[] = [...keep].filter(key => byKey.has(key)).map(key => {
      const descriptor = byKey.get(key)!, kind = descriptor.ref.kind;
      const caption = `${t(relationNodeClasses[kind]?.title ?? kind)} · ${descriptor.ref.app} · ${descriptor.version}`;
      const definition = titles.get(key);
      return { id: key, label: labels?.[key] ?? (definition ? definitionPresentation(definition).title : descriptor.ref.name), class: kind,
        tone: kind === "object" ? "warning" : relationNodeClasses[kind] ? undefined : "neutral", caption, detail: caption };
    });
    const visible = new Set(nodes.map(node => node.id));
    const edges: RelationEdge[] = nodes.flatMap(node => (byKey.get(node.id)?.requires ?? [])
      .filter(required => visible.has(assetKey(required)))
      .map(required => ({ id: `${assetKey(required)}>${node.id}`, source: assetKey(required), target: node.id, directed: true })));
    return { nodes, edges };
  }, [descriptors, definitions, focus, app, query.data, labels]);
  const drawing = JSON.stringify([focus || app || "all", nodes.map(node => node.id), edges.map(edge => edge.id)]);
  const [prepared, setPrepared] = useState<string>();
  const [fitted, setFitted] = useState(false);
  const layoutReady = useCallback(() => setPrepared(drawing), [drawing]);
  const resolve = (key: string) => descriptors.find(item => assetKey(item.ref) === key)?.ref;
  if (query.isError) return <p role="alert" className="text-sm text-danger">{t("Asset dependencies could not be loaded.")}</p>;
  if (query.isPending) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  if (!nodes.length) return <p className="text-sm text-muted">{t("No definitions available.")}</p>;
  return <div role="group" aria-label={t("Dependency diagram")} aria-busy={prepared !== drawing || !fitted}>
    <RelationCanvas key={focus || app || "all"} layout="right" nodes={nodes} edges={edges} selected={focus || undefined} height={height} label={t("Declared dependencies")} storeKey={`lineage:${focus || `app:${app ?? "all"}`}`} onLayoutReady={layoutReady} onViewportReady={setFitted} onSelect={key => { const ref = key && resolve(key); if (ref) onSelect?.(ref); }} onOpen={key => { const ref = resolve(key); if (ref) onOpen?.(ref); }} />
  </div>;
}
