import { Handle, Position, useUpdateNodeInternals, type Node, type NodeProps } from "@xyflow/react";
import { AlertTriangle, ArrowRightLeft, Blocks, Brain, Check, ChevronDown, ChevronUp, Clock, Code2, GitBranch, Loader2, Play, Workflow } from "lucide-react";
import { createContext, useContext, useEffect, type CSSProperties, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { t } from "../i18n";
import { blockNodeHeight, canvasNodeWidth, type BlockStatus, type CanvasNode, type NodeKind, type NodePort } from "./model";

export type BlockNodeData = CanvasNode & { definition: NodeKind; direction: "right" | "down"; editable: boolean; collapsedView: boolean };
export type FlowBlockNode = Node<BlockNodeData>;
export const BlockInteraction = createContext<{ onCollapse: (id: string) => void }>({ onCollapse: () => {} });
const icons: Record<string, ReactNode> = {
  trigger: <Play />, control: <GitBranch />, flow: <Workflow />, workflow: <Workflow />, lifecycle: <Workflow />,
  compute: <Code2 />, transform: <ArrowRightLeft />, ai: <Brain />, function: <Brain />, action: <Play />, query: <ArrowRightLeft />,
};
const statusMeta: Record<BlockStatus, { label: string; tone: string; icon?: ReactNode }> = {
  idle: { label: "Not run", tone: "neutral" }, running: { label: "Running", tone: "info", icon: <Loader2 className="animate-spin" /> },
  waiting: { label: "Waiting", tone: "warning", icon: <Clock /> }, success: { label: "Succeeded", tone: "success", icon: <Check /> },
  error: { label: "Failed", tone: "danger", icon: <AlertTriangle /> }, skipped: { label: "Skipped", tone: "neutral" },
};

function Port({ port, index, count, input, compact, collapsed, direction }: {
  port: NodePort; index: number; count: number; input: boolean; compact: boolean; collapsed: boolean; direction: "right" | "down";
}) {
  const vertical = direction === "down";
  const position = vertical ? input ? Position.Top : Position.Bottom : input ? Position.Left : Position.Right;
  const offset = compact || collapsed ? `${(index + 1) * 100 / (count + 1)}%` : 76 + index * 27 + 13;
  const color = port.channel === "data" ? "var(--chart-6)" : "var(--muted)";
  return <>
    <Handle type={input ? "target" : "source"} id={port.id} position={position}
      className={cn("platform-block-port", port.channel === "data" && "platform-block-port-data")}
      style={{ ...(vertical ? { left: `${(index + 1) * 100 / (count + 1)}%` } : { top: offset }), background: color }}
      title={`${port.label} · ${port.type}${port.description ? ` · ${port.description}` : ""}`}
      aria-label={t(input ? "Input port: {name}, {type}" : "Output port: {name}, {type}", { name: port.label, type: port.type })} />
    {!compact && !collapsed && !vertical && <div className={cn("platform-block-port-label", input ? "platform-block-port-label-input" : "platform-block-port-label-output")}
      style={{ top: 76 + index * 27 }} title={`${port.label} · ${port.type}`}>
      <span>{port.label}</span><code>{port.type}</code>
    </div>}
  </>;
}

/** One visual block contract for authoring and execution observation. */
export function BlockNode({ id, data, selected }: NodeProps<FlowBlockNode>) {
  const { definition: kind, compact, collapsedView: collapsed, direction } = data;
  const { onCollapse } = useContext(BlockInteraction);
  const update = useUpdateNodeInternals();
  const signature = JSON.stringify([kind.inputs, kind.outputs, compact, collapsed, direction]);
  useEffect(() => { update(id); }, [id, signature, update]);
  const errors = data.diagnostics?.filter((issue) => issue.severity === "error") ?? [];
  const notices = data.diagnostics?.length ?? 0;
  const status = data.status ? statusMeta[data.status] : undefined;
  const color = errors.length ? "var(--tone-danger)" : data.tone ? `var(--tone-${data.tone})` : kind.tone ? `var(--tone-${kind.tone})` : compact ? "var(--border)" : "var(--primary)";
  if(compact&&data.circle&&!data.editable)return <div className={cn("relative flex items-center justify-center rounded-full border-2 text-primary-foreground",selected&&"ring-2 ring-ring",data.current&&"ring-2 ring-primary")} style={{width:data.circle.size,height:data.circle.size,background:color,borderColor:color}} aria-label={data.label} title={[data.label,data.detail].filter(Boolean).join(" · ")}><span aria-hidden="true" className="max-w-full overflow-hidden text-ellipsis whitespace-nowrap px-1" style={{fontSize:data.circle.size>24?8:7}}>{data.circle.badge}</span>{kind.inputs.map((port,i)=><Port key={`input:${port.id}`} port={port} index={i} count={kind.inputs.length} input compact collapsed={false} direction={direction}/>)}{kind.outputs.map((port,i)=><Port key={`output:${port.id}`} port={port} index={i} count={kind.outputs.length} input={false} compact collapsed={false} direction={direction}/>)}</div>;
  return <div className={cn("platform-block", selected && "platform-block-selected", compact && "platform-block-compact", data.current && "platform-block-current")}
    style={{ width: compact ? 160 : canvasNodeWidth, height: blockNodeHeight(kind, compact, collapsed), "--block-color": color } as CSSProperties}
    aria-label={data.label}>
    {!compact && <span className="platform-block-accent" />}
    <div className="platform-block-heading">
      {!compact && <span className="platform-block-icon" aria-hidden="true">{kind.icon ?? icons[kind.category] ?? <Blocks />}</span>}
      <div className="min-w-0 flex-1">
        {!compact && <div className="platform-block-kind">{kind.title}{data.version && <span title={data.version}>{data.version}</span>}</div>}
        <div className="platform-block-title" title={data.label}>{data.label}</div>
        {compact && data.detail && <div className="platform-block-detail" title={data.detail}>{data.detail}</div>}
      </div>
      {!compact && (kind.inputs.length > 0 || kind.outputs.length > 0) && <button type="button" className="nodrag nopan platform-block-collapse"
        onClick={(event) => { event.stopPropagation(); onCollapse(id); }} aria-label={t(collapsed ? "Expand block ports" : "Collapse block ports")}
        title={t(collapsed ? "Expand block ports" : "Collapse block ports")}>{collapsed ? <ChevronDown /> : <ChevronUp />}</button>}
    </div>
    {!compact && !collapsed && data.detail && <div className="platform-block-detail px-3" title={data.detail}>{data.detail}</div>}
    {kind.inputs.map((port, i) => <Port key={`input:${port.id}`} port={port} index={i} count={kind.inputs.length} input compact={!!compact} collapsed={collapsed} direction={direction} />)}
    {kind.outputs.map((port, i) => <Port key={`output:${port.id}`} port={port} index={i} count={kind.outputs.length} input={false} compact={!!compact} collapsed={collapsed} direction={direction} />)}
    {!compact && (status || notices > 0) && <div className="platform-block-status">
      {status && <span style={{ color: `var(--tone-${status.tone})` }} role={data.status === "running" ? "status" : undefined}>{status.icon}{t(status.label)}</span>}
      {notices > 0 && <span className="ml-auto" style={{ color: errors.length ? "var(--tone-danger)" : "var(--tone-warning)" }}
        title={data.diagnostics!.map((issue) => issue.message).join("\n")}><AlertTriangle />{notices}</span>}
    </div>}
  </div>;
}
