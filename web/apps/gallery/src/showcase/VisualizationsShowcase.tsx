import {
  Chart, FlowGraph, Graph, NodeCanvas, PageHeader,
  type CanvasEdge, type CanvasNode, type ChartSpec, type FlowDefinition, type FlowInstanceData, type GraphEdge, type GraphNode, type NodeCatalog,
} from "@platform/ui";
import { useState } from "react";
import { ShowcaseCard } from "./ShowcaseCard";

// 1. Chart Specs (Offline inline data)
const outputChartSpec: ChartSpec = {
  title: "Monthly Output by Work Center (pcs)",
  data: {
    values: [
      { center: "WC-CNC-01", output: 1420 },
      { center: "WC-CNC-02", output: 1180 },
      { center: "WC-ASM-01", output: 1650 },
      { center: "WC-QC-01", output: 1390 },
      { center: "WC-PKG-01", output: 1510 },
    ],
  },
  mark: "bar",
  encoding: {
    x: { field: "center", type: "nominal", title: "Work Center" },
    y: { field: "output", type: "quantitative", aggregate: "sum", title: "Units Produced" },
  },
};

const tempTrendChartSpec: ChartSpec = {
  title: "Thermal Sensor 24h Telemetry (°C)",
  data: {
    values: [
      { time: "00:00", temp: 19.2 },
      { time: "04:00", temp: 18.5 },
      { time: "08:00", temp: 22.8 },
      { time: "12:00", temp: 27.4 },
      { time: "16:00", temp: 26.1 },
      { time: "20:00", temp: 21.3 },
    ],
  },
  mark: "line",
  encoding: {
    x: { field: "time", type: "nominal", title: "Time of Day" },
    y: { field: "temp", type: "quantitative", aggregate: "avg", title: "Spindle Temp (°C)" },
  },
};

// 2. Manufacturing Routing Graph
const routingNodes: GraphNode[] = [
  { id: "10", label: "10 Cast Blank", detail: "WC-SAW", tone: "success" },
  { id: "20", label: "20 CNC Mill", detail: "WC-CNC · NC SCRATCH", tone: "warning" },
  { id: "30", label: "30 Deburr & Clean", detail: "WC-BENCH · in work", tone: "info", current: true },
  { id: "40", label: "40 QA Final Inspect", detail: "WC-QA" },
  { id: "done", label: "Stock Inbound" },
];
const routingEdges: GraphEdge[] = [
  { from: "10", to: "20" },
  { from: "20", to: "30" },
  { from: "30", to: "40" },
  { from: "40", to: "done" },
  { from: "40", to: "20", dashed: true, label: "rework loop", tone: "warning" },
];

// 3. NodeCanvas Catalog & Instances
const catalog: NodeCatalog = [
  {
    id: "sensor",
    title: "Edge Sensor",
    category: "input",
    inputs: [],
    outputs: [
      { id: "temp", label: "Temperature", type: "number" },
      { id: "vibe", label: "Vibration", type: "number" },
    ],
  },
  {
    id: "evaluator",
    title: "Rule Evaluator",
    category: "logic",
    inputs: [{ id: "metric", label: "Metric Input", type: "number" }],
    outputs: [
      { id: "normal", label: "In Tolerance", type: "signal" },
      { id: "alarm", label: "Limit Breach", type: "signal" },
    ],
  },
  {
    id: "actuator",
    title: "Alert Dispatcher",
    category: "action",
    inputs: [{ id: "trigger", label: "Trigger Signal", type: "signal" }],
    outputs: [],
  },
];

const initialCanvasNodes: CanvasNode[] = [
  { id: "n1", kind: "sensor", label: "TMP-01 Spindle", detail: "Bay 4 CNC-01", position: { x: 40, y: 50 } },
  { id: "n2", kind: "evaluator", label: "Thermal Limit > 75°C", detail: "Safety Interlock", position: { x: 300, y: 60 } },
  { id: "n3", kind: "actuator", label: "E-Stop & Notification", detail: "Divert Routing", position: { x: 570, y: 70 } },
];

const initialCanvasEdges: CanvasEdge[] = [
  { id: "e1", source: "n1", sourcePort: "temp", target: "n2", targetPort: "metric" },
  { id: "e2", source: "n2", sourcePort: "alarm", target: "n3", targetPort: "trigger" },
];

// 4. FlowGraph Definition
const orderFlowDef: FlowDefinition = {
  id: "order.fulfillment",
  app: "erp",
  title: "Order Fulfillment Flow",
  version: 1,
  start: ["order.placed"],
  steps: [
    { name: "reserve", title: "Reserve Inventory", kind: "action", next: ["credit_check"], chooses: false },
    { name: "credit_check", title: "Credit Approval", kind: "ask", next: ["dispatch"], chooses: false },
    { name: "dispatch", title: "Warehouse Pick & Pack", kind: "action", next: ["delivery"], chooses: false },
    { name: "delivery", title: "Carrier Handover", kind: "action", next: [], chooses: false },
  ],
};

const orderFlowInstance: FlowInstanceData = {
  id: "inst-001",
  flow: "order.fulfillment",
  title: "Order #8421 Fulfillment",
  version: 1,
  key: "SO-8421",
  state: "running",
  tokens: [{ id: 1, step: "credit_check", waits: "ask" }],
  undo: null,
  trace: [
    { at: new Date(Date.now() - 3600000).toISOString(), step: "reserve", what: "Inventory reserved (12 units)" },
    { at: new Date().toISOString(), step: "credit_check", what: "Waiting for Finance approval" },
  ],
};

export function VisualizationsShowcase() {
  const [canvasNodes] = useState(initialCanvasNodes);
  const [canvasEdges, setCanvasEdges] = useState(initialCanvasEdges);

  const handleConnect = (connection: { source: string; target: string; sourceHandle?: string | null; targetHandle?: string | null }) => {
    if (!connection.source || !connection.target || !connection.sourceHandle || !connection.targetHandle) return;
    setCanvasEdges((prev) => [
      ...prev,
      {
        id: `e-${Date.now()}`,
        source: connection.source,
        sourcePort: connection.sourceHandle,
        target: connection.target,
        targetPort: connection.targetHandle,
      } as CanvasEdge,
    ]);
  };

  return (
    <div className="flex flex-col gap-6 pb-12">
      <PageHeader
        title="Visualizations & Canvases"
        description="ECharts analytical graphics, process routing graphs, workflow step monitors, and interactive low-code node canvases."
      />

      {/* Analytical Charts */}
      <div className="grid gap-4 md:grid-cols-2">
        <ShowcaseCard
          title="Chart / ECharts Bar Series"
          description="Vega-Lite inspired declarative spec (ADR-0019) mapped onto ECharts 6 with Oklch theme palettes."
          contentClassName="w-full block"
        >
          <Chart spec={outputChartSpec} height={220} frame={false} />
        </ShowcaseCard>

        <ShowcaseCard
          title="Chart / ECharts Line Series"
          description="High-frequency telemetry visualization with smooth tooltips, zero-DOM overhead, and dark mode synchronization."
          contentClassName="w-full block"
        >
          <Chart spec={tempTrendChartSpec} height={220} frame={false} />
        </ShowcaseCard>
      </div>

      {/* Process Routing Graph */}
      <ShowcaseCard
        title="Process Graph (Graph)"
        description="Directed acyclic graphs with tone-coded node states, current token focus, and dashed rework branches."
        contentClassName="w-full block"
      >
        <Graph nodes={routingNodes} edges={routingEdges} height={180} label="Manufacturing Routing Sequence" />
      </ShowcaseCard>

      {/* Interactive NodeCanvas */}
      <ShowcaseCard
        title="Interactive Semantic Canvas (NodeCanvas)"
        description="React Flow backed editor with port typing validation, drag-and-drop wiring, zoom/pan navigation, and semantic node headers."
        contentClassName="w-full block"
      >
        <p className="mb-2 text-xs text-muted">
          💡 Try dragging nodes around, or drag from an output handle to an input handle (type checking enforced).
        </p>
        <NodeCanvas
          label="IoT Alert Orchestration"
          catalog={catalog}
          nodes={canvasNodes}
          edges={canvasEdges}
          onConnect={handleConnect}
          height={320}
        />
      </ShowcaseCard>

      {/* Workflow Step Graph */}
      <ShowcaseCard
        title="Workflow Execution Step Graph (FlowGraph)"
        description="ADR-0020 declarative business flow status: visited steps, current token location, and rollback compensations."
        contentClassName="w-full block"
      >
        <FlowGraph definition={orderFlowDef} instance={orderFlowInstance} height={180} />
      </ShowcaseCard>
    </div>
  );
}
