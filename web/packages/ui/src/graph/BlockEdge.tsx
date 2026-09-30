import { BaseEdge, EdgeLabelRenderer, getBezierPath, getSmoothStepPath, type Edge, type EdgeProps } from "@xyflow/react";
import { Plus } from "lucide-react";
import { createContext, useContext } from "react";
import { t } from "../i18n";
import type { CanvasEdge, CanvasPosition } from "./model";

export type BlockEdgeData = Record<string, unknown> & { definition: CanvasEdge; editable: boolean };
export type FlowBlockEdge = Edge<BlockEdgeData>;
export const EdgeInteraction = createContext<{ onInsert: (edge: CanvasEdge, position: CanvasPosition) => void }>({ onInsert: () => {} });

export function BlockEdge(props: EdgeProps<FlowBlockEdge>) {
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, markerEnd, selected } = props;
  const { onInsert } = useContext(EdgeInteraction);
  const definition = data?.definition;
  const isData = definition?.channel === "data";
  const pathArgs = { sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition };
  const [path, labelX, labelY] = isData ? getBezierPath(pathArgs) : getSmoothStepPath({ ...pathArgs, borderRadius: 18 });
  const color = definition?.tone ? `var(--tone-${definition.tone})` : selected ? "var(--primary)" : isData ? "var(--chart-6)" : "var(--muted)";
  return <g className={`platform-block-edge ${selected ? "platform-block-edge-selected" : ""}`}>
    <BaseEdge id={id} path={path} markerEnd={markerEnd} interactionWidth={24}
      style={{ stroke: color, strokeWidth: selected ? 2 : 1.7, strokeDasharray: definition?.dashed || isData ? "5 4" : undefined }} />
    <EdgeLabelRenderer>
      {definition?.label && <span className="platform-block-edge-label" style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY - 15}px)` }}>{definition.label}</span>}
      {data?.editable && !isData && definition && <button type="button" className="nodrag nopan platform-block-edge-add"
        style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
        aria-label={t("Insert block on connection")} title={t("Insert block on connection")}
        onClick={(event) => { event.stopPropagation(); onInsert(definition, { x: labelX, y: labelY }); }}><Plus className="size-3" /></button>}
    </EdgeLabelRenderer>
  </g>;
}
