import { BaseEdge, Handle, Position, useUpdateNodeInternals, type Edge, type EdgeProps, type Node, type NodeProps } from "@xyflow/react";
import { Box } from "lucide-react";
import { useEffect, type CSSProperties } from "react";
import { cn } from "../../lib/cn";
import { IconGlyph } from "../../components/IconPicker";
import { t } from "../../i18n";
import { CanvasEdgeLabel, canvasEdgeColor } from "../core/edge";
import { routePath, type Route } from "../core/route";
import { relationNodeCaption, relationNodeClasses, relationNodeSize, type RelationEdge, type RelationNode } from "./model";
import { Tooltip } from "../../primitives/tooltip";
import { CanvasNodeFold } from "../core/fold";

type RelationNodeData = RelationNode & { linking: boolean; acceptsConnections: boolean; faded?: boolean; collapsedView: boolean };
export type RelationShapeNode = Node<RelationNodeData>;
export type RelationLineEdge = Edge<RelationEdge & { route?: Route; quiet?: boolean; faded?: boolean } & Record<string, unknown>>;

function Sides({ data }: { data: RelationNodeData }) {
  return <>
    {[Position.Top, Position.Right, Position.Bottom, Position.Left].map((side) => <Handle key={side} id={side} type="source" position={side}
      className={cn("platform-relation-handle", data.linking && data.linkable !== false && "platform-relation-handle-active")}
      isConnectable={data.acceptsConnections && data.linkable !== false} isConnectableStart={data.linking} />)}
  </>;
}

function RelationNodeView({ id, data, selected }: NodeProps<RelationShapeNode>) {
  const update = useUpdateNodeInternals();
  useEffect(() => { update(id); }, [id, data.collapsedView, data.facts, data.detail, update]);
  const classDecl = data.class ? relationNodeClasses[data.class] : undefined;
  const color = (data.tone ?? classDecl?.tone) ? `var(--tone-${data.tone ?? classDecl?.tone})` : "var(--primary)";
  const accessible = [data.label, relationNodeCaption(data), data.flag, selected ? t("Selected") : ""].filter(Boolean).join(" · ");
  if (data.badge) return <div className={cn("platform-relation-badge", selected && "platform-relation-badge-selected", data.linking && "platform-relation-node-linking")}
    style={{ width: data.badge.size, height: data.badge.size, background: color, borderColor: color } as CSSProperties}
    aria-label={accessible} aria-current={selected ? "true" : undefined} title={[data.label, data.detail].filter(Boolean).join(" · ")}>
    <span aria-hidden="true" className="max-w-full overflow-hidden text-ellipsis whitespace-nowrap px-1" style={{ fontSize: data.badge.size > 24 ? 8 : 7 }}>{data.badge.label}</span>
    <Sides data={data} />
  </div>;
  const caption = relationNodeCaption(data);
  const expandable = !data.badge && !!(data.facts?.length || data.detail);
  return <div className={cn("platform-relation-node", selected && "platform-relation-node-selected", data.dim && "opacity-60", data.faded && "opacity-30", data.linking && "platform-relation-node-linking")}
    style={{ width: data.size?.width ?? relationNodeSize.width, minHeight: data.size?.height ?? relationNodeSize.height, "--block-color": color } as CSSProperties} title={data.detail ?? data.label} aria-label={accessible} aria-current={selected ? "true" : undefined} aria-expanded={expandable ? !data.collapsedView : undefined}>
    <div className="platform-relation-heading">
      <span className="platform-canvas-node-icon" aria-hidden="true">{data.icon ?? (classDecl ? <IconGlyph name={classDecl.icon} /> : <Box />)}</span>
      <span className="min-w-0 flex-1">
        {caption && <span className="platform-canvas-node-caption"><span className="!max-w-full">{t(caption)}</span></span>}
        <span className="platform-canvas-node-title block">{data.label}</span>
      </span>
      {data.flag && <Tooltip content={data.flag}><span className="platform-relation-flag" /></Tooltip>}
      {expandable && <CanvasNodeFold id={data.id} open={!data.collapsedView} />}
    </div>
    <Sides data={data} />
    {expandable && !data.collapsedView && <div className="platform-relation-details" role="group" aria-label={t("Node details")} onClick={event => event.stopPropagation()}>
      {data.detail && <p className="break-words text-xs text-muted">{data.detail}</p>}
      {!!data.facts?.length && <dl className="mt-1 grid gap-1 text-xs">
        {data.facts.map(fact => <div key={fact.label} className="grid gap-0.5">
          <dt className="text-2xs uppercase tracking-wide text-muted">{fact.label}</dt>
          <dd className="break-words">{fact.value}</dd>
        </div>)}
      </dl>}
    </div>}
  </div>;
}

function RelationEdgeView({ id, sourceX, sourceY, targetX, targetY, data, markerEnd, selected }: EdgeProps<RelationLineEdge>) {
  const { path, label } = routePath(data?.route?.points ?? [{ x: sourceX, y: sourceY }, { x: targetX, y: targetY }]);
  const color = canvasEdgeColor(data?.tone, selected, "var(--muted)");
  return <g className={cn("platform-canvas-edge", selected && "platform-canvas-edge-selected", data?.faded && "opacity-30")}>
    <BaseEdge id={id} path={path} markerEnd={markerEnd} interactionWidth={20} style={{ stroke: color, strokeWidth: selected ? 2 : 1.4, strokeDasharray: data?.dashed ? "5 4" : undefined }} />
    {data?.label && (!data.quiet || selected) && <CanvasEdgeLabel x={label.x} y={label.y}>{data.label}</CanvasEdgeLabel>}
  </g>;
}

export const relationNodeTypes = { entity: RelationNodeView };
export const relationEdgeTypes = { relation: RelationEdgeView };
