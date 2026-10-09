import { BaseEdge, EdgeLabelRenderer, getBezierPath, getSmoothStepPath, type Edge, type EdgeProps } from "@xyflow/react";
import { Plus } from "lucide-react";
import { createContext, useContext } from "react";
import { t } from "../../i18n";
import { CanvasEdgeLabel, canvasEdgeColor } from "../core/edge";
import type { CanvasPosition } from "../core/types";
import type { FlowEdge } from "./model";

export type FlowEdgeData = Record<string, unknown> & { definition: FlowEdge; editable: boolean };
export type FlowLineEdge = Edge<FlowEdgeData>;
export const FlowEdgeInteraction = createContext<{ onInsert: (edge: FlowEdge, position: CanvasPosition) => void }>({ onInsert: () => {} });

/** A control line is the order things happen in and steps around blocks; a data
 * line is a typed value travelling and is drawn dashed and curved instead. */
export function FlowEdgeView(props: EdgeProps<FlowLineEdge>) {
  const { id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, markerEnd, selected } = props;
  const { onInsert } = useContext(FlowEdgeInteraction);
  const definition = data?.definition;
  const isData = definition?.channel === "data";
  const pathArgs = { sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition };
  const [path, labelX, labelY] = isData ? getBezierPath(pathArgs) : getSmoothStepPath({ ...pathArgs, borderRadius: 18 });
  const color = canvasEdgeColor(definition?.tone, selected, isData ? "var(--chart-6)" : "var(--muted)");
  return <g className={`platform-canvas-edge ${selected ? "platform-canvas-edge-selected" : ""}`}>
    <BaseEdge id={id} path={path} markerEnd={markerEnd} interactionWidth={24}
      style={{ stroke: color, strokeWidth: selected ? 2 : 1.7, strokeDasharray: definition?.dashed || isData ? "5 4" : undefined }} />
    {definition?.label && <CanvasEdgeLabel x={labelX} y={labelY} offset={-15}>{definition.label}</CanvasEdgeLabel>}
    {data?.editable && !isData && definition && <EdgeInsert x={labelX} y={labelY} edge={definition} onInsert={onInsert} />}
  </g>;
}

function EdgeInsert({ x, y, edge, onInsert }: { x: number; y: number; edge: FlowEdge; onInsert: (edge: FlowEdge, position: CanvasPosition) => void }) {
  return <EdgeLabelRenderer>
    <button type="button" className="nodrag nopan platform-flow-edge-add"
      style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)` }}
      aria-label={t("Insert block on connection")} title={t("Insert block on connection")}
      onClick={(event) => { event.stopPropagation(); onInsert(edge, { x, y }); }}><Plus className="size-3" /></button>
  </EdgeLabelRenderer>;
}
