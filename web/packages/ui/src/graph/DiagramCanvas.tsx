// One canvas for entity/relationship diagrams — enterprise models, object
// graphs, lineage. The owner supplies nodes with icons and edges with meaning;
// the canvas owns zoom, pan, fit, minimap, layouts, dragging, linking and
// dropping, so no view draws its own boxes again (ADR-0068 §6).
import { BaseEdge, ConnectionMode, EdgeLabelRenderer, Handle, MarkerType, MiniMap, Position, ReactFlow, ReactFlowProvider, getSmoothStepPath, useEdgesState, useNodesState, useReactFlow, type Edge, type EdgeProps, type Node, type NodeProps } from "@xyflow/react";
import { ArrowDownFromLine, ArrowRightFromLine, Box, ChevronDown, ChevronUp, CircleHelp, Grid3x3, Maximize, Orbit } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { t } from "../i18n";
import { CanvasFrame, CanvasFurniture, fitting } from "./CanvasFrame";
import { diagramLayout, diagramLayouts, diagramSize, type DiagramLayout } from "./diagramLayouts";
import type { CanvasPosition } from "./model";

export type DiagramNode = { id: string; label: string; caption?: string; icon?: ReactNode; tone?: string; dim?: boolean; flag?: string; detail?: string; facts?: { label: string; value: string }[] };
/** One thing a person may do to the selected node or edge; the owner supplies
 * meaning and the run, the canvas supplies the place and the wording (ADR-0084 D3). */
export type DiagramAction = { id: string; label: string; hint?: string; tone?: "default" | "danger"; disabled?: boolean; run: () => void };
export type DiagramEdge = { id: string; source: string; target: string; label?: string; tree?: boolean; dashed?: boolean; directed?: boolean; tone?: string };
export type DiagramCanvasProps = {
  nodes: DiagramNode[]; edges: DiagramEdge[]; positions: Readonly<Record<string, CanvasPosition>>;
  selected?: string; editable?: boolean; linking?: boolean; label?: string; height?: number | string; layout?: DiagramLayout;
  /** The dataTransfer type the owner's palette sets; its payload arrives in onDrop with the drop point. */
  dropType?: string; children?: ReactNode;
  onSelect?: (id?: string) => void; onOpen?: (id: string) => void;
  /** The selected node's operations, shown on the canvas beside it. */
  nodeActions?: (id: string) => DiagramAction[];
  /** Edge selection and operations: an edge is a relationship, and it can be chosen. */
  edgeActions?: (id: string) => DiagramAction[];
  onReconnect?: (id: string, source: string, target: string) => void;
  onPositionsChange?: (positions: Record<string, CanvasPosition>) => void;
  onLink?: (source: string, target: string) => void;
  onDrop?: (payload: string, at: CanvasPosition) => void;
};

type Data = DiagramNode & { linking: boolean; dropped?: boolean };
type FlowNode = Node<Data>;
type FlowEdge = Edge<DiagramEdge & Record<string, unknown>>;
const layoutMeta: Record<DiagramLayout, { icon: ReactNode; title: string }> = {
  "tree-down": { icon: <ArrowDownFromLine />, title: "Tree, top down" }, "tree-right": { icon: <ArrowRightFromLine />, title: "Tree, left to right" },
  radial: { icon: <Orbit />, title: "Radial" }, grid: { icon: <Grid3x3 />, title: "Grid" },
};

function DiagramNodeView({ data, selected }: NodeProps<FlowNode>) {
  const color = data.tone ? `var(--tone-${data.tone})` : "var(--primary)";
  return <div className={cn("platform-diagram-node", selected && "platform-diagram-node-selected", data.dim && "opacity-60", data.linking && "platform-diagram-node-linking")}
    style={{ width: diagramSize.width, minHeight: diagramSize.height, "--block-color": color } as CSSProperties} title={data.detail ?? data.label} aria-label={data.label}>
    <span className="platform-block-icon" aria-hidden="true">{data.icon ?? <Box />}</span>
    <span className="min-w-0 flex-1">
      {data.caption && <span className="platform-block-kind"><span className="!max-w-full">{data.caption}</span></span>}
      <span className="platform-block-title block">{data.label}</span>
    </span>
    {data.flag && <span className="platform-diagram-flag" title={data.flag} />}
    <Handle type="target" position={Position.Top} className="platform-diagram-handle" isConnectable={data.linking} />
    <Handle type="source" position={Position.Bottom} className={cn("platform-diagram-handle", data.linking && "platform-diagram-handle-active")} isConnectable={data.linking} />
  </div>;
}

function DiagramEdgeView({ id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, data, markerEnd, selected }: EdgeProps<FlowEdge>) {
  const [path, x, y] = getSmoothStepPath({ sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition, borderRadius: 14 });
  const color = data?.tone ? `var(--tone-${data.tone})` : selected ? "var(--primary)" : "var(--muted)";
  return <g>
    <BaseEdge id={id} path={path} markerEnd={markerEnd} interactionWidth={20} style={{ stroke: color, strokeWidth: selected ? 2 : 1.4, strokeDasharray: data?.dashed ? "5 4" : undefined }} />
    {data?.label && <EdgeLabelRenderer><span className="platform-block-edge-label" style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y}px)` }}>{data.label}</span></EdgeLabelRenderer>}
  </g>;
}
const nodeTypes = { entity: DiagramNodeView }, edgeTypes = { relation: DiagramEdgeView };

function Content({ nodes, edges, positions, selected, editable = false, linking = false, label, height = 480, layout: initial = "tree-down", dropType, children, onSelect, onOpen, onPositionsChange, onLink, onDrop, nodeActions, edgeActions, onReconnect }: DiagramCanvasProps) {
  const { screenToFlowPosition, fitView } = useReactFlow<FlowNode, FlowEdge>();
  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<FlowNode>([]);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState<FlowEdge>([]);
  const [layout, setLayout] = useState<DiagramLayout>(initial);
  const [menu, setMenu] = useState(false);
  const [help, setHelp] = useState(false);
  const [pickedEdge, setPickedEdge] = useState<string>();
  const [expanded, setExpanded] = useState<string>();
  const fitted = useRef(0);

  useEffect(() => {
    setFlowNodes((previous) => nodes.map((n) => {
      const old = previous.find((p) => p.id === n.id);
      return { id: n.id, type: "entity", position: positions[n.id] ?? old?.position ?? { x: 0, y: 0 }, selected: n.id === selected, draggable: editable && !linking, measured: old?.measured,
        connectable: linking, data: { ...n, linking } };
    }));
  }, [nodes, positions, selected, editable, linking, setFlowNodes]);
  useEffect(() => {
    setFlowEdges((previous) => edges.map((e) => ({ id: e.id, source: e.source, target: e.target, type: "relation", data: { ...e }, deletable: false,
      selected: previous.find((old) => old.id === e.id)?.selected ?? e.id === pickedEdge,
      reconnectable: editable && !!onReconnect,
      markerEnd: e.directed === false ? undefined : { type: MarkerType.ArrowClosed, color: e.tone ? `var(--tone-${e.tone})` : "var(--muted)", width: 14, height: 14 } })));
  }, [edges, setFlowEdges, editable, onReconnect, pickedEdge]);
  // Fit once per set of shown nodes, never while the reader drags.
  const shown = nodes.map((n) => n.id).join("|");
  useEffect(() => { const h = setTimeout(() => void fitView(fitting), 60); fitted.current++; return () => clearTimeout(h); }, [shown, fitView]);

  const arrange = (kind: DiagramLayout) => {
    setLayout(kind); setMenu(false);
    const next = diagramLayout(kind, nodes, edges.map((e) => ({ from: e.source, to: e.target, tree: e.tree })));
    setFlowNodes((current) => current.map((n) => ({ ...n, position: next[n.id] ?? n.position })));
    onPositionsChange?.(next);
    setTimeout(() => void fitView({ ...fitting, duration: 220 }), 30);
  };
  return <CanvasFrame label={label} height={height} role={editable ? "region" : "figure"}>
    <div className="platform-block-canvas platform-diagram h-full outline-none" aria-label={label}
      onDragOver={(e) => { if (editable && dropType && e.dataTransfer.types.includes(dropType)) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } }}
      onDrop={(e) => { if (!editable || !dropType || !onDrop) return; const payload = e.dataTransfer.getData(dropType); if (!payload) return; e.preventDefault();
        const at = screenToFlowPosition({ x: e.clientX, y: e.clientY }); onDrop(payload, { x: at.x - diagramSize.width / 2, y: at.y - diagramSize.height / 2 }); }}>
      <ReactFlow<FlowNode, FlowEdge> nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} edgeTypes={edgeTypes} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
        colorMode="system" minZoom={0.1} maxZoom={2.5} fitView fitViewOptions={fitting} zoomOnScroll panOnScroll={false} zoomOnPinch panOnDrag preventScrolling
        nodesDraggable={editable && !linking} nodesConnectable={linking} connectionMode={ConnectionMode.Loose} deleteKeyCode={null} connectionRadius={40}
        onConnect={(c) => { if (c.source && c.target && c.source !== c.target) onLink?.(c.source, c.target); }}
        onNodeClick={(_, n) => { setPickedEdge(undefined); onSelect?.(n.id); }} onNodeDoubleClick={(_, n) => onOpen?.(n.id)} onPaneClick={() => { onSelect?.(undefined); setPickedEdge(undefined); setExpanded(undefined); }}
        onEdgeClick={(_, e) => { setPickedEdge(e.id); setExpanded(undefined); onSelect?.(undefined); }}
        edgesFocusable={editable && !!edgeActions}
        onReconnect={onReconnect ? (old, c) => { if (c.source && c.target && (c.source !== old.source || c.target !== old.target)) onReconnect(old.id, c.source, c.target); } : undefined}
        onNodeDragStop={(_, __, dragged) => onPositionsChange?.(Object.fromEntries(dragged.map((n) => [n.id, { x: Math.round(n.position.x), y: Math.round(n.position.y) }])))}>
        <CanvasFurniture />
        {nodes.length > 12 && <MiniMap position="bottom-left" pannable zoomable nodeStrokeWidth={2} nodeColor="var(--border)" maskColor="color-mix(in oklch, var(--background), transparent 40%)" style={{ width: 140, height: 90 }} />}
      </ReactFlow>
      <div className="nodrag nopan platform-block-toolbar" aria-label={t("Canvas tools")}>
        <div className="relative">
          <button type="button" className="platform-block-tool" onClick={() => setMenu(!menu)} aria-haspopup="menu" aria-expanded={menu} title={t("Arrange")}>{layoutMeta[layout].icon}<span>{t("Arrange")}</span></button>
          {menu && <div role="menu" className="absolute left-0 top-full z-20 mt-1 grid min-w-44 gap-0.5 rounded-md border border-border bg-surface p-1 shadow-lg">
            {diagramLayouts.map((k) => <button key={k} type="button" role="menuitemradio" aria-checked={layout === k} className={cn("platform-block-tool !justify-start", layout === k && "bg-row-selected")} onClick={() => arrange(k)}>{layoutMeta[k].icon}<span>{t(layoutMeta[k].title)}</span></button>)}
          </div>}
        </div>
        <button type="button" className="platform-block-tool" onClick={() => void fitView({ ...fitting, duration: 220 })} title={t("Fit canvas")} aria-label={t("Fit canvas")}><Maximize /></button>
        {linking && <span className="px-2 text-[11px] text-[var(--tone-warning)]">{t("Drag from one element to another to relate them.")}</span>}
        <button type="button" className="platform-block-tool" onClick={() => setHelp(!help)} aria-expanded={help} title={t("How to work on this canvas")} aria-label={t("How to work on this canvas")}><CircleHelp /></button>
      </div>
      {help && <div role="dialog" aria-label={t("How to work on this canvas")} className="absolute right-3 top-14 z-20 w-72 rounded-md border border-border bg-surface p-3 text-xs shadow-lg">
        <p className="mb-1 font-semibold">{t("On the canvas")}</p>
        <ul className="grid gap-1 text-muted">
          <li>{t("Click an element to select it; double-click to open it.")}</li>
          {editable && <li>{t("Drag an element to move it; the position is saved with the view.")}</li>}
          {editable && linking && <li>{t("Drag from one element to another to relate them.")}</li>}
          {editable && !!nodeActions && <li>{t("The selected element's operations appear in the bar at the bottom of the canvas.")}</li>}
          {editable && !!edgeActions && <li>{t("Click a connection to select it; its operations appear in the same bar. Some only change this view, others change the model — the button's hint says which.")}</li>}
          {editable && !!onReconnect && <li>{t("Drag a connection's end onto another element to relate those instead.")}</li>}
        </ul>
        <button type="button" className="mt-2 rounded px-2 py-1 text-muted hover:bg-row-hover" onClick={() => setHelp(false)}>{t("Close")}</button>
      </div>}
      {pickedEdge && edgeActions && <div className="nodrag nopan absolute bottom-3 left-1/2 z-20 flex -translate-x-1/2 items-center gap-1 rounded-md border border-border bg-surface p-1 shadow-lg" role="toolbar" aria-label={t("Connection operations")}>
        {edgeActions(pickedEdge).map((a) => <button key={a.id} type="button" disabled={a.disabled} title={a.hint} onClick={a.run}
          className={cn("platform-block-tool", a.tone === "danger" && "text-[var(--tone-danger)]")}>{a.label}</button>)}
      </div>}
      {selected && (nodeActions || nodes.find((n) => n.id === selected)?.facts?.length) && <div className="nodrag nopan absolute bottom-3 left-1/2 z-20 flex -translate-x-1/2 items-center gap-1 rounded-md border border-border bg-surface p-1 shadow-lg" role="toolbar" aria-label={t("Element operations")}>
        {!!nodes.find((n) => n.id === selected)?.facts?.length && <button type="button" className={cn("platform-block-tool", expanded === selected && "bg-row-selected")} aria-expanded={expanded === selected}
          onClick={() => setExpanded(expanded === selected ? undefined : selected)} title={t("Details")} aria-label={t("Details")}>{expanded === selected ? <ChevronDown /> : <ChevronUp />}</button>}
        {(nodeActions?.(selected) ?? []).map((a) => <button key={a.id} type="button" disabled={a.disabled} title={a.hint} onClick={a.run}
          className={cn("platform-block-tool", a.tone === "danger" && "text-[var(--tone-danger)]")}>{a.label}</button>)}
      </div>}
      {expanded && <div role="dialog" aria-label={t("Details")} className="absolute bottom-14 left-1/2 z-20 w-72 -translate-x-1/2 rounded-md border border-border bg-surface p-3 text-xs shadow-lg">
        <p className="font-semibold">{expanded}</p>
        <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">{(nodes.find((n) => n.id === expanded)?.facts ?? []).map((f) => <span key={f.label} className="contents"><dt className="text-muted">{f.label}</dt><dd className="min-w-0 break-words">{f.value}</dd></span>)}</dl>
        <button type="button" className="mt-2 rounded px-2 py-1 text-muted hover:bg-row-hover" onClick={() => setExpanded(undefined)}>{t("Close")}</button>
      </div>}
      {!nodes.length && <div className="pointer-events-none absolute inset-0 flex items-center justify-center text-sm text-muted">{children ?? t("Nothing shown yet.")}</div>}
    </div>
  </CanvasFrame>;
}

export function DiagramCanvas(props: DiagramCanvasProps) {
  return <ReactFlowProvider><Content {...props} /></ReactFlowProvider>;
}
