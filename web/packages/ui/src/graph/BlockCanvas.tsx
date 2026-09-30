import { ConnectionLineType, MarkerType, ReactFlow, ReactFlowProvider, SelectionMode, useEdgesState, useNodesState, useReactFlow, type Connection } from "@xyflow/react";
import { AlignHorizontalJustifyStart, Copy, Maximize, Plus, Redo2, Trash2, Undo2 } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../i18n";
import { BlockInteraction, BlockNode, type FlowBlockNode } from "./BlockNode";
import { BlockEdge, EdgeInteraction, type FlowBlockEdge } from "./BlockEdge";
import { BlockPalette } from "./BlockPalette";
import { CanvasFrame, CanvasFurniture, CanvasRefit, fitting } from "./CanvasFrame";
import { layout } from "./layout";
import { canvasNodeHeight, canvasNodeWidth, canvasPlacement, validateCanvasConnection, type CanvasAddContext, type CanvasEdge, type CanvasHistory, type CanvasNode, type CanvasPosition, type ConnectionIssue, type NodeCatalog, type NodeKind } from "./model";

export type BlockCanvasProps = {
  catalog: NodeCatalog; nodes: CanvasNode[]; edges: CanvasEdge[]; selected?: string;
  label?: string; height?: number | string; mode?: "edit" | "view"; direction?: "right" | "down"; children?: ReactNode;
  onSelect?: (id: string) => void; onOpen?: (id: string) => void;
  onConnect?: (connection: Connection) => void; onDisconnect?: (edges: CanvasEdge[]) => void;
  canConnect?: (connection: Connection) => boolean;
  onAdd?: (kind: string, context: CanvasAddContext) => void;
  /** The owner inserts and reconnects atomically; the canvas never guesses branch semantics. */
  onInsert?: (edge: CanvasEdge, kind: string, context: CanvasAddContext) => void;
  onPositionsChange?: (positions: Record<string, CanvasPosition>) => void;
  onLayout?: (positions: Record<string, CanvasPosition>) => void;
  onDelete?: (nodes: CanvasNode[], edges: CanvasEdge[]) => void;
  onDuplicate?: (nodes: CanvasNode[], edges: CanvasEdge[]) => void;
  history?: CanvasHistory;
};

const nodeTypes = { block: BlockNode }, edgeTypes = { block: BlockEdge };
const missingKind = (id: string): NodeKind => ({ id, title: id, category: "unknown", inputs: [], outputs: [] });
const issueMessages: Record<ConnectionIssue, string> = {
  endpoint: "Choose two different nodes.", port: "These ports have different types.", duplicate: "This connection already exists.",
  "source-capacity": "This output already has its allowed connection.", "target-capacity": "This input already has its allowed connections.",
};
const center = (element: HTMLElement | null) => {
  const box = element?.getBoundingClientRect();
  return box ? { x: box.left + box.width / 2, y: box.top + box.height / 2 } : { x: 0, y: 0 };
};
const editableTarget = (target: EventTarget | null) => target instanceof HTMLElement && !!target.closest("input,textarea,select,[contenteditable=true]");

function CanvasContent({ catalog, nodes, edges, selected, onSelect, onOpen, onConnect, onDisconnect, onAdd, onInsert, onPositionsChange, onLayout, onDelete, onDuplicate, canConnect, history, label, height = 320, mode, direction = "right", children }: BlockCanvasProps) {
  const editable = mode === "edit" || mode !== "view" && !!(onConnect || onDisconnect || onAdd || onDelete || onPositionsChange);
  const container = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition, fitView, getViewport, setCenter } = useReactFlow<FlowBlockNode, FlowBlockEdge>();
  const localPositions = useRef<Record<string, CanvasPosition>>({});
  const externalPositions = useRef<Record<string, CanvasPosition>>({});
  const externalSelection = useRef<string | undefined>(undefined);
  const copied = useRef<{ nodes: CanvasNode[]; edges: CanvasEdge[] } | undefined>(undefined);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [palette, setPalette] = useState<CanvasAddContext>();
  const [connectionIssue, setConnectionIssue] = useState<string>();
  const [refit, setRefit] = useState(0);
  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<FlowBlockNode>([]);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState<FlowBlockEdge>([]);
  const dragStarted = useRef<Record<string, CanvasPosition>>({});

  useEffect(() => {
    const ids = new Set(nodes.map((node) => node.id));
    for (const id of Object.keys(localPositions.current)) if (!ids.has(id)) delete localPositions.current[id];
    for (const node of nodes) {
      const previous = externalPositions.current[node.id];
      if (previous && (previous.x !== node.position.x || previous.y !== node.position.y)) delete localPositions.current[node.id];
      externalPositions.current[node.id] = node.position;
    }
    setFlowNodes((previous) => {
      const old = new Map(previous.map((node) => [node.id, node]));
      const selectionChanged = selected !== externalSelection.current;
      externalSelection.current = selected;
      const multiple = previous.filter((node) => node.selected).length > 1;
      return nodes.map((node) => ({
        id: node.id, type: "block", position: localPositions.current[node.id] ?? node.position,
        ariaRole: onSelect || onOpen ? "button" as const : "group" as const,
        ariaLabel: node.label,
        selected: selected !== undefined && selectionChanged && !multiple ? node.id === selected : old.get(node.id)?.selected,
        measured: old.get(node.id)?.measured, draggable: editable, deletable: false,
        data: { ...node, definition: catalog.find((kind) => kind.id === node.kind) ?? missingKind(node.kind), direction, editable,
          collapsedView: collapsed[node.id] ?? node.collapsed ?? false },
      }));
    });
  }, [catalog, nodes, selected, editable, collapsed, direction, setFlowNodes, onSelect, onOpen]);

  useEffect(() => {
    setFlowEdges((previous) => edges.map((edge) => ({
      id: edge.id, source: edge.source, sourceHandle: edge.sourcePort, target: edge.target, targetHandle: edge.targetPort,
      type: "block", selected: previous.find((old) => old.id === edge.id)?.selected,
      markerEnd: { type: MarkerType.ArrowClosed, color: edge.tone ? `var(--tone-${edge.tone})` : "var(--muted)", width: 16, height: 16 },
      deletable: false, data: { definition: edge, editable: editable && !!onInsert },
    })));
  }, [edges, editable, onInsert, setFlowEdges]);

  useEffect(() => {
    if (!editable || !selected || !container.current) return;
    const node = flowNodes.find((item) => item.id === selected);
    if (!node?.measured?.width || !node.measured.height) return;
    const viewport = getViewport(), width = container.current.clientWidth, height = container.current.clientHeight;
    const x = node.position.x * viewport.zoom + viewport.x, y = node.position.y * viewport.zoom + viewport.y;
    if (x >= 8 && y >= 48 && x + node.measured.width * viewport.zoom <= width - 8 && y + node.measured.height * viewport.zoom <= height - 8) return;
    void setCenter(node.position.x + node.measured.width / 2, node.position.y + node.measured.height / 2, { zoom: viewport.zoom, duration: 180 });
  }, [editable, selected, flowNodes, getViewport, setCenter]);

  const currentNodes = () => nodes.map((node) => ({ ...node, position: flowNodes.find((item) => item.id === node.id)?.position ?? node.position }));
  const selectedNodes = () => currentNodes().filter((node) => flowNodes.find((item) => item.id === node.id)?.selected);
  const selectedEdges = (picked: CanvasNode[]) => edges.filter((edge) => flowEdges.find((item) => item.id === edge.id)?.selected || picked.some((node) => node.id === edge.source || node.id === edge.target));
  const copySelection = () => {
    const picked = selectedNodes(), ids = new Set(picked.map((node) => node.id));
    copied.current = { nodes: picked, edges: edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target)) };
  };
  const duplicateSelection = () => {
    const picked = selectedNodes(), ids = new Set(picked.map((node) => node.id));
    if (picked.length) onDuplicate?.(picked, edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target)));
  };
  const deleteSelection = () => {
    const picked = selectedNodes(), linked = selectedEdges(picked);
    if (picked.length && onDelete) onDelete(picked, linked);
    else if (linked.length && onDisconnect) onDisconnect(linked);
  };
  const addContext = () => ({ position: screenToFlowPosition(center(container.current)) });
  const arrange = () => {
    const compact = nodes.every((node) => node.compact);
    const positions = Object.fromEntries(layout(nodes, edges.map((edge) => ({ from: edge.source, to: edge.target })), direction,
      { width: compact ? 160 : canvasNodeWidth, height: compact ? 58 : Math.max(58, ...catalog.map((kind) => canvasNodeHeight(kind))), gapX: 80, gapY: 36 }));
    localPositions.current = positions;
    setFlowNodes((current) => current.map((node) => ({ ...node, position: positions[node.id]! })));
    if (editable) (onLayout ?? onPositionsChange)?.(positions);
    setRefit((value) => value + 1);
  };
  const choose = (kind: string) => {
    if (!palette) return;
    if (palette.edge && onInsert) {
      const graph = currentNodes(), source = graph.find((node) => node.id === palette.edge!.source), target = graph.find((node) => node.id === palette.edge!.target);
      const definition = catalog.find((item) => item.id === kind);
      let context = palette;
      if (source && target && definition) {
        const vertical = direction === "down";
        const sourceKind = catalog.find((item) => item.id === source.kind) ?? missingKind(source.kind);
        const targetKind = catalog.find((item) => item.id === target.kind) ?? missingKind(target.kind);
        const newHeight = canvasNodeHeight(definition), sourceHeight = canvasNodeHeight(sourceKind), targetHeight = canvasNodeHeight(targetKind);
        const position = vertical
          ? { x: (source.position.x + target.position.x) / 2, y: source.position.y + sourceHeight + 72 }
          : { x: source.position.x + canvasNodeWidth + 80, y: (source.position.y + sourceHeight / 2 + target.position.y + targetHeight / 2) / 2 - newHeight / 2 };
        const offset = Math.max(0, vertical ? position.y + newHeight + 72 - target.position.y : position.x + canvasNodeWidth + 80 - target.position.x);
        const shifted = new Set<string>(), pending = [target.id];
        while (pending.length) {
          const id = pending.pop()!;
          const node = graph.find((item) => item.id === id);
          if (!node || shifted.has(id) || id === source.id || (vertical ? node.position.y < target.position.y : node.position.x < target.position.x)) continue;
          shifted.add(id); pending.push(...edges.filter((edge) => edge.source === id).map((edge) => edge.target));
        }
        const positions = Object.fromEntries(graph.filter((node) => shifted.has(node.id) && offset > 0).map((node) => [node.id,
          vertical ? { x: node.position.x, y: node.position.y + offset } : { x: node.position.x + offset, y: node.position.y }]));
        context = { ...palette, position, positions };
      }
      onInsert(palette.edge, kind, context);
    }
    else {
      const definition = catalog.find((item) => item.id === kind);
      const height = definition ? canvasNodeHeight(definition) : 120;
      const position = canvasPlacement(palette.position, height, flowNodes.map((node) => ({ position: node.position, width: node.measured?.width ?? canvasNodeWidth, height: node.measured?.height ?? 120 })));
      onAdd?.(kind, { ...palette, position });
    }
    setPalette(undefined); container.current?.focus();
  };
  const valid = (connection: Connection) => !validateCanvasConnection(connection, nodes, edges, catalog) && (!canConnect || canConnect(connection));
  const commitPositions = () => {
    const positions = Object.fromEntries(flowNodes.filter((node) => node.position.x !== dragStarted.current[node.id]?.x || node.position.y !== dragStarted.current[node.id]?.y).map((node) => [node.id, node.position]));
    if (Object.keys(positions).length) { Object.assign(localPositions.current, positions); onPositionsChange?.(positions); }
  };
  const removeCount = selectedNodes().length;
  return <CanvasFrame label={label} height={height} role={editable ? "region" : "figure"}>
    <div ref={container} tabIndex={0} className="platform-block-canvas h-full outline-none" aria-label={label}
      onKeyDownCapture={(event) => {
        if (event.key !== "Enter" || editableTarget(event.target)) return;
        const id = event.target instanceof HTMLElement ? event.target.closest(".react-flow__node")?.getAttribute("data-id") : null;
        if (!id || !nodes.some((node) => node.id === id)) return;
        event.preventDefault(); event.stopPropagation(); onSelect?.(id); onOpen?.(id);
      }}
      onKeyDown={(event) => {
        if (editableTarget(event.target)) return;
        const command = event.metaKey || event.ctrlKey, key = event.key.toLowerCase();
        if (command && key === "z" && editable && history) {
          event.preventDefault(); if (event.shiftKey) history.onRedo(); else history.onUndo();
        } else if (command && key === "y" && editable && history) { event.preventDefault(); history.onRedo();
        } else if (command && key === "a") {
          event.preventDefault(); setFlowNodes((current) => current.map((node) => ({ ...node, selected: true })));
        } else if (command && key === "c" && editable) { event.preventDefault(); copySelection();
        } else if (command && key === "v" && editable && onDuplicate && copied.current) { event.preventDefault(); onDuplicate(copied.current.nodes, copied.current.edges);
        } else if (command && key === "d" && editable && onDuplicate) { event.preventDefault(); duplicateSelection();
        } else if ((event.key === "Delete" || event.key === "Backspace") && editable) { event.preventDefault(); deleteSelection();
        } else if ((key === "n" || event.key === "Tab") && editable && onAdd && event.target === container.current) { event.preventDefault(); setPalette(addContext());
        } else if (event.key === "Escape") { setPalette(undefined); setConnectionIssue(undefined); setFlowNodes((current) => current.map((node) => ({ ...node, selected: false })));
        } else if (event.key === "Enter") { const picked = selectedNodes(); if (picked.length === 1) onOpen?.(picked[0]!.id); }
      }}
      onDragOver={(event) => { if (editable && onAdd && event.dataTransfer.types.includes("application/platform-block")) { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; } }}
      onDrop={(event) => {
        const kind = event.dataTransfer.getData("application/platform-block");
        if (!editable || !onAdd || !catalog.some((item) => item.id === kind)) return;
        event.preventDefault(); onAdd(kind, { position: canvasPlacement(screenToFlowPosition({ x: event.clientX, y: event.clientY }), canvasNodeHeight(catalog.find((item) => item.id === kind)!),
          flowNodes.map((node) => ({ position: node.position, width: node.measured?.width ?? canvasNodeWidth, height: node.measured?.height ?? 120 }))) }); setPalette(undefined);
      }}>
      <BlockInteraction.Provider value={{ onCollapse: (id) => setCollapsed((current) => ({ ...current, [id]: !(current[id] ?? nodes.find((node) => node.id === id)?.collapsed ?? false) })) }}>
        <EdgeInteraction.Provider value={{ onInsert: (edge, position) => setPalette({ edge, position }) }}>
          <ReactFlow<FlowBlockNode, FlowBlockEdge> nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} edgeTypes={edgeTypes}
            onNodesChange={onNodesChange} onEdgesChange={onEdgesChange} fitView fitViewOptions={fitting}
            colorMode="system" minZoom={0.15} maxZoom={2} zoomOnScroll={false} zoomOnPinch panOnScroll={editable} preventScrolling={editable}
            panOnDrag={editable ? [1, 2] : true} selectionOnDrag={editable} selectionMode={SelectionMode.Partial} multiSelectionKeyCode={["Meta", "Control", "Shift"]}
            nodesDraggable={editable} nodesConnectable={editable && !!onConnect} edgesFocusable={editable} deleteKeyCode={null}
            connectionLineType={ConnectionLineType.SmoothStep} isValidConnection={(connection) => valid(connection as Connection)}
            onConnect={editable ? onConnect : undefined} onConnectStart={() => setConnectionIssue(undefined)}
            onConnectEnd={(event, state) => {
              if (!editable || state.isValid) return;
              const from = state.fromHandle, to = state.toHandle;
              if (from && !to && onAdd && !state.toNode) {
                const point = "changedTouches" in event ? event.changedTouches[0] : event;
                if (point) setPalette({ position: screenToFlowPosition({ x: point.clientX, y: point.clientY }),
                  ...(from.type === "source" ? { source: { node: from.nodeId, port: from.id ?? "" } } : { target: { node: from.nodeId, port: from.id ?? "" } }) });
                return;
              }
              if (!from || !to || from.type === to.type) { setConnectionIssue(t("Connect an output to a compatible input port.")); return; }
              const source = from.type === "source" ? from : to, target = from.type === "target" ? from : to;
              const issue = validateCanvasConnection({ source: source.nodeId, sourceHandle: source.id ?? null, target: target.nodeId, targetHandle: target.id ?? null }, nodes, edges, catalog);
              setConnectionIssue(t(issue ? issueMessages[issue] : "This connection is not allowed."));
            }}
            onNodeClick={(_, node) => { container.current?.focus(); onSelect?.(node.id); }} onNodeDoubleClick={(_, node) => onOpen?.(node.id)}
            onPaneClick={() => { container.current?.focus(); setPalette(undefined); setConnectionIssue(undefined); }}
            onPaneContextMenu={(event) => { if (editable && onAdd) { event.preventDefault(); setPalette({ position: screenToFlowPosition({ x: event.clientX, y: event.clientY }) }); } }}
            onNodeDragStart={() => { dragStarted.current = Object.fromEntries(flowNodes.map((node) => [node.id, node.position])); }} onNodeDragStop={commitPositions}>
            <CanvasRefit signature={editable ? String(refit) : flowNodes.map((node) => `${node.id}:${node.position.x}:${node.position.y}`).join("|")} />
            <CanvasFurniture />
          </ReactFlow>
        </EdgeInteraction.Provider>
      </BlockInteraction.Provider>
      <div className="nodrag nopan platform-block-toolbar" aria-label={t("Canvas tools")}>
        {editable && onAdd && <button type="button" className="platform-block-tool platform-block-tool-primary" onClick={() => setPalette(addContext())} title={t("Add block (N)")}><Plus /><span>{t("Add block")}</span></button>}
        <button type="button" className="platform-block-tool" onClick={arrange} title={t("Tidy up workflow")} aria-label={t("Tidy up workflow")}><AlignHorizontalJustifyStart /></button>
        <button type="button" className="platform-block-tool" onClick={() => void fitView({ ...fitting, duration: 220 })} title={t("Fit canvas")} aria-label={t("Fit canvas")}><Maximize /></button>
        {editable && history && <><span className="mx-0.5 h-4 w-px bg-border" />
          <button type="button" className="platform-block-tool" disabled={!history.canUndo} onClick={history.onUndo} title={t("Undo")} aria-label={t("Undo")}><Undo2 /></button>
          <button type="button" className="platform-block-tool" disabled={!history.canRedo} onClick={history.onRedo} title={t("Redo")} aria-label={t("Redo")}><Redo2 /></button></>}
        {editable && removeCount > 0 && <><span className="mx-0.5 h-4 w-px bg-border" />
          {onDuplicate && <button type="button" className="platform-block-tool" onClick={duplicateSelection} title={t("Duplicate selection")} aria-label={t("Duplicate selection")}><Copy /></button>}
          {onDelete && <button type="button" className="platform-block-tool" onClick={deleteSelection} title={t("Delete selection")} aria-label={t("Delete selection")}><Trash2 /></button>}</>}
      </div>
      {palette && <BlockPalette catalog={catalog} nodes={nodes} context={palette} onChoose={choose} onClose={() => { setPalette(undefined); container.current?.focus(); }} />}
      {!nodes.length && <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 text-muted"><span className="text-sm font-medium">{t("Start with a block")}</span><span className="text-xs">{t("Add a capability, then connect its typed ports.")}</span></div>}
      {connectionIssue && <div role="alert" className="absolute bottom-3 left-3 z-20 flex max-w-[70%] items-center gap-2 rounded border border-border bg-surface px-3 py-2 text-xs text-foreground shadow-sm"><span>{connectionIssue}</span><button type="button" onClick={() => setConnectionIssue(undefined)} aria-label={t("Dismiss")} className="px-1 text-muted">×</button></div>}
      {children}
    </div>
  </CanvasFrame>;
}

/** One React Flow implementation; Graph and NodeCanvas only adapt their owners' semantics. */
export function BlockCanvas(props: BlockCanvasProps) {
  return <ReactFlowProvider><CanvasContent {...props} /></ReactFlowProvider>;
}
