import { BaseEdge, Handle, Position, getSmoothStepPath, type Edge, type EdgeProps, type Node, type NodeProps } from "@xyflow/react";
import { Box } from "lucide-react";
import type { CSSProperties } from "react";
import { cn } from "../../lib/cn";
import { IconGlyph } from "../../components/IconPicker";
import { t } from "../../i18n";
import { CanvasEdgeLabel, canvasEdgeColor } from "../core/edge";
import { relationNodeCaption, relationNodeClasses, relationNodeSize, type RelationEdge, type RelationNode } from "./model";

export type RelationNodeData = RelationNode & { linking: boolean; acceptsConnections: boolean };
export type RelationShapeNode = Node<RelationNodeData>;
export type RelationLineEdge = Edge<RelationEdge & Record<string, unknown>>;

/** The four sides an element can be joined from; the line picks the two that face
 * each other, so the owner's source/target meaning never changes (ADR-0085 D5). */
function Sides({ data }: { data: RelationNodeData }) {
  return <>
    {[Position.Top, Position.Right, Position.Bottom, Position.Left].map((side) => <Handle key={side} id={side} type="source" position={side}
      className={cn("platform-relation-handle", data.linking && data.linkable !== false && "platform-relation-handle-active")}
      isConnectable={data.acceptsConnections && data.linkable !== false} isConnectableStart={data.linking} />)}
  </>;
}

export function RelationNodeView({ data, selected }: NodeProps<RelationShapeNode>) {
  const classDecl = data.class ? relationNodeClasses[data.class] : undefined;
  const color = (data.tone ?? classDecl?.tone) ? `var(--tone-${data.tone ?? classDecl?.tone})` : "var(--primary)";
  // A disc is what a reader needs when the only question is which neighbour is which.
  if (data.badge) return <div className={cn("platform-relation-badge", selected && "platform-relation-badge-selected", data.linking && "platform-relation-node-linking")}
    style={{ width: data.badge.size, height: data.badge.size, background: color, borderColor: color } as CSSProperties}
    aria-label={data.label} title={[data.label, data.detail].filter(Boolean).join(" · ")}>
    <span aria-hidden="true" className="max-w-full overflow-hidden text-ellipsis whitespace-nowrap px-1" style={{ fontSize: data.badge.size > 24 ? 8 : 7 }}>{data.badge.label}</span>
    <Sides data={data} />
  </div>;
  const caption = relationNodeCaption(data);
  return <div className={cn("platform-relation-node", selected && "platform-relation-node-selected", data.dim && "opacity-60", data.linking && "platform-relation-node-linking")}
    style={{ width: data.size?.width ?? relationNodeSize.width, minHeight: data.size?.height ?? relationNodeSize.height, "--block-color": color } as CSSProperties} title={data.detail ?? data.label} aria-label={data.label}>
    <span className="platform-canvas-node-icon" aria-hidden="true">{data.icon ?? (classDecl ? <IconGlyph name={classDecl.icon} /> : <Box />)}</span>
    <span className="min-w-0 flex-1">
      {caption && <span className="platform-canvas-node-caption"><span className="!max-w-full">{t(caption)}</span></span>}
      <span className="platform-canvas-node-title block">{data.label}</span>
    </span>
    {data.flag && <span className="platform-relation-flag" title={data.flag} />}
    <Sides data={data} />
  </div>;
}

export function RelationEdgeView({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, markerEnd, selected }: EdgeProps<RelationLineEdge>) {
  const [path, x, y] = getSmoothStepPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, borderRadius: 14 });
  const color = canvasEdgeColor(data?.tone, selected, "var(--muted)");
  return <g className={cn("platform-canvas-edge", selected && "platform-canvas-edge-selected")}>
    <BaseEdge id={id} path={path} markerEnd={markerEnd} interactionWidth={20} style={{ stroke: color, strokeWidth: selected ? 2 : 1.4, strokeDasharray: data?.dashed ? "5 4" : undefined }} />
    {data?.label && <CanvasEdgeLabel x={x} y={y}>{data.label}</CanvasEdgeLabel>}
  </g>;
}

export const relationNodeTypes = { entity: RelationNodeView };
export const relationEdgeTypes = { relation: RelationEdgeView };
