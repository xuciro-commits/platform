// The flow family's canvas: one process, drawn as activities joined through typed
// ports. The owner says what a step means, which connections are allowed and what
// a change writes; this canvas says how it looks and how a person drives it —
// select, connect, insert on a line, arrange, collapse, copy, undo (ADR-0086 D1).
import { ConnectionLineType, MarkerType, ReactFlow, ReactFlowProvider, SelectionMode, useEdgesState, useNodesState, useReactFlow, type Connection } from "@xyflow/react";
import { AlignHorizontalJustifyStart, ArrowDown, ArrowRight, ChevronsDownUp, ChevronsUpDown, Copy, Maximize, Plus, Redo2, Scissors, Trash2, Undo2 } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";
import { CanvasActionBar, CanvasActionButton, CanvasEmpty, CanvasHelp, CanvasHelpTool, CanvasTool, CanvasToolbar, CanvasToolDivider } from "../core/chrome";
import { CanvasFurniture, CanvasFrame, CanvasRefit, fitting } from "../core/frame";
import { laneBands, layeredLayout, type LaneAssignment } from "../core/layered";
import type { CanvasBox, CanvasDirection, CanvasPosition } from "../core/types";
import { FlowEdgeView, FlowEdgeInteraction, type FlowLineEdge } from "./FlowEdgeView";
import { FlowInteraction, FlowNodeView, type FlowShapeNode } from "./FlowNodeView";
import { FlowLaneView, type FlowLaneShapeNode } from "./FlowLaneView";
import { FlowPalette } from "./FlowPalette";
import { FLOW_NODE_DROP, flowBlockHeight, flowNodeBox, flowNodeHeight, flowNodeWidth, flowPlacement, validateFlowConnection,
  type FlowAddContext, type FlowCatalog, type FlowConnectionIssue, type FlowEdge, type FlowHistory, type FlowLane, type FlowNode, type FlowNodeKind } from "./model";

export type FlowCanvasProps = {
  catalog: FlowCatalog; nodes: FlowNode[]; edges: FlowEdge[]; selected?: string;
  label?: string; height?: number | string; mode?: "edit" | "view"; direction?: CanvasDirection; children?: ReactNode;
  onSelect?: (id: string) => void; onOpen?: (id: string) => void;
  onConnect?: (connection: Connection) => void; onDisconnect?: (edges: FlowEdge[]) => void;
  canConnect?: (connection: Connection) => boolean;
  onAdd?: (kind: string, context: FlowAddContext) => void;
  /** The owner inserts and reconnects atomically; the canvas never guesses branch semantics. */
  onInsert?: (edge: FlowEdge, kind: string, context: FlowAddContext) => void;
  onPositionsChange?: (positions: Record<string, CanvasPosition>) => void;
  onLayout?: (positions: Record<string, CanvasPosition>) => void;
  onDelete?: (nodes: FlowNode[], edges: FlowEdge[]) => void;
  onDuplicate?: (nodes: FlowNode[], edges: FlowEdge[]) => void;
  /** BPMN lanes: the responsibilities this process is drawn across (ADR-0087 D1).
   * The owner declares them and says which step sits in which; the canvas draws the
   * bands, arranges inside them, and reports a step dropped into another one. */
  lanes?: FlowLane[];
  onLaneChange?: (id: string, lane: string) => void;
  history?: FlowHistory;
};

/** A step node, or the lane band behind it. */
export type FlowCanvasNode = FlowShapeNode | FlowLaneShapeNode;

const nodeTypes = { block: FlowNodeView, lane: FlowLaneView }, edgeTypes = { block: FlowEdgeView };
const missingKind = (id: string): FlowNodeKind => ({ id, title: id, inputs: [], outputs: [] });
const issueMessages: Record<FlowConnectionIssue, string> = {
  endpoint: "Choose two different nodes.", port: "These ports have different types.", duplicate: "This connection already exists.",
  "source-capacity": "This output already has its allowed connection.", "target-capacity": "This input already has its allowed connections.",
};
const center = (element: HTMLElement | null) => {
  const box = element?.getBoundingClientRect();
  return box ? { x: box.left + box.width / 2, y: box.top + box.height / 2 } : { x: 0, y: 0 };
};
const editableTarget = (target: EventTarget | null) => target instanceof HTMLElement && !!target.closest("input,textarea,select,[contenteditable=true]");

function FlowCanvasContent({ catalog, nodes, edges, selected, onSelect, onOpen, onConnect, onDisconnect, onAdd, onInsert, onPositionsChange, onLayout, onDelete, onDuplicate, canConnect, lanes, onLaneChange, history, label, height = 320, mode, direction: initialDirection = "right", children }: FlowCanvasProps) {
  // The reader may re-flow the same graph the other way; the owner's direction is the starting point.
  const [direction, setDirection] = useState<CanvasDirection>(initialDirection);
  useEffect(() => { setDirection(initialDirection); }, [initialDirection]);
  const editable = mode === "edit" || mode !== "view" && !!(onConnect || onDisconnect || onAdd || onDelete || onPositionsChange);
  const container = useRef<HTMLDivElement>(null);
  const { screenToFlowPosition, fitView, getViewport, setCenter } = useReactFlow<FlowCanvasNode, FlowLineEdge>();
  const localPositions = useRef<Record<string, CanvasPosition>>({});
  const externalPositions = useRef<Record<string, CanvasPosition>>({});
  const externalSelection = useRef<string | undefined>(undefined);
  const copied = useRef<{ nodes: FlowNode[]; edges: FlowEdge[] } | undefined>(undefined);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const folded = (node: FlowNode) => collapsed[node.id] ?? node.collapsed ?? catalog.find((kind) => kind.id === node.kind)?.collapsed ?? false;
  const allCollapsed = nodes.length > 0 && nodes.every(folded);
  const [palette, setPalette] = useState<FlowAddContext>();
  const [pickedEdge, setPickedEdge] = useState<string>();
  const [help, setHelp] = useState(false);
  const [connectionIssue, setConnectionIssue] = useState<string>();
  const [refit, setRefit] = useState(0);
  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<FlowCanvasNode>([]);
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState<FlowLineEdge>([]);
  const dragStarted = useRef<Record<string, CanvasPosition>>({});

  /** The box one node occupies on this canvas: a BPMN shape when compact, the block's ports otherwise. */
  const boxOf = (id: string): CanvasBox => {
    const node = nodes.find((item) => item.id === id);
    return node ? flowNodeBox({ ...node, collapsed: folded(node) }, catalog.find((kind) => kind.id === node.kind)) : { width: flowNodeWidth, height: 96 };
  };

  /** Which lane a step belongs to; a step its owner left unlaned has none. */
  const laneOf = (id: string) => nodes.find((item) => item.id === id)?.lane;
  // Declared lanes first, then any lane a step names that was never declared, so a
  // step is never drawn outside a band its owner can see.
  const declared = lanes ?? [];
  const laneList: FlowLane[] = [...declared, ...[...new Set(nodes.map((node) => node.lane).filter((lane): lane is string => !!lane && !declared.some((item) => item.id === lane)))].map((id) => ({ id }))];
  const assignment = (): LaneAssignment | undefined => laneList.length ? { ids: laneList.map((lane) => lane.id), of: laneOf } : undefined;

  /** Lane bands are scenery recomputed from where the steps actually sit, so they
   * follow an owner-supplied drawing and an arranged one alike. */
  const withLanes = (steps: FlowCanvasNode[]): FlowCanvasNode[] => {
    const drawn = steps.filter((node) => node.type !== "lane");
    if (!laneList.length) return drawn;
    const at = new Map(drawn.map((node) => [node.id, node.position]));
    return [...laneBands(laneList, at, direction, boxOf, laneOf).map((band) => ({
      id: `lane:${band.id}`, type: "lane" as const, position: band.position, zIndex: -1,
      draggable: false, selectable: false, deletable: false, focusable: false,
      ariaLabel: t("Lane: {name}", { name: laneList.find((lane) => lane.id === band.id)?.title ?? band.id }),
      style: { width: band.box.width, height: band.box.height, pointerEvents: "none" as const },
      data: { lane: laneList.find((lane) => lane.id === band.id)!, vertical: direction === "down" },
    })), ...drawn];
  };

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
      const steps: FlowCanvasNode[] = nodes.map((node) => ({
        id: node.id, type: "block", position: localPositions.current[node.id] ?? node.position,
        ariaRole: onSelect || onOpen ? "button" as const : "group" as const,
        ariaLabel: node.label,
        selected: selected !== undefined && selectionChanged && !multiple ? node.id === selected : old.get(node.id)?.selected,
        measured: old.get(node.id)?.measured, draggable: editable, deletable: false,
        data: { ...node, definition: catalog.find((kind) => kind.id === node.kind) ?? missingKind(node.kind), direction, editable,
          collapsedView: folded(node) },
      }));
      return withLanes(steps);
    });
  }, [catalog, nodes, selected, editable, collapsed, direction, setFlowNodes, onSelect, onOpen]);

  useEffect(() => {
    setFlowEdges((previous) => edges.map((edge) => ({
      id: edge.id, source: edge.source, sourceHandle: edge.sourcePort, target: edge.target, targetHandle: edge.targetPort,
      type: "block", selected: previous.find((old) => old.id === edge.id)?.selected,
      markerEnd: edge.directed === false ? undefined : { type: MarkerType.ArrowClosed, color: edge.tone ? `var(--tone-${edge.tone})` : "var(--muted)", width: 16, height: 16 },
      deletable: false, data: { definition: edge, editable: editable && !!onInsert },
    })));
  }, [edges, editable, onInsert, setFlowEdges]);

  // Bring the chosen step into view without moving the reader's zoom.
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
  const selectedEdges = (picked: FlowNode[]) => edges.filter((edge) => flowEdges.find((item) => item.id === edge.id)?.selected || picked.some((node) => node.id === edge.source || node.id === edge.target));
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
  const addContext = (): FlowAddContext => ({ position: screenToFlowPosition(center(container.current)) });
  const arrange = (flow: CanvasDirection = direction) => {
    const compact = nodes.length > 0 && nodes.every((node) => node.compact);
    const positions = Object.fromEntries(layeredLayout(nodes, edges.map((edge) => ({ from: edge.source, to: edge.target })), flow,
      { width: compact ? 160 : flowNodeWidth, height: compact ? 58 : Math.max(58, ...catalog.map((kind) => flowNodeHeight(kind))), gapX: 80, gapY: 36 }, boxOf, assignment()));
    localPositions.current = positions;
    setFlowNodes((current) => withLanes(current.map((node) => node.type === "lane" ? node : { ...node, position: positions[node.id]! })));
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
        const newHeight = flowBlockHeight(definition), sourceHeight = flowBlockHeight(sourceKind), targetHeight = flowBlockHeight(targetKind);
        const position = vertical
          ? { x: (source.position.x + target.position.x) / 2, y: source.position.y + sourceHeight + 72 }
          : { x: source.position.x + flowNodeWidth + 80, y: (source.position.y + sourceHeight / 2 + target.position.y + targetHeight / 2) / 2 - newHeight / 2 };
        const offset = Math.max(0, vertical ? position.y + newHeight + 72 - target.position.y : position.x + flowNodeWidth + 80 - target.position.x);
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
      const height = definition ? flowBlockHeight(definition) : 120;
      const position = flowPlacement(palette.position, height, flowNodes.map((node) => ({ position: node.position, width: node.measured?.width ?? flowNodeWidth, height: node.measured?.height ?? 120 })), definition?.size?.width);
      onAdd?.(kind, { ...palette, position });
    }
    setPalette(undefined); container.current?.focus();
  };
  const valid = (connection: Connection) => !validateFlowConnection(connection, nodes, edges, catalog) && (!canConnect || canConnect(connection));
  /** The lane a dropped step landed in, read off the band its centre falls inside. */
  const laneUnder = (id: string, at: CanvasPosition): string | undefined => {
    if (!laneList.length) return undefined;
    const size = flowNodes.find((node) => node.id === id)?.measured ?? boxOf(id);
    const middle = { x: at.x + (size.width ?? 0) / 2, y: at.y + (size.height ?? 0) / 2 };
    const moved = new Map(nodes.map((node) => [node.id, node.id === id ? middle : localPositions.current[node.id] ?? node.position]));
    return laneBands(laneList, moved, direction, boxOf, laneOf)
      .find((band) => middle.x >= band.position.x && middle.x <= band.position.x + band.box.width && middle.y >= band.position.y && middle.y <= band.position.y + band.box.height)?.id;
  };
  const commitPositions = () => {
    const moved = flowNodes.filter((node) => node.type !== "lane" &&
      (node.position.x !== dragStarted.current[node.id]?.x || node.position.y !== dragStarted.current[node.id]?.y));
    const positions = Object.fromEntries(moved.map((node) => [node.id, node.position]));
    if (Object.keys(positions).length) { Object.assign(localPositions.current, positions); onPositionsChange?.(positions); }
    for (const node of moved) { const lane = laneUnder(node.id, node.position); if (lane && lane !== laneOf(node.id)) onLaneChange?.(node.id, lane); }
  };
  const removeCount = selectedNodes().length;
  const measuredBoxes = () => flowNodes.filter((node) => node.type !== "lane").map((node) => ({ position: node.position, width: node.measured?.width ?? flowNodeWidth, height: node.measured?.height ?? 120 }));

  return <CanvasFrame label={label} height={height} role={editable ? "region" : "figure"}>
    <div ref={container} tabIndex={0} className="platform-canvas platform-flow h-full outline-none" aria-label={label}
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
          event.preventDefault(); setFlowNodes((current) => current.map((node) => node.type === "lane" ? node : { ...node, selected: true }));
        } else if (command && key === "c" && editable) { event.preventDefault(); copySelection();
        } else if (command && key === "v" && editable && onDuplicate && copied.current) { event.preventDefault(); onDuplicate(copied.current.nodes, copied.current.edges);
        } else if (command && key === "d" && editable && onDuplicate) { event.preventDefault(); duplicateSelection();
        } else if ((event.key === "Delete" || event.key === "Backspace") && editable) { event.preventDefault(); deleteSelection();
        } else if ((key === "n" || event.key === "Tab") && editable && onAdd && event.target === container.current) { event.preventDefault(); setPalette(addContext());
        } else if (event.key === "Escape") { setPalette(undefined); setPickedEdge(undefined); setConnectionIssue(undefined); setFlowNodes((current) => current.map((node) => node.type === "lane" ? node : { ...node, selected: false }));
        } else if (event.key === "Enter") { const picked = selectedNodes(); if (picked.length === 1) onOpen?.(picked[0]!.id); }
      }}
      onDragOver={(event) => { if (editable && onAdd && event.dataTransfer.types.includes(FLOW_NODE_DROP)) { event.preventDefault(); event.dataTransfer.dropEffect = "copy"; } }}
      onDrop={(event) => {
        const kind = event.dataTransfer.getData(FLOW_NODE_DROP);
        const definition = catalog.find((item) => item.id === kind);
        if (!editable || !onAdd || !definition) return;
        event.preventDefault();
        onAdd(kind, { position: flowPlacement(screenToFlowPosition({ x: event.clientX, y: event.clientY }), flowBlockHeight(definition), measuredBoxes(), definition.size?.width) });
        setPalette(undefined);
      }}>
      <FlowInteraction.Provider value={{ onCollapse: (id) => setCollapsed((current) => ({ ...current, [id]: !folded(nodes.find((node) => node.id === id) ?? { id, kind: "", label: "", position: { x: 0, y: 0 } }) })) }}>
        <FlowEdgeInteraction.Provider value={{ onInsert: (edge, position) => setPalette({ edge, position }) }}>
          <ReactFlow<FlowCanvasNode, FlowLineEdge> nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} edgeTypes={edgeTypes}
            onNodesChange={onNodesChange} onEdgesChange={onEdgesChange} fitView fitViewOptions={fitting}
            colorMode="system" minZoom={0.15} maxZoom={2} zoomOnScroll={false} zoomOnPinch zoomOnDoubleClick={!onOpen} panOnScroll={editable} preventScrolling={editable}
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
              const issue = validateFlowConnection({ source: source.nodeId, sourceHandle: source.id ?? null, target: target.nodeId, targetHandle: target.id ?? null }, nodes, edges, catalog);
              setConnectionIssue(t(issue ? issueMessages[issue] : "This connection is not allowed."));
            }}
            onNodeClick={(_, node) => { container.current?.focus(); setPickedEdge(undefined); onSelect?.(node.id); }} onNodeDoubleClick={(_, node) => onOpen?.(node.id)}
            onEdgeClick={(_, edge) => { container.current?.focus(); setPickedEdge(edge.id); setFlowNodes((current) => current.map((node) => node.type === "lane" ? node : { ...node, selected: false })); }}
            onPaneClick={() => { container.current?.focus(); setPalette(undefined); setPickedEdge(undefined); setConnectionIssue(undefined); }}
            onPaneContextMenu={(event) => { if (editable && onAdd) { event.preventDefault(); setPalette({ position: screenToFlowPosition({ x: event.clientX, y: event.clientY }) }); } }}
            onNodeDragStart={() => { dragStarted.current = Object.fromEntries(flowNodes.map((node) => [node.id, node.position])); }} onNodeDragStop={commitPositions}>
            <CanvasRefit signature={editable ? String(refit) : flowNodes.map((node) => `${node.id}:${node.position.x}:${node.position.y}`).join("|")} />
            <CanvasFurniture />
          </ReactFlow>
        </FlowEdgeInteraction.Provider>
      </FlowInteraction.Provider>

      <CanvasToolbar label={t("Canvas tools")}>
        {editable && onAdd && <CanvasTool primary label={t("Add block")} title={t("Add block (N)")} text={t("Add block")} icon={<Plus />} onClick={() => setPalette(addContext())} />}
        <CanvasTool label={t("Tidy up workflow")} icon={<AlignHorizontalJustifyStart />} onClick={() => arrange()} />
        <CanvasTool label={t(direction === "right" ? "Flow top to bottom" : "Flow left to right")}
          icon={direction === "right" ? <ArrowDown /> : <ArrowRight />}
          onClick={() => { const next: CanvasDirection = direction === "right" ? "down" : "right"; setDirection(next); arrange(next); }} />
        {catalog.some((kind) => kind.inputs.length || kind.outputs.length) && !nodes.every((node) => node.compact) &&
          <CanvasTool label={t(allCollapsed ? "Expand all blocks" : "Collapse all blocks")}
            icon={allCollapsed ? <ChevronsUpDown /> : <ChevronsDownUp />}
            onClick={() => setCollapsed(Object.fromEntries(nodes.map((node) => [node.id, !allCollapsed])))} />}
        <CanvasTool label={t("Fit canvas")} icon={<Maximize />} onClick={() => void fitView({ ...fitting, duration: 220 })} />
        {editable && history && <>
          <CanvasToolDivider />
          <CanvasTool label={t("Undo")} icon={<Undo2 />} disabled={!history.canUndo} onClick={history.onUndo} />
          <CanvasTool label={t("Redo")} icon={<Redo2 />} disabled={!history.canRedo} onClick={history.onRedo} /></>}
        {editable && removeCount > 0 && <>
          <CanvasToolDivider />
          {onDuplicate && <CanvasTool label={t("Duplicate selection")} icon={<Copy />} onClick={duplicateSelection} />}
          {onDelete && <CanvasTool label={t("Delete selection")} icon={<Trash2 />} onClick={deleteSelection} />}</>}
        <CanvasHelpTool open={help} onToggle={() => setHelp(!help)} />
      </CanvasToolbar>

      <CanvasHelp open={help} onClose={() => setHelp(false)} items={[
        t("Click a block to select it; double-click or Enter to open it."),
        editable && onConnect && t("Drag from an output port to a compatible input port to connect them."),
        editable && onInsert && t("Click the plus on a connection to insert a block into it."),
        editable && onDisconnect && t("Click a connection, then remove it: the block it came from stays."),
        editable && t("Delete removes the selected blocks and their connections; drag a port's end onto another port to re-route."),
        t("The chevron on a block shows or hides its ports and details."),
        t("A gateway is a diamond, an event a circle, and a repeated step carries a loop mark."),
        !!laneList.length && t("A lane is who does the step; drag a step into another lane to hand it over."),
      ]} />

      {pickedEdge && editable && <CanvasActionBar label={t("Connection operations")}>
        {onInsert && flowEdges.find((e) => e.id === pickedEdge)?.data && <CanvasTool label={t("Insert block")} text={t("Insert block")} icon={<Plus />}
          onClick={() => { const edge = edges.find((e) => e.id === pickedEdge); if (edge) setPalette({ edge, position: { x: 0, y: 0 } }); }} />}
        {onDisconnect && <CanvasTool label={t("Remove connection")} text={t("Remove connection")} icon={<Scissors />}
          onClick={() => { const edge = edges.find((e) => e.id === pickedEdge); if (edge) { onDisconnect([edge]); setPickedEdge(undefined); } }} />}
        <CanvasActionButton action={{ id: "close", label: "", icon: <>×</>, hint: t("Close"), run: () => setPickedEdge(undefined) }} />
      </CanvasActionBar>}

      {palette && <FlowPalette catalog={catalog} nodes={nodes} context={palette} onChoose={choose} onClose={() => { setPalette(undefined); setPickedEdge(undefined); container.current?.focus(); }} />}

      {!nodes.length && <CanvasEmpty>
        <span className="text-sm font-medium">{t("Start with a block")}</span>
        <span className="text-xs">{t("Add a capability, then connect its typed ports.")}</span>
      </CanvasEmpty>}

      {connectionIssue && <div role="alert" className="absolute bottom-3 left-3 z-20 flex max-w-[70%] items-center gap-2 rounded border border-border bg-surface px-3 py-2 text-xs text-foreground shadow-sm">
        <span>{connectionIssue}</span>
        <button type="button" onClick={() => setConnectionIssue(undefined)} aria-label={t("Dismiss")} className="px-1 text-muted">×</button></div>}
      {children}
    </div>
  </CanvasFrame>;
}

/** One React Flow implementation for processes; the relation family shares its frame. */
export function FlowCanvas(props: FlowCanvasProps) {
  return <ReactFlowProvider><FlowCanvasContent {...props} /></ReactFlowProvider>;
}
