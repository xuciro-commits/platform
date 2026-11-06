import { ChevronDown, ChevronUp, ChevronsDownUp, ChevronsUpDown } from "lucide-react";
import { createContext, useContext, useState } from "react";
import { t } from "../../i18n";
import { CanvasTool } from "./chrome";

export const CanvasFoldContext = createContext<{ toggle: (id: string) => void }>({ toggle: () => {} });

export function useCanvasFold<T extends { id: string; collapsed?: boolean }>(nodes: readonly T[], fallback?: (node: T) => boolean | undefined) {
  const [state, setState] = useState<Record<string, boolean>>({});
  const folded = (node: T) => state[node.id] ?? node.collapsed ?? fallback?.(node) ?? true;
  const allCollapsed = nodes.length > 0 && nodes.every(folded);
  const toggle = (id: string) => setState(current => {
    const node = nodes.find(item => item.id === id);
    return node ? { ...current, [id]: !folded(node) } : current;
  });
  const toggleAll = () => setState(Object.fromEntries(nodes.map(node => [node.id, !allCollapsed])));
  return { folded, allCollapsed, toggle, toggleAll, state };
}

export function CanvasNodeFold({ id, open }: { id: string; open: boolean }) {
  const { toggle } = useContext(CanvasFoldContext);
  const label = open ? t("Collapse node details") : t("Expand node details");
  return <button type="button" className="nodrag nopan platform-canvas-node-fold" aria-expanded={open}
    aria-label={label} title={label}
    onClick={event => { event.stopPropagation(); toggle(id); }}>
    {open ? <ChevronUp /> : <ChevronDown />}
  </button>;
}

export function CanvasFoldAllTool({ allCollapsed, onToggle }: { allCollapsed: boolean; onToggle: () => void }) {
  return <CanvasTool label={allCollapsed ? t("Expand all nodes") : t("Collapse all nodes")}
    icon={allCollapsed ? <ChevronsUpDown /> : <ChevronsDownUp />} onClick={onToggle} />;
}
