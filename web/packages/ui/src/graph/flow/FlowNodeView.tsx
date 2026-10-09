import { Handle, Position, useUpdateNodeInternals, type Node, type NodeProps } from "@xyflow/react";
import { AlertTriangle, Check, ChevronDown, ChevronUp, Clock, Loader2, Repeat } from "lucide-react";
import { createContext, useContext, useEffect, type CSSProperties, type ReactNode } from "react";
import { t } from "../../i18n";
import { cn } from "../../lib/cn";
import { flowBlockHeight, flowNodeWidth, flowShapeBox, type FlowNode, type FlowNodeKind, type FlowNodeStatus, type FlowPort } from "./model";
import { eventRing, flowNodeIcon, flowShape, loops, notationGlyph, notationOf, notationTitle, type FlowNotation, type FlowShape } from "./notation";

export type FlowNodeData = FlowNode & { definition: FlowNodeKind; direction: "right" | "down"; editable: boolean; collapsedView: boolean };
export type FlowShapeNode = Node<FlowNodeData, "block">;
export const FlowInteraction = createContext<{ onCollapse: (id: string) => void }>({ onCollapse: () => {} });

const statusMeta: Record<FlowNodeStatus, { label: string; tone: string; icon?: ReactNode }> = {
  idle: { label: "Not run", tone: "neutral" }, running: { label: "Running", tone: "info", icon: <Loader2 className="animate-spin" /> },
  waiting: { label: "Waiting", tone: "warning", icon: <Clock /> }, success: { label: "Succeeded", tone: "success", icon: <Check /> },
  error: { label: "Failed", tone: "danger", icon: <AlertTriangle /> }, skipped: { label: "Skipped", tone: "neutral" },
};

function Port({ port, index, count, input, compact, collapsed, direction }: {
  port: FlowPort; index: number; count: number; input: boolean; compact: boolean; collapsed: boolean; direction: "right" | "down";
}) {
  const vertical = direction === "down";
  const position = vertical ? input ? Position.Top : Position.Bottom : input ? Position.Left : Position.Right;
  const offset = compact || collapsed ? `${(index + 1) * 100 / (count + 1)}%` : 76 + index * 27 + 13;
  const color = port.channel === "data" ? "var(--chart-6)" : "var(--muted)";
  return <>
    <Handle type={input ? "target" : "source"} id={port.id} position={position}
      className={cn("platform-flow-port", port.channel === "data" && "platform-flow-port-data")}
      style={{ ...(vertical ? { left: `${(index + 1) * 100 / (count + 1)}%` } : { top: offset }), background: color }}
      title={`${port.label} · ${port.type}${port.description ? ` · ${port.description}` : ""}`}
      aria-label={t(input ? "Input port: {name}, {type}" : "Output port: {name}, {type}", { name: port.label, type: port.type })} />
    {!compact && !collapsed && !vertical && <div className={cn("platform-flow-port-label", input ? "platform-flow-port-label-input" : "platform-flow-port-label-output")}
      style={{ top: 76 + index * 27 }} title={`${port.label} · ${port.type}`}>
      <span>{port.label}</span><code>{port.type}</code>
    </div>}
  </>;
}

/** The BPMN outline of a compact shape: a diamond for a gateway, a ring for an
 * event whose thickness says start, middle or end, a doubled foot for a sub-process. */
function Shape({ shape, notation, color }: { shape: FlowShape; notation: FlowNotation; color: string }) {
  const square = shape === "gateway" ? 76 : 58;
  return <svg className="platform-flow-outline" width={square} height={square} viewBox="0 0 100 100" aria-hidden="true">
    {shape === "gateway"
      ? <polygon points="50,1 99,50 50,99 1,50" fill="var(--surface)" stroke={color} strokeWidth="2" />
      : <circle cx="50" cy="50" r={50 - eventRing(notation) / 2} fill="var(--surface)" stroke={color} strokeWidth={eventRing(notation)} />}
  </svg>;
}

/** One visual contract for a step, whether a person is authoring it or watching it run. */
export function FlowNodeView({ id, data, selected }: NodeProps<FlowShapeNode>) {
  const { definition: kind, compact, collapsedView: collapsed, direction } = data;
  const { onCollapse } = useContext(FlowInteraction);
  const update = useUpdateNodeInternals();
  const notation = data.notation ?? kind.notation ?? notationOf(kind.id);
  const shape = flowShape(notation, kind.id);
  const signature = JSON.stringify([kind.inputs, kind.outputs, compact, collapsed, direction, notation]);
  useEffect(() => { update(id); }, [id, signature, update]);
  const errors = data.diagnostics?.filter((issue) => issue.severity === "error") ?? [];
  const notices = data.diagnostics?.length ?? 0;
  const status = data.status ? statusMeta[data.status] : undefined;
  const color = errors.length ? "var(--tone-danger)" : data.tone ? `var(--tone-${data.tone})` : kind.tone ? `var(--tone-${kind.tone})` : compact ? "var(--border)" : "var(--primary)";
  const glyph = notationGlyph(notation);
  const ports = <>
    {kind.inputs.map((port, i) => <Port key={`input:${port.id}`} port={port} index={i} count={kind.inputs.length} input compact={!!compact} collapsed={collapsed} direction={direction} />)}
    {kind.outputs.map((port, i) => <Port key={`output:${port.id}`} port={port} index={i} count={kind.outputs.length} input={false} compact={!!compact} collapsed={collapsed} direction={direction} />)}
  </>;

  // A compact drawing is a BPMN diagram: gateways and events are their own shapes
  // with the name underneath, activities keep the block a reader can read at a glance.
  const looping = data.loop ?? loops.has(kind.id);
  if (compact && (shape === "gateway" || shape === "event")) {
    const box = flowShapeBox(shape);
    return <div className={cn("platform-flow-shape", `platform-flow-shape-${shape}`, selected && "platform-flow-shape-selected", data.current && "platform-flow-current")}
      style={{ width: box.width, height: box.height, "--block-color": color } as CSSProperties}
      aria-label={`${notationTitle(notation)}: ${data.label}`} title={[data.label, data.detail].filter(Boolean).join(" · ")}>
      <span className="platform-flow-figure" style={{ color, width: box.width, height: box.width }}>
        <Shape shape={shape} notation={notation} color={color} />
        <span className="platform-flow-marker" aria-hidden="true">{glyph}</span>
      </span>
      <span className="platform-flow-shape-label">{data.label}</span>
      {ports}
    </div>;
  }

  const block = compact ? { width: 160, height: 58 } : { width: kind.size?.width ?? flowNodeWidth, height: flowBlockHeight(kind, collapsed) };
  return <div className={cn("platform-flow-node", selected && "platform-flow-node-selected", compact && "platform-flow-node-compact",
    data.current && "platform-flow-current", shape === "subprocess" && "platform-flow-node-subprocess")}
    style={{ ...block, "--block-color": color } as CSSProperties}
    aria-label={compact ? data.label : `${notationTitle(notation)}: ${data.label}`}>
    {!compact && <span className="platform-flow-accent" />}
    <div className="platform-flow-heading">
      {!compact && <span className="platform-canvas-node-icon" aria-hidden="true" title={notationTitle(notation)}>{flowNodeIcon({ icon: kind.icon, class: kind.class, notation, id: kind.id })}</span>}
      <div className="min-w-0 flex-1">
        {!compact && <div className="platform-canvas-node-caption">{kind.title}{data.version && <span title={data.version}>{data.version}</span>}</div>}
        <div className="platform-canvas-node-title" title={data.label}>{data.label}</div>
        {compact && data.detail && <div className="platform-canvas-node-detail" title={data.detail}>{data.detail}</div>}
      </div>
      {(shape !== "task" || looping) && <span className="platform-flow-glyph" aria-hidden="true">
        {shape !== "task" && <span title={notationTitle(notation)}>{glyph}</span>}
        {looping && <span title={t("Repeats")}><Repeat /></span>}
      </span>}
      {!compact && (kind.inputs.length > 0 || kind.outputs.length > 0) && <button type="button" className="nodrag nopan platform-flow-collapse"
        onClick={(event) => { event.stopPropagation(); onCollapse(id); }} aria-label={t(collapsed ? "Expand block ports" : "Collapse block ports")}
        title={t(collapsed ? "Expand block ports" : "Collapse block ports")}>{collapsed ? <ChevronDown /> : <ChevronUp />}</button>}
    </div>
    {!compact && !collapsed && data.detail && <div className="platform-canvas-node-detail px-3" title={data.detail}>{data.detail}</div>}
    {ports}
    {/* Events attached to the activity: what it may time out on, what it escapes through. */}
    {!compact && !!data.boundary?.length && <div className="platform-flow-boundary" aria-label={t("Attached events")}>
      {data.boundary.map((event) => <span key={event.id} className="platform-flow-boundary-event"
        style={{ "--block-color": event.tone ? `var(--tone-${event.tone})` : color } as CSSProperties}
        title={event.label ?? notationTitle(event.notation)} aria-label={event.label ?? notationTitle(event.notation)}>
        {notationGlyph(event.notation)}</span>)}
    </div>}
    {!compact && (status || notices > 0) && <div className="platform-flow-status">
      {status && <span style={{ color: `var(--tone-${status.tone})` }} role={data.status === "running" ? "status" : undefined}>{status.icon}{t(status.label)}</span>}
      {notices > 0 && <span className="ml-auto" style={{ color: errors.length ? "var(--tone-danger)" : "var(--tone-warning)" }}
        title={data.diagnostics!.map((issue) => issue.message).join("\n")}><AlertTriangle />{notices}</span>}
    </div>}
  </div>;
}
