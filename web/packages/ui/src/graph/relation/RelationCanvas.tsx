// The relation family's canvas: things and what holds them together — enterprise
// models, object graphs, lineage, execution chains, a record's neighbourhood. The
// owner supplies nodes with icons and edges with meaning; the canvas owns zoom, pan,
// fit, minimap, layouts, dragging, linking and dropping, so no view draws its own
// boxes again (ADR-0068 §6, ADR-0084 D3, ADR-0086 D3).
import { ConnectionMode, MarkerType, MiniMap, Position, ReactFlow, ReactFlowProvider, useNodesState, useReactFlow } from "@xyflow/react";
import { ArrowDownFromLine, ArrowRightFromLine, ChevronDown, ChevronUp, GitBranch, Grid3x3, Maximize, Orbit, Waves } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { cn } from "../../lib/cn";
import { CanvasActionBar, CanvasActionButton, CanvasEmpty, CanvasHelp, CanvasHelpTool, CanvasTool, CanvasToolbar } from "../core/chrome";
import { CanvasFurniture, CanvasFrame, CanvasRefit, fitting } from "../core/frame";
import { readCanvasPositions, writeCanvasPositions } from "../core/store";
import type { CanvasAction, CanvasPosition } from "../core/types";
import { arrangeRelations } from "./layouts";
import { relationLayouts, relationNodeSize, type RelationEdge, type RelationLayout, type RelationNode } from "./model";
import { useCanvasLayout } from "../core/layout/use-layout";
import { samePositions } from "../core/layout/edge-routing";
import { relationEdgeTypes, relationNodeTypes, type RelationLineEdge, type RelationShapeNode } from "./views";

export type RelationCanvasProps = {
  nodes: RelationNode[]; edges: RelationEdge[];
  /** Where each element sits. A reader's view may leave this out and let the canvas
   * arrange what it is given; an owner that persists a drawing supplies it. */
  positions?: Readonly<Record<string, CanvasPosition>>;
  selected?: string; editable?: boolean; linking?: boolean; label?: string; height?: number | string; layout?: RelationLayout;
  /** The dataTransfer type the owner's palette sets; its payload arrives in onDrop with the drop point. */
  dropType?: string; children?: ReactNode; viewportKey?: string;
  onSelect?: (id?: string) => void; onOpen?: (id: string) => void;
  /** The selected element's operations, shown on the canvas beside it. */
  nodeActions?: (id: string) => CanvasAction[];
  /** A relationship can be chosen, and chosen relationships have operations. */
  edgeActions?: (id: string) => CanvasAction[];
  onReconnect?: (id: string, source: string, target: string) => void;
  onLayoutReady?: (positions: Record<string, CanvasPosition>) => void;
  onPositionsChange?: (positions: Record<string, CanvasPosition>) => void;
  onLink?: (source: string, target: string) => void;
  onDrop?: (payload: string, at: CanvasPosition) => void;
  /** The reader's own arrangement is kept on this device for this drawing
   * (ADR-0092): even a read-only view may be dragged, and the arrangement
   * survives a reload until the reader chooses a layout again. */
  storeKey?: string;
};

const emptyPositions: Record<string, CanvasPosition> = {};

const layoutMeta: Record<RelationLayout, { icon: ReactNode; title: string }> = {
  "tree-down": { icon: <ArrowDownFromLine />, title: "Top to bottom" }, "tree-right": { icon: <ArrowRightFromLine />, title: "Tree, left to right" },
  layered: { icon: <GitBranch />, title: "Left to right" }, radial: { icon: <Orbit />, title: "Radial" }, grid: { icon: <Grid3x3 />, title: "Compact arrangement" },
  force: { icon: <Waves />, title: "Balance by distance" },
  organization: { icon: <GitBranch />, title: "Organization tree" },
  "layered-up": { icon: <ArrowDownFromLine />, title: "Bottom to top" }, "layered-left": { icon: <ArrowRightFromLine />, title: "Right to left" },
};

function RelationCanvasContent({ nodes, edges, positions, selected, editable = false, linking = false, label, height = 480, layout: initial = "tree-down", dropType, children, onSelect, onOpen, onPositionsChange, onLayoutReady, onLink, onDrop, nodeActions, edgeActions, onReconnect, viewportKey, storeKey }: RelationCanvasProps) {
  const { screenToFlowPosition, fitView } = useReactFlow<RelationShapeNode, RelationLineEdge>();
  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<RelationShapeNode>([]);
  const [layout, setLayout] = useState<RelationLayout>(initial);
  const [menu, setMenu] = useState(false);
  const [help, setHelp] = useState(false);
  const [pickedEdge, setPickedEdge] = useState<string>();
  const [expanded, setExpanded] = useState<string>();
  const canReconnect = editable && !!onReconnect;
  const relations = useMemo(() => edges.map((e) => ({ id: e.id, from: e.source, to: e.target, tree: e.tree, label: e.label })), [edges]);

  // A view that keeps no positions of its own is arranged here; the reader's own
  // arranging stays local until the owner asks for it through onPositionsChange.
  const graphSignature = JSON.stringify([nodes.map((n) => [n.id, n.size, n.badge?.size]), relations]);
  const layoutJob = useCanvasLayout(() => arrangeRelations(layout, nodes, relations), JSON.stringify([layout, nodes.map((n) => [n.id, n.size, n.badge?.size]), relations]));
  const arranged = layoutJob.data?.positions ?? emptyPositions;
  const layoutVersion = useRef(0);
  const [layoutError, setLayoutError] = useState<string>();
  const [explicitLayout, setExplicitLayout] = useState<Awaited<ReturnType<typeof arrangeRelations>>>();
  useEffect(() => { layoutVersion.current++; setExplicitLayout(undefined); }, [graphSignature]);
  useEffect(() => () => { layoutVersion.current++; }, []);
  // The reader's stored arrangement joins whatever the owner or the layout gives:
  // it wins over both until the reader chooses a layout again (ADR-0092).
  const [stored, setStored] = useState<Record<string, CanvasPosition>>(() => (storeKey ? readCanvasPositions(storeKey) : undefined) ?? {});
  useEffect(() => { setStored((storeKey ? readCanvasPositions(storeKey) : undefined) ?? {}); }, [storeKey]);
  const owned = useMemo(() => (storeKey ? { ...({ ...arranged, ...positions }), ...stored } : { ...arranged, ...positions }), [positions, arranged, stored, storeKey]);
  const persist = (next: Record<string, CanvasPosition>) => {
    if (!storeKey) return;
    setStored(next);
    writeCanvasPositions(storeKey, next);
  };

  useEffect(() => {
    setFlowNodes((previous) => nodes.map((n) => {
      const old = previous.find((p) => p.id === n.id);
      return { id: n.id, type: "entity", position: owned[n.id] ?? old?.position ?? { x: 0, y: 0 }, selected: n.id === selected, draggable: (editable || !!storeKey) && !linking, measured: old?.measured,
        connectable: editable && (linking || canReconnect) && n.linkable !== false, data: { ...n, linking, acceptsConnections: editable && (linking || canReconnect) } };
    }));
  }, [nodes, owned, selected, editable, linking, setFlowNodes, canReconnect, storeKey]);

  const flowEdges = useMemo<RelationLineEdge[]>(() => {
    const byId = new Map(flowNodes.map((n) => [n.id, n]));
    const result = explicitLayout ?? layoutJob.data;
    const routes = result && samePositions(result.positions, flowNodes) ? result.routes : {};
    return edges.map((e) => {
      const a = byId.get(e.source), b = byId.get(e.target);
      const dx = (b?.position.x ?? 0) - (a?.position.x ?? 0), dy = (b?.position.y ?? 0) - (a?.position.y ?? 0);
      // Attach to facing sides without changing the owner's source/target meaning.
      const horizontal = Math.abs(dx) > Math.abs(dy);
      const sourceHandle = horizontal ? dx >= 0 ? Position.Right : Position.Left : dy >= 0 ? Position.Bottom : Position.Top;
      const targetHandle = horizontal ? dx >= 0 ? Position.Left : Position.Right : dy >= 0 ? Position.Top : Position.Bottom;
      const sides = { NORTH: Position.Top, SOUTH: Position.Bottom, EAST: Position.Right, WEST: Position.Left };
      const route = routes[e.id];
      return { id: e.id, source: e.source, target: e.target, sourceHandle: route?.sourceSide ? sides[route.sourceSide] : sourceHandle, targetHandle: route?.targetSide ? sides[route.targetSide] : targetHandle, type: "relation", data: { ...e, route: routes[e.id] }, deletable: false,
        selected: e.id === pickedEdge, reconnectable: editable && !!onReconnect && e.reconnectable !== false,
        markerEnd: e.directed === false ? undefined : { type: MarkerType.ArrowClosed, color: e.tone ? `var(--tone-${e.tone})` : "var(--muted)", width: 14, height: 14 } };
    });
  }, [edges, flowNodes, editable, onReconnect, pickedEdge, explicitLayout, layoutJob.data]);

  // A new drawing is fitted to the window; working on the one already open is not.
  const drawing = useMemo(() => `${nodes.map((n) => n.id).join("|")}#${edges.map((e) => e.id).join("|")}`, [nodes, edges]);
  const fitKey = viewportKey ?? drawing;
  const synchronized = !layoutJob.pending && flowNodes.length > 0 && flowNodes.length === nodes.length && flowNodes.every((node) => {
    const expected = owned[node.id];
    return nodes.some((n) => n.id === node.id) && !!node.measured?.width && !!node.measured?.height
      && (!expected || node.position.x === expected.x && node.position.y === expected.y);
  });

  useEffect(() => {
    if (synchronized) onLayoutReady?.(Object.fromEntries(flowNodes.map((node) => [node.id, node.position])));
  }, [synchronized, flowNodes, onLayoutReady]);

  const arrange = async (kind: RelationLayout) => {
    setLayout(kind); setMenu(false);
    const version = ++layoutVersion.current;
    setLayoutError(undefined);
    try {
    const result = await arrangeRelations(kind, nodes.map((n) => ({ ...n, size: flowNodes.find((f) => f.id === n.id)?.measured?.width ? flowNodes.find((f) => f.id === n.id)!.measured as { width: number; height: number } : n.size })), relations);
    if (version !== layoutVersion.current) return;
    setExplicitLayout(result);
    const next = result.positions;
    persist(next); // choosing a layout is the reader resetting their own arrangement
    setFlowNodes((current) => current.map((n) => ({ ...n, position: next[n.id] ?? n.position })));
    onPositionsChange?.(next);
    setTimeout(() => void fitView({ ...fitting, duration: 220 }), 30);
    } catch (error) { if (version === layoutVersion.current) setLayoutError(String(error)); }
  };
  const chosen = nodes.find((n) => n.id === selected);
  return <CanvasFrame label={label} height={height} role={editable || storeKey ? "region" : "figure"}>
    <div className="platform-canvas platform-relation h-full outline-none" aria-label={label}
      onDragOver={(e) => { if (editable && dropType && e.dataTransfer.types.includes(dropType)) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } }}
      onDrop={(e) => { if (!editable || !dropType || !onDrop) return; const payload = e.dataTransfer.getData(dropType); if (!payload) return; e.preventDefault();
        const at = screenToFlowPosition({ x: e.clientX, y: e.clientY }); onDrop(payload, { x: at.x - relationNodeSize.width / 2, y: at.y - relationNodeSize.height / 2 }); }}>
      <ReactFlow<RelationShapeNode, RelationLineEdge> nodes={flowNodes} edges={flowEdges} nodeTypes={relationNodeTypes} edgeTypes={relationEdgeTypes} onNodesChange={onNodesChange}
        colorMode="system" minZoom={0.1} maxZoom={2.5} fitView fitViewOptions={fitting} zoomOnScroll panOnScroll={false} zoomOnPinch panOnDrag preventScrolling
        nodesDraggable={(editable || !!storeKey) && !linking} nodesConnectable={linking} connectionMode={ConnectionMode.Loose} deleteKeyCode={null} connectionRadius={40}
        isValidConnection={(c) => c.source !== c.target && nodes.some((n) => n.id === c.source && n.linkable !== false) && nodes.some((n) => n.id === c.target && n.linkable !== false)}
        onConnect={(c) => { if (c.source && c.target && c.source !== c.target) onLink?.(c.source, c.target); }}
        onNodeClick={(_, n) => { setPickedEdge(undefined); onSelect?.(n.id); }} onNodeDoubleClick={(_, n) => onOpen?.(n.id)} onPaneClick={() => { onSelect?.(undefined); setPickedEdge(undefined); setExpanded(undefined); }}
        onEdgeClick={(_, e) => { setPickedEdge(e.id); setExpanded(undefined); onSelect?.(undefined); }}
        edgesFocusable={editable && !!edgeActions}
        onReconnect={onReconnect ? (old, c) => { if (c.source && c.target && c.source !== c.target && (c.source !== old.source || c.target !== old.target)) onReconnect(old.id, c.source, c.target); } : undefined}
        onNodeDragStop={(_, __, dragged) => {
          const changed = Object.fromEntries(dragged.map((n) => [n.id, { x: Math.round(n.position.x), y: Math.round(n.position.y) }]));
          if (storeKey) persist({ ...owned, ...changed });
          onPositionsChange?.(changed);
        }}>
        <CanvasRefit signature={fitKey} ready={synchronized} once />
        <CanvasFurniture />
        {nodes.length > 12 && <MiniMap position="bottom-left" pannable zoomable nodeStrokeWidth={2} nodeColor="var(--border)" maskColor="color-mix(in oklch, var(--background), transparent 40%)" style={{ width: 140, height: 90 }} />}
      </ReactFlow>

      <CanvasToolbar label={t("Canvas tools")}>
        <div className="relative">
          <CanvasTool label={t("Arrange")} icon={layoutMeta[layout].icon} text={t("Arrange")} onClick={() => setMenu(!menu)} expanded={menu} hasPopup="menu" />
          {menu && <div role="menu" className="absolute left-0 top-full z-20 mt-1 grid min-w-44 gap-0.5 rounded-md border border-border bg-surface p-1 shadow-lg">
            {relationLayouts.map((k) => <button key={k} type="button" role="menuitemradio" aria-checked={layout === k} className={cn("platform-canvas-tool !justify-start", layout === k && "bg-row-selected")} onClick={() => void arrange(k)}>{layoutMeta[k].icon}<span>{t(layoutMeta[k].title)}</span></button>)}
          </div>}
        </div>
        <CanvasTool label={t("Fit canvas")} icon={<Maximize />} onClick={() => void fitView({ ...fitting, duration: 220 })} />
        {linking && <span className="px-2 text-[11px] text-[var(--tone-warning)]">{t("Drag from one element to another to relate them.")}</span>}
        <CanvasHelpTool open={help} onToggle={() => setHelp(!help)} />
      </CanvasToolbar>

      <CanvasHelp open={help} onClose={() => setHelp(false)} items={[
        t("Click an element to select it; double-click to open it."),
        editable && t("Drag an element to move it; the position is saved with the view."),
        editable && linking && t("Drag from one element to another to relate them."),
        editable && !!nodeActions && t("The selected element's operations appear in the bar at the bottom of the canvas."),
        editable && !!edgeActions && t("Click a connection to select it; its operations appear in the same bar. Some only change this view, others change the model — the button's hint says which."),
        editable && !!onReconnect && t("Drag a connection's end onto another element to relate those instead."),
      ]} />

      {pickedEdge && edgeActions && <CanvasActionBar label={t("Connection operations")}>
        {edgeActions(pickedEdge).map((a) => <CanvasActionButton key={a.id} action={a} />)}
      </CanvasActionBar>}

      {selected && (nodeActions || chosen?.facts?.length) && <CanvasActionBar label={t("Element operations")}>
        {!!chosen?.facts?.length && <CanvasTool label={t("Details")} icon={expanded === selected ? <ChevronDown /> : <ChevronUp />}
          pressed={expanded === selected} onClick={() => setExpanded(expanded === selected ? undefined : selected)} />}
        {(nodeActions?.(selected) ?? []).map((a) => <CanvasActionButton key={a.id} action={a} />)}
      </CanvasActionBar>}

      {expanded && <div role="dialog" aria-label={t("Details")} className="absolute bottom-14 left-1/2 z-20 w-72 -translate-x-1/2 rounded-md border border-border bg-surface p-3 text-xs shadow-lg">
        <p className="font-semibold">{expanded}</p>
        <dl className="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">{(chosen?.facts ?? []).map((f) => <span key={f.label} className="contents"><dt className="text-muted">{f.label}</dt><dd className="min-w-0 break-words">{f.value}</dd></span>)}</dl>
        <button type="button" className="mt-2 rounded px-2 py-1 text-muted hover:bg-row-hover" onClick={() => setExpanded(undefined)}>{t("Close")}</button>
      </div>}

      {(layoutError || layoutJob.error) && <div role="alert" className="absolute bottom-2 left-2 rounded bg-surface p-2 text-xs text-danger">{t("Layout failed")}: {layoutError || layoutJob.error}</div>}
      {!nodes.length && <CanvasEmpty>{children}</CanvasEmpty>}
    </div>
  </CanvasFrame>;
}

export function RelationCanvas(props: RelationCanvasProps) {
  return <ReactFlowProvider><RelationCanvasContent {...props} /></ReactFlowProvider>;
}
